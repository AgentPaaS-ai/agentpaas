package daemon

import (
	"net"
	"os"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
)

// Equivalent spellings of founder ports 7717 and 7718 must be refused before
// listen. net.Listen accepts leading zeros and a space before the port token.
// This test resolves those spellings and calls the guard. It does not bind,
// dial, or stop anything on 7717 or 7718.
func TestRefuseDefaultTriggerListen_EquivalentPortSpellings(t *testing.T) {
	cases := []struct {
		name string
		addr string
		port int
	}{
		{name: "padded grpc", addr: "127.0.0.1:07718", port: 7718},
		{name: "double padded grpc", addr: "127.0.0.1:007718", port: 7718},
		{name: "wildcard padded grpc", addr: "0.0.0.0:07718", port: 7718},
		{name: "padded rest", addr: "127.0.0.1:07717", port: 7717},
		{name: "double padded rest", addr: "127.0.0.1:007717", port: 7717},
		{name: "space before grpc port", addr: "127.0.0.1: 7718", port: 7718},
		{name: "space before rest port", addr: "127.0.0.1: 7717", port: 7717},
		{name: "ipv6 padded grpc", addr: "[::1]:07718", port: 7718},
		{name: "ipv6 space before grpc port", addr: "[::1]: 7718", port: 7718},
		{name: "exact grpc", addr: "127.0.0.1:7718", port: 7718},
		{name: "exact rest", addr: "127.0.0.1:7717", port: 7717},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tcp, err := net.ResolveTCPAddr("tcp", tc.addr)
			if err != nil {
				t.Fatalf("ResolveTCPAddr(%q): %v", tc.addr, err)
			}
			if tcp.Port != tc.port {
				t.Fatalf("ResolveTCPAddr(%q).Port = %d, want %d", tc.addr, tcp.Port, tc.port)
			}
			if !isDefaultTriggerListen(tc.addr, tc.port) {
				t.Fatalf("isDefaultTriggerListen(%q, %d) = false, want true", tc.addr, tc.port)
			}
			// A founder port on either listen address must be refused, including
			// when the gRPC address uses 7717 or the REST address uses 7718.
			if err := refuseDefaultTriggerListen(tc.addr, "127.0.0.1:9"); err == nil {
				t.Fatalf("refuseDefaultTriggerListen(%q, non-default) = nil, want refusal", tc.addr)
			}
			if err := refuseDefaultTriggerListen("127.0.0.1:9", tc.addr); err == nil {
				t.Fatalf("refuseDefaultTriggerListen(non-default, %q) = nil, want refusal", tc.addr)
			}
		})
	}

	if !isDefaultTriggerListen("", trigger.DefaultGRPCPort) {
		t.Fatal("empty address should be treated as a default listen")
	}
	if isDefaultTriggerListen("127.0.0.1:0", trigger.DefaultGRPCPort) {
		t.Fatal("ephemeral port 0 treated as a founder port")
	}
	if isDefaultTriggerListen("127.0.0.1:08080", trigger.DefaultGRPCPort) {
		t.Fatal("padded non-founder port treated as a founder port")
	}
	if isDefaultTriggerListen("127.0.0.1: 8081", trigger.DefaultRESTPort) {
		t.Fatal("space-padded non-founder port treated as a founder port")
	}
	if err := refuseDefaultTriggerListen("127.0.0.1:08080", "127.0.0.1:08081"); err != nil {
		t.Fatalf("refuseDefaultTriggerListen(non-default padded) = %v, want nil", err)
	}
}

func TestUseEphemeralTriggerAddrs_RewritesEquivalentSpellings(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", "127.0.0.1:07718")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "127.0.0.1: 7717")
	useEphemeralTriggerAddrs(t)
	if got := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR"); got != "127.0.0.1:0" {
		t.Fatalf("grpc env = %q, want 127.0.0.1:0", got)
	}
	if got := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR"); got != "127.0.0.1:0" {
		t.Fatalf("rest env = %q, want 127.0.0.1:0", got)
	}
}

func TestUseEphemeralTriggerAddrs_KeepsNonDefaultSpellings(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", "127.0.0.1:08080")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "127.0.0.1: 8081")
	useEphemeralTriggerAddrs(t)
	if got := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR"); got != "127.0.0.1:08080" {
		t.Fatalf("grpc env = %q, want the non-default spelling left alone", got)
	}
	if got := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR"); got != "127.0.0.1: 8081" {
		t.Fatalf("rest env = %q, want the non-default spelling left alone", got)
	}
}
