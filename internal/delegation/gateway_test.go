package delegation

import (
	"strings"
	"testing"
)

func TestGatewayEnforcer_AttachAndValidate(t *testing.T) {
	e := &GatewayEnforcer{}
	bindingID := "report.verify"
	workflowID := "wf-test"
	callerLeaseID := "lease-caller"
	calleeLeaseID := "lease-callee"

	// Derive the correct token the same way the enforcer will.
	expect := BindingExpectation{
		BindingID:   bindingID,
		WorkflowID:  workflowID,
		CallerLease: callerLeaseID,
		CalleeLease: calleeLeaseID,
	}
	expectedToken := deriveCapabilityToken(expect)

	// Attach returns headers for the trusted path.
	headers := e.Attach(expectedToken)
	if headers == nil {
		t.Fatal("Attach returned nil headers")
	}
	capHeader, ok := headers[CapabilityHeader]
	if !ok {
		t.Fatalf("Attach headers missing %s", CapabilityHeader)
	}
	if capHeader != expectedToken {
		t.Errorf("capability header = %q, want %q", capHeader, expectedToken)
	}

	// ValidateAndStrip with correct expectation passes.
	if err := e.ValidateAndStrip(headers, expect); err != nil {
		t.Fatalf("ValidateAndStrip: unexpected error: %v", err)
	}
}

func TestGatewayEnforcer_MissingCapabilityHeader(t *testing.T) {
	e := &GatewayEnforcer{}
	headers := map[string]string{
		"X-Unrelated": "value",
	}
	expect := BindingExpectation{
		BindingID:   "report.verify",
		WorkflowID:  "wf-test",
		CallerLease: "lease-caller",
		CalleeLease: "lease-callee",
	}
	err := e.ValidateAndStrip(headers, expect)
	if err == nil {
		t.Fatal("expected error for missing capability header")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected 'missing' in error, got: %v", err)
	}
}

func TestGatewayEnforcer_WrongToken(t *testing.T) {
	e := &GatewayEnforcer{}
	// Attach one token, validate with a different expectation that
	// doesn't match the token.
	token := "cap-test-token-123"
	headers := e.Attach(token)
	// Tamper: replace with wrong token.
	headers[CapabilityHeader] = "wrong-token"

	expect := BindingExpectation{
		BindingID:   "report.verify",
		WorkflowID:  "wf-test",
		CallerLease: "lease-caller",
		CalleeLease: "lease-callee",
	}
	err := e.ValidateAndStrip(headers, expect)
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected 'invalid' in error, got: %v", err)
	}
}

func TestGatewayEnforcer_StripRemovesCapability(t *testing.T) {
	e := &GatewayEnforcer{}
	expect := BindingExpectation{
		BindingID:   "report.verify",
		WorkflowID:  "wf-test",
		CallerLease: "lease-caller",
		CalleeLease: "lease-callee",
	}
	token := deriveCapabilityToken(expect)
	headers := e.Attach(token)
	headers["X-Keep-Me"] = "keep-value"

	if err := e.ValidateAndStrip(headers, expect); err != nil {
		t.Fatalf("ValidateAndStrip: %v", err)
	}

	// Capability header must be stripped.
	if _, ok := headers[CapabilityHeader]; ok {
		t.Errorf("%s must be stripped after validation", CapabilityHeader)
	}
	// Other headers preserved.
	if v, ok := headers["X-Keep-Me"]; !ok || v != "keep-value" {
		t.Errorf("non-capability header was removed: got %q, ok=%v", v, ok)
	}
}

func TestGatewayEnforcer_NoTokenLeakage(t *testing.T) {
	// The token itself must never appear in any validation error message
	// or returned value.
	e := &GatewayEnforcer{}
	token := "secret-cap-token-abc"
	headers := e.Attach(token)

	// Overwrite with an invalid token prefix to trigger error.
	headers[CapabilityHeader] = "evil-token"

	expect := BindingExpectation{
		BindingID:   "report.verify",
		WorkflowID:  "wf-test",
		CallerLease: "lease-caller",
		CalleeLease: "lease-callee",
	}
	err := e.ValidateAndStrip(headers, expect)
	if err == nil {
		t.Fatal("expected error")
	}
	// The original token must not be in the error.
	errStr := err.Error()
	if strings.Contains(errStr, token) {
		t.Errorf("token leaked in error message: %q", errStr)
	}
}
