package routedrun

import (
	"context"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// B26 admission-conformance suite (requirement 18)
// Parameterized across store backends and workflow topologies.
// ---------------------------------------------------------------------------

type admissionStore interface {
	DeploymentStore
	RunStore
	WorkflowStore
}

type storeFactory func(t *testing.T) admissionStore

func localAdmissionStore(t *testing.T) admissionStore {
	t.Helper()
	return openTestStore(t)
}

func memoryAdmissionStore(t *testing.T) admissionStore {
	t.Helper()
	return NewMemoryStore(WithMemoryClock(testClock(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))))
}

type topologyCase struct {
	name     string
	kind     string
	meta     map[string]string
	wantRuns int
	// Primary node READY, remaining PENDING for pipelines.
	wantNodes int
}

var admissionTopologies = []topologyCase{
	{
		name:      "standalone",
		kind:      "standalone",
		meta:      nil,
		wantRuns:  1,
		wantNodes: 1,
	},
	{
		name: "pipeline",
		kind: "pipeline",
		meta: map[string]string{
			metaWorkflowKind: "pipeline",
			metaStageCount:   "3",
		},
		wantRuns:  3,
		wantNodes: 3,
	},
	{
		name: "parent_child",
		kind: "parent_child",
		meta: map[string]string{
			metaWorkflowKind: "parent_child",
		},
		wantRuns:  1,
		wantNodes: 1,
	},
	// MCP service topology is modeled as standalone deployment with service
	// packages nested in digests; admission still creates one READY node/run.
	{
		name: "mcp_service",
		kind: "standalone",
		meta: map[string]string{
			"mcp:service:echo": "pkg-echo@1.0.0",
		},
		wantRuns:  1,
		wantNodes: 1,
	},
}

func TestAdmissionConformance_Matrix(t *testing.T) {
	backends := []struct {
		name string
		new  storeFactory
	}{
		{"LocalStore", localAdmissionStore},
		{"MemoryStore", memoryAdmissionStore},
	}

	for _, be := range backends {
		be := be
		t.Run(be.name, func(t *testing.T) {
			for _, topo := range admissionTopologies {
				topo := topo
				t.Run(topo.name, func(t *testing.T) {
					runAdmissionConformanceForTopology(t, be.new, topo)
				})
			}
		})
	}
}

func runAdmissionConformanceForTopology(t *testing.T, newStore storeFactory, topo topologyCase) {
	t.Helper()
	ctx := context.Background()

	t.Run("exact_invocation_accepted", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		req := baseInvocation(dep, "exact-key", "caller-exact", `{"q":"exact"}`)
		rec, err := s.AdmitInvocation(ctx, req, dep.Generation)
		if err != nil {
			t.Fatalf("AdmitInvocation: %v", err)
		}
		if rec.InvocationID == "" || rec.WorkflowID == "" || rec.RunID == "" {
			t.Fatalf("empty ids: %+v", rec)
		}
		if rec.ResolvedDeploymentID != dep.DeploymentID {
			t.Fatalf("resolved=%s want %s", rec.ResolvedDeploymentID, dep.DeploymentID)
		}
		if rec.RequestedDeploymentRef != string(dep.DeploymentID) {
			t.Fatalf("requested ref=%s", rec.RequestedDeploymentRef)
		}
		assertTopologyShape(t, s, rec, topo)
	})

	t.Run("alias_invocation_resolves", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		aliasName := "prod/" + topo.name
		if err := s.CompareAndSwapAlias(ctx, &AliasRecord{
			SchemaVersion:      CurrentSchemaVersion,
			Alias:              aliasName,
			TargetDeploymentID: dep.DeploymentID,
			TargetVersion:      dep.PackageVersion,
			Generation:         0,
			UpdatedBy:          "ops",
		}); err != nil {
			t.Fatalf("alias: %v", err)
		}
		req := baseInvocation(dep, "alias-key", "caller-alias", `{"via":"alias"}`)
		req.RequestedDeploymentRef = aliasName
		rec, err := s.AdmitInvocation(ctx, req, 0)
		if err != nil {
			t.Fatalf("AdmitInvocation: %v", err)
		}
		if rec.ResolvedDeploymentID != dep.DeploymentID {
			t.Fatalf("resolved=%s", rec.ResolvedDeploymentID)
		}
		if rec.RequestedDeploymentRef != aliasName {
			t.Fatalf("requested=%s", rec.RequestedDeploymentRef)
		}
		assertTopologyShape(t, s, rec, topo)
	})

	t.Run("same_key_same_payload_idempotent_replay", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		req := baseInvocation(dep, "replay-key", "caller-replay", `{"same":true}`)
		r1, err := s.AdmitInvocation(ctx, req, 0)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := s.AdmitInvocation(ctx, req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r2.InvocationID != r1.InvocationID || r2.RunID != r1.RunID || r2.WorkflowID != r1.WorkflowID {
			t.Fatalf("replay mismatch: %+v vs %+v", r1, r2)
		}
		// Exactly one workflow materialised.
		wfs, err := s.ListWorkflows(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(wfs) != 1 {
			t.Fatalf("want 1 workflow after replay, got %d", len(wfs))
		}
	})

	t.Run("same_key_different_payload_conflict", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		req := baseInvocation(dep, "conflict-key", "caller-c", `{"a":1}`)
		if _, err := s.AdmitInvocation(ctx, req, 0); err != nil {
			t.Fatal(err)
		}
		// Different input
		req2 := *req
		req2.InputJSON = `{"a":2}`
		req2.InputDigest = ""
		if _, err := s.AdmitInvocation(ctx, &req2, 0); !errorsIs(err, ErrIdempotencyConflict) {
			t.Fatalf("input change: want ErrIdempotencyConflict, got %v", err)
		}
		// Different ceiling
		req3 := *req
		req3.InitialMaxActiveDurationMs = 99_999
		if _, err := s.AdmitInvocation(ctx, &req3, 0); !errorsIs(err, ErrIdempotencyConflict) {
			t.Fatalf("ceiling change: want ErrIdempotencyConflict, got %v", err)
		}
		// Different creation options
		req4 := *req
		req4.CreationOptionsDigest = "other-opts"
		if _, err := s.AdmitInvocation(ctx, &req4, 0); !errorsIs(err, ErrIdempotencyConflict) {
			t.Fatalf("options change: want ErrIdempotencyConflict, got %v", err)
		}
		// Different deployment ref
		req5 := *req
		req5.RequestedDeploymentRef = "alias-that-differs"
		if _, err := s.AdmitInvocation(ctx, &req5, 0); !errorsIs(err, ErrIdempotencyConflict) {
			t.Fatalf("ref change: want ErrIdempotencyConflict, got %v", err)
		}
	})

	t.Run("different_key_same_payload_new_invocation", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		payload := `{"shared":true}`
		r1, err := s.AdmitInvocation(ctx, baseInvocation(dep, "key-a", "caller", payload), 0)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := s.AdmitInvocation(ctx, baseInvocation(dep, "key-b", "caller", payload), 0)
		if err != nil {
			t.Fatal(err)
		}
		if r1.InvocationID == r2.InvocationID || r1.RunID == r2.RunID {
			t.Fatalf("expected distinct invocations: %+v %+v", r1, r2)
		}
	})

	t.Run("caller_isolation", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		payload := `{"iso":1}`
		key := "shared-key"
		r1, err := s.AdmitInvocation(ctx, baseInvocation(dep, key, "caller-1", payload), 0)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := s.AdmitInvocation(ctx, baseInvocation(dep, key, "caller-2", payload), 0)
		if err != nil {
			t.Fatal(err)
		}
		if r1.InvocationID == r2.InvocationID {
			t.Fatalf("callers must not share idempotency: %s", r1.InvocationID)
		}
	})

	t.Run("alias_moved_after_acceptance_replay_original", func(t *testing.T) {
		s := newStore(t)
		depA := seedActiveDeployment(t, s, 4, topo.meta)
		depB := seedActiveDeployment(t, s, 4, topo.meta)
		aliasName := "moving/" + topo.name
		alias := &AliasRecord{
			SchemaVersion:      CurrentSchemaVersion,
			Alias:              aliasName,
			TargetDeploymentID: depA.DeploymentID,
			TargetVersion:      depA.PackageVersion,
			Generation:         0,
			UpdatedBy:          "ops",
		}
		if err := s.CompareAndSwapAlias(ctx, alias); err != nil {
			t.Fatal(err)
		}
		req := baseInvocation(depA, "move-key", "caller-move", `{"m":1}`)
		req.RequestedDeploymentRef = aliasName
		r1, err := s.AdmitInvocation(ctx, req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r1.ResolvedDeploymentID != depA.DeploymentID {
			t.Fatalf("initial resolve=%s", r1.ResolvedDeploymentID)
		}
		// Move alias to depB.
		got, err := s.ResolveAlias(ctx, aliasName)
		if err != nil {
			t.Fatal(err)
		}
		got.TargetDeploymentID = depB.DeploymentID
		got.TargetVersion = depB.PackageVersion
		if err := s.CompareAndSwapAlias(ctx, got); err != nil {
			t.Fatal(err)
		}
		// Replay must return original receipt, not re-resolve to depB.
		r2, err := s.AdmitInvocation(ctx, req, 0)
		if err != nil {
			t.Fatal(err)
		}
		if r2.InvocationID != r1.InvocationID {
			t.Fatalf("replay inv mismatch")
		}
		if r2.ResolvedDeploymentID != depA.DeploymentID {
			t.Fatalf("replay must keep original resolution %s, got %s", depA.DeploymentID, r2.ResolvedDeploymentID)
		}
	})

	t.Run("inactive_deployment_rejected", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 4, topo.meta)
		if err := s.SetDeploymentStatus(ctx, dep.DeploymentID, DeploymentInactive, dep.Generation); err != nil {
			t.Fatal(err)
		}
		_, err := s.AdmitInvocation(ctx, baseInvocation(dep, "inactive-key", "c", `{}`), 0)
		if !errorsIs(err, ErrDeploymentInactive) {
			t.Fatalf("want ErrDeploymentInactive, got %v", err)
		}
		// No workflow left behind.
		wfs, err := s.ListWorkflows(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(wfs) != 0 {
			t.Fatalf("inactive admit must not create workflow, got %d", len(wfs))
		}
	})

	t.Run("default_one_overlap_no_queue", func(t *testing.T) {
		s := newStore(t)
		// maxConcurrent=0 is treated as 1 by store.
		dep := seedActiveDeployment(t, s, 0, topo.meta)
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "slot-1", "c1", `{}`), 0); err != nil {
			t.Fatal(err)
		}
		_, err := s.AdmitInvocation(ctx, baseInvocation(dep, "slot-2", "c2", `{}`), 0)
		if !errorsIs(err, ErrAlreadyRunning) {
			t.Fatalf("want ErrAlreadyRunning, got %v", err)
		}
		invs, err := s.ListInvocations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(invs) != 1 {
			t.Fatalf("rejected admit must leave no second invocation, got %d", len(invs))
		}
	})

	t.Run("configured_safe_concurrency", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 2, topo.meta)
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "conc-1", "c1", `{}`), 0); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "conc-2", "c2", `{}`), 0); err != nil {
			t.Fatalf("second of max=2 must accept: %v", err)
		}
		_, err := s.AdmitInvocation(ctx, baseInvocation(dep, "conc-3", "c3", `{}`), 0)
		if !errorsIs(err, ErrAlreadyRunning) {
			t.Fatalf("third must be ALREADY_RUNNING, got %v", err)
		}
	})

	t.Run("paused_slot_release_and_resume_reacquire", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 1, topo.meta)
		r1, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pause-1", "c1", `{}`), 0)
		if err != nil {
			t.Fatal(err)
		}
		// Second must block while first is slot-holding (PENDING).
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pause-2", "c2", `{}`), 0); !errorsIs(err, ErrAlreadyRunning) {
			t.Fatalf("want ALREADY_RUNNING before pause, got %v", err)
		}
		// Transition workflow to PAUSED — releases concurrency slot.
		wf, err := s.GetWorkflow(ctx, r1.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		wf.Status = WorkflowStatusPaused
		if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
			t.Fatal(err)
		}
		r2, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pause-2", "c2", `{}`), 0)
		if err != nil {
			t.Fatalf("admit after PAUSED must succeed (slot released): %v", err)
		}
		if r2.WorkflowID == r1.WorkflowID {
			t.Fatal("second admit must be a new workflow")
		}
		// Resume first while second holds the only slot: re-acquire must fail
		// under max=1 (resume and admit share the same concurrency budget).
		wf1, err := s.GetWorkflow(ctx, r1.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		wf1.Status = WorkflowStatusRunning
		if err := s.UpdateWorkflow(ctx, wf1, wf1.Generation); !errorsIs(err, ErrAlreadyRunning) {
			t.Fatalf("resume with full slots: want ErrAlreadyRunning, got %v", err)
		}
		// Pause second → free the slot → first may resume.
		wf2, err := s.GetWorkflow(ctx, r2.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		wf2.Status = WorkflowStatusPaused
		if err := s.UpdateWorkflow(ctx, wf2, wf2.Generation); err != nil {
			t.Fatal(err)
		}
		wf1, err = s.GetWorkflow(ctx, r1.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		wf1.Status = WorkflowStatusRunning
		if err := s.UpdateWorkflow(ctx, wf1, wf1.Generation); err != nil {
			t.Fatalf("resume after slot free: %v", err)
		}
		// First is RUNNING (slot-holding), third still blocked.
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pause-3", "c3", `{}`), 0); !errorsIs(err, ErrAlreadyRunning) {
			t.Fatalf("running holder still blocks: %v", err)
		}
		// Pause first → no holders → third accepts.
		wf1, err = s.GetWorkflow(ctx, r1.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		wf1.Status = WorkflowStatusPaused
		if err := s.UpdateWorkflow(ctx, wf1, wf1.Generation); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pause-3", "c3", `{}`), 0); err != nil {
			t.Fatalf("all paused must free slot: %v", err)
		}
	})

	t.Run("pause_requested_still_holds_slot", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 1, topo.meta)
		r1, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pr-1", "c1", `{}`), 0)
		if err != nil {
			t.Fatal(err)
		}
		wf, err := s.GetWorkflow(ctx, r1.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		// PAUSE_REQUESTED must commit as still slot-holding (req 16).
		wf.Status = WorkflowStatusPauseRequested
		if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "pr-2", "c2", `{}`), 0); !errorsIs(err, ErrAlreadyRunning) {
			t.Fatalf("PAUSE_REQUESTED must hold slot, got %v", err)
		}
	})

	t.Run("needs_replan_releases_slot", func(t *testing.T) {
		s := newStore(t)
		dep := seedActiveDeployment(t, s, 1, topo.meta)
		r1, err := s.AdmitInvocation(ctx, baseInvocation(dep, "nr-1", "c1", `{}`), 0)
		if err != nil {
			t.Fatal(err)
		}
		wf, err := s.GetWorkflow(ctx, r1.WorkflowID)
		if err != nil {
			t.Fatal(err)
		}
		wf.Status = WorkflowStatusNeedsReplan
		if err := s.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
			t.Fatal(err)
		}
		if _, err := s.AdmitInvocation(ctx, baseInvocation(dep, "nr-2", "c2", `{}`), 0); err != nil {
			t.Fatalf("NEEDS_REPLAN must release slot: %v", err)
		}
	})
}

func TestAdmissionConformance_GetInvocationByIdempotency(t *testing.T) {
	for _, be := range []struct {
		name string
		new  storeFactory
	}{
		{"LocalStore", localAdmissionStore},
		{"MemoryStore", memoryAdmissionStore},
	} {
		t.Run(be.name, func(t *testing.T) {
			s := be.new(t)
			ctx := context.Background()
			dep := seedActiveDeployment(t, s, 2, nil)
			req := baseInvocation(dep, "lookup-key", "lookup-caller", `{"x":1}`)
			rec, err := s.AdmitInvocation(ctx, req, 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetInvocationByIdempotency(ctx, "lookup-caller", "lookup-key")
			if err != nil {
				t.Fatal(err)
			}
			if got.InvocationID != rec.InvocationID {
				t.Fatalf("got %s want %s", got.InvocationID, rec.InvocationID)
			}
			if _, err := s.GetInvocationByIdempotency(ctx, "lookup-caller", "missing"); !errorsIs(err, ErrNotFound) {
				t.Fatalf("want not found, got %v", err)
			}
		})
	}
}

func TestAdmissionConformance_NoAttemptOnAdmit(t *testing.T) {
	// Admission creates READY launch intent without an attempt (daemon later).
	s := openTestStore(t)
	ctx := context.Background()
	dep := seedActiveDeployment(t, s, 1, nil)
	rec, err := s.AdmitInvocation(ctx, baseInvocation(dep, "no-att", "c", `{}`), 0)
	if err != nil {
		t.Fatal(err)
	}
	atts, err := s.ListAttempts(ctx, rec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 0 {
		t.Fatalf("admission must not create attempts, got %d", len(atts))
	}
	run, err := s.GetRun(ctx, rec.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusPending {
		t.Fatalf("run status=%s want PENDING", run.Status)
	}
}

// baseInvocation builds a minimal valid InvocationRequest.
func baseInvocation(dep *DeploymentRecord, key, caller, input string) *InvocationRequest {
	return &InvocationRequest{
		SchemaVersion:              CurrentSchemaVersion,
		RequestedDeploymentRef:     string(dep.DeploymentID),
		InputJSON:                  input,
		InitialMaxActiveDurationMs: 60_000,
		InitialAttemptLeaseMs:      30_000,
		InitialMaxCostUsdDecimal:   "1.00",
		CreationOptionsDigest:      "opts-default",
		IdempotencyKey:             key,
		CallerIdentity:             caller,
	}
}

func assertTopologyShape(t *testing.T, s admissionStore, rec *InvocationReceipt, topo topologyCase) {
	t.Helper()
	ctx := context.Background()
	wf, err := s.GetWorkflow(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	wantKind := topo.kind
	if topo.name == "mcp_service" {
		wantKind = "standalone"
	}
	if wf.WorkflowKind != wantKind {
		t.Fatalf("workflow kind=%s want %s", wf.WorkflowKind, wantKind)
	}
	nodes, err := s.ListNodes(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != topo.wantNodes {
		t.Fatalf("nodes=%d want %d", len(nodes), topo.wantNodes)
	}
	// First stage READY; later stages PENDING.
	ready := 0
	for _, n := range nodes {
		switch n.Status {
		case NodeStatusReady:
			ready++
		case NodeStatusPending:
			// ok
		default:
			t.Fatalf("unexpected node status %s", n.Status)
		}
	}
	if ready != 1 {
		t.Fatalf("want exactly 1 READY node, got %d", ready)
	}
	runs, err := s.ListRuns(ctx, rec.WorkflowID)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != topo.wantRuns {
		t.Fatalf("runs=%d want %d", len(runs), topo.wantRuns)
	}
	// Primary run ID on receipt must exist.
	found := false
	for _, r := range runs {
		if r.RunID == rec.RunID {
			found = true
			if r.Status != RunStatusPending {
				t.Fatalf("primary run status=%s", r.Status)
			}
		}
	}
	if !found {
		t.Fatalf("receipt run %s not in list", rec.RunID)
	}
}
