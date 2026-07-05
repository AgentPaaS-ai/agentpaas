package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

type fakeHandoffAudit struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (f *fakeHandoffAudit) Append(record audit.AuditRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, record)
	return nil
}

func (f *fakeHandoffAudit) record(t *testing.T, index int) audit.AuditRecord {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.records) <= index {
		t.Fatalf("record %d missing, got %d records", index, len(f.records))
	}
	return f.records[index]
}

func TestHandoff_NoApprovedConfig(t *testing.T) {
	auditSink := &fakeHandoffAudit{}
	hm := NewHandoffManager(nil, auditSink)

	result, err := hm.Trigger(context.Background(), &HandoffRequest{
		SourceAgent:   "source",
		ParentRunID:   "run-parent",
		CorrelationID: "corr-1",
	})
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
	if result.Invoked {
		t.Fatal("expected handoff to be denied")
	}
	if result.Reason != "no_approved_config" {
		t.Fatalf("reason = %q, want no_approved_config", result.Reason)
	}

	record := auditSink.record(t, 0)
	if record.EventType != eventHandoffDenied {
		t.Fatalf("event type = %q, want %q", record.EventType, eventHandoffDenied)
	}
	if record.Actor != "system:handoff:source" {
		t.Fatalf("actor = %q", record.Actor)
	}
	if record.Payload["reason"] != "no_approved_config" {
		t.Fatalf("reason payload = %v", record.Payload["reason"])
	}
}

func TestHandoff_EmptyPayload(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent: "source",
		TargetAgent: "target",
		PayloadMode: PayloadModeEmpty,
	})

	var got *triggerv1.InvokeRequest
	hm.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		got = req
		return &triggerv1.InvokeResponse{}, nil
	}

	result, err := hm.Trigger(context.Background(), newTestHandoffRequest())
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
	if !result.Invoked {
		t.Fatalf("expected invoked result, reason %q", result.Reason)
	}
	if got == nil {
		t.Fatal("invoke was not called")
	}
	if got.Payload != nil {
		t.Fatalf("payload = %v, want nil", got.Payload)
	}
	if got.ContentType != "" {
		t.Fatalf("content type = %q, want empty", got.ContentType)
	}
}

func TestHandoff_SummaryRef(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent: "source",
		TargetAgent: "target",
		PayloadMode: PayloadModeSummaryRef,
	})

	var got *triggerv1.InvokeRequest
	hm.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		got = req
		return &triggerv1.InvokeResponse{}, nil
	}

	_, err := hm.Trigger(context.Background(), &HandoffRequest{
		SourceAgent:   "source",
		ParentRunID:   "run-parent",
		CorrelationID: "corr-1",
		SummaryRef:    "agentpaas://summary/s1",
	})
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["summary_ref"] != "agentpaas://summary/s1" {
		t.Fatalf("summary_ref = %q", payload["summary_ref"])
	}
	if payload["correlation"] != "corr-1" {
		t.Fatalf("correlation = %q", payload["correlation"])
	}
	if payload["parent_run"] != "run-parent" {
		t.Fatalf("parent_run = %q", payload["parent_run"])
	}
	if got.ContentType != "application/json" {
		t.Fatalf("content type = %q", got.ContentType)
	}
}

func TestHandoff_ArtifactRef(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent: "source",
		TargetAgent: "target",
		PayloadMode: PayloadModeArtifactRef,
	})

	var got *triggerv1.InvokeRequest
	hm.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		got = req
		return &triggerv1.InvokeResponse{}, nil
	}

	_, err := hm.Trigger(context.Background(), &HandoffRequest{
		SourceAgent:   "source",
		ParentRunID:   "run-parent",
		CorrelationID: "corr-1",
		ArtifactRefs: []ArtifactRef{{
			URI:    "agentpaas://artifact/a1",
			Digest: "sha256:abc",
			Size:   42,
		}},
	})
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}

	var payload struct {
		ArtifactRefs []ArtifactRef `json:"artifact_refs"`
		Correlation  string        `json:"correlation"`
		ParentRun    string        `json:"parent_run"`
	}
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ArtifactRefs) != 1 {
		t.Fatalf("artifact refs len = %d", len(payload.ArtifactRefs))
	}
	if payload.ArtifactRefs[0].URI != "agentpaas://artifact/a1" {
		t.Fatalf("uri = %q", payload.ArtifactRefs[0].URI)
	}
	if payload.ArtifactRefs[0].Digest != "sha256:abc" {
		t.Fatalf("digest = %q", payload.ArtifactRefs[0].Digest)
	}
	if payload.ArtifactRefs[0].Size != 42 {
		t.Fatalf("size = %d", payload.ArtifactRefs[0].Size)
	}
	if payload.Correlation != "corr-1" {
		t.Fatalf("correlation = %q", payload.Correlation)
	}
	if payload.ParentRun != "run-parent" {
		t.Fatalf("parent_run = %q", payload.ParentRun)
	}
}

func TestHandoff_FixedJSON(t *testing.T) {
	fixed := json.RawMessage(`{"mode":"fixed","ok":true}`)
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent: "source",
		TargetAgent: "target",
		PayloadMode: PayloadModeFixedJSON,
		FixedJSON:   fixed,
	})

	var got *triggerv1.InvokeRequest
	hm.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		got = req
		return &triggerv1.InvokeResponse{}, nil
	}

	_, err := hm.Trigger(context.Background(), newTestHandoffRequest())
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
	if string(got.Payload) != string(fixed) {
		t.Fatalf("payload = %s, want %s", string(got.Payload), string(fixed))
	}
	if got.ContentType != "application/json" {
		t.Fatalf("content type = %q", got.ContentType)
	}
}

func TestHandoff_MaxDepthExceeded(t *testing.T) {
	auditSink := &fakeHandoffAudit{}
	hm := NewHandoffManager([]*HandoffConfig{{
		SourceAgent: "source",
		TargetAgent: "target",
		MaxDepth:    1,
	}}, auditSink)
	hm.activeChains["corr-1"] = 1

	result, err := hm.Trigger(context.Background(), newTestHandoffRequest())
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
	if result.Invoked {
		t.Fatal("expected handoff to be denied")
	}
	if result.Reason != "max_depth_exceeded" {
		t.Fatalf("reason = %q", result.Reason)
	}

	record := auditSink.record(t, 0)
	if record.EventType != eventHandoffDenied {
		t.Fatalf("event type = %q, want %q", record.EventType, eventHandoffDenied)
	}
	if record.Payload["reason"] != "max_depth_exceeded" {
		t.Fatalf("reason payload = %v", record.Payload["reason"])
	}
}

func TestHandoff_CallerID(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent: "source",
		TargetAgent: "target",
	})

	hm.invoke = func(ctx context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		caller, ok := CallerFromContext(ctx)
		if !ok {
			t.Fatal("caller missing from context")
		}
		if caller != "system:handoff:source" {
			t.Fatalf("caller = %q", caller)
		}
		return &triggerv1.InvokeResponse{}, nil
	}

	if _, err := hm.Trigger(context.Background(), newTestHandoffRequest()); err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
}

func TestHandoff_IdempotencyKey(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent:          "source",
		TargetAgent:          "target",
		IdempotencyKeyPrefix: "handoff",
	})

	hm.invoke = func(_ context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		if req.IdempotencyKey != "handoff:corr-1" {
			t.Fatalf("idempotency key = %q", req.IdempotencyKey)
		}
		return &triggerv1.InvokeResponse{}, nil
	}

	if _, err := hm.Trigger(context.Background(), newTestHandoffRequest()); err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
}

func TestHandoff_EnvelopeRoundTrip(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent:      "source",
		TargetAgent:      "target",
		TargetLockDigest: "sha256:lock",
	})

	req := &HandoffRequest{
		SourceAgent:   "source",
		ParentRunID:   "run-parent",
		ContextID:     "ctx-1",
		CorrelationID: "corr-1",
		ArtifactRefs: []ArtifactRef{{
			URI:    "agentpaas://artifact/a1",
			Digest: "sha256:abc",
			Size:   42,
		}},
		Metadata: map[string]string{"k": "v"},
	}

	result, err := hm.Trigger(context.Background(), req)
	if err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}
	envelope := result.Envelope
	if envelope.SourceAgentCard != "agentpaas://agent/source" {
		t.Fatalf("source card = %q", envelope.SourceAgentCard)
	}
	if envelope.TargetAgentCard != "agentpaas://agent/target" {
		t.Fatalf("target card = %q", envelope.TargetAgentCard)
	}
	if envelope.TargetLockDigest != "sha256:lock" {
		t.Fatalf("target lock digest = %q", envelope.TargetLockDigest)
	}
	if envelope.ParentRunID != "run-parent" || envelope.ParentTaskID != "run-parent" {
		t.Fatalf("parent fields = %q/%q", envelope.ParentRunID, envelope.ParentTaskID)
	}
	if envelope.ContextID != "ctx-1" {
		t.Fatalf("context id = %q", envelope.ContextID)
	}
	if envelope.CorrelationID != "corr-1" {
		t.Fatalf("correlation id = %q", envelope.CorrelationID)
	}
	if len(envelope.Parts) != 1 || envelope.Parts[0].Type != "text" {
		t.Fatalf("parts = %#v", envelope.Parts)
	}
	if len(envelope.ArtifactRefs) != 1 || envelope.ArtifactRefs[0].URI != "agentpaas://artifact/a1" {
		t.Fatalf("artifact refs = %#v", envelope.ArtifactRefs)
	}
	if envelope.Metadata["k"] != "v" {
		t.Fatalf("metadata k = %q", envelope.Metadata["k"])
	}
	if envelope.Metadata["target_lock_digest"] != "sha256:lock" {
		t.Fatalf("metadata target lock digest = %q", envelope.Metadata["target_lock_digest"])
	}

	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var roundTrip A2AEnvelope
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if roundTrip.SourceAgentCard != envelope.SourceAgentCard {
		t.Fatalf("round trip source card = %q", roundTrip.SourceAgentCard)
	}
}

func TestHandoff_CycleGuard(t *testing.T) {
	hm := newTestHandoffManager(t, &HandoffConfig{
		SourceAgent: "source",
		TargetAgent: "target",
		MaxDepth:    2,
	})

	hm.invoke = func(_ context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		hm.mu.Lock()
		defer hm.mu.Unlock()
		if hm.activeChains["corr-1"] != 1 {
			t.Fatalf("active chain depth = %d", hm.activeChains["corr-1"])
		}
		if !hm.activeTargets["target"] {
			t.Fatal("target was not marked active")
		}
		return &triggerv1.InvokeResponse{}, nil
	}

	if _, err := hm.Trigger(context.Background(), newTestHandoffRequest()); err != nil {
		t.Fatalf("Trigger returned error: %v", err)
	}

	hm.mu.Lock()
	depth := hm.activeChains["corr-1"]
	active := hm.activeTargets["target"]
	hm.mu.Unlock()
	if depth != 0 {
		t.Fatalf("active chain depth after trigger = %d", depth)
	}
	if active {
		t.Fatal("target remained active after trigger")
	}

	hm.invoke = func(_ context.Context, _ *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		return nil, errors.New("invoke failed")
	}
	result, err := hm.Trigger(context.Background(), newTestHandoffRequest())
	if err == nil {
		t.Fatal("expected invoke error")
	}
	if result.Invoked {
		t.Fatal("expected skipped result")
	}

	hm.mu.Lock()
	depth = hm.activeChains["corr-1"]
	active = hm.activeTargets["target"]
	hm.mu.Unlock()
	if depth != 0 {
		t.Fatalf("active chain depth after error = %d", depth)
	}
	if active {
		t.Fatal("target remained active after error")
	}
}

func newTestHandoffManager(t *testing.T, cfg *HandoffConfig) *HandoffManager {
	t.Helper()
	hm := NewHandoffManager([]*HandoffConfig{cfg}, &fakeHandoffAudit{})
	hm.invoke = func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		return &triggerv1.InvokeResponse{}, nil
	}
	return hm
}

func newTestHandoffRequest() *HandoffRequest {
	return &HandoffRequest{
		SourceAgent:   "source",
		ParentRunID:   "run-parent",
		CorrelationID: "corr-1",
	}
}
