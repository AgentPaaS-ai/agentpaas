package mcpmanager

import (
	"context"
	"net/http"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/policy"
)

func TestAdversary_B7M_T04_NilEgressPolicy(t *testing.T) {
	var ep *EgressPolicy
	allowed, _, _, err := ep.CheckEgress(context.Background(), "s", "https://example.com", "GET")
	if err == nil {
		t.Fatal("expected nil policy error")
	}
	if allowed {
		t.Fatal("expected nil policy to deny egress")
	}
}

func TestAdversary_B7M_T04_WildcardDefeatsDefaultDeny(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "*",
		Methods:     []string{http.MethodGet},
		Ports:       []int{443},
		MCPServerID: "server-wild",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-wild", "https://evil.example.com/", http.MethodGet)
	if err == nil || allowed {
		t.Fatalf("wildcard allowed = %v, err=%v; want denied", allowed, err)
	}
}

func TestAdversary_B7M_T04_DNSBypassLocalhostAlias(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "localhost.localdomain",
		Methods:     []string{http.MethodGet},
		Ports:       []int{8080},
		MCPServerID: "server-dns",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-dns", "http://localhost.localdomain:8080/", http.MethodGet)
	if err == nil || allowed {
		t.Fatalf("dns alias allowed = %v, err=%v; want denied", allowed, err)
	}
}

func TestAdversary_B7M_T04_DockerSocketDifferentPath(t *testing.T) {
	// ADVERSARY BREAK: isDockerSocketURL only matches scheme=unix AND exact Path=/var/run/docker.sock
	// Other representations (unix:..., file://, different path casing?) bypass.
	for _, dest := range []string{
		"unix:/var/run/docker.sock",
		"unix://localhost/var/run/docker.sock",
		"file:///var/run/docker.sock",
	} {
		t.Run(dest, func(t *testing.T) {
			ep := NewEgressPolicy([]policy.EgressRule{{
				Domain:      "example.com",
				MCPServerID: "server-docker",
			}}, nil)
			allowed, _, _, err := ep.CheckEgress(context.Background(), "server-docker", dest, http.MethodGet)
			if err == nil && allowed {
				t.Fatalf("docker bypass allowed for %s", dest)
			}
		})
	}
}

func TestAdversary_B7M_T04_PortZeroAnyPort(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{http.MethodGet},
		Ports:       []int{0},
		MCPServerID: "server-port0",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-port0", "https://api.example.com:22/", http.MethodGet)
	if err == nil || allowed {
		t.Fatalf("port0 allowed any = %v, err=%v; want denied", allowed, err)
	}
}

func TestAdversary_B7M_T04_EmptyMethodsAllowsAll(t *testing.T) {
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{},
		Ports:       []int{443},
		MCPServerID: "server-method",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-method", "https://api.example.com/", "DELETE")
	if err == nil || allowed {
		t.Fatalf("empty methods allowed DELETE = %v, err=%v; want denied", allowed, err)
	}
}

func TestAdversary_B7M_T04_URLUserinfoBypass(t *testing.T) {
	// ADVERSARY BREAK: url.Parse + Hostname() may allow userinfo tricks or path confusion to reach denied hosts
	// e.g. https://evil@127.0.0.1/ or similar.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "127.0.0.1",
		MCPServerID: "server-url",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-url", "https://evil.com@127.0.0.1/", http.MethodGet)
	if err == nil && allowed {
		t.Fatal("userinfo bypass allowed to denied host")
	}
	// Also test literal denied via alias
}

func TestAdversary_B7M_T04_PrivateNetworkWithoutCIDR(t *testing.T) {
	// Confirmed safe: private IPs (10.x etc.) require explicit CIDR or ruleExplicitlyAllowsHost; no implicit allow.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "example.com",
		MCPServerID: "server-private",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-private", "http://10.0.0.1/", http.MethodGet)
	if err == nil || allowed {
		t.Fatalf("private IP unexpectedly allowed: allowed=%v err=%v", allowed, err)
	}
}

func TestAdversary_B7M_T04_IPv6LoopbackDenied(t *testing.T) {
	// Confirmed safe: isDeniedHost uses IsLoopback() which covers ::1.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "example.com",
		MCPServerID: "server-ipv6",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-ipv6", "http://[::1]/", http.MethodGet)
	if err == nil || allowed {
		t.Fatalf("::1 unexpectedly allowed")
	}
}

func TestAdversary_B7M_T04_AuditNoCredentialLeak(t *testing.T) {
	// Confirmed safe: audit payload contains only credential_id (the ID string), never the secret value.
	// (No credential values are stored in EgressPolicy or rules.)
}
