//go:build adversary

package trigger

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

type adversaryFakeAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (f *adversaryFakeAudit) Append(record audit.AuditRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, record)
	return nil
}

func (f *adversaryFakeAudit) recordCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.records)
}

func (f *adversaryFakeAudit) lastRecord() audit.AuditRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.records) == 0 {
		return audit.AuditRecord{}
	}
	return f.records[len(f.records)-1]
}

func TestAdversaryB9T07_UnapprovedSourceDenial(t *testing.T) {
	auditSink := &adversaryFakeAudit{}
	hm := NewHandoffManager([]*HandoffConfig{{SourceAgent: "approved", TargetAgent: "target"}}, auditSink)

	result, err := hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "evil-agent", ParentRunID: "run-1", CorrelationID: "corr-1"})
	if err != nil {
		t.Fatalf("Trigger error: %v", err)
	}
	if result.Invoked {
		t.Errorf("SECURITY BREAK: unapproved source invoked target")
		t.Fail()
	}
	if result.Reason != "no_approved_config" {
		t.Fatalf("reason=%q", result.Reason)
	}
	rec := auditSink.lastRecord()
	if rec.EventType != eventHandoffDenied || rec.Actor != "system:handoff:evil-agent" || rec.Payload["reason"] != "no_approved_config" {
		t.Errorf("SECURITY BREAK: denied audit missing or wrong actor/event for unapproved source")
		t.Fail()
	}
	t.Logf("Tested: unapproved source denial produces handoff_denied audit (good)")
}

func TestAdversaryB9T07_MaxDepthEnforcement(t *testing.T) {
	auditSink := &adversaryFakeAudit{}
	cfg := &HandoffConfig{SourceAgent: "source", TargetAgent: "target", MaxDepth: 5}
	hm := NewHandoffManager([]*HandoffConfig{cfg}, auditSink)

	// Pre-set depth to MaxDepth
	hm.mu.Lock()
	hm.activeChains["corr-1"] = 5
	hm.mu.Unlock()

	result, err := hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source", ParentRunID: "run-1", CorrelationID: "corr-1"})
	if err != nil {
		t.Fatalf("Trigger error: %v", err)
	}
	if result.Invoked || result.Reason != "max_depth_exceeded" {
		t.Errorf("SECURITY BREAK: depth=5 with Max=5 not denied")
		t.Fail()
	}
	rec := auditSink.lastRecord()
	if rec.EventType != eventHandoffDenied || rec.Payload["reason"] != "max_depth_exceeded" {
		t.Errorf("SECURITY BREAK: max_depth audit missing/wrong")
		t.Fail()
	}

	// Negative MaxDepth (config allows negative since only ==0 is defaulted)
	cfgNeg := &HandoffConfig{SourceAgent: "source2", TargetAgent: "target", MaxDepth: -1}
	hm2 := NewHandoffManager([]*HandoffConfig{cfgNeg}, &adversaryFakeAudit{})
	hm2.mu.Lock()
	hm2.activeChains["corr-2"] = 0
	hm2.mu.Unlock()
	result2, _ := hm2.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source2", CorrelationID: "corr-2"})
	if result2.Invoked {
		t.Errorf("SECURITY BREAK: negative MaxDepth allowed invocation")
		t.Fail()
	}

	// Huge depth
	cfgHuge := &HandoffConfig{SourceAgent: "source3", TargetAgent: "target", MaxDepth: 999999}
	hm3 := NewHandoffManager([]*HandoffConfig{cfgHuge}, &adversaryFakeAudit{})
	hm3.mu.Lock()
	hm3.activeChains["corr-3"] = 999999
	hm3.mu.Unlock()
	result3, _ := hm3.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source3", CorrelationID: "corr-3"})
	if result3.Invoked {
		t.Errorf("SECURITY BREAK: huge depth allowed")
		t.Fail()
	}

	t.Logf("Tested: max depth enforcement including negative/huge (good)")
}

func TestAdversaryB9T07_CallerIDInjection(t *testing.T) {
	hm := NewHandoffManager([]*HandoffConfig{
		{SourceAgent: "source", TargetAgent: "target"},
		{SourceAgent: "api_key:admin", TargetAgent: "target"},
		{SourceAgent: "system:cron:evil", TargetAgent: "target"},
	}, &adversaryFakeAudit{})

	var gotCaller CallerID
	hm.invoke = func(ctx context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		if caller, ok := CallerFromContext(ctx); ok {
			gotCaller = caller
		}
		return &triggerv1.InvokeResponse{}, nil
	}

	// Attack: source name designed to inject different caller semantics
	req := &HandoffRequest{SourceAgent: "api_key:admin", CorrelationID: "corr-1"}
	_, err := hm.Trigger(context.Background(), req)
	if err != nil {
		t.Fatalf("Trigger error: %v", err)
	}
	expected := CallerID("system:handoff:api_key:admin")
	if gotCaller != expected {
		t.Errorf("SECURITY BREAK: caller ID injection via source name failed to produce expected %q, got %q", expected, gotCaller)
		t.Fail()
	}

	// Another injection attempt
	req2 := &HandoffRequest{SourceAgent: "system:cron:evil", CorrelationID: "corr-2"}
	hm.invoke = func(ctx context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		if caller, ok := CallerFromContext(ctx); ok {
			gotCaller = caller
		}
		return &triggerv1.InvokeResponse{}, nil
	}
	_, _ = hm.Trigger(context.Background(), req2)
	expected2 := CallerID("system:handoff:system:cron:evil")
	if gotCaller != expected2 {
		t.Errorf("SECURITY BREAK: caller ID with system:cron prefix not set correctly, got %q", gotCaller)
		t.Fail()
	}

	t.Logf("Tested: caller ID injection attempts via malicious source names (good)")
}

func TestAdversaryB9T07_PayloadModeValidation(t *testing.T) {
	auditSink := &adversaryFakeAudit{}

	// FixedJSON empty
	cfgFixed := &HandoffConfig{SourceAgent: "source", TargetAgent: "target", PayloadMode: PayloadModeFixedJSON, FixedJSON: nil}
	hm := NewHandoffManager([]*HandoffConfig{cfgFixed}, auditSink)
	_, err := hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source", CorrelationID: "corr-1"})
	if err == nil || err.Error() != "fixed_json mode requires FixedJSON" {
		t.Errorf("SECURITY BREAK: empty FixedJSON did not error properly: %v", err)
		t.Fail()
	}
	rec := auditSink.lastRecord()
	if rec.EventType != eventHandoffSkipped || rec.Payload["reason"] != "payload_build_error" {
		t.Errorf("SECURITY BREAK: payload error not audited as skipped")
		t.Fail()
	}

	// ArtifactRef with nil refs
	cfgArt := &HandoffConfig{SourceAgent: "source2", TargetAgent: "target", PayloadMode: PayloadModeArtifactRef}
	hm2 := newAdversaryTestManager(t, cfgArt)
	var gotPayload []byte
	hm2.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		gotPayload = req.Payload
		return &triggerv1.InvokeResponse{}, nil
	}
	_, err = hm2.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source2", CorrelationID: "corr-2", ArtifactRefs: nil})
	if err != nil {
		t.Fatalf("nil artifact trigger error: %v", err)
	}
	// Should produce valid JSON with empty array, no panic
	var p map[string]interface{}
	if json.Unmarshal(gotPayload, &p) != nil || p["artifact_refs"] == nil {
		t.Errorf("SECURITY BREAK: nil ArtifactRefs caused bad payload")
		t.Fail()
	}

	// Empty summary
	cfgSum := &HandoffConfig{SourceAgent: "source3", TargetAgent: "target", PayloadMode: PayloadModeSummaryRef}
	hm3 := newAdversaryTestManager(t, cfgSum)
	hm3.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		gotPayload = req.Payload
		return &triggerv1.InvokeResponse{}, nil
	}
	_, err = hm3.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source3", CorrelationID: "corr-3", SummaryRef: ""})
	if err != nil {
		t.Fatalf("empty summary error: %v", err)
	}
	var p2 map[string]interface{}
	json.Unmarshal(gotPayload, &p2)
	if p2["summary_ref"] != "" {
		t.Errorf("SECURITY BREAK: empty summary not handled")
		t.Fail()
	}

	t.Logf("Tested: payload mode validation for empty/nil cases (good)")
}

func TestAdversaryB9T07_IdempotencyKeyFormat(t *testing.T) {
	hm := newAdversaryTestManager(t, &HandoffConfig{SourceAgent: "source", TargetAgent: "target", IdempotencyKeyPrefix: ""})
	var gotKey string
	hm.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		gotKey = req.IdempotencyKey
		return &triggerv1.InvokeResponse{}, nil
	}
	_, _ = hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source", CorrelationID: "corr-1"})
	if gotKey != "" {
		t.Errorf("SECURITY BREAK: empty prefix produced non-empty key %q", gotKey)
		t.Fail()
	}

	hm2 := newAdversaryTestManager(t, &HandoffConfig{SourceAgent: "source2", TargetAgent: "target", IdempotencyKeyPrefix: "bad:prefix"})
	hm2.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		gotKey = req.IdempotencyKey
		return &triggerv1.InvokeResponse{}, nil
	}
	_, _ = hm2.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source2", CorrelationID: "corr:with:colon"})
	expected := "bad:prefix:corr:with:colon"
	if gotKey != expected {
		t.Errorf("SECURITY BREAK: idempotency key format with special chars/colons wrong: %q", gotKey)
		t.Fail()
	}

	t.Logf("Tested: idempotency key format with empty/special/corr-colons (good)")
}

func TestAdversaryB9T07_EnvelopeIntegrity(t *testing.T) {
	hm := newAdversaryTestManager(t, &HandoffConfig{
		SourceAgent:      "source",
		TargetAgent:      "target",
		TargetLockDigest: "sha256:lock",
	})
	req := &HandoffRequest{
		SourceAgent:   "source",
		ParentRunID:   "run-parent",
		ContextID:     "ctx-1",
		CorrelationID: "corr-1",
		ArtifactRefs:  []ArtifactRef{{URI: "a://1", Digest: "d1", Size: 10}},
		Metadata:      map[string]string{"k": "v"},
	}
	result, err := hm.Trigger(context.Background(), req)
	if err != nil {
		t.Fatalf("Trigger error: %v", err)
	}
	env := result.Envelope
	if env == nil ||
		env.SourceAgentCard == "" || env.TargetAgentCard == "" ||
		env.ParentTaskID == "" || env.ParentRunID == "" ||
		env.ContextID == "" || env.CorrelationID == "" ||
		env.Parts == nil || len(env.Parts) == 0 ||
		env.ArtifactRefs == nil || env.Metadata == nil {
		t.Errorf("SECURITY BREAK: envelope missing required fields: %+v", env)
		t.Fail()
	}
	// All fields populated even if some values derived
	if env.MessageRole != "assistant" || env.TargetLockDigest != "sha256:lock" {
		t.Errorf("SECURITY BREAK: envelope core fields not set")
		t.Fail()
	}

	t.Logf("Tested: A2A envelope integrity all fields populated (good)")
}

func TestAdversaryB9T07_ConcurrencyRace(t *testing.T) {
	auditSink := &adversaryFakeAudit{}
	cfg := &HandoffConfig{SourceAgent: "source", TargetAgent: "target", ConcurrencyPolicy: HandoffConcurrencyAllow, MaxDepth: 10}
	hm := NewHandoffManager([]*HandoffConfig{cfg}, auditSink)

	var wg sync.WaitGroup
	invokeCount := 0
	var mu sync.Mutex
	hm.invoke = func(_ context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		mu.Lock()
		invokeCount++
		mu.Unlock()
		return &triggerv1.InvokeResponse{}, nil
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source", CorrelationID: "corr-race", ParentRunID: "r"})
		}()
	}
	wg.Wait()

	if invokeCount < 1 {
		t.Errorf("SECURITY BREAK: concurrent handoffs did not invoke")
		t.Fail()
	}
	// Depth tracking under race should not corrupt (no panic, final depth reasonable)
	hm.mu.Lock()
	finalDepth := hm.activeChains["corr-race"]
	hm.mu.Unlock()
	if finalDepth < 0 {
		t.Errorf("SECURITY BREAK: race caused negative depth %d", finalDepth)
		t.Fail()
	}

	t.Logf("Tested: concurrent handoffs race with -race (good)")
}

func TestAdversaryB9T07_AuditCompleteness(t *testing.T) {
	auditSink := &adversaryFakeAudit{}
	hm := NewHandoffManager([]*HandoffConfig{{SourceAgent: "source", TargetAgent: "target"}}, auditSink)

	// Denied case
	hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "bad", CorrelationID: "c1"})
	if auditSink.recordCount() < 1 || auditSink.lastRecord().EventType != eventHandoffDenied || auditSink.lastRecord().Actor != "system:handoff:bad" {
		t.Errorf("SECURITY BREAK: denied case missing audit or wrong actor")
		t.Fail()
	}

	// Invoked case (reset sink conceptually by new hm, but reuse count check)
	hm2 := newAdversaryTestManager(t, &HandoffConfig{SourceAgent: "source2", TargetAgent: "target"})
	auditSink2 := &adversaryFakeAudit{}
	hm2.audit = auditSink2 // replace
	_, _ = hm2.Trigger(context.Background(), &HandoffRequest{SourceAgent: "source2", CorrelationID: "c2"})
	if auditSink2.recordCount() != 1 || auditSink2.lastRecord().EventType != eventHandoffInvoked || auditSink2.lastRecord().Actor != "system:handoff:source2" {
		t.Errorf("SECURITY BREAK: invoked case missing/wrong audit")
		t.Fail()
	}

	t.Logf("Tested: audit completeness for denied/invoked/skipped (good)")
}

func TestAdversaryB9T07_CycleGuard(t *testing.T) {
	auditSink := &adversaryFakeAudit{}
	cfgA := &HandoffConfig{SourceAgent: "a", TargetAgent: "b", MaxDepth: 3}
	cfgB := &HandoffConfig{SourceAgent: "b", TargetAgent: "a", MaxDepth: 3}
	hm := NewHandoffManager([]*HandoffConfig{cfgA, cfgB}, auditSink)

	// Simulate chain by pre-setting depth
	hm.mu.Lock()
	hm.activeChains["corr-cycle"] = 3
	hm.mu.Unlock()

	result, _ := hm.Trigger(context.Background(), &HandoffRequest{SourceAgent: "a", CorrelationID: "corr-cycle"})
	if result.Invoked {
		t.Errorf("SECURITY BREAK: cycle depth exceeded not denied")
		t.Fail()
	}
	rec := auditSink.lastRecord()
	if rec.EventType != eventHandoffDenied || rec.Payload["reason"] != "max_depth_exceeded" {
		t.Errorf("SECURITY BREAK: cycle guard audit wrong")
		t.Fail()
	}

	t.Logf("Tested: cycle guard with correlation/depth (good)")
}

func newAdversaryTestManager(t *testing.T, cfg *HandoffConfig) *HandoffManager {
	t.Helper()
	hm := NewHandoffManager([]*HandoffConfig{cfg}, &adversaryFakeAudit{})
	hm.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		return &triggerv1.InvokeResponse{}, nil
	}
	return hm
}
