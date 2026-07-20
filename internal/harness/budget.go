package harness

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

const (
	StatusBudgetExceeded = "BUDGET_EXCEEDED"

	// defaultWallClockBudget is the legacy v0.2.3 wall-clock budget fallback.
	// It is used ONLY when no TimeEnvelope is available on the durable path
	// (v0.2.3 compat) and no explicit WallClockSeconds override is set.
	// On the durable path the budget is derived from the TimeEnvelope's
	// ActiveTimeRemainingMs (B30-T03 Part B, ceiling 4).
	defaultWallClockBudget = 120 * time.Second
	defaultMaxIterations   = 10000
	defaultMaxTokens       = 100000

	wallClockBudgetCategory = "wall_clock"
	iterationBudgetCategory = "iterations"
	tokenBudgetCategory     = "tokens"
)

var ErrBudgetExceeded = errors.New("budget exceeded")

type AuditAppender interface {
	Append(record audit.AuditRecord) error
}

type BudgetConfig struct {
	WallClockSeconds int64 `json:"wall_clock_seconds,omitempty"`
	MaxIterations    int64 `json:"max_iterations,omitempty"`
	MaxTokens        int64 `json:"max_tokens,omitempty"`

	// TimeEnvelope is the authoritative active-time envelope (B30-T03 Part B,
	// ceiling 4). When present and WallClockSeconds is not explicitly set, the
	// wall-clock budget is derived from env.ActiveTimeRemainingMs(nowMs) rather
	// than the legacy 120s default. nil = legacy v0.2.3 compat path.
	TimeEnvelope *routedrun.TimeEnvelope `json:"-"`

	// NowMonotonicMs supplies the monotonic millisecond timestamp used to
	// evaluate the envelope. When nil, time.Now().UnixMilli() is used.
	NowMonotonicMs func() int64 `json:"-"`
}

type budgetExceededError struct {
	category string
	limit    int64
	observed int64
}

func (e *budgetExceededError) Error() string {
	return fmt.Sprintf("%s budget exceeded: observed %d over limit %d", e.category, e.observed, e.limit)
}

func (e *budgetExceededError) Is(target error) bool {
	return target == ErrBudgetExceeded
}

type BudgetEnforcer struct {
	cfg BudgetConfig

	runID    string
	invokeID string
	audit    AuditAppender
	now      func() time.Time

	mu        sync.Mutex
	startedAt time.Time
	exceeded  *budgetExceededError

	iterations int64
	tokens     int64
}

func NewBudgetEnforcer(cfg BudgetConfig) *BudgetEnforcer {
	return newBudgetEnforcer(cfg, "", "", nil, time.Now)
}

func newBudgetEnforcer(cfg BudgetConfig, runID, invokeID string, appender AuditAppender, now func() time.Time) *BudgetEnforcer {
	if now == nil {
		now = time.Now
	}
	return &BudgetEnforcer{
		cfg:      normalizeBudgetConfig(cfg),
		runID:    runID,
		invokeID: invokeID,
		audit:    appender,
		now:      now,
	}
}

func normalizeBudgetConfig(cfg BudgetConfig) BudgetConfig {
	// Explicit WallClockSeconds override wins over everything (policy can still
	// pin the budget). When unset and a TimeEnvelope is present, the envelope
	// drives the budget via ActiveTimeRemainingMs (computed at read time in
	// WallClockBudget), so we do NOT assign the legacy default here. Only when
	// neither override nor envelope is present do we fall back to the legacy
	// 120s defaultWallClockBudget (v0.2.3 compat).
	if cfg.WallClockSeconds <= 0 && cfg.TimeEnvelope == nil {
		cfg.WallClockSeconds = int64(defaultWallClockBudget / time.Second)
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultMaxIterations
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	if cfg.NowMonotonicMs == nil {
		cfg.NowMonotonicMs = func() int64 { return time.Now().UnixMilli() }
	}
	return cfg
}

func (b *BudgetEnforcer) Start() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.startedAt = b.now()
}

func (b *BudgetEnforcer) WallClockBudget() time.Duration {
	return time.Duration(b.WallClockBudgetMs()) * time.Millisecond
}

// WallClockBudgetMs returns the wall-clock budget in milliseconds. When a
// TimeEnvelope is attached and no explicit WallClockSeconds override is set,
// the budget is the envelope's ActiveTimeRemainingMs(nowMs) (B30-T03 Part B,
// ceiling 4). Otherwise the explicit override (or the legacy 120s default)
// applies.
func (b *BudgetEnforcer) WallClockBudgetMs() int64 {
	if b.cfg.TimeEnvelope != nil && b.cfg.WallClockSeconds <= 0 {
		return b.cfg.TimeEnvelope.ActiveTimeRemainingMs(b.cfg.NowMonotonicMs())
	}
	return b.cfg.WallClockSeconds * int64(time.Second/time.Millisecond)
}

func (b *BudgetEnforcer) Elapsed() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.startedAt.IsZero() {
		return 0
	}
	return b.now().Sub(b.startedAt)
}

func (b *BudgetEnforcer) RecordIteration() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.exceeded != nil {
		return b.exceeded
	}
	b.iterations++
	if b.iterations > b.cfg.MaxIterations {
		return b.markExceeded(iterationBudgetCategory, b.cfg.MaxIterations, b.iterations)
	}
	return nil
}

func (b *BudgetEnforcer) RecordTokens(count int64) error {
	if count < 0 {
		return fmt.Errorf("token count must be non-negative")
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.exceeded != nil {
		return b.exceeded
	}
	if count > math.MaxInt64-b.tokens {
		b.tokens = math.MaxInt64
	} else {
		b.tokens += count
	}
	if b.tokens > b.cfg.MaxTokens {
		return b.markExceeded(tokenBudgetCategory, b.cfg.MaxTokens, b.tokens)
	}
	return nil
}

func (b *BudgetEnforcer) MarkWallClockExceeded(observed time.Duration) error {
	observedMS := max(int64(observed/time.Millisecond), int64(0))
	limitMS := int64(b.WallClockBudget() / time.Millisecond)
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.markExceeded(wallClockBudgetCategory, limitMS, observedMS)
}

func (b *BudgetEnforcer) markExceeded(category string, limit, observed int64) error {
	if b.exceeded != nil {
		return b.exceeded
	}
	b.exceeded = &budgetExceededError{category: category, limit: limit, observed: observed}
	if b.audit != nil {
		payload := map[string]interface{}{
			"category":  category,
			"limit":     limit,
			"observed":  observed,
			"run_id":    b.runID,
			"invoke_id": b.invokeID,
		}
		if category == wallClockBudgetCategory {
			payload["overage_ms"] = max(observed-limit, int64(0))
		}
		if err := b.audit.Append(audit.AuditRecord{
			Timestamp:      b.now().UTC().Format(time.RFC3339Nano),
			EventType:      "budget_exceeded",
			DeploymentMode: "local",
			Actor:          "harness",
			Payload:        payload,
		}); err != nil {
			return fmt.Errorf("record budget audit event: %w", err)
		}
	}
	return b.exceeded
}

func budgetFromPayload(payload map[string]any) BudgetConfig {
	raw, ok := payload["budget"]
	if !ok {
		return BudgetConfig{}
	}
	budget, ok := raw.(map[string]any)
	if !ok {
		return BudgetConfig{}
	}
	return BudgetConfig{
		WallClockSeconds: int64Number(budget["wall_clock_seconds"]),
		MaxIterations:    int64Number(budget["max_iterations"]),
		MaxTokens:        int64Number(budget["max_tokens"]),
	}
}

func runIDFromPayload(payload map[string]any) string {
	return stringFromPayload(payload, "run_id")
}

func invokeIDFromPayload(payload map[string]any) string {
	return stringFromPayload(payload, "invoke_id")
}

func stringFromPayload(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return value
}

func int64Number(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func contextWithOptionalTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
