package install

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/naming"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

// ErrCredentialMapRefused is returned when interactive credential mapping is aborted.
var ErrCredentialMapRefused = errors.New("credential mapping refused")

// ErrCredentialMapInvalid is returned for invalid non-interactive mapping input.
var ErrCredentialMapInvalid = errors.New("invalid credential mapping")

const credentialMapPromptLimit = 3

// CredentialMapOpts carries explicit inputs for install-time credential mapping.
type CredentialMapOpts struct {
	Policy     *policy.Policy
	Store      secrets.SecretStore
	InstallRef string

	IsTTY bool
	// MapCredentials holds non-TTY mappings as "<declared>=<local>" strings.
	MapCredentials []string

	Prompt    func(prompt string) (string, error)
	PrintWarn func(msg string)
	EmitAudit func(eventType string, payload map[string]string)
}

// CredentialMapResult holds mapping output for the install orchestrator.
type CredentialMapResult struct {
	Map          map[string]string
	Warnings     []string
	DisplayLines []string
}

// MapCredentialOpts updates a single mapping on an installed agent (CLI subcommand).
type MapCredentialOpts struct {
	State   InstallStateStore
	Store   secrets.SecretStore
	Ref     string
	Mapping string // declared=local
	EmitAudit func(eventType string, payload map[string]string)
}

// ResolveCredentialMapping collects declared→local credential renames for brokered credentials.
func ResolveCredentialMapping(opts CredentialMapOpts) (*CredentialMapResult, error) {
	if opts.Policy == nil {
		return nil, fmt.Errorf("credential mapping requires signed policy")
	}
	if opts.Store == nil {
		return nil, fmt.Errorf("credential mapping requires secret store")
	}
	ids := brokeredCredentialIDs(opts.Policy)
	if len(ids) == 0 {
		return &CredentialMapResult{Map: map[string]string{}}, nil
	}

	ref := strings.TrimSpace(opts.InstallRef)
	if ref == "" && opts.Policy.Agent.Name != "" {
		ref = opts.Policy.Agent.Name
	}

	ctx := context.Background()
	names, err := listSecretNames(ctx, opts.Store)
	if err != nil {
		return nil, err
	}

	result := &CredentialMapResult{Map: make(map[string]string)}
	if opts.IsTTY {
		if err := mapCredentialsTTY(opts, ids, names, ref, result); err != nil {
			return nil, err
		}
	} else {
		if err := mapCredentialsNonTTY(opts, ids, names, result); err != nil {
			return nil, err
		}
	}

	for _, id := range ids {
		if _, ok := result.Map[id]; !ok {
			msg := unmappedWarning(id, ref)
			result.Warnings = append(result.Warnings, msg)
			if opts.PrintWarn != nil {
				opts.PrintWarn(msg)
			}
		}
	}

	for declared, local := range result.Map {
		emitAudit(opts.EmitAudit, audit.EventTypeInstallCredentialMapped, map[string]string{
			"agent_ref":      ref,
			"declared_id":    declared,
			"local_name":     local,
			"policy_agent":   opts.Policy.Agent.Name,
		})
	}

	return result, nil
}

// ApplyMapCredential adds or updates one mapping on a saved install manifest.
func ApplyMapCredential(opts MapCredentialOpts) error {
	if opts.State == nil || opts.Store == nil {
		return fmt.Errorf("map credential requires state store and secret store")
	}
	declared, local, err := parseCredentialMapping(opts.Mapping)
	if err != nil {
		return err
	}
	prior, err := opts.State.GetInstallByRef(opts.Ref)
	if err != nil {
		return err
	}
	if prior == nil {
		return fmt.Errorf("no install found for reference %q", opts.Ref)
	}
	pol, err := policy.ParsePolicy(strings.NewReader(string(prior.PolicyYAML)))
	if err != nil {
		return fmt.Errorf("parse installed policy: %w", err)
	}
	if !isDeclaredBrokeredCredential(pol, declared) {
		return fmt.Errorf("%w: credential %q is not a declared brokered credential in the signed policy", ErrCredentialMapInvalid, declared)
	}
	ctx := context.Background()
	if _, err := opts.Store.Get(ctx, local); err != nil {
		if errors.Is(err, secrets.ErrSecretNotFound) {
			return fmt.Errorf("%w: local secret %q not found in store", ErrCredentialMapInvalid, local)
		}
		return fmt.Errorf("validate local secret %q: %w", local, err)
	}

	m := prior.Manifest
	if m.CredentialMap == nil {
		m.CredentialMap = make(map[string]string)
	}
	m.CredentialMap[declared] = local
	if err := opts.State.SaveApprovedInstall(m, prior.PolicyYAML); err != nil {
		return err
	}
	emitAudit(opts.EmitAudit, audit.EventTypeInstallCredentialMapped, map[string]string{
		"agent_ref":   opts.Ref,
		"declared_id": declared,
		"local_name":  local,
	})
	return nil
}

// MergeCredentialMapIntoManifest copies a resolved map onto the manifest (names only).
func MergeCredentialMapIntoManifest(m *InstallManifest, credMap map[string]string) {
	if len(credMap) == 0 {
		return
	}
	if m.CredentialMap == nil {
		m.CredentialMap = make(map[string]string, len(credMap))
	}
	for k, v := range credMap {
		m.CredentialMap[k] = v
	}
}

func mapCredentialsTTY(opts CredentialMapOpts, ids []string, names map[string]struct{}, ref string, result *CredentialMapResult) error {
	if opts.Prompt == nil {
		return ErrCredentialMapRefused
	}
	listLine := formatSecretNameList(names)
	if listLine != "" {
		result.DisplayLines = append(result.DisplayLines, listLine)
	}
	for _, id := range ids {
		prompt := fmt.Sprintf("Map credential %s to local secret name (or 'defer'): ", id)
		if listLine != "" {
			prompt = listLine + "\n" + prompt
			listLine = ""
		}
		var mapped bool
		for attempt := 0; attempt < credentialMapPromptLimit; attempt++ {
			response, err := opts.Prompt(prompt)
			if err != nil {
				return ErrCredentialMapRefused
			}
			choice := strings.TrimSpace(response)
			if strings.EqualFold(choice, "defer") {
				mapped = true
				break
			}
			if choice == "" {
				prompt = fmt.Sprintf("Map credential %s to local secret name (or 'defer'): ", id)
				continue
			}
			if _, ok := names[choice]; !ok {
				prompt = fmt.Sprintf("Secret %q not found. Map credential %s to local secret name (or 'defer'): ", choice, id)
				continue
			}
			result.Map[id] = choice
			mapped = true
			break
		}
		if !mapped {
			return ErrCredentialMapRefused
		}
	}
	return nil
}

func mapCredentialsNonTTY(opts CredentialMapOpts, ids []string, names map[string]struct{}, result *CredentialMapResult) error {
	declaredSet := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		declaredSet[id] = struct{}{}
	}
	for _, raw := range opts.MapCredentials {
		declared, local, err := parseCredentialMapping(raw)
		if err != nil {
			return err
		}
		if _, ok := declaredSet[declared]; !ok {
			return fmt.Errorf("%w: credential %q is not declared in the signed policy", ErrCredentialMapInvalid, declared)
		}
		if _, ok := names[local]; !ok {
			return fmt.Errorf("%w: local secret %q not found in store", ErrCredentialMapInvalid, local)
		}
		result.Map[declared] = local
	}
	return nil
}

func parseCredentialMapping(raw string) (declared, local string, err error) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("%w: expected <declared>=<local>", ErrCredentialMapInvalid)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func brokeredCredentialIDs(p *policy.Policy) []string {
	seen := make(map[string]struct{})
	for _, c := range p.Credentials {
		if c.Type != "brokered" || c.ID == "" {
			continue
		}
		seen[c.ID] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func isDeclaredBrokeredCredential(p *policy.Policy, id string) bool {
	for _, c := range p.Credentials {
		if c.ID == id && c.Type == "brokered" {
			return true
		}
	}
	return false
}

func listSecretNames(ctx context.Context, store secrets.SecretStore) (map[string]struct{}, error) {
	metas, err := store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	names := make(map[string]struct{}, len(metas))
	for _, m := range metas {
		names[m.Name] = struct{}{}
	}
	return names, nil
}

func formatSecretNameList(names map[string]struct{}) string {
	if len(names) == 0 {
		return "Available secrets: (none)"
	}
	list := make([]string, 0, len(names))
	for n := range names {
		list = append(list, n)
	}
	sort.Strings(list)
	return "Available secrets: " + strings.Join(list, ", ")
}

func unmappedWarning(declaredID, ref string) string {
	return fmt.Sprintf(
		"WARNING: credential %s not mapped; run will fail closed until you map it: agentpaas installed map-credential %s %s=<local>",
		declaredID, ref, declaredID,
	)
}

// MatchPublisherPub8 reports whether the full fingerprint matches a pub8 suffix.
func MatchPublisherPub8(fingerprint, pub8 string) bool {
	fp := trust.NormalizeFingerprint(fingerprint)
	if len(fp) < 8 {
		return false
	}
	return strings.EqualFold(fp[:8], pub8)
}

// FormatInstallRef builds name@pub8 for warnings and broker errors.
func FormatInstallRef(agentName, publisherFingerprint string) string {
	return naming.FormatAgentRef(agentName, trust.NormalizeFingerprint(publisherFingerprint))
}