package mcpmanager

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const eventTypeMCPEgressDecision = "mcp_egress_decision"

// EgressPolicy enforces default-deny outbound network access for MCP servers.
// MCP servers are separate managed workloads; outbound HTTP requests must be
// gateway-mediated and policy-checked.
type EgressPolicy struct {
	mu    sync.RWMutex
	rules map[string][]egressRule // serverID -> rules
	audit audit.AuditAppender
}

type egressRule struct {
	Destination  string   // domain or host:port pattern
	Methods      []string // allowed HTTP methods (empty = none)
	Port         int      // allowed port (0 = none)
	CredentialID string   // brokered credential ID (empty = no cred)
	PolicyRuleID string   // policy rule identifier for auditing
	CIDR         string   // optional IP network
}

// NewEgressPolicy creates an EgressPolicy from declared policy egress rules.
// Rules come from policy.EgressRule entries that reference MCP server IDs.
func NewEgressPolicy(rules []policy.EgressRule, appender audit.AuditAppender) *EgressPolicy {
	ep := &EgressPolicy{
		rules: make(map[string][]egressRule),
		audit: appender,
	}
	for i, rule := range rules {
		if rule.MCPServerID == "" {
			continue
		}
		methods := normalizeMethods(rule.Methods)
		policyRuleID := fmt.Sprintf("egress[%d]", i)
		if len(rule.Ports) == 0 {
			ep.rules[rule.MCPServerID] = append(ep.rules[rule.MCPServerID], egressRule{
				Destination:  rule.Domain,
				Methods:      methods,
				CredentialID: rule.Credential,
				PolicyRuleID: policyRuleID,
				CIDR:         rule.CIDR,
			})
			continue
		}
		for _, port := range rule.Ports {
			ep.rules[rule.MCPServerID] = append(ep.rules[rule.MCPServerID], egressRule{
				Destination:  rule.Domain,
				Methods:      methods,
				Port:         port,
				CredentialID: rule.Credential,
				PolicyRuleID: policyRuleID,
				CIDR:         rule.CIDR,
			})
		}
	}
	return ep
}

// CheckEgress validates whether an MCP server may make an outbound request to
// the given destination with the given method.
func (ep *EgressPolicy) CheckEgress(ctx context.Context, serverID, destination, method string) (bool, string, string, error) {
	if ep == nil {
		return false, "", "", errors.New("egress policy is not configured (default-deny)")
	}
	if err := ctx.Err(); err != nil {
		ep.auditEgressDecision(serverID, destination, method, "", "", "denied", err.Error())
		return false, "", "", err
	}

	normalizedMethod := normalizeMethod(method)
	parsed, err := url.Parse(destination)
	if err != nil {
		reason := fmt.Sprintf("parse destination: %v", err)
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("parse destination: %w", err)
	}
	if parsed.Scheme == "" {
		reason := "destination URL has no scheme"
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}
	if isDockerSocketURL(parsed) {
		reason := "docker socket access is denied"
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}
	if !isNetworkScheme(parsed.Scheme) {
		reason := fmt.Sprintf("unsupported egress scheme %q", parsed.Scheme)
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}

	host := parsed.Hostname()
	if host == "" {
		reason := "destination URL has no host"
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}
	if isDeniedHost(host) {
		reason := "host access is denied"
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}
	if isLinkLocalHost(host) {
		reason := "link-local access is denied"
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}

	port, err := destinationPort(parsed)
	if err != nil {
		reason := fmt.Sprintf("parse destination port: %v", err)
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("parse destination port: %w", err)
	}

	ep.mu.RLock()
	rules := append([]egressRule(nil), ep.rules[serverID]...)
	ep.mu.RUnlock()
	if len(rules) == 0 {
		reason := "no egress rules for MCP server"
		ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
		return false, "", "", fmt.Errorf("%s", reason)
	}

	privateHost := isPrivateHost(host)
	for _, rule := range rules {
		if strings.TrimSpace(rule.Destination) == "*" {
			continue
		}
		if rule.Port == 0 {
			continue
		}
		if len(rule.Methods) == 0 {
			continue
		}
		if privateHost && !ruleExplicitlyAllowsHost(rule, host) {
			continue
		}
		if !ruleMatchesDestination(rule, host, port) {
			continue
		}
		if !ruleAllowsMethod(rule, normalizedMethod) {
			continue
		}
		if rule.Port != port {
			continue
		}
		ep.auditEgressDecision(serverID, destination, normalizedMethod, rule.CredentialID, rule.PolicyRuleID, "allowed", "")
		return true, rule.CredentialID, rule.PolicyRuleID, nil
	}

	reason := "no matching egress rule"
	ep.auditEgressDecision(serverID, destination, normalizedMethod, "", "", "denied", reason)
	return false, "", "", fmt.Errorf("%s", reason)
}

// AuditEgress emits an audit event for an egress decision (allowed or denied).
func (ep *EgressPolicy) AuditEgress(serverID, destination, method, credentialID, policyRuleID, decision string) {
	ep.auditEgressDecision(serverID, destination, normalizeMethod(method), credentialID, policyRuleID, decision, "")
}

func (ep *EgressPolicy) auditEgressDecision(serverID, destination, method, credentialID, policyRuleID, decision, reason string) {
	if ep == nil || ep.audit == nil {
		return
	}
	_ = ep.audit.Append(audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		EventType:      eventTypeMCPEgressDecision,
		DeploymentMode: "local",
		Actor:          serverID,
		Payload: map[string]interface{}{
			"credential_id":  credentialID,
			"decision":       decision,
			"destination":    destination,
			"method":         method,
			"policy_rule_id": policyRuleID,
			"reason":         reason,
			"server_id":      serverID,
		},
	})
}

func normalizeMethods(methods []string) []string {
	normalized := make([]string, 0, len(methods))
	for _, method := range methods {
		if method == "" {
			continue
		}
		normalized = append(normalized, normalizeMethod(method))
	}
	return normalized
}

func normalizeMethod(method string) string {
	if method == "" {
		return http.MethodGet
	}
	return strings.ToUpper(method)
}

func isNetworkScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https", "ws", "wss":
		return true
	default:
		return false
	}
}

func isDockerSocketURL(parsed *url.URL) bool {
	return strings.EqualFold(parsed.Scheme, "unix") && parsed.Path == "/var/run/docker.sock"
}

func destinationPort(parsed *url.URL) (int, error) {
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil {
			return 0, err
		}
		return port, nil
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "wss":
		return 443, nil
	case "http", "ws":
		return 80, nil
	default:
		return 0, nil
	}
}

func ruleAllowsMethod(rule egressRule, method string) bool {
	if len(rule.Methods) == 0 {
		return false
	}
	for _, allowed := range rule.Methods {
		if allowed == method {
			return true
		}
	}
	return false
}

func ruleMatchesDestination(rule egressRule, host string, port int) bool {
	if rule.CIDR != "" && cidrContainsHost(rule.CIDR, host) {
		return true
	}
	return domainPatternMatches(rule.Destination, host, port)
}

func domainPatternMatches(pattern, host string, port int) bool {
	if pattern == "" {
		return false
	}
	normalizedPattern := strings.ToLower(strings.TrimSpace(pattern))
	normalizedHost := strings.ToLower(strings.TrimSuffix(host, "."))
	if normalizedPattern == "*" {
		return false
	}
	if patternHost, patternPort, ok := splitPatternHostPort(normalizedPattern); ok {
		return patternHost == normalizedHost && patternPort == port
	}
	if strings.HasPrefix(normalizedPattern, "*.") {
		suffix := strings.TrimPrefix(normalizedPattern, "*")
		return strings.HasSuffix(normalizedHost, suffix)
	}
	return strings.TrimSuffix(normalizedPattern, ".") == normalizedHost
}

func splitPatternHostPort(pattern string) (string, int, bool) {
	host, portText, err := net.SplitHostPort(pattern)
	if err != nil {
		host, portText, err = net.SplitHostPort("[" + pattern + "]")
		if err != nil {
			return "", 0, false
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return "", 0, false
	}
	return strings.ToLower(strings.Trim(host, "[]")), port, true
}

func ruleExplicitlyAllowsHost(rule egressRule, host string) bool {
	if rule.CIDR != "" && cidrContainsHost(rule.CIDR, host) {
		return true
	}
	if rule.Destination == "" || rule.Destination == "*" || strings.HasPrefix(rule.Destination, "*.") {
		return false
	}
	return domainPatternMatches(rule.Destination, host, 0) || domainPatternMatches(rule.Destination, host, 80) || domainPatternMatches(rule.Destination, host, 443)
}

func isDeniedHost(host string) bool {
	return isLocalhost(host)
}

func isLocalhost(host string) bool {
	normalizedHost := strings.ToLower(strings.Trim(strings.TrimSuffix(host, "."), "[]"))
	switch normalizedHost {
	case "localhost", "localhost.localdomain", "localhost4", "localhost6":
		return true
	}
	if addr, ok := parseHostAddr(normalizedHost); ok {
		return addr.Unmap().IsLoopback()
	}
	ips, err := net.LookupIP(normalizedHost)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip)
		if ok && addr.Unmap().IsLoopback() {
			return true
		}
	}
	return false
}

func isLinkLocalHost(host string) bool {
	addr, ok := parseHostAddr(host)
	if !ok {
		return false
	}
	return addr.IsLinkLocalUnicast()
}

func isPrivateHost(host string) bool {
	addr, ok := parseHostAddr(host)
	if !ok {
		return false
	}
	return addr.IsPrivate()
}

func cidrContainsHost(cidr, host string) bool {
	addr, ok := parseHostAddr(host)
	if !ok {
		return false
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	return prefix.Contains(addr)
}

func parseHostAddr(host string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr, true
}
