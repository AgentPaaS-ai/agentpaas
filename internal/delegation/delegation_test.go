package delegation

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helper factories
// ---------------------------------------------------------------------------

func makeTaskID() TaskID {
	id, _ := NewTaskID()
	return id
}

func makeResultID() ResultID {
	id, _ := NewResultID()
	return id
}

func makeEventID() EventID {
	id, _ := NewEventID()
	return id
}

func makeValidTask() Task {
	now := time.Now().UTC()
	tid, _ := NewTaskID()
	return Task{
		SchemaVersion:                   CurrentSchemaVersion,
		TaskID:                          tid,
		WorkflowID:                      "wf-test",
		TenantID:                        "tenant-test",
		Caller:                          CallerRef{DeploymentID: "dep-caller", RunID: "run-caller", AttemptID: "at-caller", PackageName: "weather-agent", PackageDigest: "sha256:abc"},
		Callee:                          CalleeRef{DeploymentID: "dep-callee", PackageName: "report-verifier", PackageVersion: "1.0.0", PackageDigest: "sha256:def"},
		BindingID:                       "bind-1",
		Capability:                      "report.verify",
		Status:                          TaskStatusPending,
		Generation:                      0,
		IdempotencyKey:                  "idem-test-1",
		CallerIdentity:                  "caller-id-1",
		CommunicationSnapshotGeneration: 1,
		MaxActiveDurationMs:             60000,
		MaxCostUsdDecimal:               "0.50",
		CreatedAt:                       now,
		UpdatedAt:                       now,
	}
}

func makeValidMessage(taskID TaskID, seq int64) Message {
	mid, _ := NewMessageID()
	return Message{
		SchemaVersion:      CurrentSchemaVersion,
		MessageID:          mid,
		TaskID:             taskID,
		WorkflowID:         "wf-test",
		TenantID:           "tenant-test",
		Sequence:           seq,
		Role:               RoleUser,
		SenderLogicalID:    "weather-agent",
		RecipientLogicalID: "report-verifier",
		Parts: []MessagePart{
			{Kind: PartKindText, Text: "Hello, please verify.", MediaType: "text/plain"},
		},
		ContentDigest:  "sha256:placeholder",
		ByteSize:       100,
		Classification: ClassificationInternal,
		CreatedAt:      time.Now().UTC(),
	}
}

func makeValidResult(taskID TaskID) Result {
	rid, _ := NewResultID()
	return Result{
		SchemaVersion: CurrentSchemaVersion,
		ResultID:      rid,
		TaskID:        taskID,
		WorkflowID:    "wf-test",
		Status:        TaskStatusSucceeded,
		ContentDigest: "sha256:placeholder",
		CreatedAt:     time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// 1. All status JSON round-trips
// ---------------------------------------------------------------------------

func TestTaskStatus_JSONRoundTrip(t *testing.T) {
	for _, s := range AllTaskStatuses() {
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal %s: %v", s, err)
		}
		var got TaskStatus
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", s, err)
		}
		if got != s {
			t.Errorf("round-trip: got %s, want %s", got, s)
		}
	}
}

func TestMessageRole_JSONRoundTrip(t *testing.T) {
	for _, r := range AllMessageRoles() {
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal %s: %v", r, err)
		}
		var got MessageRole
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", r, err)
		}
		if got != r {
			t.Errorf("round-trip: got %s, want %s", got, r)
		}
	}
}

func TestPartKind_JSONRoundTrip(t *testing.T) {
	for _, k := range AllPartKinds() {
		b, err := json.Marshal(k)
		if err != nil {
			t.Fatalf("marshal %s: %v", k, err)
		}
		var got PartKind
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", k, err)
		}
		if got != k {
			t.Errorf("round-trip: got %s, want %s", got, k)
		}
	}
}

func TestEventType_JSONRoundTrip(t *testing.T) {
	for _, et := range AllEventTypes() {
		b, err := json.Marshal(et)
		if err != nil {
			t.Fatalf("marshal %s: %v", et, err)
		}
		var got EventType
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", et, err)
		}
		if got != et {
			t.Errorf("round-trip: got %s, want %s", got, et)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. Full transition matrix (legal + illegal)
// ---------------------------------------------------------------------------

func TestTaskTransitions_Legal(t *testing.T) {
	legal := []struct{ from, to TaskStatus }{
		{TaskStatusPending, TaskStatusAdmitted},
		{TaskStatusPending, TaskStatusDenied},
		{TaskStatusPending, TaskStatusCancelled},
		{TaskStatusPending, TaskStatusExpired},
		{TaskStatusAdmitted, TaskStatusRunning},
		{TaskStatusAdmitted, TaskStatusCancelled},
		{TaskStatusAdmitted, TaskStatusExpired},
		{TaskStatusAdmitted, TaskStatusDenied},
		{TaskStatusRunning, TaskStatusSucceeded},
		{TaskStatusRunning, TaskStatusFailed},
		{TaskStatusRunning, TaskStatusCancelled},
		{TaskStatusRunning, TaskStatusExpired},
	}
	for _, tc := range legal {
		if err := ValidateTaskTransition(tc.from, tc.to); err != nil {
			t.Errorf("expected legal transition %s -> %s, got error: %v", tc.from, tc.to, err)
		}
	}
}

func TestTaskTransitions_Illegal(t *testing.T) {
	illegal := []struct{ from, to TaskStatus }{
		{TaskStatusSucceeded, TaskStatusRunning},
		{TaskStatusFailed, TaskStatusRunning},
		{TaskStatusCancelled, TaskStatusRunning},
		{TaskStatusExpired, TaskStatusRunning},
		{TaskStatusDenied, TaskStatusRunning},
		{TaskStatusSucceeded, TaskStatusFailed},
		{TaskStatusRunning, TaskStatusPending},
		{TaskStatusAdmitted, TaskStatusPending},
		{TaskStatusPending, TaskStatusSucceeded},
		{TaskStatusPending, TaskStatusRunning},
	}
	for _, tc := range illegal {
		if err := ValidateTaskTransition(tc.from, tc.to); err == nil {
			t.Errorf("expected illegal transition %s -> %s, got nil", tc.from, tc.to)
		}
	}
}

func TestTaskTransitions_DuplicateTerminal(t *testing.T) {
	// Duplicate transition to same terminal is idempotent success.
	if err := ValidateTaskTransition(TaskStatusSucceeded, TaskStatusSucceeded); err != nil {
		t.Errorf("expected duplicate terminal transition Succeeded->Succeeded to be ok, got: %v", err)
	}
	if err := ValidateTaskTransition(TaskStatusFailed, TaskStatusFailed); err != nil {
		t.Errorf("expected duplicate terminal transition Failed->Failed to be ok, got: %v", err)
	}
	if err := ValidateTaskTransition(TaskStatusDenied, TaskStatusDenied); err != nil {
		t.Errorf("expected duplicate terminal transition Denied->Denied to be ok, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 3. Terminal is terminal
// ---------------------------------------------------------------------------

func TestIsTerminal(t *testing.T) {
	terminal := []TaskStatus{
		TaskStatusSucceeded, TaskStatusFailed, TaskStatusCancelled,
		TaskStatusExpired, TaskStatusDenied,
	}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("expected %s to be terminal", s)
		}
	}
	nonTerminal := []TaskStatus{
		TaskStatusPending, TaskStatusAdmitted, TaskStatusRunning,
	}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("expected %s to be non-terminal", s)
		}
	}
}

// ---------------------------------------------------------------------------
// 4. Message content validation
// ---------------------------------------------------------------------------

func TestValidateMessage_RejectsControlChars(t *testing.T) {
	tid := makeTaskID()
	m := makeValidMessage(tid, 1)
	m.Parts = []MessagePart{
		{Kind: PartKindText, Text: "Hello\x00World", MediaType: "text/plain"},
	}
	if err := ValidateMessage(&m); err == nil {
		t.Error("expected error for control chars in text")
	}
}

func TestValidateMessage_RejectsSecretSentinels(t *testing.T) {
	tid := makeTaskID()
	tests := []string{
		"sk-abc123",
		"sk-or-abc123",
		"Bearer token123",
		"BEGIN PRIVATE KEY",
		"journal-key",
		"journal_key",
	}
	for _, sentinel := range tests {
		m := makeValidMessage(tid, 1)
		m.Parts = []MessagePart{
			{Kind: PartKindText, Text: "Hello " + sentinel, MediaType: "text/plain"},
		}
		if err := ValidateMessage(&m); err == nil {
			t.Errorf("expected error for secret sentinel %q", sentinel)
		}
	}
}

func TestValidateMessage_RejectsAbsoluteArtifactPath(t *testing.T) {
	tid := makeTaskID()
	m := makeValidMessage(tid, 1)
	m.Parts = []MessagePart{
		{Kind: PartKindArtifactRef, ArtifactRef: "/etc/passwd", MediaType: "application/octet-stream"},
	}
	if err := ValidateMessage(&m); err == nil {
		t.Error("expected error for absolute artifact_ref path")
	}
}

func TestValidateMessage_RejectsDotDotPath(t *testing.T) {
	tid := makeTaskID()
	m := makeValidMessage(tid, 1)
	m.Parts = []MessagePart{
		{Kind: PartKindArtifactRef, ArtifactRef: "../sensitive", MediaType: "application/octet-stream"},
	}
	if err := ValidateMessage(&m); err == nil {
		t.Error("expected error for .. in artifact_ref path")
	}
}

func TestValidateMessage_RejectsOversizedParts(t *testing.T) {
	tid := makeTaskID()
	m := makeValidMessage(tid, 1)
	// Create a text part that exceeds 64 KiB.
	big := strings.Repeat("x", maxTextPartBytes+1)
	m.Parts = []MessagePart{
		{Kind: PartKindText, Text: big, MediaType: "text/plain"},
	}
	if err := ValidateMessage(&m); err == nil {
		t.Error("expected error for oversized text part")
	}
}

func TestValidateMessage_RejectsTooManyParts(t *testing.T) {
	tid := makeTaskID()
	m := makeValidMessage(tid, 1)
	parts := make([]MessagePart, maxParts+1)
	for i := range parts {
		parts[i] = MessagePart{Kind: PartKindText, Text: "x", MediaType: "text/plain"}
	}
	m.Parts = parts
	if err := ValidateMessage(&m); err == nil {
		t.Error("expected error for too many parts")
	}
}

func TestValidateMessage_RejectsForbiddenPartKind(t *testing.T) {
	tid := makeTaskID()
	m := makeValidMessage(tid, 1)
	m.Parts = []MessagePart{
		{Kind: PartKind("hidden_reasoning"), Text: "secret", MediaType: "text/plain"},
	}
	if err := ValidateMessage(&m); err == nil {
		t.Error("expected error for hidden_reasoning part kind")
	}

	m2 := makeValidMessage(tid, 1)
	m2.Parts = []MessagePart{
		{Kind: PartKind("provider_continuation"), Text: "secret", MediaType: "text/plain"},
	}
	if err := ValidateMessage(&m2); err == nil {
		t.Error("expected error for provider_continuation part kind")
	}
}

// ---------------------------------------------------------------------------
// 5. ValidateTask
// ---------------------------------------------------------------------------

func TestValidateTask_RejectsEmptyIdempotency(t *testing.T) {
	task := makeValidTask()
	task.IdempotencyKey = ""
	if err := ValidateTask(&task); err == nil {
		t.Error("expected error for empty idempotency_key")
	}
}

func TestValidateTask_RejectsEmptyBinding(t *testing.T) {
	task := makeValidTask()
	task.BindingID = ""
	if err := ValidateTask(&task); err == nil {
		t.Error("expected error for empty binding_id")
	}
}

func TestValidateTask_RejectsInvalidStatus(t *testing.T) {
	task := makeValidTask()
	task.Status = TaskStatus(999)
	if err := ValidateTask(&task); err == nil {
		t.Error("expected error for invalid status")
	}
}

func TestValidateTask_RejectsEndpointLike(t *testing.T) {
	// Tasks don't have endpoints, but we test the scan doesn't false-positive
	// on valid fields.
	task := makeValidTask()
	if err := ValidateTask(&task); err != nil {
		t.Errorf("unexpected error for valid task: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 6. MemoryStore: CreateTask idempotent replay
// ---------------------------------------------------------------------------

func TestMemoryStore_CreateTask_IdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("first CreateTask: %v", err)
	}

	// Same key + same body -> idempotent success.
	task2 := task // Same fields
	if err := store.CreateTask(ctx, task2); err != nil {
		t.Errorf("idempotent replay should succeed: %v", err)
	}
}

func TestMemoryStore_CreateTask_IdempotencyConflict(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("first CreateTask: %v", err)
	}

	// Same key, different body -> conflict.
	task2 := makeValidTask()
	task2.CallerIdentity = task.CallerIdentity
	task2.IdempotencyKey = task.IdempotencyKey
	task2.Capability = "different.cap"
	if err := store.CreateTask(ctx, task2); err == nil {
		t.Error("expected idempotency conflict error")
	} else {
		ve, ok := err.(*ValidationError)
		if !ok || ve.Message != ErrIdempotencyConflict {
			t.Errorf("expected ERR_IDEMPOTENCY_CONFLICT, got: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. MemoryStore: AppendMessage enforces contiguous sequences
// ---------------------------------------------------------------------------

func TestMemoryStore_AppendMessage_SequenceGap(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	msg1 := makeValidMessage(task.TaskID, 1)
	if err := store.AppendMessage(ctx, msg1); err != nil {
		t.Fatalf("AppendMessage seq=1: %v", err)
	}

	// Skip sequence 2 -> gap.
	msg3 := makeValidMessage(task.TaskID, 3)
	if err := store.AppendMessage(ctx, msg3); err == nil {
		t.Error("expected sequence gap error")
	} else {
		ve, ok := err.(*ValidationError)
		if !ok || ve.Message != ErrSequenceGap {
			t.Errorf("expected ERR_SEQUENCE_GAP, got: %v", err)
		}
	}
}

func TestMemoryStore_AppendMessage_HappyPath(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	msg1 := makeValidMessage(task.TaskID, 1)
	if err := store.AppendMessage(ctx, msg1); err != nil {
		t.Fatalf("AppendMessage seq=1: %v", err)
	}

	msg2 := makeValidMessage(task.TaskID, 2)
	if err := store.AppendMessage(ctx, msg2); err != nil {
		t.Fatalf("AppendMessage seq=2: %v", err)
	}

	msgs, err := store.ListMessages(ctx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// 8. PutResult only when task terminal and status matches
// ---------------------------------------------------------------------------

func TestMemoryStore_PutResult_NonTerminalFails(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusPending
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded
	if err := store.PutResult(ctx, result); err == nil {
		t.Error("expected error: task is not terminal")
	}
}

func TestMemoryStore_PutResult_StatusMismatchFails(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusFailed
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded // mismatch
	if err := store.PutResult(ctx, result); err == nil {
		t.Error("expected error: status mismatch")
	}
}

func TestMemoryStore_PutResult_Success(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusSucceeded
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded
	if err := store.PutResult(ctx, result); err != nil {
		t.Fatalf("PutResult: %v", err)
	}

	got, err := store.GetResult(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetResult: %v", err)
	}
	if got.ResultID != result.ResultID {
		t.Errorf("result ID mismatch: got %s, want %s", got.ResultID, result.ResultID)
	}
}

func TestMemoryStore_PutResult_SecondDifferentFails(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusSucceeded
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result1 := makeValidResult(task.TaskID)
	result1.Status = TaskStatusSucceeded
	if err := store.PutResult(ctx, result1); err != nil {
		t.Fatalf("PutResult: %v", err)
	}

	result2 := makeValidResult(task.TaskID)
	result2.Status = TaskStatusSucceeded
	result2.ResultID = makeResultID() // Different ID
	if err := store.PutResult(ctx, result2); err == nil {
		t.Error("expected error: second different result")
	}
}

func TestMemoryStore_PutResult_SameResultIdempotent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	task.Status = TaskStatusSucceeded
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	result := makeValidResult(task.TaskID)
	result.Status = TaskStatusSucceeded
	if err := store.PutResult(ctx, result); err != nil {
		t.Fatalf("PutResult first: %v", err)
	}
	if err := store.PutResult(ctx, result); err != nil {
		t.Errorf("PutResult second (same) should be idempotent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 9. Events
// ---------------------------------------------------------------------------

func TestMemoryStore_AppendEvent(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	eid, _ := NewEventID()
	ev := TaskEvent{
		EventID:    eid,
		TaskID:     task.TaskID,
		WorkflowID: "wf-test",
		TenantID:   "tenant-test",
		Type:       EventTaskAdmitted,
		CreatedAt:  time.Now().UTC(),
	}

	seq, err := store.AppendEvent(ctx, ev)
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected seq=1, got %d", seq)
	}

	// Second event.
	ev2 := TaskEvent{
		EventID:    makeEventID(),
		TaskID:     task.TaskID,
		WorkflowID: "wf-test",
		TenantID:   "tenant-test",
		Type:       EventTaskStarted,
		CreatedAt:  time.Now().UTC(),
	}
	seq2, err := store.AppendEvent(ctx, ev2)
	if err != nil {
		t.Fatalf("AppendEvent 2: %v", err)
	}
	if seq2 != 2 {
		t.Errorf("expected seq=2, got %d", seq2)
	}

	evts, err := store.ListEvents(ctx, task.TaskID, 0)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evts) != 2 {
		t.Errorf("expected 2 events, got %d", len(evts))
	}
}

// ---------------------------------------------------------------------------
// 10. Fixture golden: load testdata/*.json and Validate
// ---------------------------------------------------------------------------

func TestGolden_ValidTask(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "valid_task.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if err := ValidateTask(&task); err != nil {
		t.Errorf("ValidateTask: %v", err)
	}
}

func TestGolden_ValidMessage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "valid_message.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if err := ValidateMessage(&msg); err != nil {
		t.Errorf("ValidateMessage: %v", err)
	}
}

func TestGolden_DeniedTask(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "denied_task.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if err := ValidateTask(&task); err != nil {
		t.Errorf("ValidateTask: %v", err)
	}
	if task.Status != TaskStatusDenied {
		t.Errorf("expected status DENIED, got %s", task.Status)
	}
	if task.DenialReason != DenyCallerBinding {
		t.Errorf("expected denial reason DENY_CALLER_BINDING, got %s", task.DenialReason)
	}
}

func TestGolden_ValidArtifactRef(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "valid_artifact_ref.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var ref TransferableArtifactRef
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatalf("unmarshal artifact ref: %v", err)
	}
	if err := ValidateTransferableArtifactRef(&ref); err != nil {
		t.Errorf("ValidateTransferableArtifactRef: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 11. ValidateResult
// ---------------------------------------------------------------------------

func TestValidateResult_NonTerminalFails(t *testing.T) {
	r := Result{
		SchemaVersion: CurrentSchemaVersion,
		ResultID:      makeResultID(),
		TaskID:        makeTaskID(),
		WorkflowID:    "wf-test",
		Status:        TaskStatusPending,
		ContentDigest: "sha256:abc",
		CreatedAt:     time.Now().UTC(),
	}
	if err := ValidateResult(&r); err == nil {
		t.Error("expected error for non-terminal result status")
	}
}

func TestValidateResult_ErrorMessageNoSecrets(t *testing.T) {
	r := Result{
		SchemaVersion: CurrentSchemaVersion,
		ResultID:      makeResultID(),
		TaskID:        makeTaskID(),
		WorkflowID:    "wf-test",
		Status:        TaskStatusFailed,
		ErrorCode:     "ERR_SOMETHING",
		ErrorMessage:  "Bearer token leaked",
		ContentDigest: "sha256:abc",
		CreatedAt:     time.Now().UTC(),
	}
	if err := ValidateResult(&r); err == nil {
		t.Error("expected error for secret sentinel in error_message")
	}
}

// ---------------------------------------------------------------------------
// 12. ValidateSystemRole
// ---------------------------------------------------------------------------

func TestValidateSystemRole_AgentRejected(t *testing.T) {
	if err := ValidateSystemRole(RoleSystem, WriterAgent); err == nil {
		t.Error("expected error: system role from agent writer")
	}
}

func TestValidateSystemRole_RuntimeAllowed(t *testing.T) {
	if err := ValidateSystemRole(RoleSystem, WriterRuntime); err != nil {
		t.Errorf("unexpected error: system role from runtime writer: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 13. Digest tests
// ---------------------------------------------------------------------------

func TestCanonicalMessagePartsDigest(t *testing.T) {
	parts := []MessagePart{
		{Kind: PartKindText, Text: "hello", MediaType: "text/plain"},
		{Kind: PartKindJSON, JSON: `{"key":"value"}`, MediaType: "application/json"},
	}
	digest, err := CanonicalMessagePartsDigest(parts)
	if err != nil {
		t.Fatalf("CanonicalMessagePartsDigest: %v", err)
	}
	if digest == "" {
		t.Error("expected non-empty digest")
	}
	// Deterministic: same input should produce same digest.
	digest2, err := CanonicalMessagePartsDigest(parts)
	if err != nil {
		t.Fatalf("CanonicalMessagePartsDigest 2: %v", err)
	}
	if digest != digest2 {
		t.Error("expected deterministic digest")
	}
}

func TestCanonicalResultDigest(t *testing.T) {
	r := makeValidResult(makeTaskID())
	digest, err := CanonicalResultDigest(&r)
	if err != nil {
		t.Fatalf("CanonicalResultDigest: %v", err)
	}
	if digest == "" {
		t.Error("expected non-empty digest")
	}
}

// ---------------------------------------------------------------------------
// 14. ID generation and validation
// ---------------------------------------------------------------------------

func TestTaskID_Validate(t *testing.T) {
	id, err := NewTaskID()
	if err != nil {
		t.Fatalf("NewTaskID: %v", err)
	}
	if !id.Validate() {
		t.Error("expected TaskID to validate")
	}
	if TaskID("").Validate() {
		t.Error("empty TaskID should not validate")
	}
	if TaskID("bad-prefix").Validate() {
		t.Error("wrong prefix should not validate")
	}
}

func TestMessageID_Validate(t *testing.T) {
	id, err := NewMessageID()
	if err != nil {
		t.Fatalf("NewMessageID: %v", err)
	}
	if !id.Validate() {
		t.Error("expected MessageID to validate")
	}
}

func TestResultID_Validate(t *testing.T) {
	id, err := NewResultID()
	if err != nil {
		t.Fatalf("NewResultID: %v", err)
	}
	if !id.Validate() {
		t.Error("expected ResultID to validate")
	}
}

func TestEventID_Validate(t *testing.T) {
	id, err := NewEventID()
	if err != nil {
		t.Fatalf("NewEventID: %v", err)
	}
	if !id.Validate() {
		t.Error("expected EventID to validate")
	}
}

// ---------------------------------------------------------------------------
// 15. Error codes
// ---------------------------------------------------------------------------

func TestValidErrorCode(t *testing.T) {
	for _, code := range AllErrorCodes() {
		if !ValidErrorCode(code) {
			t.Errorf("expected %q to be valid", code)
		}
	}
	if ValidErrorCode("NOT_A_REAL_CODE") {
		t.Error("expected unknown code to be invalid")
	}
}

// ---------------------------------------------------------------------------
// 16. Classification
// ---------------------------------------------------------------------------

func TestClassification_Valid(t *testing.T) {
	for _, c := range AllClassifications() {
		if !c.Valid() {
			t.Errorf("expected %q to be valid", c)
		}
	}
	if Classification("secret").Valid() {
		t.Error("expected 'secret' to be invalid")
	}
}

// ---------------------------------------------------------------------------
// 17. ValidateTaskTransitionString
// ---------------------------------------------------------------------------

func TestValidateTaskTransitionString(t *testing.T) {
	if err := ValidateTaskTransitionString("PENDING", "ADMITTED"); err != nil {
		t.Errorf("expected valid transition: %v", err)
	}
	if err := ValidateTaskTransitionString("SUCCEEDED", "RUNNING"); err == nil {
		t.Error("expected illegal transition")
	}
	if err := ValidateTaskTransitionString("INVALID", "PENDING"); err == nil {
		t.Error("expected unknown status error")
	}
}

// ---------------------------------------------------------------------------
// 18. ValidateTaskEvent
// ---------------------------------------------------------------------------

func TestValidateTaskEvent(t *testing.T) {
	eid, _ := NewEventID()
	tid, _ := NewTaskID()
	ev := TaskEvent{
		EventID:    eid,
		TaskID:     tid,
		WorkflowID: "wf-test",
		TenantID:   "tenant-test",
		Sequence:   1,
		Type:       EventTaskAdmitted,
		CreatedAt:  time.Now().UTC(),
	}
	if err := ValidateTaskEvent(&ev); err != nil {
		t.Errorf("ValidateTaskEvent: %v", err)
	}
}

func TestValidateTaskEvent_InvalidSequence(t *testing.T) {
	eid, _ := NewEventID()
	tid, _ := NewTaskID()
	ev := TaskEvent{
		EventID:    eid,
		TaskID:     tid,
		WorkflowID: "wf-test",
		TenantID:   "tenant-test",
		Sequence:   0,
		Type:       EventTaskAdmitted,
		CreatedAt:  time.Now().UTC(),
	}
	if err := ValidateTaskEvent(&ev); err == nil {
		t.Error("expected error for sequence < 1")
	}
}

// ---------------------------------------------------------------------------
// 19. CASTask
// ---------------------------------------------------------------------------

func TestMemoryStore_CASTask(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Update with correct generation.
	task.Generation = 0
	task.Status = TaskStatusAdmitted
	if err := store.CASTask(ctx, task, 0); err != nil {
		t.Fatalf("CASTask: %v", err)
	}

	got, err := store.GetTask(ctx, task.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Generation != 1 {
		t.Errorf("expected generation 1, got %d", got.Generation)
	}
	if got.Status != TaskStatusAdmitted {
		t.Errorf("expected ADMITTED, got %s", got.Status)
	}
}

func TestMemoryStore_CASTask_Conflict(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	task := makeValidTask()
	if err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// First CAS succeeds.
	task2 := task
	task2.Status = TaskStatusAdmitted
	if err := store.CASTask(ctx, task2, 0); err != nil {
		t.Fatalf("CASTask first: %v", err)
	}

	// Second CAS with stale generation fails.
	task3 := task
	task3.Status = TaskStatusRunning
	if err := store.CASTask(ctx, task3, 0); err == nil {
		t.Error("expected CAS conflict")
	}
}

// ---------------------------------------------------------------------------
// 20. TransferableArtifactRef validation
// ---------------------------------------------------------------------------

func TestValidateTransferableArtifactRef_EmptyAudience(t *testing.T) {
	ref := TransferableArtifactRef{
		ArtifactID:       "artif-001",
		Digest:           "sha256:abc",
		WorkflowID:       "wf-test",
		ProducerRunID:    "run-1",
		ProducerTaskID:   "task-1",
		MediaType:        "application/json",
		ByteSize:         1024,
		Classification:   ClassificationInternal,
		Audience:         nil,
		ExpiresAt:        time.Now().UTC(),
		LogicalRef:       "output.json",
	}
	if err := ValidateTransferableArtifactRef(&ref); err == nil {
		t.Error("expected error for empty audience")
	}
}

func TestValidateTransferableArtifactRef_URLInArtifactID(t *testing.T) {
	ref := TransferableArtifactRef{
		ArtifactID:       "https://evil.com/artifact",
		Digest:           "sha256:abc",
		WorkflowID:       "wf-test",
		ProducerRunID:    "run-1",
		ProducerTaskID:   "task-1",
		MediaType:        "application/json",
		ByteSize:         1024,
		Classification:   ClassificationInternal,
		Audience:         []string{"reader"},
		ExpiresAt:        time.Now().UTC(),
		LogicalRef:       "output.json",
	}
	if err := ValidateTransferableArtifactRef(&ref); err == nil {
		t.Error("expected error for URL in artifact_id")
	}
}