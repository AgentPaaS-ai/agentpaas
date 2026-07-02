//go:build linux

package harness

import (
	"log"

	"golang.org/x/sys/unix"
)

const capNetAdmin = 12

// DropNetAdminCapability removes CAP_NET_ADMIN from effective, permitted, and
// inheritable sets on the current process so child processes (e.g. the Python
// worker) cannot flush or modify iptables/ip6tables rules.
func DropNetAdminCapability() {
	hdr := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&hdr, &data[0]); err != nil {
		log.Printf("harness: drop CAP_NET_ADMIN skipped (capget: %v)", err)
		return
	}
	mask := uint32(1 << capNetAdmin)
	for i := range data {
		data[i].Effective &^= mask
		data[i].Permitted &^= mask
		data[i].Inheritable &^= mask
	}
	if err := unix.Capset(&hdr, &data[0]); err != nil {
		log.Printf("harness: drop CAP_NET_ADMIN failed (capset: %v); agent child may retain NET_ADMIN — use init-container firewall pattern for P2", err)
		return
	}
	log.Printf("harness: dropped CAP_NET_ADMIN from process capability sets")
}