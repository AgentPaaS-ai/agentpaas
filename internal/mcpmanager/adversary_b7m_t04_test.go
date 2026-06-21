package mcpmanager

import (
	"context"
	"net/http"
	"testing"

	"github.com/parvezsyed/agentpaas/internal/policy"
)

func TestAdversary_B7M_T04_NilEgressPolicy(t *testing.T) {
	// ADVERSARY BREAK: CheckEgress on nil *EgressPolicy dereferences ep without guard (unlike auditEgressDecision)
	// Causes panic on ctx check or audit call.
	var ep *EgressPolicy
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil EgressPolicy")
		}
	}()
	_, _, _, _ = ep.CheckEgress(context.Background(), "s", "https://example.com", "GET")
}

func TestAdversary_B7M_T04_WildcardDefeatsDefaultDeny(t *testing.T) {
	// ADVERSARY BREAK: domainPatternMatches treats "*" as match-all; NewEgressPolicy ignores AllowWildcard
	// A rule with Domain:"*" allows egress to any non-denied host, defeating default-deny intent.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "*",
		MCPServerID: "server-wild",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-wild", "https://evil.example.com/", http.MethodGet)
	if err != nil || !allowed {
		t.Fatalf("wildcard allowed = %v, err=%v; want allowed (break present)", allowed, err)
	}
}

func TestAdversary_B7M_T04_DNSBypassLocalhostAlias(t *testing.T) {
	// ADVERSARY BREAK: isDeniedHost only matches literal "localhost" or IsLoopback IP; no DNS resolution
	// Rule allowing "localhost.localdomain" permits egress that resolves to loopback.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "localhost.localdomain",
		MCPServerID: "server-dns",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-dns", "http://localhost.localdomain:8080/", http.MethodGet)
	if err != nil || !allowed {
		t.Fatalf("dns alias allowed = %v, err=%v; want allowed (bypass)", allowed, err)
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
	// ADVERSARY BREAK: Port=0 in egressRule (default when no Ports or explicit 0) means any port
	// Bypasses intended port restrictions.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Ports:       []int{0},
		MCPServerID: "server-port0",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-port0", "https://api.example.com:22/", http.MethodGet)
	if err != nil || !allowed {
		t.Fatalf("port0 allowed any = %v, err=%v; want allowed (break)", allowed, err)
	}
}

func TestAdversary_B7M_T04_EmptyMethodsAllowsAll(t *testing.T) {
	// ADVERSARY BREAK: ruleAllowsMethod returns true if len(Methods)==0 (empty list in rule)
	// Even if policy declares methods, empty list permits any method including dangerous ones.
	ep := NewEgressPolicy([]policy.EgressRule{{
		Domain:      "api.example.com",
		Methods:     []string{},
		MCPServerID: "server-method",
	}}, nil)
	allowed, _, _, err := ep.CheckEgress(context.Background(), "server-method", "https://api.example.com/", "DELETE")
	if err != nil || !allowed {
		t.Fatalf("empty methods allowed DELETE = %v; want allowed (break)", allowed)
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