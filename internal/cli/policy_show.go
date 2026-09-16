package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/operator"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
	"github.com/spf13/cobra"
)

// policyShowResult is the compiled policy contract rendered by `policy show`.
// It is a view of ParsePolicy output, not a second store and not raw YAML.
type policyShowResult struct {
	SchemaVersion string                     `json:"schema_version"`
	ProjectDir    string                     `json:"project_dir"`
	Digest        string                     `json:"digest"`
	Ingress       policyShowIngressSection   `json:"ingress"`
	Egress        policyShowEgressSection    `json:"egress"`
	Credentials   policyShowCredSection      `json:"credentials"`
	MCPServers    policyShowMCPSection       `json:"mcp_servers"`
	Guardrails    policyShowGuardrailSection `json:"guardrails"`
	LLMBudget     policyShowLLMBudget        `json:"llm_budget"`
	LLMRateLimit  policyShowLLMRateLimit     `json:"llm_rate_limit"`
	ModelRoutes   policyShowModelRoutes      `json:"model_routes"`
}

type policyShowIngressSection struct {
	Enabled bool                    `json:"enabled"`
	Rules   []policyShowIngressRule `json:"rules,omitempty"`
}

type policyShowIngressRule struct {
	Path    string `json:"path,omitempty"`
	Port    int    `json:"port,omitempty"`
	Enabled bool   `json:"enabled"`
}

type policyShowEgressSection struct {
	Enabled bool                   `json:"enabled"`
	Hosts   []policyShowEgressHost `json:"hosts,omitempty"`
}

type policyShowEgressHost struct {
	Domain  string `json:"domain,omitempty"`
	CIDR    string `json:"cidr,omitempty"`
	Enabled bool   `json:"enabled"`
}

type policyShowCredSection struct {
	Enabled bool                   `json:"enabled"`
	Items   []policyShowCredential `json:"items,omitempty"`
}

type policyShowCredential struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type,omitempty"`
	Header  string `json:"header,omitempty"`
	Enabled bool   `json:"enabled"`
}

type policyShowMCPSection struct {
	Enabled bool            `json:"enabled"`
	Items   []policyShowMCP `json:"items,omitempty"`
}

type policyShowMCP struct {
	Name         string   `json:"name,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"`
	Enabled      bool     `json:"enabled"`
}

type policyShowGuardrailSection struct {
	Enabled  bool                  `json:"enabled"`
	Sequence []policyShowGuardrail `json:"sequence,omitempty"`
	PII      *policyShowPII        `json:"pii,omitempty"`
}

type policyShowGuardrail struct {
	Type       string `json:"type,omitempty"`
	Pattern    string `json:"pattern,omitempty"`
	Action     string `json:"action,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Credential string `json:"credential,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type policyShowPII struct {
	Enabled  bool     `json:"enabled"`
	Action   string   `json:"action,omitempty"`
	Builtins []string `json:"builtins,omitempty"`
	Patterns []string `json:"patterns,omitempty"`
}

type policyShowLLMBudget struct {
	Enabled             bool   `json:"enabled"`
	MaxTokens           int    `json:"max_tokens,omitempty"`
	MaxTokensPerRequest int    `json:"max_tokens_per_request,omitempty"`
	MaxCostUSD          string `json:"max_cost_usd,omitempty"`
}

type policyShowLLMRateLimit struct {
	Enabled           bool `json:"enabled"`
	RequestsPerMinute int  `json:"requests_per_minute,omitempty"`
	TokensPerMinute   int  `json:"tokens_per_minute,omitempty"`
}

type policyShowModelRoutes struct {
	Enabled bool                            `json:"enabled"`
	Routes  map[string]policyShowModelRoute `json:"routes,omitempty"`
}

type policyShowModelRoute struct {
	Pattern       string                `json:"pattern,omitempty"`
	CloudTransfer string                `json:"cloud_transfer,omitempty"`
	Minimum       *policyShowModelMin   `json:"minimum,omitempty"`
	Candidates    []policyShowCandidate `json:"candidates,omitempty"`
}

type policyShowModelMin struct {
	CapabilityTier string   `json:"capability_tier,omitempty"`
	ContextTokens  int      `json:"context_tokens,omitempty"`
	Features       []string `json:"features,omitempty"`
	Effort         string   `json:"effort,omitempty"`
}

type policyShowCandidate struct {
	ID                string   `json:"id,omitempty"`
	Role              string   `json:"role,omitempty"`
	Provider          string   `json:"provider,omitempty"`
	Model             string   `json:"model,omitempty"`
	UpstreamProviders []string `json:"upstream_providers,omitempty"`
	Credential        string   `json:"credential,omitempty"`
	Location          string   `json:"location,omitempty"`
	Endpoint          string   `json:"endpoint,omitempty"`
	AuthMode          string   `json:"auth_mode,omitempty"`
}

func showProjectPolicy(cmd *cobra.Command, projectDir string) error {
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

	result, err := compilePolicyShow(projectDir, parsed)
	if err != nil {
		return err
	}
	return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
		r, ok := v.(*policyShowResult)
		if !ok {
			return ""
		}
		return formatPolicyShowText(r)
	})
}

func compilePolicyShow(projectDir string, p *policy.Policy) (*policyShowResult, error) {
	digest, err := policy.Digest(p)
	if err != nil {
		return nil, err
	}

	out := &policyShowResult{
		SchemaVersion: operator.SchemaVersion,
		ProjectDir:    projectDir,
		Digest:        digest,
	}

	for _, rule := range p.Ingress {
		out.Ingress.Rules = append(out.Ingress.Rules, policyShowIngressRule{
			Path:    rule.Path,
			Port:    rule.Port,
			Enabled: true,
		})
	}
	out.Ingress.Enabled = len(out.Ingress.Rules) > 0

	for _, rule := range p.Egress {
		out.Egress.Hosts = append(out.Egress.Hosts, policyShowEgressHost{
			Domain:  rule.Domain,
			CIDR:    rule.CIDR,
			Enabled: true,
		})
	}
	out.Egress.Enabled = len(out.Egress.Hosts) > 0

	for _, cred := range p.Credentials {
		out.Credentials.Items = append(out.Credentials.Items, policyShowCredential{
			ID:      cred.ID,
			Type:    cred.Type,
			Header:  cred.Header,
			Enabled: true,
		})
	}
	out.Credentials.Enabled = len(out.Credentials.Items) > 0

	for _, srv := range p.MCPServers {
		out.MCPServers.Items = append(out.MCPServers.Items, policyShowMCP{
			Name:         srv.Name,
			AllowedTools: append([]string(nil), srv.AllowedTools...),
			DeniedTools:  append([]string(nil), srv.DeniedTools...),
			Enabled:      true,
		})
	}
	out.MCPServers.Enabled = len(out.MCPServers.Items) > 0

	for _, g := range p.Guardrails {
		out.Guardrails.Sequence = append(out.Guardrails.Sequence, policyShowGuardrail{
			Type:       g.Type,
			Pattern:    g.Pattern,
			Action:     g.Action,
			Provider:   g.Provider,
			Credential: g.Credential,
			Enabled:    true,
		})
	}
	if p.PII != nil {
		out.Guardrails.PII = &policyShowPII{
			Enabled:  true,
			Action:   p.PII.Action,
			Builtins: append([]string(nil), p.PII.Builtins...),
			Patterns: append([]string(nil), p.PII.Patterns...),
		}
	}
	out.Guardrails.Enabled = len(out.Guardrails.Sequence) > 0 || out.Guardrails.PII != nil

	if p.LLMBudget != nil {
		out.LLMBudget = policyShowLLMBudget{
			Enabled:             true,
			MaxTokens:           p.LLMBudget.MaxTokens,
			MaxTokensPerRequest: p.LLMBudget.MaxTokensPerRequest,
			MaxCostUSD:          p.LLMBudget.MaxCostUSD,
		}
	}

	if p.LLMRateLimit != nil {
		out.LLMRateLimit = policyShowLLMRateLimit{
			Enabled:           true,
			RequestsPerMinute: p.LLMRateLimit.RequestsPerMinute,
			TokensPerMinute:   p.LLMRateLimit.TokensPerMinute,
		}
	}

	if len(p.ModelRoutes) > 0 {
		out.ModelRoutes.Enabled = true
		out.ModelRoutes.Routes = make(map[string]policyShowModelRoute, len(p.ModelRoutes))
		for name, route := range p.ModelRoutes {
			out.ModelRoutes.Routes[name] = declaredModelRoute(route)
		}
	}

	return out, nil
}

func declaredModelRoute(route policy.ModelRoute) policyShowModelRoute {
	view := policyShowModelRoute{
		Pattern:       route.Pattern,
		CloudTransfer: route.CloudTransfer,
	}
	if route.Minimum != nil {
		view.Minimum = &policyShowModelMin{
			CapabilityTier: route.Minimum.CapabilityTier,
			ContextTokens:  route.Minimum.ContextTokens,
			Features:       append([]string(nil), route.Minimum.Features...),
			Effort:         route.Minimum.Effort,
		}
	}
	for _, c := range route.Candidates {
		view.Candidates = append(view.Candidates, policyShowCandidate{
			ID:                c.ID,
			Role:              c.Role,
			Provider:          c.Provider,
			Model:             c.Model,
			UpstreamProviders: append([]string(nil), c.UpstreamProviders...),
			Credential:        c.Credential,
			Location:          c.Location,
			Endpoint:          c.Endpoint,
			AuthMode:          c.AuthMode,
		})
	}
	return view
}

func formatPolicyShowText(r *policyShowResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Project: %s\n", r.ProjectDir)
	fmt.Fprintf(&b, "Digest: %s\n", r.Digest)

	writeShowSection(&b, "Ingress", r.Ingress.Enabled)
	for _, rule := range r.Ingress.Rules {
		status := itemStatus(rule.Enabled)
		fmt.Fprintf(&b, "  - %s %s :%d\n", status, rule.Path, rule.Port)
	}

	writeShowSection(&b, "Egress", r.Egress.Enabled)
	for _, host := range r.Egress.Hosts {
		status := itemStatus(host.Enabled)
		name := host.Domain
		if name == "" {
			name = host.CIDR
		}
		fmt.Fprintf(&b, "  - %s %s\n", status, name)
	}

	writeShowSection(&b, "Credentials", r.Credentials.Enabled)
	for _, cred := range r.Credentials.Items {
		status := itemStatus(cred.Enabled)
		line := fmt.Sprintf("  - %s %s (%s", status, cred.ID, cred.Type)
		if cred.Header != "" {
			line += ", " + cred.Header
		}
		line += ")\n"
		b.WriteString(line)
	}

	writeShowSection(&b, "MCP", r.MCPServers.Enabled)
	for _, srv := range r.MCPServers.Items {
		status := itemStatus(srv.Enabled)
		fmt.Fprintf(&b, "  - %s %s allowed=%v denied=%v\n", status, srv.Name, srv.AllowedTools, srv.DeniedTools)
	}

	writeShowSection(&b, "Guardrails", r.Guardrails.Enabled)
	for _, g := range r.Guardrails.Sequence {
		status := itemStatus(g.Enabled)
		fmt.Fprintf(&b, "  - %s %s action=%s\n", status, g.Type, g.Action)
	}
	if r.Guardrails.PII != nil {
		fmt.Fprintf(&b, "  PII: %s action=%s builtins=%v\n",
			itemStatus(r.Guardrails.PII.Enabled), r.Guardrails.PII.Action, r.Guardrails.PII.Builtins)
	}

	writeShowSection(&b, "LLM budget", r.LLMBudget.Enabled)
	if r.LLMBudget.Enabled {
		fmt.Fprintf(&b, "  max_tokens=%d max_tokens_per_request=%d", r.LLMBudget.MaxTokens, r.LLMBudget.MaxTokensPerRequest)
		if r.LLMBudget.MaxCostUSD != "" {
			fmt.Fprintf(&b, " max_cost_usd=%s", r.LLMBudget.MaxCostUSD)
		}
		b.WriteByte('\n')
	}

	writeShowSection(&b, "LLM rate limit", r.LLMRateLimit.Enabled)
	if r.LLMRateLimit.Enabled {
		fmt.Fprintf(&b, "  requests_per_minute=%d tokens_per_minute=%d\n",
			r.LLMRateLimit.RequestsPerMinute, r.LLMRateLimit.TokensPerMinute)
	}

	writeShowSection(&b, "Model routes", r.ModelRoutes.Enabled)
	for name, route := range r.ModelRoutes.Routes {
		fmt.Fprintf(&b, "  - %s pattern=%s\n", name, route.Pattern)
	}

	return strings.TrimRight(b.String(), "\n")
}

func writeShowSection(b *strings.Builder, name string, enabled bool) {
	fmt.Fprintf(b, "%s: %s\n", name, itemStatus(enabled))
}

func itemStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}
