package harness

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

const (
	StatusBudgetExceeded = "BUDGET_EXCEEDED"

	defaultWallClockBudget = 30 * time.Second
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
	if cfg.WallClockSeconds <= 0 {
		cfg.WallClockSeconds = int64(defaultWallClockBudget / time.Second)
	}
	if cfg.MaxIterations <= 0 {
		cfg.MaxIterations = defaultMaxIterations
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = defaultMaxTokens
	}
	return cfg
}

func (b *BudgetEnforcer) Start() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.startedAt = b.now()
}

func (b *BudgetEnforcer) WallClockBudget() time.Duration {
	return time.Duration(b.cfg.WallClockSeconds) * time.Second
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
