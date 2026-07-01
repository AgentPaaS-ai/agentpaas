//go:build !linux

package harness

import "testing"

func TestDropNetAdminCapability_StubNoOp(t *testing.T) {
	// On non-Linux platforms, DropNetAdminCapability is a no-op.
	// Just verify it doesn't panic.
	DropNetAdminCapability()
}
