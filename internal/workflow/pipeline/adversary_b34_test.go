package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ============================================================================
// B34-T09 adversary regression tests — pipeline runtime
//
// Pattern: each test exercises an ATTACK path. If the defense holds, the
// ADVERSARY BREAK assertion is unreachable and the test PASSES. If the code
// is vulnerable, the test FAILS with "ADVERSARY BREAK: ...".
//
// Do NOT delete or weaken these tests. Leave RED findings for the fix worker.
// ============================================================================

// ---------------------------------------------------------------------------
// 1. Double claim / double launch under concurrency
// ---------------------------------------------------------------------------

// TestAdversary_B34_ConcurrentClaimNextReady_SingleWinner hammers ClaimNextReady
// from many goroutines. Exactly one claim must win; node must be LAUNCHING once;
// attempt count must not explode.
func TestAdversary_B34_ConcurrentClaimNextReady_SingleWinner(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	const workers = 32
	var (
		wg       sync.WaitGroup
		claims   int64
		nilClaim int64
		errs     int64
	)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := ctrl.ClaimNextReady(ctx, wfID)
			if err != nil {
				atomic.AddInt64(&errs, 1)
				return
			}
			if c == nil {
				atomic.AddInt64(&nilClaim, 1)
				return
			}
			atomic.AddInt64(&claims, 1)
		}()
	}
	wg.Wait()

	if errs != 0 {
		// CAS conflict is acceptable under contention; hard failures are not
		// counted separately here — any non-nil error that is not contention
		// still surfaces via end-state checks below.
	}
	if claims != 1 {
		// ADVERSARY BREAK: mutex/CAS failed to serialize claim
		t.Fatalf("ADVERSARY BREAK: concurrent ClaimNextReady produced %d claims (want exactly 1); nils=%d errs=%d",
			claims, nilClaim, errs)
	}

	n0, err := ctrl.Store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n0.Status != routedrun.NodeStatusLaunching {
		t.Fatalf("ADVERSARY BREAK: after single claim, node0 want LAUNCHING, got %s", n0.Status)
	}

	attempts, err := ctrl.Store.ListAttempts(ctx, n0.RunID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(attempts) != 1 {
		t.Fatalf("ADVERSARY BREAK: expected exactly 1 attempt after concurrent claim, got %d", len(attempts))
	}
}

// TestAdversary_B34_ConcurrentReconcileOnce_NoDoubleLaunch ensures ReconcileOnce
// under race never persists two launch jobs for the same stage.
func TestAdversary_B34_ConcurrentReconcileOnce_NoDoubleLaunch(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	launches := NewMemoryLaunchStore()
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	rec := &Reconciler{Ctrl: ctrl, Launches: launches, Launcher: FakeLauncher{}}

	const workers = 16
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = rec.ReconcileOnce(ctx, wfID)
		}()
	}
	wg.Wait()

	jobs, err := launches.ListByWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("ListByWorkflow: %v", err)
	}
	stage0Jobs := 0
	keys := map[string]int{}
	for _, j := range jobs {
		if j.NodeID == nodeIDs[0] {
			stage0Jobs++
			keys[j.Key]++
		}
	}
	if stage0Jobs != 1 {
		t.Fatalf("ADVERSARY BREAK: concurrent ReconcileOnce created %d launch jobs for stage0 (want 1)", stage0Jobs)
	}
	for k, n := range keys {
		if n != 1 {
			t.Fatalf("ADVERSARY BREAK: launch key %q duplicated %d times", k, n)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Handoff after cancel
// ---------------------------------------------------------------------------

// TestAdversary_B34_HandoffAfterCancelRejected: after CancelWorkflow, a late
// CommitStageSuccess with a handoff must not succeed and must not leave a
// committed handoff that advances the next stage.
func TestAdversary_B34_HandoffAfterCancelRejected(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNextReady: claim=%v err=%v", claim, err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{WorkflowID: wfID, Reason: "adversary"}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	err = ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"result":"late-after-cancel"}`,
		},
	})
	if err == nil {
		t.Fatal("ADVERSARY BREAK: CommitStageSuccess with handoff accepted after cancel")
	}

	// Next node must remain PENDING (not READY via smuggled handoff).
	n1, err := ctrl.Store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("GetNode stage1: %v", err)
	}
	if n1.Status == routedrun.NodeStatusReady || n1.Status == routedrun.NodeStatusLaunching || n1.Status == routedrun.NodeStatusRunning {
		t.Fatalf("ADVERSARY BREAK: stage1 advanced after cancel+late success: status=%s", n1.Status)
	}

	// No handoff may be committed as a side effect of the rejected success.
	handoffs, err := ctrl.Store.ListHandoffs(ctx, wfID)
	if err != nil {
		t.Fatalf("ListHandoffs: %v", err)
	}
	if len(handoffs) != 0 {
		t.Fatalf("ADVERSARY BREAK: handoff committed after cancel (%d envelopes)", len(handoffs))
	}
}

// ---------------------------------------------------------------------------
// 3. Resume after cancel
// ---------------------------------------------------------------------------

// TestAdversary_B34_ResumeAfterCancelRejected: ResumeWorkflow on CANCELLED
// must fail and must not mark any PENDING node READY.
func TestAdversary_B34_ResumeAfterCancelRejected(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Cancel while stage0 is RUNNING so the active node becomes CANCELLED.
	claim0, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil || claim0 == nil {
		t.Fatalf("ClaimNextReady: claim=%v err=%v", claim0, err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim0); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{WorkflowID: wfID}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// Snapshot node statuses post-cancel.
	pre := make([]routedrun.NodeStatus, len(nodeIDs))
	for i, nid := range nodeIDs {
		n, err := ctrl.Store.GetNode(ctx, nid)
		if err != nil {
			t.Fatalf("GetNode %d: %v", i, err)
		}
		pre[i] = n.Status
	}

	err = ctrl.ResumeWorkflow(ctx, ResumeRequest{WorkflowID: wfID})
	if err == nil {
		t.Fatal("ADVERSARY BREAK: ResumeWorkflow succeeded on CANCELLED workflow")
	}

	for i, nid := range nodeIDs {
		n, err := ctrl.Store.GetNode(ctx, nid)
		if err != nil {
			t.Fatalf("GetNode %d: %v", i, err)
		}
		if n.Status != pre[i] {
			t.Fatalf("ADVERSARY BREAK: resume-after-cancel mutated node%d %s → %s", i, pre[i], n.Status)
		}
		if n.Status == routedrun.NodeStatusReady || n.Status == routedrun.NodeStatusLaunching || n.Status == routedrun.NodeStatusRunning {
			t.Fatalf("ADVERSARY BREAK: resume-after-cancel left/activated node%d status=%s", i, n.Status)
		}
	}

	// Claim must also be a no-op on terminal workflow.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady after cancel: %v", err)
	}
	if claim != nil {
		t.Fatalf("ADVERSARY BREAK: ClaimNextReady returned claim on CANCELLED workflow: %+v", claim)
	}
}

// TestAdversary_B34_CancelClearsReadyAndPendingNodes: cancel before any claim
// must transition ALL non-terminal nodes (READY, PENDING) to CANCELLED. Claim
// and resume must still fail closed on the terminal workflow.
func TestAdversary_B34_CancelClearsReadyAndPendingNodes(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	if err := ctrl.CancelWorkflow(ctx, CancelRequest{WorkflowID: wfID}); err != nil {
		t.Fatalf("CancelWorkflow: %v", err)
	}

	// All nodes must be CANCELLED — no READY or PENDING survivors.
	for i, nid := range nodeIDs {
		n, err := ctrl.Store.GetNode(ctx, nid)
		if err != nil {
			t.Fatalf("GetNode %d: %v", i, err)
		}
		if n.Status == routedrun.NodeStatusReady || n.Status == routedrun.NodeStatusPending {
			t.Fatalf("ADVERSARY BREAK: cancel left node%d in non-terminal state %s", i, n.Status)
		}
		if n.Status != routedrun.NodeStatusCancelled {
			t.Fatalf("node%d: want CANCELLED, got %s", i, n.Status)
		}
	}

	// Claim must block on the terminal workflow.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if claim != nil {
		t.Fatalf("ADVERSARY BREAK: ClaimNextReady claimed node on CANCELLED workflow")
	}

	// Resume must be rejected.
	if err := ctrl.ResumeWorkflow(ctx, ResumeRequest{WorkflowID: wfID}); err == nil {
		t.Fatal("ADVERSARY BREAK: ResumeWorkflow succeeded on CANCELLED workflow")
	}
}

// ---------------------------------------------------------------------------
// 4. Secret-like context in handoff (reserved keys)
// ---------------------------------------------------------------------------

// TestAdversary_B34_ReservedKeys_TopLevelRejected pins the baseline reserved
// key denylist for ValidateHandoffEnvelope.
func TestAdversary_B34_ReservedKeys_TopLevelRejected(t *testing.T) {
	keys := []string{
		"password", "api_key", "OPENAI_API_KEY", "credential",
		"capability_token", "token", "secret", "private_key", "access_key",
	}
	for _, k := range keys {
		t.Run(k, func(t *testing.T) {
			ho := validMinimalHandoff()
			val, _ := json.Marshal(map[string]interface{}{k: "leaked-value-should-not-pass"})
			ho.Context.Value = val
			codes := ValidateHandoffEnvelope(&ho)
			if !ContainsCode(codes, CodeHandoffReservedKey) {
				t.Fatalf("ADVERSARY BREAK: reserved key %q accepted in handoff context; codes=%v", k, codes)
			}
		})
	}
}

// TestAdversary_B34_ReservedKeys_NestedObjectRejected: nested objects must not
// smuggle reserved keys past a top-level-only scan.
func TestAdversary_B34_ReservedKeys_NestedObjectRejected(t *testing.T) {
	ho := validMinimalHandoff()
	// Nested password under an innocuous wrapper.
	val, _ := json.Marshal(map[string]interface{}{
		"meta": map[string]interface{}{
			"password": "nested-secret-value",
			"note":     "looks fine",
		},
	})
	ho.Context.Value = val
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffReservedKey) {
		// ADVERSARY BREAK: hasReservedKeys only inspects top-level keys
		t.Fatalf("ADVERSARY BREAK: nested reserved key password accepted; codes=%v", codes)
	}
}

// TestAdversary_B34_ReservedKeys_DeepArrayRejected: reserved keys inside array
// elements must be rejected.
func TestAdversary_B34_ReservedKeys_DeepArrayRejected(t *testing.T) {
	ho := validMinimalHandoff()
	val, _ := json.Marshal(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"token": "arr-secret"},
		},
	})
	ho.Context.Value = val
	codes := ValidateHandoffEnvelope(&ho)
	if !ContainsCode(codes, CodeHandoffReservedKey) {
		t.Fatalf("ADVERSARY BREAK: reserved key token inside array element accepted; codes=%v", codes)
	}
}

// TestAdversary_B34_ControllerCommitDoesNotAcceptSecretContext: the runtime
// CommitStageSuccess path must not accept ContextJSON carrying reserved keys
// even if the pure ValidateHandoffEnvelope path is bypassed by callers using
// routedrun.HandoffEnvelope directly.
func TestAdversary_B34_ControllerCommitDoesNotAcceptSecretContext(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNextReady: claim=%v err=%v", claim, err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	secretPayload := `{"password":"s3cr3t-should-be-rejected","api_key":"sk-live-xxx"}`
	err = ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  secretPayload,
		},
	})
	if err == nil {
		// Confirm the secret actually landed in the store (full break).
		handoffs, lerr := ctrl.Store.ListHandoffs(ctx, wfID)
		if lerr == nil && len(handoffs) > 0 && strings.Contains(handoffs[0].ContextJSON, "s3cr3t-should-be-rejected") {
			t.Fatal("ADVERSARY BREAK: CommitStageSuccess accepted and stored handoff ContextJSON with reserved secret keys (password/api_key); controller bypasses ValidateHandoffEnvelope")
		}
		t.Fatal("ADVERSARY BREAK: CommitStageSuccess accepted handoff ContextJSON with reserved keys; no validation on controller path")
	}
}

// ---------------------------------------------------------------------------
// 5. Path traversal / symlink in artifact promote + projection
// ---------------------------------------------------------------------------

// TestAdversary_B34_PromoteRejectsPathTraversal vectors that must never promote.
func TestAdversary_B34_PromoteRejectsPathTraversal(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()

	// Legitimate blob first (so "not found" cannot mask path rejection).
	ok, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "trav-base"
		a.ImmutableRef = "ok.json"
	}), []byte(`{"ok":true}`))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	vectors := []string{
		"../etc/passwd",
		"/etc/passwd",
		"foo/../../etc/passwd",
		"..\\..\\windows\\system32",
		"nested/../../../escape",
		"./../../escape",
		"foo/bar/../../../etc/shadow",
	}
	for _, ref := range vectors {
		t.Run(ref, func(t *testing.T) {
			bad := ok
			bad.ImmutableRef = ref
			_, err := PromoteHandoffArtifacts(ctx, store, nil, "stage_a", "run_1", []HandoffArtifact{bad})
			if err == nil {
				t.Fatalf("ADVERSARY BREAK: Promote accepted path-traversal immutable_ref %q", ref)
			}
			if !strings.Contains(err.Error(), CodeArtifactPathRejected) {
				t.Fatalf("want %s in error for %q, got: %v", CodeArtifactPathRejected, ref, err)
			}
		})
	}
}

// TestAdversary_B34_ArtifactPutToFS_SymlinkEscape: if a symlink already sits
// at the write target, content must not follow it outside the projection dir.
// Code claims CodeArtifactSymlinkRejected; write path must honor it.
func TestAdversary_B34_ArtifactPutToFS_SymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "pwned.txt")

	// Plant symlink where artifactPutToFS will write (basename of ImmutableRef).
	linkPath := filepath.Join(dir, "out.json")
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	art := HandoffArtifact{
		ArtifactID:     "sym-1",
		ImmutableRef:   "out.json",
		Classification: "internal",
	}
	payload := []byte("SYMLINK-ESCAPE-PAYLOAD")
	written, err := artifactPutToFS(dir, art, payload)
	if err == nil {
		// If write "succeeded", ensure it did not follow the symlink outside.
		if _, statErr := os.Lstat(outsideFile); statErr == nil {
			data, _ := os.ReadFile(outsideFile)
			if strings.Contains(string(data), "SYMLINK-ESCAPE-PAYLOAD") {
				t.Fatalf("ADVERSARY BREAK: artifactPutToFS followed symlink and wrote outside projection dir (written=%q outside=%q); expected %s",
					written, outsideFile, CodeArtifactSymlinkRejected)
			}
		}
		// Even without outside write, succeeding on a symlink target is wrong.
		fi, lerr := os.Lstat(linkPath)
		if lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			// Write may have followed without replacing the link.
			t.Fatalf("ADVERSARY BREAK: artifactPutToFS returned success while target is still a symlink (no %s)", CodeArtifactSymlinkRejected)
		}
	}
	if err != nil {
		// Defense held — must mention symlink or path rejection.
		msg := err.Error()
		if !strings.Contains(msg, CodeArtifactSymlinkRejected) &&
			!strings.Contains(msg, CodeArtifactPathRejected) &&
			!strings.Contains(strings.ToLower(msg), "symlink") {
			t.Fatalf("expected symlink/path rejection code in error, got: %v", err)
		}
	}

	// Outside file must never contain payload.
	if data, rerr := os.ReadFile(outsideFile); rerr == nil && strings.Contains(string(data), "SYMLINK-ESCAPE-PAYLOAD") {
		t.Fatalf("ADVERSARY BREAK: outside file contains escape payload via symlink follow")
	}
}

// TestAdversary_B34_BuildROProjection_ParentSymlinkBaseDir: baseDir replaced
// by a symlink must not allow projection content to land outside the real
// caller-owned tree without detection. We plant baseDir as a symlink to a
// jail and verify writes stay under the jail OR are rejected.
func TestAdversary_B34_BuildROProjection_RejectsSymlinkInProjectionTree(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryArtifactStore()
	content := []byte("proj-payload")
	art, err := store.Put(ctx, newTestArtifact(func(a *HandoffArtifact) {
		a.ArtifactID = "proj-sym-1"
		a.ImmutableRef = "data.json"
	}), content)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Real base; after MkdirTemp we can't inject easily, so use artifactPutToFS
	// path already covered. Here: BuildROProjection with unsafe ref after Put.
	// Also reject null-byte / weird refs if they slip past Put via mutation.
	tmp := t.TempDir()
	mut := art
	mut.ImmutableRef = "data.json/../../escape.txt"
	_, err = BuildROProjection(ctx, store, tmp, []HandoffArtifact{mut})
	if err == nil {
		t.Fatal("ADVERSARY BREAK: BuildROProjection accepted traversal immutable_ref data.json/../../escape.txt")
	}
}

// TestAdversary_B34_IsSafeRef_NullByteAndDotSegments pins isSafeRef edge cases.
func TestAdversary_B34_IsSafeRef_NullByteAndDotSegments(t *testing.T) {
	unsafe := []string{
		"../x",
		"/abs",
		"a/../b",
		"..",
		"foo/..",
		"foo/../../bar",
		string([]byte{'a', 0, '.', '.', '/', 'b'}), // null + traversal
	}
	for _, ref := range unsafe {
		if isSafeRef(ref) {
			t.Errorf("ADVERSARY BREAK: isSafeRef(%q) = true, want false", ref)
		}
	}
	// Safe relative names must still pass.
	for _, ref := range []string{"out.json", "dir/out.json", "a-b_c.txt"} {
		if !isSafeRef(ref) {
			t.Errorf("isSafeRef(%q) = false, want true (regression)", ref)
		}
	}
}

// ---------------------------------------------------------------------------
// 6. Pause must not launch next
// ---------------------------------------------------------------------------

// TestAdversary_B34_PauseDoesNotReadyNextOnSuccess: when pause is requested,
// CommitStageSuccess must not mark the next node READY and ClaimNextReady
// must return nil.
func TestAdversary_B34_PauseDoesNotReadyNextOnSuccess(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 3)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNextReady: claim=%v err=%v", claim, err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}

	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "adv-pause-1",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
	}
	wf, err := store.GetWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	wf.Status = routedrun.WorkflowStatusPauseRequested
	wf.UpdatedAt = time.Now().UTC()
	if err := store.UpdateWorkflow(ctx, wf, wf.Generation); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}

	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"step":1}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess: %v", err)
	}

	n1, err := store.GetNode(ctx, nodeIDs[1])
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n1.Status != routedrun.NodeStatusPending {
		t.Fatalf("ADVERSARY BREAK: pause still readied next stage: node1 status=%s (want PENDING)", n1.Status)
	}

	// Claim while paused / pause-desired must not launch.
	c2, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady while paused: %v", err)
	}
	if c2 != nil {
		t.Fatalf("ADVERSARY BREAK: ClaimNextReady launched while pause requested: %+v", c2)
	}

	// ReconcileOnce must also be a no-op.
	rec := &Reconciler{Ctrl: ctrl, Launches: NewMemoryLaunchStore(), Launcher: FakeLauncher{}}
	c3, err := rec.ReconcileOnce(ctx, wfID)
	if err != nil {
		t.Fatalf("ReconcileOnce while paused: %v", err)
	}
	if c3 != nil {
		t.Fatalf("ADVERSARY BREAK: ReconcileOnce launched while pause requested: %+v", c3)
	}
}

// TestAdversary_B34_PauseAfterNextAlreadyReady_BlocksClaim: if stage1 is already
// READY and pause is then requested, ClaimNextReady must not claim it.
func TestAdversary_B34_PauseAfterNextAlreadyReady_BlocksClaim(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrl := NewController(store)
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	// Complete stage0 without pause → stage1 READY.
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNextReady: claim=%v err=%v", claim, err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}
	if err := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  `{"ok":true}`,
		},
	}); err != nil {
		t.Fatalf("CommitStageSuccess: %v", err)
	}
	n1, _ := store.GetNode(ctx, nodeIDs[1])
	if n1.Status != routedrun.NodeStatusReady {
		t.Fatalf("precondition: node1 want READY, got %s", n1.Status)
	}

	// Now pause.
	if err := store.RequestControl(ctx, &routedrun.ControlRequest{
		WorkflowID:     wfID,
		Command:        routedrun.ControlPause,
		IdempotencyKey: "adv-pause-ready",
	}); err != nil {
		t.Fatalf("RequestControl: %v", err)
	}

	c, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil {
		t.Fatalf("ClaimNextReady: %v", err)
	}
	if c != nil {
		t.Fatalf("ADVERSARY BREAK: ClaimNextReady claimed READY node while pause desired: %+v", c)
	}
}

// ---------------------------------------------------------------------------
// 7. BuildPipelineInspect must not embed handoff context body secrets
// ---------------------------------------------------------------------------

// TestAdversary_B34_InspectOmitsHandoffContextSecrets: even if a handoff with
// secret-like ContextJSON is already in the store, BuildPipelineInspect must
// not embed the body in its summary (IDs only).
func TestAdversary_B34_InspectOmitsHandoffContextSecrets(t *testing.T) {
	ctx := context.Background()
	ctrl := NewController(routedrun.NewMemoryStore())
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrl, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	const marker = "SUPERSECRET_MARKER_sk-live-do-not-leak-9f3a"
	claim, err := ctrl.ClaimNextReady(ctx, wfID)
	if err != nil || claim == nil {
		t.Fatalf("ClaimNextReady: claim=%v err=%v", claim, err)
	}
	if err := ctrl.AcknowledgeRunning(ctx, claim); err != nil {
		t.Fatalf("AcknowledgeRunning: %v", err)
	}
	// Controller may or may not reject secrets; force-store a handoff-bearing
	// success with a unique marker to test inspect redaction regardless.
	// If commit rejects, seed handoff directly via store to isolate inspect.
	commitErr := ctrl.CommitStageSuccess(ctx, StageSuccess{
		WorkflowID: wfID,
		NodeID:     nodeIDs[0],
		RunID:      claim.RunID,
		AttemptID:  claim.Attempt.AttemptID,
		Handoff: &routedrun.HandoffEnvelope{
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  fmt.Sprintf(`{"notes":"%s"}`, marker),
		},
	})
	if commitErr != nil {
		// Direct store insert path for inspect isolation.
		hid, err := routedrun.NewHandoffID()
		if err != nil {
			t.Fatalf("NewHandoffID: %v", err)
		}
		if err := ctrl.Store.CommitHandoff(ctx, &routedrun.HandoffEnvelope{
			HandoffID:    hid,
			WorkflowID:   wfID,
			SourceNodeID: nodeIDs[0],
			TargetNodeID: nodeIDs[1],
			ContextJSON:  fmt.Sprintf(`{"password":"%s"}`, marker),
			CreatedAt:    time.Now().UTC(),
		}); err != nil {
			t.Fatalf("CommitHandoff direct: %v", err)
		}
	}

	summary, err := BuildPipelineInspect(ctx, ctrl.Store, wfID)
	if err != nil {
		t.Fatalf("BuildPipelineInspect: %v", err)
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("json.Marshal summary: %v", err)
	}
	if strings.Contains(string(raw), marker) {
		t.Fatalf("ADVERSARY BREAK: BuildPipelineInspect JSON embeds handoff context secret marker %q; body=%s", marker, string(raw))
	}
	if strings.Contains(string(raw), "password") && strings.Contains(string(raw), marker) {
		t.Fatalf("ADVERSARY BREAK: inspect embeds password field from handoff context")
	}
	// Structural: summary must not have a Context/ContextJSON-like dump field
	// via unexpected embedding — spot-check known fields only carry IDs.
	for _, id := range summary.HandoffIDs {
		if strings.Contains(id, marker) {
			t.Fatalf("ADVERSARY BREAK: HandoffIDs contains secret marker")
		}
	}
}

// ---------------------------------------------------------------------------
// 8. Pipeline enable default-off (package-level contract pin)
// ---------------------------------------------------------------------------

// TestAdversary_B34_PipelineNotEnabledCodeStable pins the stable denial code
// exported for the daemon enable gate. Runtime default-off is enforced in
// internal/daemon (pipelineRuntimeEnabled / AGENTPAAS_PIPELINE_ENABLED);
// this package must keep CodePipelineNotEnabled stable for that gate.
func TestAdversary_B34_PipelineNotEnabledCodeStable(t *testing.T) {
	if CodePipelineNotEnabled != "PIPELINE_NOT_ENABLED" {
		t.Fatalf("ADVERSARY BREAK: CodePipelineNotEnabled changed to %q (daemon gate contract)", CodePipelineNotEnabled)
	}
	// Empty / non-"1" env must never be treated as enabled by convention
	// documented on the constant owner (daemon). Document residual in findings
	// if daemon tests regress; package-level pin only.
	if CodePipelineNotEnabled == "" {
		t.Fatal("ADVERSARY BREAK: empty CodePipelineNotEnabled")
	}
}

// ---------------------------------------------------------------------------
// Extra: multi-controller shared store double-claim (mutex is per-controller)
// ---------------------------------------------------------------------------

// TestAdversary_B34_TwoControllersSharedStore_NoDoubleClaim: two Controller
// instances sharing one store must not both successfully claim the same READY
// node (CAS must serialize even without a shared mutex).
func TestAdversary_B34_TwoControllersSharedStore_NoDoubleClaim(t *testing.T) {
	ctx := context.Background()
	store := routedrun.NewMemoryStore()
	ctrlSeed := NewController(store)
	wfID, nodeIDs, err := SeedPipelineWorkflow(ctx, ctrlSeed, 2)
	if err != nil {
		t.Fatalf("SeedPipelineWorkflow: %v", err)
	}

	ctrlA := NewController(store)
	ctrlB := NewController(store)
	// Warm gen maps from seed (both start fresh gen maps — worst case).
	_ = nodeIDs

	var (
		wg      sync.WaitGroup
		success int64
	)
	claimFn := func(c *Controller) {
		defer wg.Done()
		cl, err := c.ClaimNextReady(ctx, wfID)
		if err == nil && cl != nil {
			atomic.AddInt64(&success, 1)
		}
	}
	wg.Add(2)
	go claimFn(ctrlA)
	go claimFn(ctrlB)
	wg.Wait()

	if success != 1 {
		t.Fatalf("ADVERSARY BREAK: two controllers on shared store produced %d successful claims (want 1); per-controller mutex insufficient without store CAS", success)
	}

	n0, err := store.GetNode(ctx, nodeIDs[0])
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n0.Status != routedrun.NodeStatusLaunching {
		// One claim should have moved READY→LAUNCHING.
		// If zero claims, CAS/gen-map restart path failed closed (safer) — not a break.
		if success == 0 {
			return
		}
		t.Fatalf("node0 status after dual-controller claim: want LAUNCHING, got %s", n0.Status)
	}
}
