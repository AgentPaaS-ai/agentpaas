package supervisor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// ---------------------------------------------------------------------------
// ReferenceWorker: deterministic multi-turn research dossier worker
// ---------------------------------------------------------------------------

// ReferenceWorkerConfig holds the dependencies for a ReferenceWorker run.
type ReferenceWorkerConfig struct {
	Supervisor   *Supervisor
	AttemptID    routedrun.AttemptID
	LeaseID      routedrun.LeaseID
	ControlKey   []byte
	ArtifactRoot string

	// RunID is the run this worker belongs to (F28).
	// If empty, defaults to "run-ref-worker".
	RunID routedrun.RunID
	// WorkflowID is the workflow this worker belongs to (F28).
	// If empty, defaults to "wf-ref-worker".
	WorkflowID routedrun.WorkflowID
	// InvocationID is the invocation this worker serves (F28).
	// If empty, defaults to "inv-ref-worker".
	InvocationID routedrun.InvocationID

	// ContextBound is the maximum accumulated context in bytes (F27).
	// If zero, defaults to 65536 (64KB).
	ContextBound int
}

// RunOptions configures a Run execution.
type RunOptions struct {
	// ResumeFrom is an optional checkpoint ID. When set, the worker skips
	// all completed phases before the checkpoint and resumes from the next
	// uncommitted phase.
	ResumeFrom string

	// TargetTurns is the number of work turns to execute before finalize.
	// Zero means the default (20 work turns + 1 final = 21 total). When set
	// higher (e.g. 100), the worker repeats the read/analyze/cross-reference/
	// compile cycle until the turn count reaches the target, then finalizes.
	// The minimum effective value is 21 (the default run).
	TargetTurns int
}

// ReferenceWorker is a deterministic simulation of a durable multi-turn agent
// that exercises the supervisor's lifecycle APIs exactly as a real durable
// worker would. No LLM is called; no network is used.
type ReferenceWorker struct {
	supervisor   *Supervisor
	attemptID    routedrun.AttemptID
	leaseID      routedrun.LeaseID
	controlKey   []byte
	artifactRoot string

	// Identity fields from the claim (F28).
	runID        routedrun.RunID
	workflowID   routedrun.WorkflowID
	invocationID routedrun.InvocationID

	// Fixture set: bounded list of fake documents.
	fixtures []string

	// Internal state accumulated during the run.
	mu              sync.Mutex
	turnCount       int
	progressSeq     int64
	completedTurns  []int
	checkpointDigests []string
	checkpointIDs   []string
	artifactRefs    []string
	promptsPerTurn  map[int]string
	responsesPerTurn map[int]string
	accumulatedTokens int
	contextBound    int
	finalDigest     string
	finalResult     string
	currentCPID     string

	// Checkpoint digests by phase (1-indexed).
	phaseDigests    map[int]string
	phaseArtifactRefs map[int]string
}

// NewReferenceWorker creates a new ReferenceWorker with the given config.
func NewReferenceWorker(cfg ReferenceWorkerConfig) *ReferenceWorker {
	fixtures := []string{
		"document_0: The quick brown fox jumps over the lazy dog. System design requires careful planning.",
		"document_1: AgentPaaS is a platform for running durable AI agents. It supports checkpoints and progress tracking.",
		"document_2: Distributed systems need consensus protocols like Raft or Paxos for reliability.",
		"document_3: The supervisor pattern decouples lifecycle management from business logic in long-running processes.",
		"document_4: Cryptographic hash chains provide tamper-evident audit logs for security-sensitive operations.",
	}

	// Apply defaults for identity fields (F28).
	runID := cfg.RunID
	if runID == "" {
		runID = "run-ref-worker"
	}
	workflowID := cfg.WorkflowID
	if workflowID == "" {
		workflowID = "wf-ref-worker"
	}
	invocationID := cfg.InvocationID
	if invocationID == "" {
		invocationID = "inv-ref-worker"
	}

	// Apply default for context bound (F27).
	contextBound := cfg.ContextBound
	if contextBound == 0 {
		contextBound = 65536 // 64KB text bound
	}

	return &ReferenceWorker{
		supervisor:    cfg.Supervisor,
		attemptID:     cfg.AttemptID,
		leaseID:       cfg.LeaseID,
		controlKey:    cfg.ControlKey,
		artifactRoot:  cfg.ArtifactRoot,
		runID:         runID,
		workflowID:    workflowID,
		invocationID:  invocationID,
		fixtures:      fixtures,
		promptsPerTurn:  make(map[int]string),
		responsesPerTurn: make(map[int]string),
		phaseDigests:    make(map[int]string),
		phaseArtifactRefs: make(map[int]string),
		contextBound:   contextBound,
	}
}

// ---------------------------------------------------------------------------
// Public accessors for tests
// ---------------------------------------------------------------------------

// TurnCount returns the total number of turns executed.
func (w *ReferenceWorker) TurnCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.turnCount
}

// PromptForTurn returns the model prompt sent at the given turn (1-indexed).
func (w *ReferenceWorker) PromptForTurn(turn int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.promptsPerTurn[turn]
}

// CheckpointDigestForPhase returns the checkpoint digest for the given phase (1-indexed).
func (w *ReferenceWorker) CheckpointDigestForPhase(phase int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.phaseDigests[phase]
}

// ArtifactRefForPhase returns the artifact reference for the given phase.
func (w *ReferenceWorker) ArtifactRefForPhase(phase int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.phaseArtifactRefs[phase]
}

// ProgressSequences returns all progress event sequences emitted.
func (w *ReferenceWorker) ProgressSequences() []int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	// All sequences from 1 to progressSeq.
	seqs := make([]int64, w.progressSeq)
	for i := int64(1); i <= w.progressSeq; i++ {
		seqs[i-1] = i
	}
	return seqs
}

// CheckpointDigests returns all checkpoint digests accumulated during the run.
func (w *ReferenceWorker) CheckpointDigests() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]string, len(w.checkpointDigests))
	copy(out, w.checkpointDigests)
	return out
}

// FinalResultDigest returns the final result digest.
func (w *ReferenceWorker) FinalResultDigest() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.finalDigest
}

// FinalResultResult returns the final structured result JSON.
func (w *ReferenceWorker) FinalResultResult() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.finalResult
}

// MaxAccumulatedTokens returns the peak accumulated token count during the run.
func (w *ReferenceWorker) MaxAccumulatedTokens() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.accumulatedTokens
}

// ContextBound returns the configured context bound.
func (w *ReferenceWorker) ContextBound() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.contextBound
}

// RunID returns the run ID used by this worker (F28).
func (w *ReferenceWorker) GetRunID() routedrun.RunID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.runID
}

// GetWorkflowID returns the workflow ID used by this worker (F28).
func (w *ReferenceWorker) GetWorkflowID() routedrun.WorkflowID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.workflowID
}

// GetInvocationID returns the invocation ID used by this worker (F28).
func (w *ReferenceWorker) GetInvocationID() routedrun.InvocationID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.invocationID
}

// CompletedTurns returns the list of completed turn numbers.
func (w *ReferenceWorker) CompletedTurns() []int {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]int, len(w.completedTurns))
	copy(out, w.completedTurns)
	return out
}

// CurrentCheckpointID returns the latest checkpoint ID.
func (w *ReferenceWorker) CurrentCheckpointID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.currentCPID
}

// runPhases is exposed for the resume test to simulate partial execution.
func (w *ReferenceWorker) runPhases(ctx context.Context, opts RunOptions, fromPhase, toPhase int) error {
	return w.runPhasesInternal(ctx, fromPhase, toPhase, 0)
}

// ---------------------------------------------------------------------------
// Run executes the full deterministic sequence.
// ---------------------------------------------------------------------------

// Run executes the deterministic research dossier.
func (w *ReferenceWorker) Run(ctx context.Context, opts RunOptions) error {
	w.mu.Lock()
	w.turnCount = 0
	w.progressSeq = 0
	w.mu.Unlock()

	startPhase := 1
	if opts.ResumeFrom != "" {
		// Determine which phase to resume from.
		// The checkpoint ID encodes the phase: cp-<attemptID>-<seq>
		// Sequence after phase N is: (N*5)+1
		// Phase 1 -> seq 6, Phase 2 -> seq 11, Phase 3 -> seq 16, Phase 4 -> seq 21
		for p := 1; p <= 4; p++ {
			cpSeq := int64(p*5 + 1)
			cpID := fmt.Sprintf("cp-%s-%d", w.attemptID, cpSeq)
			if cpID == opts.ResumeFrom {
				startPhase = p + 1
				break
			}
		}
	}

	targetTurns := opts.TargetTurns
	if targetTurns <= 0 {
		targetTurns = 20 // default: 20 work turns + 1 finalize = 21 total
	}

	return w.runPhasesInternal(ctx, startPhase, 5, targetTurns)
}

// runPhasesInternal executes phases from fromPhase to toPhase inclusive.
// When targetTurns > 0 and the default phases (1-4) don't reach the target,
// the worker repeats phases 1-4 in cycles until the turn count reaches
// targetTurns, then runs the finalize phase.
func (w *ReferenceWorker) runPhasesInternal(ctx context.Context, fromPhase, toPhase int, targetTurns int) error {
	// Phase 1: Turns 1-5 -> Read fixtures (tool phase)
	if fromPhase <= 1 && toPhase >= 1 {
		if err := w.executePhase(ctx, 1, "read_fixtures", w.phase1ReadFixtures); err != nil {
			return err
		}
	}

	// Phase 2: Turns 6-10 -> Analyze (model phase)
	if fromPhase <= 2 && toPhase >= 2 {
		if err := w.executePhase(ctx, 2, "analyze", w.phase2Analyze); err != nil {
			return err
		}
	}

	// Phase 3: Turns 11-15 -> Cross-reference (tool + model)
	if fromPhase <= 3 && toPhase >= 3 {
		if err := w.executePhase(ctx, 3, "cross_reference", w.phase3CrossReference); err != nil {
			return err
		}
	}

	// Phase 4: Turns 16-20 -> Compile dossier (model phase)
	if fromPhase <= 4 && toPhase >= 4 {
		if err := w.executePhase(ctx, 4, "compile_dossier", w.phase4CompileDossier); err != nil {
			return err
		}
	}

	// If targetTurns > 20, repeat phases 1-4 in cycles until the turn count
	// reaches targetTurns.
	currentPhase := 5
	w.mu.Lock()
	currentTurns := w.turnCount
	w.mu.Unlock()

	for targetTurns > 0 && currentTurns < targetTurns {
		// Repeat phase 1-type: read_fixtures (5 turns)
		currentPhase++
		if err := w.executePhase(ctx, currentPhase, "read_fixtures", w.phaseReadFixturesGeneric); err != nil {
			return err
		}
		// Repeat phase 2-type: analyze (5 turns)
		currentPhase++
		if err := w.executePhase(ctx, currentPhase, "analyze", w.phaseAnalyzeGeneric); err != nil {
			return err
		}
		// Repeat phase 3-type: cross_reference (5 turns)
		currentPhase++
		if err := w.executePhase(ctx, currentPhase, "cross_reference", w.phaseCrossReferenceGeneric); err != nil {
			return err
		}
		// Repeat phase 4-type: compile_dossier (5 turns)
		currentPhase++
		if err := w.executePhase(ctx, currentPhase, "compile_dossier", w.phaseCompileDossierGeneric); err != nil {
			return err
		}

		w.mu.Lock()
		currentTurns = w.turnCount
		w.mu.Unlock()
	}

	// Phase finalize: 1 turn
	if fromPhase <= 5 && toPhase >= 5 {
		finalizePhase := currentPhase
		if finalizePhase <= 5 {
			finalizePhase = 5
		} else {
			finalizePhase++ // next phase after last repeat cycle
		}
		return w.executePhase(ctx, finalizePhase, "finalize", w.phaseFinalizeGeneric)
	}

	return nil
}

// executePhase runs a single phase: 5 turns (or 1 turn for finalize), then checkpoint.
func (w *ReferenceWorker) executePhase(ctx context.Context, phase int, phaseName string, phaseFn func(ctx context.Context, phase int) (string, error)) error {
	result, err := phaseFn(ctx, phase)
	if err != nil {
		return err
	}

	// Emit a heartbeat after the phase. The sequence is incremented.
	w.emitProgress(ctx, phaseName+"_complete")

	// Write artifact for this phase (except finalize which writes the final result).
	isFinalize := phaseName == "finalize"
	var artifactRef string
	if !isFinalize {
		artifactRef, err = w.writePhaseArtifact(phase, result)
		if err != nil {
			return fmt.Errorf("write phase %d artifact: %w", phase, err)
		}
	}

	// Compute checkpoint digest.
	cpDigest := w.computeDigest(result)
	w.mu.Lock()
	w.checkpointDigests = append(w.checkpointDigests, cpDigest)
	w.phaseDigests[phase] = cpDigest
	if artifactRef != "" {
		w.artifactRefs = append(w.artifactRefs, artifactRef)
		w.phaseArtifactRefs[phase] = artifactRef
	}
	w.mu.Unlock()

	// Commit checkpoint (except finalize).
	if !isFinalize {
		cpSeq := int64(phase*5 + 1)
		cpID := routedrun.CheckpointID(fmt.Sprintf("cp-%s-%d", w.attemptID, cpSeq))
		cp := &routedrun.SemanticCheckpoint{
			CheckpointID:       cpID,
			AttemptID:          w.attemptID,
			RunID:              w.runID,
			WorkflowID:         w.workflowID,
			LeaseID:            w.leaseID,
			Phase:              phaseName,
			CompletedWork:      []string{fmt.Sprintf("phase_%d_complete", phase)},
			RemainingWork:     []string{fmt.Sprintf("phase_%d_pending", phase+1)},
			ArtifactRefs:       []string{artifactRef},
			LastCommittedAction: fmt.Sprintf("phase_%d", phase),
			SafeToResume:       true,
			// B30-2: CheckpointDigest is auto-computed by SaveCheckpoint.
			// The reference worker's own content digest is tracked separately
			// in phaseDigests for cross-reference within the worker.
			Sequence:           cpSeq,
		}
		if err := w.supervisor.HandleCheckpoint(ctx, w.attemptID, w.signCheckpoint(CheckpointEvent{
			AttemptID:  w.attemptID,
			LeaseID:    w.leaseID,
			Checkpoint: cp,
		})); err != nil {
			return fmt.Errorf("checkpoint phase %d: %w", phase, err)
		}
		w.mu.Lock()
		w.currentCPID = string(cpID)
		w.checkpointIDs = append(w.checkpointIDs, string(cpID))
		w.mu.Unlock()
	}

	return nil
}

// ---------------------------------------------------------------------------
// Phase implementations
// ---------------------------------------------------------------------------

// phase1ReadFixtures: Turns 1-5. Each turn reads a fixture via tool call
// and accumulates a running summary.
func (w *ReferenceWorker) phase1ReadFixtures(ctx context.Context, _ int) (string, error) {
	var summary strings.Builder
	for i := 0; i < 5; i++ {
		turn := i + 1

		// Tool phase: read_document
		w.startGovernedOp(ctx, "http")
		docID := fmt.Sprintf("document_%d", i)
		content := w.fakeToolReadDocument(docID)
		w.recordToolResponse(turn, docID, content)
		w.endGovernedOp(ctx, "http")

		// Accumulate summary.
		fmt.Fprintf(&summary, "Read %s: %.50s...\n", docID, content)

		w.mu.Lock()
		if err := w.checkContextBoundLocked(len(content)); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += len(content)
		w.mu.Unlock()

		w.emitProgress(ctx, "reading_fixture")
	}
	return summary.String(), nil
}

// phase2Analyze: Turns 6-10. Model phase using summary from phase 1.
func (w *ReferenceWorker) phase2Analyze(ctx context.Context, phase int) (string, error) {
	var analysis strings.Builder
	phase1Digest := w.phaseDigests[1]

	for i := 0; i < 5; i++ {
		turn := 6 + i

		w.startGovernedOp(ctx, "model")
		prompt := fmt.Sprintf("Analyze documents. Prior checkpoint digest: %s. Step: %d/5",
			phase1Digest, i+1)
		response := w.fakeModelCall(turn, prompt)
		w.endGovernedOp(ctx, "model")

		w.mu.Lock()
		addition := len(prompt) + len(response)
		if err := w.checkContextBoundLocked(addition); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.promptsPerTurn[turn] = prompt
		w.responsesPerTurn[turn] = response
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += addition
		w.mu.Unlock()

		fmt.Fprintf(&analysis, "Analysis step %d: %s\n", i+1, response)
		w.emitProgress(ctx, "analyzing")
	}
	return analysis.String(), nil
}

// phase3CrossReference: Turns 11-15. Tool lookup + model synthesis.
func (w *ReferenceWorker) phase3CrossReference(ctx context.Context, phase int) (string, error) {
	var crossRef strings.Builder
	phase1Artifact := w.phaseArtifactRefs[1]

	lookupKeys := []string{"agentpaas", "consensus", "supervisor_pattern", "hash_chains", "system_design"}
	for i := 0; i < 5; i++ {
		turn := 11 + i

		// Tool phase: lookup
		w.startGovernedOp(ctx, "http")
		key := lookupKeys[i]
		lookupResult := w.fakeToolLookup(key)
		w.recordToolResponse(turn, key, lookupResult)
		w.endGovernedOp(ctx, "http")

		// Model phase: synthesize with artifact ref
		w.startGovernedOp(ctx, "model")
		prompt := fmt.Sprintf("Cross-reference lookup(%q) = %q. Prior artifact: %s. Step: %d/5",
			key, lookupResult, phase1Artifact, i+1)
		response := w.fakeModelCall(turn, prompt)
		w.endGovernedOp(ctx, "model")

		w.mu.Lock()
		addition := len(prompt) + len(response)
		if err := w.checkContextBoundLocked(addition); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.promptsPerTurn[turn] = prompt
		w.responsesPerTurn[turn] = response
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += addition
		w.mu.Unlock()

		fmt.Fprintf(&crossRef, "Cross-ref %s: %s\n", key, response)
		w.emitProgress(ctx, "cross_referencing")
	}
	return crossRef.String(), nil
}

// phase4CompileDossier: Turns 16-20. Model synthesis from all prior phases.
func (w *ReferenceWorker) phase4CompileDossier(ctx context.Context, phase int) (string, error) {
	var dossier strings.Builder
	phase3Digest := w.phaseDigests[3]

	for i := 0; i < 5; i++ {
		turn := 16 + i

		w.startGovernedOp(ctx, "model")
		prompt := fmt.Sprintf("Compile dossier section %d/5. Prior cross-reference digest: %s.",
			i+1, phase3Digest)
		response := w.fakeModelCall(turn, prompt)
		w.endGovernedOp(ctx, "model")

		w.mu.Lock()
		addition := len(prompt) + len(response)
		if err := w.checkContextBoundLocked(addition); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.promptsPerTurn[turn] = prompt
		w.responsesPerTurn[turn] = response
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += addition
		w.mu.Unlock()

		fmt.Fprintf(&dossier, "Dossier section %d: %s\n", i+1, response)
		w.emitProgress(ctx, "compiling_dossier")
	}
	return dossier.String(), nil
}

// ---------------------------------------------------------------------------
// Generic phase implementations (parameterized by phase number and turn offset)
// ---------------------------------------------------------------------------

// phaseReadFixturesGeneric reads 5 fixture documents, starting from the current
// turn count. Identical semantics to phase1ReadFixtures but with dynamic turns.
func (w *ReferenceWorker) phaseReadFixturesGeneric(ctx context.Context, _ int) (string, error) {
	w.mu.Lock()
	baseTurn := w.turnCount
	w.mu.Unlock()

	var summary strings.Builder
	for i := 0; i < 5; i++ {
		turn := baseTurn + i + 1

		// Tool phase: read_document
		w.startGovernedOp(ctx, "http")
		docID := fmt.Sprintf("document_%d", i)
		content := w.fakeToolReadDocument(docID)
		w.recordToolResponse(turn, docID, content)
		w.endGovernedOp(ctx, "http")

		// Accumulate summary.
		fmt.Fprintf(&summary, "Read %s: %.50s...\n", docID, content)

		w.mu.Lock()
		if err := w.checkContextBoundLocked(len(content)); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += len(content)
		w.mu.Unlock()

		w.emitProgress(ctx, "reading_fixture")
	}
	return summary.String(), nil
}

// phaseAnalyzeGeneric runs 5 model turns, referencing the digest from the
// preceding phase (phase-1) in the cycle.
func (w *ReferenceWorker) phaseAnalyzeGeneric(ctx context.Context, phase int) (string, error) {
	w.mu.Lock()
	baseTurn := w.turnCount
	prevDigest := w.phaseDigests[phase-1]
	w.mu.Unlock()

	var analysis strings.Builder
	for i := 0; i < 5; i++ {
		turn := baseTurn + i + 1

		w.startGovernedOp(ctx, "model")
		prompt := fmt.Sprintf("Analyze documents. Prior checkpoint digest: %s. Step: %d/5",
			prevDigest, i+1)
		response := w.fakeModelCall(turn, prompt)
		w.endGovernedOp(ctx, "model")

		w.mu.Lock()
		addition := len(prompt) + len(response)
		if err := w.checkContextBoundLocked(addition); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.promptsPerTurn[turn] = prompt
		w.responsesPerTurn[turn] = response
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += addition
		w.mu.Unlock()

		fmt.Fprintf(&analysis, "Analysis step %d: %s\n", i+1, response)
		w.emitProgress(ctx, "analyzing")
	}
	return analysis.String(), nil
}

// phaseCrossReferenceGeneric runs 5 cross-reference turns, using the artifact
// from the read_fixtures phase in the current cycle (phase-2).
func (w *ReferenceWorker) phaseCrossReferenceGeneric(ctx context.Context, phase int) (string, error) {
	w.mu.Lock()
	baseTurn := w.turnCount
	readPhaseArtifact := w.phaseArtifactRefs[phase-2]
	w.mu.Unlock()

	lookupKeys := []string{"agentpaas", "consensus", "supervisor_pattern", "hash_chains", "system_design"}
	var crossRef strings.Builder
	for i := 0; i < 5; i++ {
		turn := baseTurn + i + 1

		// Tool phase: lookup
		w.startGovernedOp(ctx, "http")
		key := lookupKeys[i]
		lookupResult := w.fakeToolLookup(key)
		w.recordToolResponse(turn, key, lookupResult)
		w.endGovernedOp(ctx, "http")

		// Model phase: synthesize with artifact ref
		w.startGovernedOp(ctx, "model")
		prompt := fmt.Sprintf("Cross-reference lookup(%q) = %q. Prior artifact: %s. Step: %d/5",
			key, lookupResult, readPhaseArtifact, i+1)
		response := w.fakeModelCall(turn, prompt)
		w.endGovernedOp(ctx, "model")

		w.mu.Lock()
		addition := len(prompt) + len(response)
		if err := w.checkContextBoundLocked(addition); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.promptsPerTurn[turn] = prompt
		w.responsesPerTurn[turn] = response
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += addition
		w.mu.Unlock()

		fmt.Fprintf(&crossRef, "Cross-ref %s: %s\n", key, response)
		w.emitProgress(ctx, "cross_referencing")
	}
	return crossRef.String(), nil
}

// phaseCompileDossierGeneric runs 5 compilation turns, referencing the digest
// from the preceding cross_reference phase (phase-1) in the cycle.
func (w *ReferenceWorker) phaseCompileDossierGeneric(ctx context.Context, phase int) (string, error) {
	w.mu.Lock()
	baseTurn := w.turnCount
	crossRefDigest := w.phaseDigests[phase-1]
	w.mu.Unlock()

	var dossier strings.Builder
	for i := 0; i < 5; i++ {
		turn := baseTurn + i + 1

		w.startGovernedOp(ctx, "model")
		prompt := fmt.Sprintf("Compile dossier section %d/5. Prior cross-reference digest: %s.",
			i+1, crossRefDigest)
		response := w.fakeModelCall(turn, prompt)
		w.endGovernedOp(ctx, "model")

		w.mu.Lock()
		addition := len(prompt) + len(response)
		if err := w.checkContextBoundLocked(addition); err != nil {
			w.mu.Unlock()
			return "", err
		}
		w.promptsPerTurn[turn] = prompt
		w.responsesPerTurn[turn] = response
		w.turnCount++
		w.completedTurns = append(w.completedTurns, turn)
		w.accumulatedTokens += addition
		w.mu.Unlock()

		fmt.Fprintf(&dossier, "Dossier section %d: %s\n", i+1, response)
		w.emitProgress(ctx, "compiling_dossier")
	}
	return dossier.String(), nil
}

// phaseFinalizeGeneric writes the final result and sends the ResultEvent.
// The phase parameter indicates which phase number this finalize corresponds to.
func (w *ReferenceWorker) phaseFinalizeGeneric(ctx context.Context, phase int) (string, error) {
	w.mu.Lock()
	turn := w.turnCount + 1
	prevDigest := w.phaseDigests[phase-1]
	totalTurns := w.turnCount
	artifacts := make([]string, len(w.artifactRefs))
	copy(artifacts, w.artifactRefs)
	// Count non-finalize phases for the result.
	phaseCount := len(w.checkpointDigests)
	w.mu.Unlock()

	w.startGovernedOp(ctx, "model")
	prompt := fmt.Sprintf("Finalize dossier using digest: %s", prevDigest)
	response := w.fakeModelCall(turn, prompt)
	w.endGovernedOp(ctx, "model")

	w.mu.Lock()
	addition := len(prompt) + len(response)
	if err := w.checkContextBoundLocked(addition); err != nil {
		w.mu.Unlock()
		return "", err
	}
	w.promptsPerTurn[turn] = prompt
	w.responsesPerTurn[turn] = response
	w.turnCount++
	w.completedTurns = append(w.completedTurns, turn)
	w.accumulatedTokens += addition
	w.mu.Unlock()

	w.emitProgress(ctx, "finalizing")

	// Build the structured result (does NOT claim "verified" or "correct").
	type dossierResult struct {
		SchemaVersion      string   `json:"schema_version"`
		TotalDocuments     int      `json:"total_documents"`
		PhasesCompleted    int      `json:"phases_completed"`
		TotalTurns         int      `json:"total_turns"`
		ArtifactReferences []string `json:"artifact_references"`
		CheckpointDigest   string   `json:"checkpoint_digest"`
		Summary            string   `json:"summary"`
	}

	result := dossierResult{
		SchemaVersion:      "1.0",
		TotalDocuments:     5,
		PhasesCompleted:    phaseCount,
		TotalTurns:         totalTurns + 1, // include finalize turn
		ArtifactReferences: artifacts,
		CheckpointDigest:   prevDigest,
		Summary:            "Research dossier complete with 5 documents analyzed across multiple phases.",
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("marshal result: %w", err)
	}
	resultJSON := string(resultBytes)

	digest := w.computeDigest(resultJSON)
	w.mu.Lock()
	w.finalDigest = digest
	w.finalResult = resultJSON
	w.mu.Unlock()

	// Write final artifact.
	artifactPath := filepath.Join(w.artifactRoot, "final_result.json")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
		return "", fmt.Errorf("mkdir final artifact dir: %w", err)
	}
	if err := os.WriteFile(artifactPath, resultBytes, 0o600); err != nil {
		return "", fmt.Errorf("write final artifact: %w", err)
	}

	// Send the terminal ResultEvent with an HMAC.
	resultEvent := ResultEvent{
		AttemptID:          w.attemptID,
		LeaseID:            w.leaseID,
		RunID:              w.runID,
		WorkflowID:         w.workflowID,
		InvocationID:       w.invocationID,
		TerminalStatus:     routedrun.InvokeJobResultSucceeded,
		StructuredResult:   resultJSON,
		ResultDigest:       digest,
		ArtifactReferences: artifacts,
	}
	resultEvent = w.signResult(resultEvent)

	if err := w.supervisor.HandleResult(ctx, w.attemptID, resultEvent); err != nil {
		return "", fmt.Errorf("handle result: %w", err)
	}

	return resultJSON, nil
}

// ---------------------------------------------------------------------------
// Fake model / tool responses (deterministic)
// ---------------------------------------------------------------------------

// fakeModelCall returns a deterministic response keyed by turn number.
func (w *ReferenceWorker) fakeModelCall(turn int, prompt string) string {
	responses := map[int]string{
		6:  `{"analysis":"Documents cover system design, platform architecture, distributed consensus, lifecycle patterns, and cryptographic integrity.","keywords":["design","platform","consensus","lifecycle","integrity"]}`,
		7:  `{"analysis":"Cross-document analysis reveals complementary themes: AgentPaaS as an instance of the supervisor pattern, with hash chains for audit integrity.","themes":["supervisor_pattern","platform"]}`,
		8:  `{"analysis":"The quick brown fox document introduces basic patterns. AgentPaaS extends them to durable execution. Consensus and hash chains provide reliability foundations.","foundations":["durability","consensus"]}`,
		9:  `{"analysis":"Supervisor pattern decouples lifecycle management. Combined with cryptographic hash chains, it creates tamper-evident durable agents.","insight":"decoupling+integrity"}`,
		10: `{"analysis":"Synthesis: platforms like AgentPaaS need (1) consensus for distributed state, (2) supervisor for lifecycle, (3) hash chains for audit. These form a layered architecture.","layers":["consensus","supervisor","audit"]}`,
		11: `{"synthesis":"agentpaas lookup confirms the platform implements supervisor-driven lifecycle management with checkpoint support.","confirmed":true}`,
		12: `{"synthesis":"consensus protocols (Raft/Paxos) enable reliable state replication. AgentPaaS uses them for distributed supervision.","confirmed":true}`,
		13: `{"synthesis":"supervisor_pattern is a well-established pattern for long-running process management. AgentPaaS applies it to AI agent lifecycle.","confirmed":true}`,
		14: `{"synthesis":"hash_chains provide tamper-evident audit trails. AgentPaaS uses SHA-256 HMAC chains for progress and result verification.","confirmed":true}`,
		15: `{"synthesis":"system_design principles inform all aspects: decoupling, redundancy, integrity. AgentPaaS architecture reflects these systematically.","confirmed":true}`,
		16: `{"dossier_section":1,"title":"Introduction","content":"This dossier analyzes five documents covering system design, AgentPaaS platform, distributed consensus, supervisor pattern, and cryptographic integrity."}`,
		17: `{"dossier_section":2,"title":"Platform Architecture","content":"AgentPaaS implements the supervisor pattern with checkpoint support, progress tracking, and HMAC-authenticated journal for durable AI agent execution."}`,
		18: `{"dossier_section":3,"title":"Distributed Foundations","content":"Consensus protocols (Raft/Paxos) combined with cryptographic hash chains provide the reliability and audit integrity foundations."}`,
		19: `{"dossier_section":4,"title":"Cross-References","content":"All documents converge on themes: decoupled lifecycle management, tamper-evident audit, and deterministic progress tracking."}`,
		20: `{"dossier_section":5,"title":"Conclusions","content":"A durable agent platform requires layered architecture: consensus layer, supervisor layer, audit layer. AgentPaaS demonstrates this architecture."}`,
		21: `{"final":"Dossier complete. All 5 documents analyzed across 4 material phases with 20 governed turns plus finalization.","status":"complete"}`,
	}
	if r, ok := responses[turn]; ok {
		return r
	}
	return fmt.Sprintf(`{"turn":%d,"status":"ok"}`, turn)
}

// fakeToolReadDocument returns the content of a fixture document.
func (w *ReferenceWorker) fakeToolReadDocument(docID string) string {
	for _, f := range w.fixtures {
		if strings.HasPrefix(f, docID+":") {
			return f
		}
	}
	return fmt.Sprintf("%s: [empty]", docID)
}

// fakeToolLookup returns a deterministic lookup result.
func (w *ReferenceWorker) fakeToolLookup(key string) string {
	lookups := map[string]string{
		"agentpaas":          "platform for durable AI agents with checkpoint and progress tracking",
		"consensus":          "Raft and Paxos protocols for distributed state agreement",
		"supervisor_pattern": "pattern for decoupling lifecycle management from business logic",
		"hash_chains":        "cryptographic hash chains for tamper-evident audit logs",
		"system_design":      "principles of decoupling, redundancy, and integrity in software architecture",
	}
	if v, ok := lookups[key]; ok {
		return v
	}
	return fmt.Sprintf("no data for %q", key)
}

// ---------------------------------------------------------------------------
// Artifact persistence
// ---------------------------------------------------------------------------

// writePhaseArtifact writes a JSON artifact for the given phase to the artifact root.
func (w *ReferenceWorker) writePhaseArtifact(phase int, content string) (string, error) {
	type phaseArtifact struct {
		SchemaVersion string `json:"schema_version"`
		Phase         int    `json:"phase"`
		Content       string `json:"content"`
		Digest        string `json:"digest"`
	}

	digest := w.computeDigest(content)
	artifact := phaseArtifact{
		SchemaVersion: "1.0",
		Phase:         phase,
		Content:       content,
		Digest:        digest,
	}

	data, err := json.Marshal(artifact)
	if err != nil {
		return "", fmt.Errorf("marshal artifact: %w", err)
	}

	filename := fmt.Sprintf("phase_%d_artifact.json", phase)
	path := filepath.Join(w.artifactRoot, filename)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("mkdir artifact dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write artifact: %w", err)
	}

	return filename, nil
}

// ---------------------------------------------------------------------------
// Helper methods
// ---------------------------------------------------------------------------

// startGovernedOp records the start of a governed operation (model/http/mcp).
func (w *ReferenceWorker) startGovernedOp(ctx context.Context, kind string) {
	switch kind {
	case "model":
		_ = w.supervisor.HandleModelStart(ctx, w.attemptID, w.leaseID)
	case "http":
		_ = w.supervisor.HandleHTTPStart(ctx, w.attemptID, w.leaseID)
	case "mcp":
		_ = w.supervisor.HandleMCPStart(ctx, w.attemptID, w.leaseID)
	}
}

// endGovernedOp records the end of a governed operation.
func (w *ReferenceWorker) endGovernedOp(ctx context.Context, kind string) {
	switch kind {
	case "model":
		_ = w.supervisor.HandleModelEnd(ctx, w.attemptID, w.leaseID)
	case "http":
		_ = w.supervisor.HandleHTTPEnd(ctx, w.attemptID, w.leaseID)
	case "mcp":
		_ = w.supervisor.HandleMCPEnd(ctx, w.attemptID, w.leaseID)
	}
}

// emitProgress sends an authenticated progress heartbeat to the supervisor.
func (w *ReferenceWorker) emitProgress(ctx context.Context, phase string) {
	w.mu.Lock()
	w.progressSeq++
	seq := w.progressSeq
	w.mu.Unlock()

	p := ProgressEvent{
		AttemptID: w.attemptID,
		LeaseID:   w.leaseID,
		Sequence:  seq,
		Timestamp: time.Now().UTC(),
		Phase:     phase,
	}
	p = w.signProgress(p)
	_ = w.supervisor.TrackProgress(ctx, w.attemptID, p)
}

// signProgress signs a ProgressEvent with the HMAC key.
func (w *ReferenceWorker) signProgress(p ProgressEvent) ProgressEvent {
	mac := hmac.New(sha256.New, w.controlKey)
	mac.Write(canonicalProgressBytes(p))
	p.HMAC = hex.EncodeToString(mac.Sum(nil))
	return p
}

// signResult signs a ResultEvent with the HMAC key.
func (w *ReferenceWorker) signResult(r ResultEvent) ResultEvent {
	mac := hmac.New(sha256.New, w.controlKey)
	mac.Write(canonicalResultBytes(r))
	r.HMAC = hex.EncodeToString(mac.Sum(nil))
	return r
}

// signCheckpoint signs a CheckpointEvent with the HMAC key.
func (w *ReferenceWorker) signCheckpoint(c CheckpointEvent) CheckpointEvent {
	mac := hmac.New(sha256.New, w.controlKey)
	mac.Write(canonicalCheckpointBytes(c))
	c.HMAC = hex.EncodeToString(mac.Sum(nil))
	return c
}

// computeDigest returns a deterministic hex digest for a string.
func (w *ReferenceWorker) computeDigest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// checkContextBound returns ErrContextBoundExceeded if adding additional
// tokens would exceed the configured context bound (F27). Must be called
// while w.mu is held.
func (w *ReferenceWorker) checkContextBoundLocked(additional int) error {
	if w.accumulatedTokens+additional > w.contextBound {
		return fmt.Errorf("%w: %d+%d > %d", ErrContextBoundExceeded, w.accumulatedTokens, additional, w.contextBound)
	}
	return nil
}

// recordToolResponse stores a tool response for test inspection.
func (w *ReferenceWorker) recordToolResponse(turn int, key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.promptsPerTurn[turn] = fmt.Sprintf("tool:%s", key)
	w.responsesPerTurn[turn] = value
}

