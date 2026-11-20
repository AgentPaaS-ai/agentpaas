//go:build linux

package harness

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestDropNetAdminCapability_ClearsBit12(t *testing.T) {
	original := readCapabilitySets(t)
	t.Cleanup(func() { writeCapabilitySets(t, original) })

	withNetAdmin := readCapabilitySets(t)
	mask := uint32(1 << capNetAdmin)
	withNetAdmin[0].Effective |= mask
	withNetAdmin[0].Permitted |= mask
	withNetAdmin[0].Inheritable |= mask
	writeCapabilitySets(t, withNetAdmin)

	DropNetAdminCapability()

	after := readCapabilitySets(t)
	assertNetAdminCleared(t, after)
}

func readCapabilitySets(t *testing.T) [2]unix.CapUserData {
	t.Helper()
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		t.Fatalf("Capget: %v", err)
	}
	return data
}

func writeCapabilitySets(t *testing.T, data [2]unix.CapUserData) {
	t.Helper()
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		t.Fatalf("Capset: %v", err)
	}
}

func assertNetAdminCleared(t *testing.T, data [2]unix.CapUserData) {
	t.Helper()
	mask := uint32(1 << capNetAdmin)
	if data[0].Effective&mask != 0 {
		t.Errorf("CAP_NET_ADMIN still set in effective set")
	}
	if data[0].Permitted&mask != 0 {
		t.Errorf("CAP_NET_ADMIN still set in permitted set")
	}
	if data[0].Inheritable&mask != 0 {
		t.Errorf("CAP_NET_ADMIN still set in inheritable set")
	}
}
