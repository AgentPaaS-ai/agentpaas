package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestWriteDelegationSnapshotForRun_PersistsLiveCallForbidden(t *testing.T) {
	snap := &delegation.CommunicationSnapshot{
		SchemaVersion:       delegation.CurrentSchemaVersion,
		SnapshotGeneration:  1,
		WorkflowID:          "wf-pipeline",
		TenantID:            "tenant-test",
		CallerDeploymentID:  "dep-caller",
		CallerPackageName:   "pipeline-agent",
		CallerPackageDigest: "sha256:caller",
		Bindings: []delegation.WorkflowDelegationBinding{
			{
				BindingID:            "report.verify",
				CalleePackageName:    "report-verifier",
				CalleePackageVersion: "1.0.0",
				CalleeBundleDigest:   "sha256:callee",
				MaxDataClass:         "internal",
			},
		},
	}
	dg, err := delegation.ComputeSnapshotDigest(snap)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	snap.SnapshotDigest = dg

	deployed := t.TempDir()
	gateway := t.TempDir()
	lock := &pack.AgentLock{
		WorkflowYAML: &pack.WorkflowYAML{
			Kind: pack.WorkflowKindPipeline,
		},
		CommunicationSnapshot: snap,
	}
	if err := pack.WriteAgentLock(lock, filepath.Join(deployed, "agent.lock")); err != nil {
		t.Fatalf("WriteAgentLock: %v", err)
	}

	s := &controlServer{}
	path, ok := s.writeDelegationSnapshotForRun(deployed, gateway)
	if !ok {
		t.Fatal("expected sidecar to be written for pipeline snapshot with bindings")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var sidecar struct {
		WorkflowKind      string `json:"workflow_kind"`
		LiveCallForbidden bool   `json:"live_call_forbidden"`
		Snapshot          struct {
			Bindings []delegation.WorkflowDelegationBinding `json:"bindings"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &sidecar); err != nil {
		t.Fatalf("unmarshal sidecar: %v (%s)", err, data)
	}
	if sidecar.WorkflowKind != pack.WorkflowKindPipeline {
		t.Errorf("workflow_kind = %q, want pipeline", sidecar.WorkflowKind)
	}
	if !sidecar.LiveCallForbidden {
		t.Fatal("pipeline sidecar must persist live_call_forbidden so bindings cannot re-enable delegate")
	}
	if len(sidecar.Snapshot.Bindings) == 0 {
		t.Fatal("bindings must still be written; the bit is what denies")
	}
}
