package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// D3TrustDisclaimer is fixed copy for bundle inspect and consent surfaces (PRD D3).
const D3TrustDisclaimer = "A valid signature proves who signed this and that it is unmodified.\n" +
	"It does not mean the agent is safe. Review the policy below."

// PolicyLint is a non-fatal policy warning (PRD A3).
type PolicyLint struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	LintWildcardDomain       = "wildcard_domain"
	LintRawIPEgress          = "raw_ip_egress"
	LintNonTLSPort           = "non_tls_port"
	LintManyEgressDomains    = "many_egress_domains"
	LintCredWildcardDest     = "credential_wildcard_destination"
)

// InspectHeader is section 1 of the inspect report.
type InspectHeader struct {
	File                 string `json:"file"`
	SizeBytes            int64  `json:"size_bytes"`
	BundleDigest         string `json:"bundle_digest"`
	BundleSchemaVersion  int    `json:"bundle_schema_version"`
	LockSchemaVersion    int    `json:"lock_schema_version"`
	AgentName            string `json:"agent_name"`
	AgentVersion         string `json:"agent_version"`
}

// InspectPublisher is section 3 (only when verified).
type InspectPublisher struct {
	Name                 string `json:"name"`
	Fingerprint          string `json:"fingerprint"`
	FingerprintDisplay   string `json:"fingerprint_display"`
	TrustDisclaimer      string `json:"trust_disclaimer"`
}

// PolicySummaryLine is one rendered policy row for inspect output.
type PolicySummaryLine struct {
	Section string `json:"section"`
	Detail  string `json:"detail"`
}

// InspectRequirements is section 7.
type InspectRequirements struct {
	Credentials      []CredentialRequirement `json:"credentials"`
	LLMProvider      string                  `json:"llm_provider"`
	Image            string                  `json:"image"`
	Platform         string                  `json:"platform"`
}

// CredentialRequirement is a credential the receiver must map at install.
type CredentialRequirement struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Header      string `json:"header,omitempty"`
	Destination string `json:"destination,omitempty"`
}

// SBOMSummary is section 8.
type SBOMSummary struct {
	PackageCount   int      `json:"package_count"`
	TopLevelDeps   []string `json:"top_level_deps"`
	ParseWarning   string   `json:"parse_warning,omitempty"`
}

// InspectReport is the full structured bundle inspect report (B25 consumes --json).
type InspectReport struct {
	Header         InspectHeader            `json:"header"`
	Integrity      *VerifyReport            `json:"integrity"`
	Verified       bool                     `json:"verified"`
	Publisher      *InspectPublisher        `json:"publisher,omitempty"`
	Provenance     *pack.ProvenanceReport   `json:"provenance,omitempty"`
	ProvenanceText string                   `json:"provenance_text,omitempty"`
	PolicySummary  []PolicySummaryLine      `json:"policy_summary,omitempty"`
	PolicyLints    []PolicyLint             `json:"policy_lints,omitempty"`
	Requirements   *InspectRequirements     `json:"requirements,omitempty"`
	SBOM           *SBOMSummary             `json:"sbom,omitempty"`
	ExtraFiles     []ManifestExtraFile      `json:"extra_files,omitempty"`
}

// Inspect builds an offline inspect report for an opened bundle file.
func Inspect(path string, b *Bundle, verifyReport *VerifyReport) (*InspectReport, error) {
	if b == nil {
		return nil, fmt.Errorf("bundle must not be nil")
	}
	if verifyReport == nil {
		return nil, fmt.Errorf("verify report must not be nil")
	}

	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat bundle: %w", err)
	}
	digest, err := FileBundleDigest(path)
	if err != nil {
		return nil, err
	}

	report := &InspectReport{
		Header: InspectHeader{
			File:         path,
			SizeBytes:    fi.Size(),
			BundleDigest: digest,
		},
		Integrity: verifyReport,
		Verified:  verifyReport.Verified,
		ExtraFiles: append([]ManifestExtraFile(nil), b.Manifest.ExtraFiles...),
	}

	if b.Manifest != nil {
		report.Header.BundleSchemaVersion = b.Manifest.BundleSchemaVersion
	}
	if b.Lock != nil {
		report.Header.LockSchemaVersion = b.Lock.SchemaVersion
		report.Header.AgentName = b.Lock.AgentName
		report.Header.AgentVersion = b.Lock.AgentVersion
	}

	if !verifyReport.Verified {
		return report, nil
	}

	// Sections 3+ only when integrity verified (anti-phishing gate).
	report.Publisher = buildPublisherSection(b)
	if b.Lock != nil {
		provReport, err := pack.VerifyProvenance(b.Lock)
		if err != nil {
			return nil, fmt.Errorf("provenance: %w", err)
		}
		report.Provenance = provReport
		report.ProvenanceText = pack.FormatProvenance(provReport)
	}

	pol, err := policy.ParsePolicy(bytes.NewReader(b.PolicyYAML))
	if err != nil {
		return nil, fmt.Errorf("parse policy: %w", err)
	}
	report.PolicySummary = renderPolicySummary(pol)
	report.PolicyLints = ComputePolicyLints(pol)
	report.Requirements = buildRequirements(b, pol)
	report.SBOM = summarizeSBOM(b.SBOM)

	return report, nil
}

// FileBundleDigest returns SHA-256 hex of the bundle file bytes.
func FileBundleDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open bundle: %w", err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read bundle: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func buildPublisherSection(b *Bundle) *InspectPublisher {
	if b.Manifest == nil {
		return nil
	}
	pub := b.Manifest.Publisher
	return &InspectPublisher{
		Name:               pub.Name,
		Fingerprint:        pub.Fingerprint,
		FingerprintDisplay: identity.FormatFingerprintDisplay(pub.Fingerprint),
		TrustDisclaimer:    D3TrustDisclaimer,
	}
}

func buildRequirements(b *Bundle, pol *policy.Policy) *InspectRequirements {
	req := &InspectRequirements{
		Credentials: []CredentialRequirement{},
		LLMProvider: "(not declared)",
		Image:       "rebuild required (source in bundle; no prebuilt image)",
		Platform:    "",
	}
	if b.Lock != nil {
		req.Platform = b.Lock.Platform
		if b.Lock.AgentYAML != nil && strings.TrimSpace(b.Lock.AgentYAML.LLM.Provider) != "" {
			req.LLMProvider = b.Lock.AgentYAML.LLM.Provider
		}
	}
	if b.Manifest != nil && b.Manifest.Contents.Image != nil {
		img := b.Manifest.Contents.Image
		req.Image = fmt.Sprintf("included (digest %s, platform %s)", img.Digest, img.Platform)
	}

	egressDest := make(map[string]string)
	for _, e := range pol.Egress {
		dest := formatEgressDest(&e)
		if dest != "" {
			egressDest[e.Credential] = dest
		}
	}
	for _, c := range pol.Credentials {
		cr := CredentialRequirement{
			ID:   c.ID,
			Type: c.Type,
		}
		if c.Header != "" {
			cr.Header = c.Header
		}
		if d, ok := egressDest[c.ID]; ok {
			cr.Destination = d
		} else if c.Path != "" {
			cr.Destination = c.Path
		}
		req.Credentials = append(req.Credentials, cr)
	}
	sort.Slice(req.Credentials, func(i, j int) bool {
		return req.Credentials[i].ID < req.Credentials[j].ID
	})
	return req
}

func formatEgressDest(e *policy.EgressRule) string {
	if strings.TrimSpace(e.CIDR) != "" {
		return e.CIDR
	}
	if strings.TrimSpace(e.Domain) == "" {
		return ""
	}
	ports := e.Ports
	if len(ports) == 0 {
		ports = []int{443}
	}
	var parts []string
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%s:%d", e.Domain, p))
	}
	methods := e.Methods
	if len(methods) == 0 {
		methods = []string{"*"}
	}
	return strings.Join(parts, ",") + " " + strings.Join(methods, ",")
}

func renderPolicySummary(pol *policy.Policy) []PolicySummaryLine {
	var lines []PolicySummaryLine
	for _, e := range pol.Egress {
		lines = append(lines, PolicySummaryLine{
			Section: "egress",
			Detail:  formatEgressDest(&e),
		})
	}
	for _, c := range pol.Credentials {
		detail := fmt.Sprintf("id=%s type=%s", c.ID, c.Type)
		if c.Header != "" {
			detail += fmt.Sprintf(" header=%s", c.Header)
		}
		if c.Path != "" {
			detail += fmt.Sprintf(" destination=%s", c.Path)
		}
		lines = append(lines, PolicySummaryLine{Section: "credential", Detail: detail})
	}
	for _, m := range pol.MCPServers {
		tools := "*"
		if len(m.AllowedTools) > 0 {
			tools = strings.Join(m.AllowedTools, ",")
		}
		detail := fmt.Sprintf("server=%s url=%s tools=%s", m.Name, m.URL, tools)
		lines = append(lines, PolicySummaryLine{Section: "mcp", Detail: detail})
	}
	for _, in := range pol.Ingress {
		lines = append(lines, PolicySummaryLine{
			Section: "ingress",
			Detail:  fmt.Sprintf("path=%s port=%d", in.Path, in.Port),
		})
	}
	for _, h := range pol.Hooks {
		lines = append(lines, PolicySummaryLine{
			Section: "hook",
			Detail:  fmt.Sprintf("name=%s url=%s", h.Name, h.URL),
		})
	}
	return lines
}

// ComputePolicyLints returns PRD A3 warnings for a parsed policy.
func ComputePolicyLints(pol *policy.Policy) []PolicyLint {
	if pol == nil {
		return nil
	}
	var lints []PolicyLint
	domainCount := 0
	for i := range pol.Egress {
		e := &pol.Egress[i]
		if strings.TrimSpace(e.Domain) != "" {
			domainCount++
		}
		if isWildcardDomain(e.Domain) {
			lints = append(lints, PolicyLint{
				Code:    LintWildcardDomain,
				Message: fmt.Sprintf("egress allows wildcard domain %q", e.Domain),
			})
		}
		if strings.TrimSpace(e.CIDR) != "" || isRawIPDomain(e.Domain) {
			dest := e.CIDR
			if dest == "" {
				dest = e.Domain
			}
			lints = append(lints, PolicyLint{
				Code:    LintRawIPEgress,
				Message: fmt.Sprintf("egress allows raw IP or CIDR %q", dest),
			})
		}
		ports := e.Ports
		if len(ports) == 0 {
			ports = []int{443}
		}
		for _, p := range ports {
			if p != 443 {
				lints = append(lints, PolicyLint{
					Code:    LintNonTLSPort,
					Message: fmt.Sprintf("egress %q allows non-443 port %d", e.Domain, p),
				})
			}
		}
		if e.Credential != "" && (isWildcardDomain(e.Domain) || strings.TrimSpace(e.CIDR) != "") {
			lints = append(lints, PolicyLint{
				Code:    LintCredWildcardDest,
				Message: fmt.Sprintf("credential %q bound to broad egress destination", e.Credential),
			})
		}
	}
	if domainCount > 8 {
		lints = append(lints, PolicyLint{
			Code:    LintManyEgressDomains,
			Message: fmt.Sprintf("policy declares %d egress domains (>8)", domainCount),
		})
	}
	sort.Slice(lints, func(i, j int) bool {
		if lints[i].Code != lints[j].Code {
			return lints[i].Code < lints[j].Code
		}
		return lints[i].Message < lints[j].Message
	})
	return lints
}

func isWildcardDomain(domain string) bool {
	d := strings.TrimSpace(domain)
	if d == "" {
		return false
	}
	return strings.Contains(d, "*") || strings.HasPrefix(d, ".")
}

func isRawIPDomain(domain string) bool {
	d := strings.TrimSpace(domain)
	if d == "" {
		return false
	}
	if strings.Contains(d, "/") {
		if _, _, err := net.ParseCIDR(d); err == nil {
			return true
		}
	}
	host := d
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}
	return net.ParseIP(host) != nil
}

type spdxInspect struct {
	Packages      []spdxPackage      `json:"packages"`
	Relationships []spdxRelationship `json:"relationships"`
}

type spdxPackage struct {
	Name    string `json:"name"`
	SPDXID  string `json:"SPDXID"`
}

type spdxRelationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
	RelationshipType   string `json:"relationshipType"`
}

func summarizeSBOM(raw []byte) *SBOMSummary {
	out := &SBOMSummary{TopLevelDeps: []string{}}
	if len(raw) == 0 {
		out.ParseWarning = "empty sbom"
		return out
	}
	var doc spdxInspect
	if err := json.Unmarshal(raw, &doc); err != nil {
		out.ParseWarning = "sbom is not valid SPDX JSON"
		return out
	}
	out.PackageCount = len(doc.Packages)
	nameByID := make(map[string]string)
	for _, p := range doc.Packages {
		if p.SPDXID != "" && p.Name != "" {
			nameByID[p.SPDXID] = p.Name
		}
	}
	depNames := make(map[string]struct{})
	for _, rel := range doc.Relationships {
		if !strings.EqualFold(rel.RelationshipType, "DEPENDS_ON") {
			continue
		}
		if name, ok := nameByID[rel.RelatedSPDXElement]; ok {
			depNames[name] = struct{}{}
		}
	}
	if len(depNames) == 0 {
		for _, p := range doc.Packages {
			if p.Name != "" {
				depNames[p.Name] = struct{}{}
			}
		}
	}
	for name := range depNames {
		out.TopLevelDeps = append(out.TopLevelDeps, name)
	}
	sort.Strings(out.TopLevelDeps)
	return out
}

// FormatInspectText renders the inspect report for terminal output.
func FormatInspectText(r *InspectReport) string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "BUNDLE INSPECT\n\n")
	fmt.Fprintf(&b, "Header\n")
	fmt.Fprintf(&b, "  File:                  %s\n", r.Header.File)
	fmt.Fprintf(&b, "  Size:                  %d bytes\n", r.Header.SizeBytes)
	fmt.Fprintf(&b, "  Bundle digest:         %s\n", r.Header.BundleDigest)
	fmt.Fprintf(&b, "  Bundle schema:         %d\n", r.Header.BundleSchemaVersion)
	fmt.Fprintf(&b, "  Lock schema:           %d\n", r.Header.LockSchemaVersion)
	if r.Header.AgentName != "" {
		fmt.Fprintf(&b, "  Agent:                 %s@%s\n", r.Header.AgentName, r.Header.AgentVersion)
	}
	fmt.Fprintf(&b, "\nIntegrity\n")
	if r.Integrity != nil {
		tw := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, c := range r.Integrity.Checks {
			status := "FAIL"
			if c.Passed {
				status = "PASS"
			}
			_, _ = fmt.Fprintf(tw, "  %s\t%s\t%s\n", c.Name, status, c.Detail)
		}
		_ = tw.Flush()
	}
	if !r.Verified {
		fmt.Fprintf(&b, "\nIntegrity verification failed. Publisher, provenance, and policy sections are withheld.\n")
		return b.String()
	}

	if r.Publisher != nil {
		fmt.Fprintf(&b, "\nPublisher\n")
		fmt.Fprintf(&b, "  Name:        %s\n", r.Publisher.Name)
		fmt.Fprintf(&b, "  Fingerprint: %s\n", r.Publisher.FingerprintDisplay)
		fmt.Fprintf(&b, "  %s\n", strings.ReplaceAll(r.Publisher.TrustDisclaimer, "\n", "\n  "))
	}
	if r.ProvenanceText != "" {
		fmt.Fprintf(&b, "\n%s\n", r.ProvenanceText)
	}
	if len(r.PolicySummary) > 0 {
		fmt.Fprintf(&b, "\nPolicy summary\n")
		for _, line := range r.PolicySummary {
			fmt.Fprintf(&b, "  [%s] %s\n", line.Section, line.Detail)
		}
	}
	if len(r.PolicyLints) > 0 {
		fmt.Fprintf(&b, "\nPolicy lints (warnings)\n")
		for _, lint := range r.PolicyLints {
			fmt.Fprintf(&b, "  - [%s] %s\n", lint.Code, lint.Message)
		}
	}
	if r.Requirements != nil {
		fmt.Fprintf(&b, "\nRequirements\n")
		fmt.Fprintf(&b, "  Platform:    %s\n", r.Requirements.Platform)
		fmt.Fprintf(&b, "  LLM:         %s\n", r.Requirements.LLMProvider)
		fmt.Fprintf(&b, "  Image:       %s\n", r.Requirements.Image)
		if len(r.Requirements.Credentials) > 0 {
			fmt.Fprintf(&b, "  Credentials:\n")
			for _, c := range r.Requirements.Credentials {
				line := fmt.Sprintf("    - %s (%s)", c.ID, c.Type)
				if c.Header != "" {
					line += fmt.Sprintf(" header=%s", c.Header)
				}
				if c.Destination != "" {
					line += fmt.Sprintf(" destination=%s", c.Destination)
				}
				fmt.Fprintf(&b, "%s\n", line)
			}
		} else {
			fmt.Fprintf(&b, "  Credentials: (none declared)\n")
		}
	}
	if r.SBOM != nil {
		fmt.Fprintf(&b, "\nSBOM\n")
		fmt.Fprintf(&b, "  Packages: %d\n", r.SBOM.PackageCount)
		if r.SBOM.ParseWarning != "" {
			fmt.Fprintf(&b, "  Warning: %s\n", r.SBOM.ParseWarning)
		}
		if len(r.SBOM.TopLevelDeps) > 0 {
			fmt.Fprintf(&b, "  Top-level deps: %s\n", strings.Join(r.SBOM.TopLevelDeps, ", "))
		}
	}
	if len(r.ExtraFiles) > 0 {
		fmt.Fprintf(&b, "\nextra files (not part of build)\n")
		for _, ef := range r.ExtraFiles {
			fmt.Fprintf(&b, "  %s  %s  %d bytes\n", ef.Path, ef.Digest, ef.Bytes)
		}
	}
	return b.String()
}