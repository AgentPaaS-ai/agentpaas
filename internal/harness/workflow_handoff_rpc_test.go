package harness

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/workflow/pipeline"
)

// --- helpers ---

func newTestRPCServer(t *testing.T) *harnessRPCServer {
	t.Helper()
	s, err := startHarnessRPCServer(&noOpHarnessAudit{})
	if err != nil {
		t.Fatalf("start harness rpc server: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// noOpHarnessAudit implements AuditAppender with no-op Append.
type noOpHarnessAudit struct{}

func (n *noOpHarnessAudit) Append(audit.AuditRecord) error { return nil }

func setTestInvoke(t *testing.T, s *harnessRPCServer) *rpcInvokeState {
	t.Helper()
	state := &rpcInvokeState{
		payload: map[string]any{},
	}
	s.mu.Lock()
	s.invoke = state
	s.mu.Unlock()
	return state
}

func setPipelineContext(s *harnessRPCServer, ctx PipelineStageContext) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.invoke != nil {
		s.invoke.pipelineCtx = &ctx
	}
}

func clearInvokeState(s *harnessRPCServer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invoke = nil
}

// --- workflow_input tests ---

func TestWorkflowInput_Stage0_NotAvailable(t *testing.T) {
	s := newTestRPCServer(t)
	setTestInvoke(t, s)

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:  "pipeline",
		NodeID:        "stage_research",
		StageOrder:    0,
		IsFinalStage:  false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "1",
		Method: "workflow_input",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if available, _ := result["available"].(bool); available {
		t.Fatal("expected available=false for stage 0")
	}
}

func TestWorkflowInput_MidStage_ReturnsEnvelope(t *testing.T) {
	s := newTestRPCServer(t)
	setTestInvoke(t, s)

	incomingJSON := json.RawMessage(`{"schema_version":"agentpaas.workflow.handoff/v1","workflow_id":"wf_1","handoff_id":"ho_1","from_node_id":"stage_a","to_node_id":"stage_b","producer_run_id":"run_1","producer_attempt_id":"att_1","producer_result_digest":"sha256:` + strings.Repeat("a", 64) + `","sequence":1,"created_at":"2026-07-16T00:00:00Z","classification":"internal","context":{"schema":"ns/test/v1","value":{"notes":"hello"}}}`)

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:       "pipeline",
		NodeID:             "stage_b",
		StageOrder:         1,
		IsFinalStage:       false,
		IncomingHandoffJSON: incomingJSON,
		LeaseExpiresAt:     time.Now().Add(time.Hour),
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "2",
		Method: "workflow_input",
	})
	if !resp.OK {
		t.Fatalf("expected ok, got error: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if available, _ := result["available"].(bool); !available {
		t.Fatal("expected available=true for mid-stage")
	}
	if result["handoff"] == nil {
		t.Fatal("expected handoff in result")
	}
}

func TestWorkflowInput_Standalone_Unavailable(t *testing.T) {
	s := newTestRPCServer(t)
	setTestInvoke(t, s)

	// No pipeline context at all — standalone/non-pipeline invoke.
	resp := s.handleRequest(rpcRequest{
		ID:     "3",
		Method: "workflow_input",
	})
	if resp.OK {
		t.Fatal("expected error for standalone workflow_input")
	}
	if resp.Code != "WORKFLOW_CONTEXT_UNAVAILABLE" {
		t.Fatalf("expected WORKFLOW_CONTEXT_UNAVAILABLE, got %s", resp.Code)
	}
}

func TestWorkflowInput_NoInvoke(t *testing.T) {
	s := newTestRPCServer(t)
	// No invoke set.
	resp := s.handleRequest(rpcRequest{
		ID:     "4",
		Method: "workflow_input",
	})
	if resp.OK {
		t.Fatal("expected error for no active invoke")
	}
	if resp.Code != "no_active_invoke" {
		t.Fatalf("expected no_active_invoke, got %s", resp.Code)
	}
}

func TestWorkflowInput_StaleLease(t *testing.T) {
	s := newTestRPCServer(t)
	setTestInvoke(t, s)

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_c",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(-time.Hour), // expired
		Terminal:       false,
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "5",
		Method: "workflow_input",
	})
	if resp.OK {
		t.Fatal("expected error for stale lease")
	}
	if resp.Code != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %s", resp.Code)
	}
}

func TestWorkflowInput_Terminal(t *testing.T) {
	s := newTestRPCServer(t)
	setTestInvoke(t, s)

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_d",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
		Terminal:       true,
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "6",
		Method: "workflow_input",
	})
	if resp.OK {
		t.Fatal("expected error for terminal invoke")
	}
	if resp.Code != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %s", resp.Code)
	}
}

// --- commit_handoff tests ---

func makeCommitHandoffReq(schema, context string, artifacts []map[string]any) rpcRequest {
	params := map[string]any{
		"schema":  schema,
		"context": json.RawMessage(context),
	}
	if artifacts != nil {
		params["artifacts"] = artifacts
	}
	return rpcRequest{
		ID:     "c1",
		Method: "commit_handoff",
		Params: params,
	}
}

func TestCommitHandoff_MidStage_Success(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_b",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	resp := s.handleRequest(makeCommitHandoffReq(
		"ns/test/v1",
		`{"notes":"hello world"}`,
		nil,
	))
	if !resp.OK {
		t.Fatalf("expected ok, got error: %s (code=%s)", resp.Error, resp.Code)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", resp.Result)
	}
	if accepted, _ := result["accepted"].(bool); !accepted {
		t.Fatal("expected accepted=true")
	}
	if result["handoff_digest"] == nil || result["handoff_digest"].(string) == "" {
		t.Fatal("expected handoff_digest")
	}
	if staged, _ := result["staged"].(bool); !staged {
		t.Fatal("expected staged=true")
	}
}

func TestCommitHandoff_Idempotent(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_b",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	req := makeCommitHandoffReq("ns/test/v1", `{"notes":"hello"}`, nil)

	// First commit.
	resp1 := s.handleRequest(req)
	if !resp1.OK {
		t.Fatalf("first commit: expected ok, got error: %s", resp1.Error)
	}

	// Second commit with identical bytes.
	resp2 := s.handleRequest(req)
	if !resp2.OK {
		t.Fatalf("second commit (idempotent): expected ok, got error: %s (code=%s)", resp2.Error, resp2.Code)
	}
}

func TestCommitHandoff_Conflict(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_b",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	// First commit.
	resp1 := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"notes":"hello"}`, nil))
	if !resp1.OK {
		t.Fatalf("first commit: expected ok, got error: %s", resp1.Error)
	}

	// Different bytes — should conflict.
	resp2 := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"notes":"different"}`, nil))
	if resp2.OK {
		t.Fatal("expected HANDOFF_CONFLICT, got ok")
	}
	if resp2.Code != "HANDOFF_CONFLICT" {
		t.Fatalf("expected HANDOFF_CONFLICT, got %s", resp2.Code)
	}
}

func TestCommitHandoff_FinalStage_NotAllowed(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_final",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_final",
		StageOrder:     3,
		IsFinalStage:   true,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	resp := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"notes":"final"}`, nil))
	if resp.OK {
		t.Fatal("expected HANDOFF_NOT_ALLOWED for final stage")
	}
	if resp.Code != "HANDOFF_NOT_ALLOWED" {
		t.Fatalf("expected HANDOFF_NOT_ALLOWED, got %s", resp.Code)
	}
}

func TestCommitHandoff_Standalone_NotAllowed(t *testing.T) {
	s := newTestRPCServer(t)
	_ = setTestInvoke(t, s)

	// No pipeline context — standalone invoke.
	resp := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"notes":"nope"}`, nil))
	if resp.OK {
		t.Fatal("expected HANDOFF_NOT_ALLOWED for standalone")
	}
	if resp.Code != "HANDOFF_NOT_ALLOWED" {
		t.Fatalf("expected HANDOFF_NOT_ALLOWED, got %s", resp.Code)
	}
}

func TestCommitHandoff_ReservedKey(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_b",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	resp := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"password":"secret123","data":"ok"}`, nil))
	if resp.OK {
		t.Fatal("expected HANDOFF_INVALID for reserved key")
	}
	code := resp.Code
	if code != pipeline.CodeHandoffReservedKey {
		t.Fatalf("expected %s, got %s", pipeline.CodeHandoffReservedKey, code)
	}
}

func TestCommitHandoff_Oversize(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_b",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	bigPayload := `{"data":"` + strings.Repeat("x", pipeline.HandoffContextMaxBytes+1) + `"}`
	resp := s.handleRequest(makeCommitHandoffReq("ns/test/v1", bigPayload, nil))
	if resp.OK {
		t.Fatal("expected HANDOFF_CONTEXT_OVERSIZE for oversize context")
	}
	if resp.Code != pipeline.CodeHandoffContextOversize {
		t.Fatalf("expected %s, got %s", pipeline.CodeHandoffContextOversize, resp.Code)
	}
}

func TestCommitHandoff_StaleLease(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_test",
		NodeID:     "stage_b",
		RunID:      "run_test",
		AttemptID:  "att_test",
		LeaseID:    "lease_test",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(-time.Hour), // expired
	})

	resp := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"notes":"stale"}`, nil))
	if resp.OK {
		t.Fatal("expected STALE_LEASE")
	}
	if resp.Code != "STALE_LEASE" {
		t.Fatalf("expected STALE_LEASE, got %s", resp.Code)
	}
}

func TestCommitHandoff_ForgedIdentityIgnored(t *testing.T) {
	s := newTestRPCServer(t)
	state := setTestInvoke(t, s)
	state.progressIdentity = progressIdentity{
		WorkflowID: "wf_real",
		NodeID:     "stage_b",
		RunID:      "run_real",
		AttemptID:  "att_real",
		LeaseID:    "lease_real",
	}

	setPipelineContext(s, PipelineStageContext{
		WorkflowKind:   "pipeline",
		NodeID:         "stage_b",
		StageOrder:     1,
		IsFinalStage:   false,
		LeaseExpiresAt: time.Now().Add(time.Hour),
	})

	// Worker tries to forge identity fields in params — harness ignores them.
	params := map[string]any{
		"schema":        "ns/test/v1",
		"context":       json.RawMessage(`{"notes":"ok"}`),
		"workflow_id":   "wf_forged",
		"run_id":        "run_forged",
		"attempt_id":    "att_forged",
		"from_node_id":  "stage_forged",
		"producer_run_id": "run_forged",
	}
	resp := s.handleRequest(rpcRequest{
		ID:     "f1",
		Method: "commit_handoff",
		Params: params,
	})
	if !resp.OK {
		t.Fatalf("expected ok despite forged identity, got error: %s (code=%s)", resp.Error, resp.Code)
	}

	// Verify the candidate envelope uses real identity.
	s.mu.RLock()
	candidate := s.invoke.pipelineCtx.HandoffCandidateJSON
	s.mu.RUnlock()
	if candidate == nil {
		t.Fatal("expected candidate to be set")
	}
	var env pipeline.HandoffEnvelope
	if err := json.Unmarshal(candidate, &env); err != nil {
		t.Fatalf("unmarshal candidate: %v", err)
	}
	if env.WorkflowID != "wf_real" {
		t.Fatalf("expected workflow_id=wf_real, got %s", env.WorkflowID)
	}
	if env.ProducerRunID != "run_real" {
		t.Fatalf("expected producer_run_id=run_real, got %s", env.ProducerRunID)
	}
}

func TestCommitHandoff_NoActiveInvoke(t *testing.T) {
	s := newTestRPCServer(t)
	// No invoke set.
	resp := s.handleRequest(makeCommitHandoffReq("ns/test/v1", `{"notes":"nope"}`, nil))
	if resp.OK {
		t.Fatal("expected error for no active invoke")
	}
	if resp.Code != "no_active_invoke" {
		t.Fatalf("expected no_active_invoke, got %s", resp.Code)
	}
}
