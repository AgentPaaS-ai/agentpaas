//go:build !linux

package harness

import "log"

// DropNetAdminCapability is a no-op on non-Linux platforms.
func DropNetAdminCapability() {
	log.Printf("harness: drop CAP_NET_ADMIN skipped (GOOS != linux)")
}