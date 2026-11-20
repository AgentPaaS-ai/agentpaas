package mcpmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Bounds constants (B33-T06)
// ---------------------------------------------------------------------------

const (
	// MaxRequestBytes is the maximum MCP request body size (256 KiB).
	MaxRequestBytes = 256 << 10

	// MaxResponseBytes is the maximum MCP response body size (1 MiB).
	// Mirrors maxBodySize (legacy compat constant kept).
	MaxResponseBytes = 1 << 20

	// MaxJSONDepth bounds JSON nesting depth to prevent stack overflow attacks.
	MaxJSONDepth = 32

	// DefaultMaxConcurrentMCPCalls is the default per-caller concurrent MCP
	// call bound when no explicit limit is configured.
	DefaultMaxConcurrentMCPCalls = 8
)

// ---------------------------------------------------------------------------
// Error codes (B33-T06)
// ---------------------------------------------------------------------------

const (
	ErrCodeOverloaded   = "mcp_overloaded"
	ErrCodeBodyTooLarge = "mcp_body_too_large"
	ErrCodeDepthTooDeep = "mcp_depth_too_deep"
)

// ---------------------------------------------------------------------------
// Size / depth validation
// ---------------------------------------------------------------------------

// CheckRequestSize validates raw JSON request body is within MaxRequestBytes.
// Returns nil when ok, or a typed error when too large.
func CheckRequestSize(body []byte) error {
	if len(body) > MaxRequestBytes {
		return newTypedError(ErrCodeBodyTooLarge,
			fmt.Sprintf("request body %d bytes exceeds %d byte limit", len(body), MaxRequestBytes))
	}
	return nil
}

// CheckResponseSize validates response body is within MaxResponseBytes.
func CheckResponseSize(body []byte) error {
	if len(body) > MaxResponseBytes {
		return newTypedError(ErrCodeBodyTooLarge,
			fmt.Sprintf("response body %d bytes exceeds %d byte limit", len(body), MaxResponseBytes))
	}
	return nil
}

// CheckJSONDepth walks a parsed json.RawMessage and returns an error if any
// nesting exceeds MaxJSONDepth. A top-level scalar has depth 0; an object/array
// adds one level.
func CheckJSONDepth(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	return checkJSONDepthDecoder(dec, 0)
}

func checkJSONDepthDecoder(dec *json.Decoder, depth int) error {
	if depth > MaxJSONDepth {
		return newTypedError(ErrCodeDepthTooDeep,
			fmt.Sprintf("JSON depth %d exceeds %d limit", depth, MaxJSONDepth))
	}
	t, err := dec.Token()
	if err != nil {
		return nil // end of input or malformed JSON (handled by json.Unmarshal elsewhere)
	}
	switch t {
	case json.Delim('{'), json.Delim('['):
		for dec.More() {
			if err := checkJSONDepthDecoder(dec, depth+1); err != nil {
				return err
			}
		}
		// Consume closing delimiter.
		if _, err := dec.Token(); err != nil {
			return nil
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Concurrency semaphore (per-service call gate)
// ---------------------------------------------------------------------------

// CallSemaphore is a bounded concurrency gate that rejects calls when at
// capacity. It does NOT queue — calls either take a slot or are rejected
// with ErrCodeOverloaded.
type CallSemaphore struct {
	mu       sync.Mutex
	capacity int
	used     int
}

// NewCallSemaphore creates a semaphore with the given capacity.
// Capacity <= 0 means unlimited.
func NewCallSemaphore(capacity int) *CallSemaphore {
	if capacity <= 0 {
		capacity = 0 // treat 0 as unlimited
	}
	return &CallSemaphore{capacity: capacity}
}

// Capacity returns the configured maximum.
func (s *CallSemaphore) Capacity() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capacity
}

// Acquire takes a slot. Returns a release function on success, or an error
// when at capacity. Caller MUST call release when done.
func (s *CallSemaphore) Acquire() (release func(), err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.capacity == 0 {
		// Unlimited.
		return func() {}, nil
	}
	if s.used >= s.capacity {
		return nil, newTypedError(ErrCodeOverloaded,
			fmt.Sprintf("concurrency limit %d reached", s.capacity))
	}
	s.used++
	return func() { s.release() }, nil
}

func (s *CallSemaphore) release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.used > 0 {
		s.used--
	}
}

// Used returns the current number of held slots.
func (s *CallSemaphore) Used() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.used
}

// ---------------------------------------------------------------------------
// Cancellation tracking per service
// ---------------------------------------------------------------------------

// CancelTracker tracks in-flight call cancel functions for a service binding.
// When Fence or lease revoke occurs, it cancels all in-flight calls.
type CancelTracker struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc // keyed by call identifier
	nextID  int64
}

// NewCancelTracker creates a new cancel tracker.
func NewCancelTracker() *CancelTracker {
	return &CancelTracker{cancels: make(map[string]context.CancelFunc)}
}

// Register stores a cancel function and returns a unique call ID.
// The caller MUST call Unregister when the call completes.
func (t *CancelTracker) Register(cancel context.CancelFunc) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nextID++
	id := fmt.Sprintf("mcp-call-%d", t.nextID)
	t.cancels[id] = cancel
	return id
}

// Unregister removes a call from tracking. Safe to call multiple times.
func (t *CancelTracker) Unregister(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.cancels, id)
}

// CancelAll cancels every tracked in-flight call and clears the tracker.
func (t *CancelTracker) CancelAll() {
	t.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(t.cancels))
	for id, cancel := range t.cancels {
		cancels = append(cancels, cancel)
		delete(t.cancels, id)
	}
	t.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// Active returns the number of currently tracked calls.
func (t *CancelTracker) Active() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.cancels)
}

// ---------------------------------------------------------------------------
// Effective deadline computation
// ---------------------------------------------------------------------------

// EffectiveCallDeadline computes the minimum of the provided deadline sources.
// Each source is optional (zero value = unset/not applicable). Returns the
// smallest non-zero deadline, or zero if all are zero/unset.
func EffectiveCallDeadline(now time.Time, sources ...time.Time) time.Time {
	var best time.Time
	for _, s := range sources {
		if s.IsZero() {
			continue
		}
		if best.IsZero() || s.Before(best) {
			best = s
		}
	}
	return best
}
