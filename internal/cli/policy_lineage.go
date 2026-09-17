package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/home"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/spf13/cobra"
)

type policyLineageResult struct {
	Lineage     policyLineageMeta        `json:"lineage"`
	Policy      *policyShowResult        `json:"policy"`
	Enforcement policyLineageEnforcement `json:"enforcement"`
	Proof       policyLineageProof       `json:"proof"`
}

type policyLineageMeta struct {
	Packer    string `json:"packer"`
	Tool      string `json:"tool"`
	Digest    string `json:"digest"`
	Signature string `json:"signature"`
	Parent    string `json:"parent"`
}

type policyLineageEnforcement struct {
	EgressAllowed        []string `json:"egress_allowed"`
	EgressDenied         []string `json:"egress_denied"`
	CredentialInjections []string `json:"credential_injections"`
	PIIMasks             int      `json:"pii_masks"`
	BudgetConsumed       string   `json:"budget_consumed"`
}

type policyLineageProof struct {
	HashChainVerified bool   `json:"hash_chain_verified"`
	ExportPointer     string `json:"export_pointer"`
}

func newPolicyLineageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lineage [project-dir]",
		Short: "Show policy lineage, compiled contract, enforcement, and proof",
		Long: `Show the compiled policy contract for a project, its lineage fields,
enforcement counts, and how to export proof.

Reads policy.yaml from the project directory (default: current directory).
Optional --run-id is accepted; counts stay at zero when no aggregate rows exist.`,
		Example: `  agentpaas policy lineage
  agentpaas policy lineage ./my-agent
  agentpaas policy lineage ./my-agent --json
  agentpaas policy lineage ./my-agent --run-id run-01HXYZ`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}
			runID, _ := cmd.Flags().GetString("run-id") // cobra flag default on missing
			return showProjectPolicyLineage(cmd, projectDir, strings.TrimSpace(runID))
		},
	}
	cmd.Flags().String("run-id", "", "Optional run id; enforcement counts stay zero without aggregate rows")
	return cmd
}

func showProjectPolicyLineage(cmd *cobra.Command, projectDir, runID string) error {
	if projectDir == "" {
		projectDir = "."
	}
	policyPath := filepath.Join(projectDir, "policy.yaml")

	data, err := os.ReadFile(policyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("policy.yaml not found in %s; run 'agentpaas policy init %s' to create one", projectDir, projectDir)
		}
		return fmt.Errorf("read policy: %w", err)
	}

	parsed, err := policy.ParsePolicy(bytes.NewReader(data))
	if err != nil {
		return err
	}

	compiled, err := compilePolicyShow(projectDir, parsed)
	if err != nil {
		return err
	}

	result := &policyLineageResult{
		Lineage: policyLineageMeta{
			Packer:    "",
			Tool:      "",
			Digest:    compiled.Digest,
			Signature: "",
			Parent:    "",
		},
		Policy:      compiled,
		Enforcement: emptyPolicyLineageEnforcement(),
		Proof: policyLineageProof{
			HashChainVerified: false,
			ExportPointer:     "agentpaas audit export",
		},
	}
	if runID != "" {
		result.Enforcement = loadLineageEnforcement(cmd, runID)
	}

	return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
		r, ok := v.(*policyLineageResult)
		if !ok {
			return ""
		}
		return formatPolicyLineageText(r)
	})
}

func emptyPolicyLineageEnforcement() policyLineageEnforcement {
	return policyLineageEnforcement{
		EgressAllowed:        []string{},
		EgressDenied:         []string{},
		CredentialInjections: []string{},
		PIIMasks:             0,
		BudgetConsumed:       "",
	}
}

func loadLineageEnforcement(cmd *cobra.Command, runID string) policyLineageEnforcement {
	empty := emptyPolicyLineageEnforcement()
	if runID == "" {
		return empty
	}
	var rows []lineageAuditRow
	for _, path := range lineageAuditJSONLPaths(cmd, runID) {
		got, _ := readLocalAuditRows(path) // keep already-parsed rows if a later line fails
		rows = append(rows, got...)
	}
	return enforcementFromAuditRows(rows, runID)
}

func localAuditJSONLPath(cmd *cobra.Command) (string, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return "", err
	}
	return filepath.Join(home.NewHomePaths(homeDir).State, "audit.jsonl"), nil
}

func lineageAuditJSONLPaths(cmd *cobra.Command, runID string) []string {
	path, err := localAuditJSONLPath(cmd)
	if err != nil || path == "" {
		return nil
	}
	paths := []string{path}
	if runID == "" {
		return paths
	}
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return paths
	}
	paths = append(paths, filepath.Join(home.NewHomePaths(homeDir).State, "runs", runID, "harness-audit", "harness-audit.jsonl"))
	return paths
}

type lineageAuditRow struct {
	EventType string         `json:"event_type"`
	Type      string         `json:"type"`
	RunID     string         `json:"run_id"`
	Payload   map[string]any `json:"payload"`
}

func (r *lineageAuditRow) UnmarshalJSON(data []byte) error {
	var aux struct {
		EventType string          `json:"event_type"`
		Type      string          `json:"type"`
		RunID     string          `json:"run_id"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.EventType = aux.EventType
	r.Type = aux.Type
	r.RunID = aux.RunID
	r.Payload = decodeLineagePayload(aux.Payload)
	return nil
}

func decodeLineagePayload(raw json.RawMessage) map[string]any {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return map[string]any{}
		}
		raw = bytes.TrimSpace([]byte(s))
		if len(raw) == 0 {
			return map[string]any{}
		}
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

func (r lineageAuditRow) kind() string {
	if r.EventType != "" {
		return r.EventType
	}
	return r.Type
}

func (r lineageAuditRow) run() string {
	if r.RunID != "" {
		return r.RunID
	}
	return payloadString(r.Payload, "run_id")
}

func readLocalAuditRows(path string) ([]lineageAuditRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }() // best-effort close

	var rows []lineageAuditRow
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadString('\n')
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			var row lineageAuditRow
			if err := json.Unmarshal([]byte(trimmed), &row); err == nil {
				rows = append(rows, row)
			}
		}
		if readErr == io.EOF {
			return rows, nil
		}
		if readErr != nil {
			return rows, readErr
		}
	}
}

func enforcementFromAuditRows(rows []lineageAuditRow, runID string) policyLineageEnforcement {
	out := emptyPolicyLineageEnforcement()
	if runID == "" {
		return out
	}
	allowByHost := map[string]int{}
	denyByHost := map[string]int{}
	denyExtra := map[string][]string{}
	denyExtraSeen := map[string]map[string]struct{}{}
	credSeen := map[string]struct{}{}
	var creds []string
	pii := 0
	budget := ""

	for _, row := range rows {
		if row.run() != runID {
			continue
		}
		payload := row.Payload
		if payload == nil {
			payload = map[string]any{}
		}
		kind := row.kind()
		switch {
		case kind == "egress.allowed" || kind == "egress_allowed":
			host := payloadHost(payload)
			n := payloadCount(payload)
			if host == "" || n <= 0 {
				continue
			}
			allowByHost[host] += n
			creds = appendCredentialID(creds, credSeen, payloadString(payload, "credential_id"))
		case kind == "egress.denied" || kind == "egress_denied":
			reason := payloadString(payload, "reason")
			if isPIIMaskedReason(reason) {
				pii++
				continue
			}
			host := payloadHost(payload)
			if host == "" {
				continue
			}
			denyByHost[host]++
			extra := strings.TrimSpace(payloadString(payload, "method") + " " + reason)
			appendUniqueDenyExtra(denyExtra, denyExtraSeen, host, extra)
		case kind == "secret_injected" || kind == "credential.injected" || kind == "credential_injected":
			creds = appendCredentialID(creds, credSeen, payloadString(payload, "credential_id"))
		case kind == "pii.mask" || kind == "pii.masked" || kind == "pii_mask" || kind == "pii_masked":
			pii++
		case kind == "budget.consumed" || kind == "budget_consumed" || kind == "budget_exceeded":
			if s := budgetConsumedFromPayload(payload); s != "" {
				budget = s
			}
		}
	}

	for _, host := range sortedKeys(allowByHost) {
		n := allowByHost[host]
		if n <= 0 {
			continue
		}
		out.EgressAllowed = append(out.EgressAllowed, fmt.Sprintf("%s ×%d", host, n))
	}
	for _, host := range sortedKeys(denyByHost) {
		n := denyByHost[host]
		if n <= 0 {
			continue
		}
		line := fmt.Sprintf("%s ×%d", host, n)
		if extra := strings.Join(denyExtra[host], " "); extra != "" {
			line += " " + extra
		}
		out.EgressDenied = append(out.EgressDenied, line)
	}
	sort.Strings(creds)
	if creds == nil {
		creds = []string{}
	}
	out.CredentialInjections = creds
	out.PIIMasks = pii
	out.BudgetConsumed = budget
	return out
}

func payloadHost(payload map[string]any) string {
	return firstNonEmpty(payloadString(payload, "host"), payloadString(payload, "destination"))
}

func isPIIMaskedReason(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "pii_masked", "pii_mask", "pii.masked", "pii.mask":
		return true
	default:
		return false
	}
}

func appendUniqueDenyExtra(extras map[string][]string, seen map[string]map[string]struct{}, host, extra string) {
	if extra == "" {
		return
	}
	got := seen[host]
	if got == nil {
		got = map[string]struct{}{}
		seen[host] = got
	}
	if _, ok := got[extra]; ok {
		return
	}
	got[extra] = struct{}{}
	extras[host] = append(extras[host], extra)
}

func budgetConsumedFromPayload(payload map[string]any) string {
	if s := firstNonEmpty(
		payloadString(payload, "budget_consumed"),
		payloadString(payload, "consumed"),
		payloadString(payload, "amount"),
	); s != "" {
		return s
	}
	cat := payloadString(payload, "category")
	limit := payloadString(payload, "limit")
	observed := payloadString(payload, "observed")
	if cat == "" && limit == "" && observed == "" {
		return ""
	}
	return strings.TrimSpace(strings.Join([]string{cat, observed, limit}, " "))
}

func appendCredentialID(ids []string, seen map[string]struct{}, id string) []string {
	id = strings.TrimSpace(id)
	if id == "" || isSecretShapedCredentialID(id) {
		return ids
	}
	if _, ok := seen[id]; ok {
		return ids
	}
	seen[id] = struct{}{}
	return append(ids, id)
}

func isSecretShapedCredentialID(id string) bool {
	if len(id) > 80 {
		return true
	}
	if strings.HasPrefix(id, "eyJ") || strings.HasPrefix(id, "vault:") {
		return true
	}
	if strings.Contains(id, "Bearer") {
		return true
	}
	if strings.Contains(id, "sk-") || strings.Contains(id, "sk_") {
		return true
	}
	if strings.Contains(id, "AKIA") || strings.Contains(id, "ASIA") {
		return true
	}
	if strings.Contains(id, "ghp_") || strings.Contains(id, "gho_") || strings.Contains(id, "github_pat_") {
		return true
	}
	return false
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func payloadCount(payload map[string]any) int {
	if payload == nil {
		return 1
	}
	v, ok := payload["count"]
	if !ok || v == nil {
		return 1
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 1
		}
		return int(i)
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 1
		}
		return i
	default:
		return 1
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func formatPolicyLineageText(r *policyLineageResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "LINEAGE\n")
	fmt.Fprintf(&b, "packer: %s\n", r.Lineage.Packer)
	fmt.Fprintf(&b, "tool: %s\n", r.Lineage.Tool)
	fmt.Fprintf(&b, "digest: %s\n", r.Lineage.Digest)
	fmt.Fprintf(&b, "signature: %s\n", r.Lineage.Signature)
	fmt.Fprintf(&b, "parent: %s\n", r.Lineage.Parent)
	fmt.Fprintf(&b, "POLICY\n")
	fmt.Fprintf(&b, "%s\n", formatPolicyShowText(r.Policy))
	fmt.Fprintf(&b, "ENFORCEMENT\n")
	writeEnforcementLines(&b, "egress allowed", r.Enforcement.EgressAllowed)
	writeEnforcementLines(&b, "egress denied", r.Enforcement.EgressDenied)
	writeEnforcementLines(&b, "credential injections", r.Enforcement.CredentialInjections)
	fmt.Fprintf(&b, "pii masks: %d\n", r.Enforcement.PIIMasks)
	fmt.Fprintf(&b, "budget consumed: %s\n", r.Enforcement.BudgetConsumed)
	fmt.Fprintf(&b, "PROOF\n")
	fmt.Fprintf(&b, "hash chain verified: %v\n", r.Proof.HashChainVerified)
	fmt.Fprintf(&b, "export pointer: %s", r.Proof.ExportPointer)
	return b.String()
}

func writeEnforcementLines(b *strings.Builder, label string, lines []string) {
	if len(lines) == 0 {
		fmt.Fprintf(b, "%s: 0\n", label)
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}
