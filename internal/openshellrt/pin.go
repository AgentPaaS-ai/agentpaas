// Package openshellrt drives a local OpenShell gateway through the official
// Go SDK. OpenShell stays the pinned N-1 stable release. This package does
// not fork OpenShell and does not vendor a replacement runtime.
package openshellrt

// PinVersion is the N-1 stable OpenShell release. Latest stable at pin time
// was v0.1.2 (2026-09-28); N-1 is v0.1.1. The Go SDK module has no sdk/go
// version tag, so go.mod pins the v0.1.1 commit 4ce767fc0cad.
const (
	PinVersion = "v0.1.1"
	PinCommit  = "4ce767fc0cadad773c398e15109c0286f4b7aa30"

	// GatewayAddr is the loopback plaintext address the daemon drives.
	GatewayAddr = "127.0.0.1:17670"
	GatewayURL  = "http://127.0.0.1:17670"

	workspaceDefault = "default"
	profileID        = "agentpaas-broker"
	placeholderEnv   = "AGENTPAAS_OS_BROKER_PLACEHOLDER"
)

// PlaceholderEnv is the sandbox env var OpenShell fills with an opaque
// credential placeholder. The real secret stays in the provider.
func PlaceholderEnv() string { return placeholderEnv }
