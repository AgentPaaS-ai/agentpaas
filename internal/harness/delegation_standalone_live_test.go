package harness

import (
	"encoding/json"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
)

const standaloneLiveBindingID = "dep_bfc82159"

func standaloneLiveSidecar(workflowKind string, standalone *bool) map[string]any {
	snap := delegation.CommunicationSnapshot{
		SchemaVersion:       "1",
		SnapshotGeneration:  1,
		WorkflowID:          "",
		TenantID:            "tenant-ss",
		CallerDeploymentID:  "dep-ss-caller",
		CallerPackageName:   "dep-ss-caller",
		CallerPackageDigest: "sha256:caller",
		Bindings: []delegation.WorkflowDelegationBinding{
			{
				BindingID:            standaloneLiveBindingID,
				CalleePackageName:    standaloneLiveBindingID,
				CalleePackageVersion: "pinned",
				CalleeBundleDigest:   "sha256:pinned",
				MaxDataClass:         "internal",
			},
		},
	}
	sidecar := map[string]any{
		"snapshot":             snap,
		"binding_capabilities": map[string]string{standaloneLiveBindingID: "cap-ss-token-0123456789abcdef"},
		"live_call_forbidden":  false,
		"workflow_id":          "",
	}
	if workflowKind != "" {
		sidecar["workflow_kind"] = workflowKind
	}
	if standalone != nil {
		sidecar["standalone"] = *standalone
	}
	return sidecar
}

func loadStandaloneSidecar(t *testing.T, sidecar map[string]any) *harnessRPCServer {
	t.Helper()
	data, err := json.Marshal(sidecar)
	if err != nil {
		t.Fatalf("marshal sidecar: %v", err)
	}
	s := &harnessRPCServer{done: make(chan struct{})}
	if err := s.LoadDelegationSnapshotJSON(string(data)); err != nil {
		t.Fatalf("LoadDelegationSnapshotJSON: %v", err)
	}
	return s
}

func delegateStandaloneLive(s *harnessRPCServer, id string) rpcResponse {
	return s.handleRequest(rpcRequest{
		ID:     id,
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      standaloneLiveBindingID,
			"idempotency_key": "idem-" + id,
		},
	})
}

func TestDelegateTask_StandaloneLiveSidecarNotNoSnapshot(t *testing.T) {
	s := loadStandaloneSidecar(t, standaloneLiveSidecar("standalone_live", nil))
	dts := s.getDelegationTrustState()
	if dts == nil {
		t.Fatal("expected trust state after standalone_live sidecar load")
	}
	if dts.Snapshot.WorkflowID != "" {
		t.Fatalf("must not mint WorkflowID, got %q", dts.Snapshot.WorkflowID)
	}
	if dts.LiveCallForbidden {
		t.Fatal("standalone_live must not set LiveCallForbidden")
	}
	if len(dts.Snapshot.Bindings) == 0 {
		t.Fatal("standalone_live sidecar must keep bindings")
	}
	if !dts.StandaloneLive {
		t.Fatal("applyDelegationSnapshot must plumb StandaloneLive from workflow_kind")
	}

	resp := delegateStandaloneLive(s, "req-ss-live")
	if resp.Code == "no_snapshot" {
		t.Fatalf("standalone_live sidecar must not be rejected as no_snapshot: %s", resp.Error)
	}
}

func TestDelegateTask_StandaloneTrueSidecarNotNoSnapshot(t *testing.T) {
	standalone := true
	s := loadStandaloneSidecar(t, standaloneLiveSidecar("", &standalone))
	dts := s.getDelegationTrustState()
	if dts == nil {
		t.Fatal("expected trust state after standalone=true sidecar load")
	}
	if !dts.StandaloneLive {
		t.Fatal("applyDelegationSnapshot must plumb StandaloneLive from top-level standalone=true")
	}
	if dts.Snapshot.WorkflowID != "" {
		t.Fatalf("must not mint WorkflowID, got %q", dts.Snapshot.WorkflowID)
	}

	resp := delegateStandaloneLive(s, "req-ss-flag")
	if resp.Code == "no_snapshot" {
		t.Fatalf("standalone=true sidecar must not be rejected as no_snapshot: %s", resp.Error)
	}
}

func TestDelegateTask_NoSnapshotHalfConfiguredSidecar(t *testing.T) {
	s := loadStandaloneSidecar(t, standaloneLiveSidecar("", nil))
	dts := s.getDelegationTrustState()
	if dts == nil {
		t.Fatal("expected trust state after half-configured sidecar load")
	}
	if dts.StandaloneLive {
		t.Fatal("half-configured sidecar must not be treated as standalone_live")
	}
	if dts.LiveCallForbidden {
		t.Fatal("half-configured case is not live-call-forbidden")
	}
	if dts.Snapshot.WorkflowID != "" || len(dts.Snapshot.Bindings) == 0 {
		t.Fatal("half-configured sidecar must keep empty WorkflowID and bindings")
	}

	resp := delegateStandaloneLive(s, "req-ss-half")
	if resp.OK {
		t.Fatalf("half-configured sidecar must not admit, got %+v", resp.Result)
	}
	if resp.Code != "no_snapshot" {
		t.Fatalf("expected code no_snapshot, got %q (%s)", resp.Code, resp.Error)
	}
}
