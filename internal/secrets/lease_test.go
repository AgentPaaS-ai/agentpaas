package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/policy"
)

const leaseSecretValue = "direct-lease-secret-value"

func TestDirectLeaseCreatesFileOnlyLeaseAndAudits(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "file_lease")

	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	data, err := os.ReadFile(handle.FilePath)
	if err != nil {
		t.Fatalf("ReadFile lease: %v", err)
	}
	if string(data) != leaseSecretValue {
		t.Fatalf("lease file content = %q, want secret value", data)
	}
	info, err := os.Stat(handle.FilePath)
	if err != nil {
		t.Fatalf("Stat lease: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o400 {
		t.Fatalf("lease file mode = %o, want 0400", got)
	}

	rec := flow.audit.last(t)
	if rec.EventType != audit.EventTypeSecretLeased {
		t.Fatalf("event type = %q, want %q", rec.EventType, audit.EventTypeSecretLeased)
	}
	assertLeaseAuditPayload(t, rec, "run-active", "egress[0]", "api-token", true)
	if rec.Timestamp == "" {
		t.Fatal("audit timestamp is empty")
	}
	assertAuditDoesNotContainSecret(t, rec)
}

func TestDirectLeaseRequiresFileLeasePolicyReason(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "brokered")

	_, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err == nil {
		t.Fatal("Lease succeeded for brokered credential, want error")
	}
	if !strings.Contains(err.Error(), "file_lease") {
		t.Fatalf("Lease error = %v, want file_lease opt-in", err)
	}
	if strings.Contains(err.Error(), leaseSecretValue) {
		t.Fatalf("Lease error leaked secret: %v", err)
	}
}

func TestReadLeaseReadsFileAndAuditsSecretRead(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "file_lease")
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	data, err := ReadLease(ctx, handle)
	if err != nil {
		t.Fatalf("ReadLease: %v", err)
	}
	if string(data) != leaseSecretValue {
		t.Fatalf("ReadLease data = %q, want secret value", data)
	}

	records := flow.audit.recordsSnapshot()
	if len(records) != 2 {
		t.Fatalf("audit records = %d, want 2", len(records))
	}
	rec := records[1]
	if rec.EventType != audit.EventTypeSecretRead {
		t.Fatalf("event type = %q, want %q", rec.EventType, audit.EventTypeSecretRead)
	}
	assertLeaseAuditPayload(t, rec, "run-active", "egress[0]", "api-token", true)
	assertAuditDoesNotContainSecret(t, rec)
}

func TestLeaseCleanupRemovesFile(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "file_lease")
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}

	if err := handle.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	_, err = os.Stat(handle.FilePath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat after Cleanup error = %v, want not exist", err)
	}
}

func TestReadLeaseRejectsNonLeaseFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.txt")
	if err := os.WriteFile(path, []byte("not a lease"), 0o600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}

	_, err := ReadLease(ctx, LeaseHandle{FilePath: path})
	if err == nil {
		t.Fatal("ReadLease succeeded for non-lease file, want error")
	}
	if !strings.Contains(err.Error(), "valid lease") {
		t.Fatalf("ReadLease error = %v, want valid lease error", err)
	}
}

func TestDirectLeaseRejectsEnvLeaseType(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "env_lease")

	_, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err == nil {
		t.Fatal("Lease succeeded for env_lease, want error")
	}
	if !strings.Contains(err.Error(), "env_lease not supported in P1") {
		t.Fatalf("Lease error = %v, want P1 env_lease denial", err)
	}
}

func TestDirectLeaseSecretAbsentBeforeRuntimeMount(t *testing.T) {
	p := newDirectLeasePolicy("file_lease")
	p.Credentials[0].Value = leaseSecretValue

	gatewayConfig, err := policy.CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("CompileGatewayConfig: %v", err)
	}
	credentialRules, err := policy.CompileCredentialRules(p)
	if err != nil {
		t.Fatalf("CompileCredentialRules: %v", err)
	}
	if bytes.Contains(gatewayConfig, []byte(leaseSecretValue)) {
		t.Fatalf("gateway config contains raw secret value: %s", gatewayConfig)
	}
	if bytes.Contains(credentialRules, []byte(leaseSecretValue)) {
		t.Fatalf("credential rules contain raw secret value: %s", credentialRules)
	}
}

func TestLeaseHandleRedactsStringAndJSON(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "file_lease")
	handle, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err != nil {
		t.Fatalf("Lease: %v", err)
	}
	defer func() { _ = handle.Cleanup() }()

	data, err := json.Marshal(handle)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	rendered := strings.Join([]string{
		string(data),
		handle.String(),
	}, "\n")
	if strings.Contains(rendered, leaseSecretValue) {
		t.Fatalf("LeaseHandle rendering leaked secret: %q", rendered)
	}
	if strings.Contains(rendered, handle.FilePath) {
		t.Fatalf("LeaseHandle rendering leaked file path: %q", rendered)
	}
	if !strings.Contains(rendered, "[REDACTED]") {
		t.Fatalf("LeaseHandle rendering missing redaction marker: %q", rendered)
	}
}

func TestDirectLeaseRejectsSymlinkLeasePath(t *testing.T) {
	ctx := context.Background()
	flow := newDirectLeaseFlow(t, "file_lease")
	runDir := filepath.Join(flow.dir, "run-active")
	if err := os.Mkdir(runDir, 0o700); err != nil {
		t.Fatalf("Mkdir run dir: %v", err)
	}
	target := filepath.Join(flow.dir, "target")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatalf("WriteFile target: %v", err)
	}
	leasePath := filepath.Join(runDir, "api-token")
	if err := os.Symlink(target, leasePath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	_, err := flow.lease.Lease(ctx, "run-active", "api-token", "egress[0]")
	if err == nil {
		t.Fatal("Lease succeeded over symlink path, want error")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Lease error = %v, want symlink rejection", err)
	}
	info, lstatErr := os.Lstat(leasePath)
	if lstatErr != nil {
		t.Fatalf("Lstat lease path: %v", lstatErr)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("lease path mode = %v, want symlink still present", info.Mode())
	}
}

type directLeaseFlow struct {
	lease *DirectLease
	audit *recordingAuditSink
	dir   string
}

func newDirectLeaseFlow(t *testing.T, credentialType string) directLeaseFlow {
	t.Helper()
	ctx := context.Background()
	store := NewFakeKeyStore()
	if err := store.Set(ctx, "api-token", []byte(leaseSecretValue)); err != nil {
		t.Fatalf("Set secret: %v", err)
	}
	auditSink := &recordingAuditSink{}
	dir := t.TempDir()
	lease, err := NewDirectLease(DirectLeaseConfig{
		Store:      store,
		Policy:     newDirectLeasePolicy(credentialType),
		ActiveRuns: []string{"run-active"},
		Audit:      auditSink,
		LeaseDir:   dir,
		Now: func() time.Time {
			return time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewDirectLease: %v", err)
	}
	return directLeaseFlow{lease: lease, audit: auditSink, dir: dir}
}

func newDirectLeasePolicy(credentialType string) *policy.Policy {
	return &policy.Policy{
		Version: "1.0",
		Agent:   policy.AgentConfig{Name: "test-agent"},
		Egress: []policy.EgressRule{
			{Domain: "api.example.com", Ports: []int{443}, Credential: "api-token"},
		},
		Credentials: []policy.Credential{
			{ID: "api-token", Type: credentialType, Service: "test-store", Reason: "legacy file API"},
		},
	}
}

func (r *recordingAuditSink) recordsSnapshot() []audit.AuditRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.AuditRecord(nil), r.records...)
}

func assertLeaseAuditPayload(t *testing.T, rec audit.AuditRecord, runID, policyRuleID, credentialID string, visible bool) {
	t.Helper()
	if rec.Payload["run_id"] != runID {
		t.Fatalf("run_id = %v, want %s", rec.Payload["run_id"], runID)
	}
	if rec.Payload["policy_rule_id"] != policyRuleID {
		t.Fatalf("policy_rule_id = %v, want %s", rec.Payload["policy_rule_id"], policyRuleID)
	}
	if rec.Payload["credential_id"] != credentialID {
		t.Fatalf("credential_id = %v, want %s", rec.Payload["credential_id"], credentialID)
	}
	if rec.Payload["visible_to_agent"] != visible {
		t.Fatalf("visible_to_agent = %v, want %v", rec.Payload["visible_to_agent"], visible)
	}
}

func assertAuditDoesNotContainSecret(t *testing.T, rec audit.AuditRecord) {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal audit record: %v", err)
	}
	if bytes.Contains(data, []byte(leaseSecretValue)) {
		t.Fatalf("audit record leaked secret value: %s", data)
	}
}
