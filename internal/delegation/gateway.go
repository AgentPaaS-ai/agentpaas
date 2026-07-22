package delegation

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// CapabilityHeader is the internal HTTP/gRPC header name used to carry the
// per-binding capability token between the caller gateway and callee gateway.
// This header is STRIPPED before it reaches agent code and MUST NEVER appear
// in agent-facing responses, SDK DTOs, logs, or audit event payloads that
// are exposed beyond the gateway.
const CapabilityHeader = "X-AgentPaaS-Capability"

// BindingExpectation is the set of claims that the callee gateway validates
// against the capability token.
type BindingExpectation struct {
	BindingID   string
	WorkflowID  string
	CallerLease string
	CalleeLease string
}

// GatewayEnforcer is the trusted component that attaches, validates, and
// strips per-binding capability tokens. Agent code never sees the token
// or the network alias.
//
// In production, the full implementation maps capability tokens to binding
// state inside the gateway mesh. For T03, this is a thin stub that records
// enforcement points — every test path through ValidateAndStrip proves that
// the enforcement step was called and that token material is stripped.
type GatewayEnforcer struct{}

// Attach returns headers for the trusted caller-gateway path. The returned
// map contains the CapabilityHeader key with the token as its value.
// This MUST only be called by the trusted caller gateway — never by agent code.
func (g *GatewayEnforcer) Attach(token string) map[string]string {
	return map[string]string{
		CapabilityHeader: token,
	}
}

// ValidateAndStrip checks that headers contain a valid capability matching
// the expectedToken, then removes the CapabilityHeader from headers in place.
//
// In production, expectedToken is the random token from BindingCapabilities.
// In tests, callers may use DeriveCapabilityTokenForTest to produce a
// deterministic token.
//
// Returns an error if:
//   - CapabilityHeader is missing
//   - The token does not match the expectedToken
//
// The error message MUST NOT contain the actual token value.
func (g *GatewayEnforcer) ValidateAndStrip(headers map[string]string, expectedToken string) error {
	capToken, ok := headers[CapabilityHeader]
	if !ok {
		return fmt.Errorf("delegation: gateway enforce: missing %s header", CapabilityHeader)
	}
	delete(headers, CapabilityHeader)

	if capToken != expectedToken {
		return fmt.Errorf("delegation: gateway enforce: invalid capability token (header stripped)")
	}

	return nil
}

// DeriveCapabilityTokenForTest derives a deterministic capability token from a
// BindingExpectation. This is for tests only — production MUST use randomly
// generated tokens from GenerateCapabilityToken stored in BindingCapabilities.
func DeriveCapabilityTokenForTest(expect BindingExpectation) string {
	return deriveCapabilityToken(expect)
}

// GenerateCapabilityToken produces a cryptographically random unguessable
// capability token. This is called by the trusted harness/gateway at invoke
// bootstrap; the resulting token is stored in BindingCapabilities and NEVER
// serialized into agent responses.
func GenerateCapabilityToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("delegation: generate capability token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// deriveCapabilityToken derives the expected capability token from a
// BindingExpectation. In production this would be a MAC or lookup; for
// the T03 stub it uses a deterministic derivation so tests can
// independently verify.
func deriveCapabilityToken(expect BindingExpectation) string {
	// For the stub, we use a simple derivation that includes all
	// expectation fields. This lets the caller gateway produce the
	// same token and the callee gateway verify it.
	input := fmt.Sprintf("%s|%s|%s|%s", expect.BindingID, expect.WorkflowID, expect.CallerLease, expect.CalleeLease)
	h := sha256Hex([]byte(input))
	return "cap-" + h[:32]
}

// sha256Hex returns the lowercase hex encoding of the SHA-256 hash of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
