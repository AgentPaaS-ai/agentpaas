package policy

// RoutedRunPolicy defines runtime limits for a routed policy (v1.1+).
type RoutedRunPolicy struct {
	ModelCallTimeout               string `yaml:"model_call_timeout"`
	StallTimeout                   string `yaml:"stall_timeout"`
	AttemptLease                   string `yaml:"attempt_lease"`
	MaxActiveDuration              string `yaml:"max_active_duration"`
	RecoveryMargin                 string `yaml:"recovery_margin"`
	MaxLLMCalls                    int    `yaml:"max_llm_calls"`
	MaxModelRecoveriesPerAttempt   int    `yaml:"max_model_recoveries_per_attempt"`
	MaxWorkerRetries               int    `yaml:"max_worker_retries"`
	MaxIdenticalToolActions        int    `yaml:"max_identical_tool_actions"`
	MaxActionsWithoutProgress      int    `yaml:"max_actions_without_progress"`
}

// ModelRoute defines a logical model route with pattern, cloud transfer policy,
// minimum requirements, and a set of candidates.
type ModelRoute struct {
	Pattern        string          `yaml:"pattern"`
	CloudTransfer  string          `yaml:"cloud_transfer"`
	Minimum        *ModelMinimum   `yaml:"minimum,omitempty"`
	Candidates     []Candidate     `yaml:"candidates"`
}

// ModelMinimum defines the minimum requirements for a model route.
type ModelMinimum struct {
	CapabilityTier string   `yaml:"capability_tier"`
	ContextTokens  int      `yaml:"context_tokens"`
	Features       []string `yaml:"features"`
	Effort         string   `yaml:"effort,omitempty"`
}

// Candidate defines a single model provider candidate within a route.
type Candidate struct {
	ID                string   `yaml:"id"`
	Role              string   `yaml:"role"`
	Provider          string   `yaml:"provider"`
	Model             string   `yaml:"model"`
	UpstreamProviders []string `yaml:"upstream_providers,omitempty"`
	Credential        string   `yaml:"credential,omitempty"`
	Location          string   `yaml:"location"`
	Endpoint          string   `yaml:"endpoint,omitempty"`
	AuthMode          string   `yaml:"auth_mode,omitempty"` // "none" only for local custom endpoints
}

// ValidateMode enums for route/candidate fields
const (
	PatternLocalFirst     = "local-first"
	PatternCloudCostFirst = "cloud-cost-first"

	RolePrimary  = "primary"
	RoleRecovery = "recovery"

	LocationLocal = "local"
	LocationCloud = "cloud"

	CloudTransferAllowed = "allowed"
	CloudTransferDenied  = "denied"

	CapabilityBasic    = "basic"
	CapabilityStandard = "standard"
	CapabilityAdvanced = "advanced"

	FeatureChat           = "chat"
	FeatureStructuredJSON = "structured_json"
	FeatureReasoningEffort = "reasoning_effort"

	EffortStandard = "standard"
	EffortHigh     = "high"

	AuthModeNone = "none"
)

// Known schema versions
const (
	SchemaVersion10 = "1.0"
	SchemaVersion11 = "1.1"
)

// HasRoutedFields returns true if the policy has v1.1 routed fields.
func (p *Policy) HasRoutedFields() bool {
	return p.RoutedRun != nil || len(p.ModelRoutes) > 0 || p.LLMBudget != nil && p.LLMBudget.MaxCostUSD != ""
}

// IsSchema11 returns true if the policy version is "1.1".
func (p *Policy) IsSchema11() bool {
	return p.Version == SchemaVersion11
}
