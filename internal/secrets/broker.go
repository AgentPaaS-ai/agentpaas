package secrets

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

type AuditAppender interface {
	Append(record audit.AuditRecord) error
}

type BrokerConfig struct {
	Store              SecretStore
	Policy             *policy.Policy
	ActiveRuns         []string
	ActiveDirectLeases map[string][]string
	RuleMethods        map[string][]string
	Audit              AuditAppender
	Now                func() time.Time
	CredentialResolver CredentialResolver
	InstallRef         string
}

// Broker is the sole credential access path for agent workloads. SecretStore
// instances must not be passed to agent code or any package outside the secrets
// broker; direct store.Get calls bypass revocation checks.
type Broker struct {
	store              SecretStore
	policy             *policy.Policy
	mu                 sync.RWMutex
	revoked            map[string]bool
	activeRuns         map[string]struct{}
	activeDirectLeases map[string]map[string]struct{}
	ruleMethods        map[string]map[string]struct{}
	audit              AuditAppender
	now                func() time.Time
	resolver           CredentialResolver
	installRef         string
}

type CredentialInjection struct {
	HeaderName  string `json:"-"`
	HeaderValue string `json:"-"`
}

func (c CredentialInjection) String() string {
	return fmt.Sprintf("CredentialInjection{HeaderName:%q HeaderValue:[REDACTED]}", c.HeaderName)
}

func (c CredentialInjection) GoString() string {
	return c.String()
}

func (c CredentialInjection) Format(s fmt.State, _ rune) { // intentionally ignored (reviewed)
	_, _ = fmt.Fprint(s, c.String()) // best-effort write
}

func NewBroker(cfg BrokerConfig) (*Broker, error) {
	if cfg.Store == nil {
		return nil, errors.New("secrets broker requires a secret store")
	}
	if cfg.Policy == nil {
		return nil, errors.New("secrets broker requires a policy")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	activeRuns := make(map[string]struct{}, len(cfg.ActiveRuns))
	for _, runID := range cfg.ActiveRuns {
		activeRuns[runID] = struct{}{}
	}

	activeDirectLeases := make(map[string]map[string]struct{}, len(cfg.ActiveDirectLeases))
	for credentialID, runIDs := range cfg.ActiveDirectLeases {
		runs := make(map[string]struct{}, len(runIDs))
		for _, runID := range runIDs {
			runs[runID] = struct{}{}
		}
		activeDirectLeases[credentialID] = runs
	}

	ruleMethods := make(map[string]map[string]struct{}, len(cfg.RuleMethods))
	for ruleID, methods := range cfg.RuleMethods {
		allowed := make(map[string]struct{}, len(methods))
		for _, method := range methods {
			allowed[strings.ToUpper(method)] = struct{}{}
		}
		ruleMethods[ruleID] = allowed
	}

	return &Broker{
		store:              cfg.Store,
		policy:             cfg.Policy,
		revoked:            make(map[string]bool),
		activeRuns:         activeRuns,
		activeDirectLeases: activeDirectLeases,
		ruleMethods:        ruleMethods,
		audit:              cfg.Audit,
		now:                now,
		resolver:           cfg.CredentialResolver,
		installRef:         strings.TrimSpace(cfg.InstallRef),
	}, nil
}

func (b *Broker) RequestCredential(ctx context.Context, runID, policyRuleID, destination, method string) (CredentialInjection, error) {
	dest, err := parseDestination(destination)
	if err != nil {
		credentialID := b.credentialIDForRule(policyRuleID)
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, destination, method, "parse destination: %w", err)
	}

	rule, ruleIndex, err := b.egressRule(policyRuleID)
	if err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, "", dest.String(), method, "%w", err)
	}
	credentialID := rule.Credential

	if err := b.validateActiveRun(runID); err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "%w", err)
	}
	if credentialID == "" {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "policy rule %s does not reference a brokered credential", policyRuleID)
	}
	if b.IsRevoked(credentialID) {
		return CredentialInjection{}, b.denyWithReason(ctx, runID, policyRuleID, credentialID, dest.String(), method, "revoked", "credential %s is revoked", credentialID)
	}
	credential, err := b.credential(credentialID)
	if err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "%w", err)
	}
	if credential.Type != "brokered" {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "credential %s is not brokered", credentialID)
	}
	if err := validateRuleDestination(rule, dest); err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "%w", err)
	}
	if err := b.validateCredentialMethod(policyRuleID, method); err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "%w", err)
	}

	headerName := credential.Header
	if headerName == "" {
		headerName = "Authorization"
	}
	if err := validateHeaderName(headerName); err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "%w", err)
	}

	lookupName := credentialID
	if b.resolver != nil {
		local, ok := b.resolver.Resolve(credentialID)
		if !ok {
			ref := b.installRef
			if ref == "" {
				ref = "<ref>"
			}
			msg := fmt.Sprintf(
				"map credential %s: `agentpaas installed map-credential %s %s=<local>`",
				credentialID, ref, credentialID,
			)
			return CredentialInjection{}, b.denyWithReason(
				ctx, runID, policyRuleID, credentialID, dest.String(), method, "unmapped",
				"%w: %s", ErrCredentialUnmapped, msg,
			)
		}
		lookupName = local
	}

	value, err := b.store.Get(ctx, lookupName)
	if err != nil {
		msg := "brokered credential " + credentialID + " is referenced by " + ruleID(ruleIndex) + " but is not set in the secret store"
		if errors.Is(err, ErrSecretNotFound) {
			return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "%w: %s", ErrSecretNotFound, msg)
		}
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "load brokered credential %s: %w", credentialID, err)
	}
	if err := b.store.TouchLastUsed(ctx, lookupName); err != nil {
		return CredentialInjection{}, b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "touch brokered credential %s: %w", credentialID, err)
	}

	if err := b.auditSecret(ctx, "injected", runID, policyRuleID, credentialID, dest.String(), method); err != nil {
		return CredentialInjection{}, fmt.Errorf("broker request credential: %w", err)
	}
	return CredentialInjection{HeaderName: headerName, HeaderValue: string(value)}, nil
}

func (b *Broker) Revoke(_ context.Context, credentialID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.revoked[credentialID] = true
	return nil
}

func (b *Broker) IsRevoked(credentialID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.revoked[credentialID]
}

// RestartAffectedAgents identifies active runs with direct leases for a revoked
// credential. Direct-lease revocation cannot claw back a secret value already
// visible to agent code; the daemon must decide how to restart affected runs.
func (b *Broker) RestartAffectedAgents(_ context.Context, credentialID string) ([]string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	leases := b.activeDirectLeases[credentialID]
	if len(leases) == 0 {
		return nil, nil
	}
	runIDs := make([]string, 0, len(leases))
	for runID := range leases {
		if _, ok := b.activeRuns[runID]; ok {
			runIDs = append(runIDs, runID)
		}
	}
	sort.Strings(runIDs)
	return runIDs, nil
}

func (b *Broker) ValidateEgress(ctx context.Context, runID, destination, method string) error {
	dest, err := parseDestination(destination)
	if err != nil {
		return fmt.Errorf("parse destination: %w", err)
	}
	if err := b.validateActiveRun(runID); err != nil {
		return fmt.Errorf("broker validate egress: %w", err)
	}
	for i, rule := range b.policy.Egress {
		if rule.Credential != "" {
			continue
		}
		if err := validateRuleDestination(rule, dest); err != nil {
			continue
		}
		if err := b.validateOptionalMethod(ruleID(i), method); err != nil {
			continue
		}
		return nil
	}
	return fmt.Errorf("destination %s method %s is not allowed by policy", dest.String(), strings.ToUpper(method))
}

func (b *Broker) DenyCredentialedRedirect(ctx context.Context, runID, policyRuleID, destination, method string) error {
	dest, err := parseDestination(destination)
	if err != nil {
		return fmt.Errorf("parse redirect destination: %w", err)
	}
	credentialID := b.credentialIDForRule(policyRuleID)
	return b.deny(ctx, runID, policyRuleID, credentialID, dest.String(), method, "credentialed redirect denied before injection to %s", dest.String())
}

func (b *Broker) validateActiveRun(runID string) error {
	if runID == "" {
		return errors.New("run id is required")
	}
	if _, ok := b.activeRuns[runID]; !ok {
		return fmt.Errorf("run id %s is not an active run", runID)
	}
	return nil
}

func (b *Broker) egressRule(policyRuleID string) (policy.EgressRule, int, error) {
	index, err := parseRuleID(policyRuleID)
	if err != nil {
		return policy.EgressRule{}, 0, fmt.Errorf("broker egress rule: %w", err)
	}
	if index < 0 || index >= len(b.policy.Egress) {
		return policy.EgressRule{}, 0, fmt.Errorf("policy rule %s does not exist", policyRuleID)
	}
	return b.policy.Egress[index], index, nil
}

func (b *Broker) credential(id string) (policy.Credential, error) {
	for _, credential := range b.policy.Credentials {
		if credential.ID == id {
			return credential, nil
		}
	}
	return policy.Credential{}, fmt.Errorf("policy rule references undeclared credential %s", id)
}

func (b *Broker) credentialIDForRule(policyRuleID string) string {
	rule, _, err := b.egressRule(policyRuleID) // intentionally ignored (reviewed)
	if err != nil {
		return ""
	}
	return rule.Credential
}

func (b *Broker) validateCredentialMethod(policyRuleID, method string) error {
	allowed, ok := b.ruleMethods[policyRuleID]
	if !ok || len(allowed) == 0 {
		return fmt.Errorf("policy rule %s has no allowed methods configured", policyRuleID)
	}
	_, ok = allowed[strings.ToUpper(method)]
	if !ok {
		return fmt.Errorf("method %s is not allowed for policy rule %s", strings.ToUpper(method), policyRuleID)
	}
	return nil
}

func (b *Broker) validateOptionalMethod(policyRuleID, method string) error {
	allowed, ok := b.ruleMethods[policyRuleID]
	if !ok || len(allowed) == 0 {
		return nil
	}
	_, ok = allowed[strings.ToUpper(method)]
	if !ok {
		return fmt.Errorf("method %s is not allowed for policy rule %s", strings.ToUpper(method), policyRuleID)
	}
	return nil
}

func (b *Broker) deny(ctx context.Context, runID, policyRuleID, credentialID, destination, method, format string, args ...interface{}) error {
	return b.denyWithReason(ctx, runID, policyRuleID, credentialID, destination, method, "", format, args...)
}

func (b *Broker) denyWithReason(ctx context.Context, runID, policyRuleID, credentialID, destination, method, reason, format string, args ...interface{}) error {
	err := fmt.Errorf(format, args...)
	if auditErr := b.auditSecret(ctx, "denied", runID, policyRuleID, credentialID, destination, method, reason); auditErr != nil {
		return auditErr
	}
	return err
}

func (b *Broker) auditSecret(_ context.Context, status, runID, policyRuleID, credentialID, destination, method string, reason ...string) error {
	if b.audit == nil {
		return nil
	}
	payload := map[string]interface{}{
		"status":           status,
		"run_id":           runID,
		"policy_rule_id":   policyRuleID,
		"credential_id":    credentialID,
		"destination":      destination,
		"method":           strings.ToUpper(method),
		"visible_to_agent": false,
	}
	if b.policy.Agent.Name != "" {
		payload["agent_id"] = b.policy.Agent.Name
	}
	if len(reason) > 0 && reason[0] != "" {
		payload["reason"] = reason[0]
	}
	return b.audit.Append(audit.AuditRecord{
		Timestamp:      b.now().UTC().Format(time.RFC3339),
		EventType:      audit.EventTypeSecretInjected,
		DeploymentMode: "local",
		Actor:          "secrets-broker",
		Payload:        payload,
	})
}

type brokerDestination struct {
	domain string
	port   int
}

func (d brokerDestination) String() string {
	return net.JoinHostPort(d.domain, strconv.Itoa(d.port))
}

func parseDestination(raw string) (brokerDestination, error) {
	if strings.TrimSpace(raw) == "" {
		return brokerDestination{}, errors.New("destination is required")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return brokerDestination{}, fmt.Errorf("parse destination: %w", err)
		}
		host := parsed.Hostname()
		if host == "" {
			return brokerDestination{}, errors.New("destination host is required")
		}
		port, err := destinationPort(parsed)
		if err != nil {
			return brokerDestination{}, fmt.Errorf("parse destination: %w", err)
		}
		return brokerDestination{domain: normalizeBrokerDomain(host), port: port}, nil
	}

	host, portText, err := net.SplitHostPort(raw)
	if err != nil {
		return brokerDestination{}, fmt.Errorf("parse destination: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return brokerDestination{}, fmt.Errorf("destination port: %w", err)
	}
	return brokerDestination{domain: normalizeBrokerDomain(host), port: port}, nil
}

func destinationPort(u *url.URL) (int, error) {
	if portText := u.Port(); portText != "" {
		port, err := strconv.Atoi(portText)
		if err != nil {
			return 0, fmt.Errorf("destination port: %w", err)
		}
		return port, nil
	}
	switch u.Scheme {
	case "https":
		return 443, nil
	case "http":
		return 80, nil
	default:
		return 0, fmt.Errorf("destination scheme %q does not imply a port", u.Scheme)
	}
}

func validateRuleDestination(rule policy.EgressRule, dest brokerDestination) error {
	ruleDomain := normalizeBrokerDomain(rule.Domain)
	if ruleDomain == "" {
		return errors.New("policy rule has no domain destination")
	}
	if !domainMatches(ruleDomain, dest.domain, rule.AllowWildcard != nil && *rule.AllowWildcard) {
		return fmt.Errorf("destination domain %s does not match policy domain %s", dest.domain, ruleDomain)
	}
	for _, port := range rule.Ports {
		if port == dest.port {
			return nil
		}
	}
	return fmt.Errorf("destination port %d is not allowed by policy domain %s", dest.port, ruleDomain)
}

func validateHeaderName(name string) error {
	if name == "" {
		return errors.New("credential header name is required")
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch < 0x20 || ch == 0x7f {
			return fmt.Errorf("credential header name %q contains a control character", name)
		}
		if !isHeaderTokenChar(ch) {
			return fmt.Errorf("credential header name %q is not a valid RFC 7230 token", name)
		}
	}
	return nil
}

func isHeaderTokenChar(ch byte) bool {
	if ch >= 'a' && ch <= 'z' {
		return true
	}
	if ch >= 'A' && ch <= 'Z' {
		return true
	}
	if ch >= '0' && ch <= '9' {
		return true
	}
	switch ch {
	case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
		return true
	default:
		return false
	}
}

func domainMatches(ruleDomain, destination string, allowWildcard bool) bool {
	if ruleDomain == destination {
		return true
	}
	if !allowWildcard || !strings.HasPrefix(ruleDomain, "*.") {
		return false
	}
	suffix := strings.TrimPrefix(ruleDomain, "*.")
	return strings.HasSuffix(destination, "."+suffix)
}

func normalizeBrokerDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.Trim(domain, "[]"), "."))
}

func parseRuleID(policyRuleID string) (int, error) {
	if !strings.HasPrefix(policyRuleID, "egress[") || !strings.HasSuffix(policyRuleID, "]") {
		return 0, fmt.Errorf("policy rule id %s must use egress[index] format", policyRuleID)
	}
	indexText := strings.TrimSuffix(strings.TrimPrefix(policyRuleID, "egress["), "]")
	index, err := strconv.Atoi(indexText)
	if err != nil {
		return 0, fmt.Errorf("policy rule id %s has invalid index: %w", policyRuleID, err)
	}
	return index, nil
}

func ruleID(index int) string {
	return fmt.Sprintf("egress[%d]", index)
}
