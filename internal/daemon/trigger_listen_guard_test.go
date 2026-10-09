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

// dns:/// and passthrough:/// targets are what grpc.NewClient dials. ResolveTCPAddr
// rejects those strings, so a parse error must not mean the port is not 7717 or
// 7718. This test does not bind, dial, or stop anything on 7717 or 7718.
func TestRefuseDefaultTriggerListen_SchemeDialTargets(t *testing.T) {
	cases := []struct {
		name string
		addr string
		port int
	}{
		{name: "dns grpc", addr: "dns:///127.0.0.1:7718", port: 7718},
		{name: "passthrough grpc", addr: "passthrough:///127.0.0.1:7718", port: 7718},
		{name: "dns rest", addr: "dns:///127.0.0.1:7717", port: 7717},
		{name: "passthrough rest", addr: "passthrough:///127.0.0.1:7717", port: 7717},
		// The endpoint grpc.NewClient dials still has the listen-side spellings.
		{name: "dns padded grpc", addr: "dns:///127.0.0.1:07718", port: 7718},
		{name: "passthrough space before rest port", addr: "passthrough:///127.0.0.1: 7717", port: 7717},
		{name: "dns ipv6 grpc", addr: "dns:///[::1]:7718", port: 7718},
		{name: "passthrough opaque grpc", addr: "passthrough:127.0.0.1:7718", port: 7718},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := net.ResolveTCPAddr("tcp", tc.addr); err == nil {
				t.Fatalf("ResolveTCPAddr(%q) succeeded; scheme targets are the miss", tc.addr)
			}
			// dialTriggerGRPC always passes DefaultGRPCPort. A 7717 scheme
			// target must still be refused before grpc.NewClient.
			if !isDefaultTriggerListen(tc.addr, trigger.DefaultGRPCPort) {
				t.Fatalf("isDefaultTriggerListen(%q, %d) = false, want true", tc.addr, trigger.DefaultGRPCPort)
			}
			if !isDefaultTriggerListen(tc.addr, tc.port) {
				t.Fatalf("isDefaultTriggerListen(%q, %d) = false, want true", tc.addr, tc.port)
			}
			if err := refuseDefaultTriggerListen(tc.addr, "127.0.0.1:9"); err == nil {
				t.Fatalf("refuseDefaultTriggerListen(%q, non-default) = nil, want refusal", tc.addr)
			}
			if err := refuseDefaultTriggerListen("127.0.0.1:9", tc.addr); err == nil {
				t.Fatalf("refuseDefaultTriggerListen(non-default, %q) = nil, want refusal", tc.addr)
			}
		})
	}

	if isDefaultTriggerListen("dns:///127.0.0.1:9", trigger.DefaultGRPCPort) {
		t.Fatal("dns non-founder port treated as a founder port")
	}
	if isDefaultTriggerListen("passthrough:///127.0.0.1:8080", trigger.DefaultRESTPort) {
		t.Fatal("passthrough non-founder port treated as a founder port")
	}
	if isDefaultTriggerListen("dns:///127.0.0.1:0", trigger.DefaultGRPCPort) {
		t.Fatal("dns ephemeral port treated as a founder port")
	}
	if isDefaultTriggerListen("dns:///127.0.0.1:08080", trigger.DefaultGRPCPort) {
		t.Fatal("dns padded non-founder port treated as a founder port")
	}
	if isDefaultTriggerListen("dns:///127.0.0.1:65536", trigger.DefaultGRPCPort) {
		t.Fatal("dns port 65536 treated as a founder port")
	}
	if isDefaultTriggerListen("passthrough:///127.0.0.1:65536", trigger.DefaultRESTPort) {
		t.Fatal("passthrough port 65536 treated as a founder port")
	}
	// Two-slash form is the authority, not the dial endpoint. grpc.NewClient
	// does not connect. A bare endpoint token is a hostname, not port 7718.
	if isDefaultTriggerListen("dns://127.0.0.1:7718", trigger.DefaultGRPCPort) {
		t.Fatal("dns authority form treated as a dial endpoint")
	}
	if isDefaultTriggerListen("dns:///7718", trigger.DefaultGRPCPort) {
		t.Fatal("dns bare token treated as founder port 7718")
	}
	if isDefaultTriggerListen("unix:///127.0.0.1:7718", trigger.DefaultGRPCPort) {
		t.Fatal("unix target treated as a founder TCP port")
	}
}

func TestUseEphemeralTriggerAddrs_RewritesSchemeDialTargets(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", "dns:///127.0.0.1:7718")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "passthrough:///127.0.0.1:7717")
	useEphemeralTriggerAddrs(t)
	if got := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR"); got != "127.0.0.1:0" {
		t.Fatalf("grpc env = %q, want 127.0.0.1:0", got)
	}
	if got := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR"); got != "127.0.0.1:0" {
		t.Fatalf("rest env = %q, want 127.0.0.1:0", got)
	}
}

func TestUseEphemeralTriggerAddrs_KeepsNonFounderSchemeTargets(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", "dns:///127.0.0.1:9")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "passthrough:///127.0.0.1:65536")
	useEphemeralTriggerAddrs(t)
	if got := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR"); got != "dns:///127.0.0.1:9" {
		t.Fatalf("grpc env = %q, want the non-founder scheme target left alone", got)
	}
	if got := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR"); got != "passthrough:///127.0.0.1:65536" {
		t.Fatalf("rest env = %q, want port 65536 left alone, not rewritten to 0", got)
	}
}

// dns://authority/name dials the authority when the name is not an IP.
// The endpoint is not a founder port, so classifying only the endpoint
// misses the dial. This test does not bind, dial, or stop anything on
// 7717 or 7718.
func TestRefuseDefaultTriggerListen_DNSAuthorityHostnameEndpoint(t *testing.T) {
	cases := []struct {
		name string
		addr string
		port int
	}{
		{name: "ipv4 grpc", addr: "dns://127.0.0.1:7718/not-an-ip.invalid", port: 7718},
		{name: "ipv4 rest", addr: "dns://127.0.0.1:7717/not-an-ip.invalid", port: 7717},
		{name: "localhost grpc", addr: "dns://localhost:7718/not-an-ip.invalid", port: 7718},
		{name: "empty host grpc", addr: "dns://:7718/not-an-ip.invalid", port: 7718},
		{name: "padded grpc", addr: "dns://127.0.0.1:07718/not-an-ip.invalid", port: 7718},
		{name: "ipv6 grpc", addr: "dns://[::1]:7718/not-an-ip.invalid", port: 7718},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := net.ResolveTCPAddr("tcp", tc.addr); err == nil {
				t.Fatalf("ResolveTCPAddr(%q) succeeded; the authority is the miss", tc.addr)
			}
			if !isDefaultTriggerListen(tc.addr, trigger.DefaultGRPCPort) {
				t.Fatalf("isDefaultTriggerListen(%q, %d) = false, want true", tc.addr, trigger.DefaultGRPCPort)
			}
			if !isDefaultTriggerListen(tc.addr, tc.port) {
				t.Fatalf("isDefaultTriggerListen(%q, %d) = false, want true", tc.addr, tc.port)
			}
			if err := refuseDefaultTriggerListen(tc.addr, "127.0.0.1:9"); err == nil {
				t.Fatalf("refuseDefaultTriggerListen(%q, non-default) = nil, want refusal", tc.addr)
			}
			if err := refuseDefaultTriggerListen("127.0.0.1:9", tc.addr); err == nil {
				t.Fatalf("refuseDefaultTriggerListen(non-default, %q) = nil, want refusal", tc.addr)
			}
		})
	}

	// An IP endpoint is dialed directly. The authority is not the dial.
	if isDefaultTriggerListen("dns://127.0.0.1:7718/127.0.0.1:9", trigger.DefaultGRPCPort) {
		t.Fatal("dns IP endpoint treated as an authority dial")
	}
	// A founder port on an IP endpoint is still the dial. Do not skip it
	// because the authority port is not 7717 or 7718.
	if !isDefaultTriggerListen("dns://127.0.0.1:53/127.0.0.1:7718", trigger.DefaultGRPCPort) {
		t.Fatal("dns IP endpoint on a founder port was allowed")
	}
	if isDefaultTriggerListen("dns://127.0.0.1:53/example.com", trigger.DefaultGRPCPort) {
		t.Fatal("non-founder dns authority treated as a founder port")
	}
	if isDefaultTriggerListen("dns://127.0.0.1/not-an-ip.invalid", trigger.DefaultGRPCPort) {
		t.Fatal("dns authority without a port treated as a founder port")
	}
	if isDefaultTriggerListen("passthrough://127.0.0.1:7718/not-an-ip.invalid", trigger.DefaultGRPCPort) {
		t.Fatal("passthrough authority treated as a dial")
	}
	// Empty endpoint, including a trailing slash, is not a dial. The
	// two-slash form stays allowed in TestRefuseDefaultTriggerListen_SchemeDialTargets.
	if isDefaultTriggerListen("dns://127.0.0.1:7718/", trigger.DefaultGRPCPort) {
		t.Fatal("dns empty endpoint with a trailing slash treated as a dial")
	}
}

func TestUseEphemeralTriggerAddrs_RewritesDNSAuthorityHostnameEndpoint(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", "dns://127.0.0.1:7718/not-an-ip.invalid")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "dns://127.0.0.1:7717/not-an-ip.invalid")
	useEphemeralTriggerAddrs(t)
	if got := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR"); got != "127.0.0.1:0" {
		t.Fatalf("grpc env = %q, want 127.0.0.1:0", got)
	}
	if got := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR"); got != "127.0.0.1:0" {
		t.Fatalf("rest env = %q, want 127.0.0.1:0", got)
	}
}

func TestUseEphemeralTriggerAddrs_KeepsNonFounderDNSAuthority(t *testing.T) {
	t.Setenv("AGENTPAAS_TRIGGER_GRPC_ADDR", "dns://127.0.0.1:53/example.com")
	t.Setenv("AGENTPAAS_TRIGGER_REST_ADDR", "dns://127.0.0.1:9/not-an-ip.invalid")
	useEphemeralTriggerAddrs(t)
	if got := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR"); got != "dns://127.0.0.1:53/example.com" {
		t.Fatalf("grpc env = %q, want the non-founder authority left alone", got)
	}
	if got := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR"); got != "dns://127.0.0.1:9/not-an-ip.invalid" {
		t.Fatalf("rest env = %q, want the non-founder authority left alone", got)
	}
}
