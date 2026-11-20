package delegation

import "context"

// Store is the pluggable interface for durable task delegation state.
// The MemoryStore implementation is suitable for unit tests.
type Store interface {
	// CreateTask persists a new task. Idempotent on caller_identity+idempotency_key.
	// Returns ERR_IDEMPOTENCY_CONFLICT if the same key exists with a different body.
	CreateTask(ctx context.Context, t Task) error

	// GetTask returns the task by ID, or an error if not found.
	GetTask(ctx context.Context, taskID TaskID) (*Task, error)

	// GetTaskByIdempotencyKey returns the task by caller identity and idempotency key,
	// or nil if not found.
	GetTaskByIdempotencyKey(ctx context.Context, callerIdentity, idempotencyKey string) (*Task, error)

	// CASTask updates a task atomically. Succeeds only if the current
	// generation matches expectedGen. Returns the new generation on success.
	CASTask(ctx context.Context, t Task, expectedGen int64) error

	// AppendMessage appends a message to a task, enforcing contiguous
	// sequence numbers (no gaps). Returns ERR_SEQUENCE_GAP if the sequence
	// is not lastSeq+1.
	AppendMessage(ctx context.Context, msg Message) error

	// ListMessages returns messages for a task with sequence > afterSeq,
	// ordered by sequence ascending.
	ListMessages(ctx context.Context, taskID TaskID, afterSeq int64) ([]Message, error)

	// PutResult stores the terminal result for a task. Only succeeds once
	// per task and the result status must match the task's terminal status.
	PutResult(ctx context.Context, r Result) error

	// GetResult returns the result for a task, or an error if not found.
	GetResult(ctx context.Context, taskID TaskID) (*Result, error)

	// AppendEvent appends an event to a task's event stream. Returns the
	// assigned sequence number.
	AppendEvent(ctx context.Context, ev TaskEvent) (int64, error)

	// ListEvents returns events for a task with sequence > afterSeq,
	// ordered by sequence ascending.
	ListEvents(ctx context.Context, taskID TaskID, afterSeq int64) ([]TaskEvent, error)

	// SubscribeEvents returns a channel of events for a task, replaying
	// existing events with sequence > afterSeq before delivering new events.
	// The channel is closed when the context is cancelled or the task's
	// event stream is closed (terminal event delivered). The returned cancel
	// function unsubscribes the channel; it is safe to call more than once.
	SubscribeEvents(ctx context.Context, taskID TaskID, afterSeq int64) (<-chan TaskEvent, func(), error)
}