// Package v023 provides immutable v0.2.3 baseline fixtures for backward
// compatibility tests. These fixtures are read-only — any test that writes
// to these paths is a regression.
//
// Source locations for the values they record:
//   - Policy schema: internal/policy/schema.go (Policy struct, version "1.0")
//   - Canonical digest: internal/policy/canonical.go (Digest, MustDigest)
//   - Agent lock: internal/pack/lock.go (AgentLock, SchemaVersion=2)
//   - Invoke payload: internal/daemon/control_handlers.go (buildInvokePayload)
//   - Operator JSON shapes: internal/operator/schema.go
//   - Harness budget: internal/harness/budget.go (defaultWallClockBudget=120s)
//   - Daemon invoke timeout: internal/daemon/control_handlers.go:794 (2*time.Minute)
//   - Model client timeout: internal/harness/rpc_server.go:402 (120*time.Second)
//   - RLIMIT_CPU=30, RLIMIT_NPROC=0: internal/harness/python_worker.go:460-462
//   - MCP router: internal/mcpmanager/router.go, production daemon does NOT install it
package v023

import _ "embed"

//go:embed fixtures/openrouter/agent.yaml
var OpenRouterAgentYAML []byte

//go:embed fixtures/openrouter/policy.yaml
var OpenRouterPolicyYAML []byte

//go:embed fixtures/direct-provider/agent.yaml
var DirectProviderAgentYAML []byte

//go:embed fixtures/direct-provider/policy.yaml
var DirectProviderPolicyYAML []byte

//go:embed fixtures/no-llm/agent.yaml
var NoLLMAgentYAML []byte

//go:embed fixtures/no-llm/policy.yaml
var NoLLMPolicyYAML []byte

//go:embed fixtures/full-policy/policy.yaml
var FullPolicyYAML []byte

//go:embed fixtures/bundle/agent.lock
var BundleAgentLockJSON []byte

//go:embed fixtures/bundle/policy.yaml
var BundlePolicyYAML []byte