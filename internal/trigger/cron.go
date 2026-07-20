package trigger

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

// CronSchedule defines a cron trigger.
type CronSchedule struct {
	// Expr is the 5-field cron expression.
	Expr string
	// ScheduleID is the unique identifier for this schedule.
	ScheduleID string
	// AgentName is the target agent to invoke.
	AgentName string
	// AgentVersion is the target agent version (optional).
	AgentVersion string
	// Payload is the fixed payload bytes for the cron invoke.
	Payload []byte
	// ContentType is the payload content type.
	ContentType string
	// Timezone is the timezone (empty = local).
	Timezone string
	// MissedRunPolicy is "skip" (default) or "catchup".
	MissedRunPolicy string
	// ConcurrencyPolicy is "forbid" (default).
	ConcurrencyPolicy string
	// IdempotencyKey is an optional idempotency key prefix.
	IdempotencyKey string
}

// CronConfig configures the cron scheduler.
type CronConfig struct {
	// Schedules is the list of cron schedules to manage.
	Schedules []*CronSchedule
	// StatePath is an optional path to a JSON file for schedule persistence.
	StatePath string
	// Audit is the audit appender for cron events.
	Audit audit.AuditAppender
	// TriggerService is the service to invoke when cron fires.
	TriggerService *TriggerService
}

type cronInvokeFunc func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error)

// CronScheduler manages cron-triggered invocations.
type CronScheduler struct {
	mu         sync.Mutex
	schedules  []*CronSchedule
	statePath  string
	audit      audit.AuditAppender
	triggerSvc *TriggerService
	invoke     cronInvokeFunc
	activeRuns map[string]bool
	ticker     *time.Ticker
	stopCh     chan struct{}
	stopOnce   sync.Once
	lastFire   map[string]time.Time
}

// NewCronScheduler creates a new cron scheduler.
func NewCronScheduler(cfg CronConfig) *CronScheduler {
	if cfg.Audit == nil {
		cfg.Audit = noOpCronAuditAppender{}
	}
	for _, schedule := range cfg.Schedules {
		if schedule.MissedRunPolicy == "" {
			schedule.MissedRunPolicy = "skip"
		}
		if schedule.ConcurrencyPolicy == "" {
			schedule.ConcurrencyPolicy = "forbid"
		}
	}

	invoke := cronInvokeFunc(func(context.Context, *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
		return nil, fmt.Errorf("trigger service not configured")
	})
	if cfg.TriggerService != nil {
		invoke = cfg.TriggerService.Invoke
	}

	cs := &CronScheduler{
		schedules:  cfg.Schedules,
		statePath:  cfg.StatePath,
		audit:      cfg.Audit,
		triggerSvc: cfg.TriggerService,
		invoke:     invoke,
		activeRuns: make(map[string]bool),
		lastFire:   make(map[string]time.Time),
		stopCh:     make(chan struct{}),
	}

	if cfg.StatePath != "" {
		if loaded, err := cs.loadSchedules(); err != nil || !loaded {
			if err := cs.persistSchedules(); err != nil {
				log.Printf("trigger: persistSchedules failed: %v", err)
			}
		}
	}

	return cs
}

// Start begins the cron scheduler. It checks every minute on the minute.
func (cs *CronScheduler) Start() {
	go cs.run()
}

// Stop stops the cron scheduler.
func (cs *CronScheduler) Stop() {
	cs.mu.Lock()
	if cs.ticker != nil {
		cs.ticker.Stop()
	}
	cs.mu.Unlock()

	cs.stopOnce.Do(func() {
		close(cs.stopCh)
	})
}

func (cs *CronScheduler) run() {
	timer := time.NewTimer(cs.nextTickInterval())
	defer timer.Stop()

	select {
	case <-cs.stopCh:
		return
	case now := <-timer.C:
		cs.tick(now)
	}

	cs.mu.Lock()
	cs.ticker = time.NewTicker(time.Minute)
	ticker := cs.ticker
	cs.mu.Unlock()

	for {
		select {
		case <-cs.stopCh:
			return
		case now := <-ticker.C:
			cs.tick(now)
		}
	}
}

func (cs *CronScheduler) tick(now time.Time) {
	cs.mu.Lock()
	schedules := append([]*CronSchedule(nil), cs.schedules...)
	cs.mu.Unlock()

	for _, schedule := range schedules {
		loc := time.Local
		if schedule.Timezone != "" {
			loaded, err := time.LoadLocation(schedule.Timezone)
			if err != nil {
				continue
			}
			loc = loaded
		}

		localNow := now.In(loc)
		if cs.shouldFire(schedule, localNow) {
			cs.fire(schedule, localNow)
		}
	}
}

func (cs *CronScheduler) shouldFire(schedule *CronSchedule, now time.Time) bool {
	parsed, err := ParseCron(schedule.Expr)
	if err != nil {
		return false
	}
	if !parsed.Match(now) {
		return false
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	last, ok := cs.lastFire[scheduleKey(schedule)]
	return !ok || !sameWallMinute(last, now)
}

func (cs *CronScheduler) fire(schedule *CronSchedule, now time.Time) {
	cs.mu.Lock()
	cs.lastFire[scheduleKey(schedule)] = now
	if schedule.ConcurrencyPolicy == "forbid" && cs.activeRuns[schedule.AgentName] {
		cs.mu.Unlock()
		cs.auditCronSkipped(schedule, "concurrency_forbid")
		return
	}
	cs.activeRuns[schedule.AgentName] = true
	cs.mu.Unlock()

	go func() {
		defer func() {
			cs.mu.Lock()
			delete(cs.activeRuns, schedule.AgentName)
			cs.mu.Unlock()
		}()

		callerID := CallerID("system:cron:" + schedule.AgentName)
		ctx := context.WithValue(context.Background(), callerKey{}, callerID)
		req := &triggerv1.InvokeRequest{
			AgentName:      schedule.AgentName,
			AgentVersion:   schedule.AgentVersion,
			Payload:        schedule.Payload,
			ContentType:    schedule.ContentType,
			IdempotencyKey: cronIdempotencyKey(schedule, now),
		}
		if _, err := cs.invoke(ctx, req); err != nil {
			cs.auditCronMissed(schedule, fmt.Sprintf("invoke error: %v", err))
		}
	}()
}

func (cs *CronScheduler) nextTickInterval() time.Duration {
	now := time.Now()
	next := now.Truncate(time.Minute).Add(time.Minute)
	return next.Sub(now)
}

func (cs *CronScheduler) auditCronMissed(schedule *CronSchedule, reason string) {
	if err := cs.audit.Append(audit.AuditRecord{
		EventType:      "cron_missed",
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:cron:" + schedule.AgentName,
		Payload: map[string]interface{}{
			"agent_name": schedule.AgentName,
			"cron_expr":  schedule.Expr,
			"reason":     reason,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", "cron_missed", err)
	}
}

func (cs *CronScheduler) auditCronSkipped(schedule *CronSchedule, reason string) {
	if err := cs.audit.Append(audit.AuditRecord{
		EventType:      "cron_skipped_concurrency",
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:cron:" + schedule.AgentName,
		Payload: map[string]interface{}{
			"agent_name": schedule.AgentName,
			"cron_expr":  schedule.Expr,
			"reason":     reason,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", "cron_skipped_concurrency", err)
	}
}

type noOpCronAuditAppender struct{}

func (noOpCronAuditAppender) Append(audit.AuditRecord) error {
	return nil
}

// CronExpr is a parsed 5-field cron expression.
type CronExpr struct {
	Minute     CronField
	Hour       CronField
	DayOfMonth CronField
	Month      CronField
	DayOfWeek  CronField
}

// CronField represents a parsed cron field.
type CronField struct {
	Values []int
}

// Match checks if the field matches the given value.
func (f CronField) Match(v int) bool {
	for _, value := range f.Values {
		if value == v {
			return true
		}
	}
	return false
}

// ParseCron parses a 5-field cron expression.
func ParseCron(expr string) (*CronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must have 5 fields, got %d", len(fields))
	}

	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return nil, fmt.Errorf("minute field: %w", err)
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return nil, fmt.Errorf("hour field: %w", err)
	}
	dayOfMonth, err := parseCronField(fields[2], 1, 31)
	if err != nil {
		return nil, fmt.Errorf("day-of-month field: %w", err)
	}
	month, err := parseCronField(fields[3], 1, 12)
	if err != nil {
		return nil, fmt.Errorf("month field: %w", err)
	}
	dayOfWeek, err := parseCronField(fields[4], 0, 6)
	if err != nil {
		return nil, fmt.Errorf("day-of-week field: %w", err)
	}

	return &CronExpr{
		Minute:     minute,
		Hour:       hour,
		DayOfMonth: dayOfMonth,
		Month:      month,
		DayOfWeek:  dayOfWeek,
	}, nil
}

func parseCronField(field string, minValue, maxValue int) (CronField, error) {
	if field == "" {
		return CronField{}, fmt.Errorf("field is empty")
	}

	values := make([]int, 0)
	for _, part := range strings.Split(field, ",") {
		parsed, err := parseCronPart(part, minValue, maxValue)
		if err != nil {
			return CronField{}, fmt.Errorf("parse cron field: %w", err)
		}
		values = append(values, parsed...)
	}

	seen := make(map[int]bool, len(values))
	unique := make([]int, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		unique = append(unique, value)
	}
	sort.Ints(unique)

	return CronField{Values: unique}, nil
}

func parseCronPart(part string, minValue, maxValue int) ([]int, error) {
	if part == "" {
		return nil, fmt.Errorf("field part is empty")
	}

	step := 1
	if before, after, ok := strings.Cut(part, "/"); ok {
		if after == "" {
			return nil, fmt.Errorf("step is empty")
		}
		parsedStep, err := strconv.Atoi(after)
		if err != nil {
			return nil, fmt.Errorf("invalid step %q: %w", after, err)
		}
		if parsedStep <= 0 {
			return nil, fmt.Errorf("step must be positive, got %d", parsedStep)
		}
		step = parsedStep
		part = before
		if part == "" {
			return nil, fmt.Errorf("step range is empty")
		}
	}

	start, end, err := parseCronBounds(part, minValue, maxValue)
	if err != nil {
		return nil, fmt.Errorf("parse cron part: %w", err)
	}

	values := make([]int, 0, ((end-start)/step)+1)
	for value := start; value <= end; value += step {
		values = append(values, value)
	}
	return values, nil
}

func parseCronBounds(part string, minValue, maxValue int) (int, int, error) {
	if part == "*" {
		return minValue, maxValue, nil
	}

	if startText, endText, ok := strings.Cut(part, "-"); ok {
		if startText == "" || endText == "" {
			return 0, 0, fmt.Errorf("range boundary is empty")
		}
		start, err := strconv.Atoi(startText)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range start %q: %w", startText, err)
		}
		end, err := strconv.Atoi(endText)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid range end %q: %w", endText, err)
		}
		if start < minValue || start > maxValue {
			return 0, 0, fmt.Errorf("range start %d out of bounds [%d, %d]", start, minValue, maxValue)
		}
		if end < minValue || end > maxValue {
			return 0, 0, fmt.Errorf("range end %d out of bounds [%d, %d]", end, minValue, maxValue)
		}
		if start > end {
			return 0, 0, fmt.Errorf("range start %d > end %d", start, end)
		}
		return start, end, nil
	}

	value, err := strconv.Atoi(part)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid value %q: %w", part, err)
	}
	if value < minValue || value > maxValue {
		return 0, 0, fmt.Errorf("value %d out of bounds [%d, %d]", value, minValue, maxValue)
	}
	return value, value, nil
}

// Match checks if the cron expression matches the given time.
func (e *CronExpr) Match(t time.Time) bool {
	return e.Minute.Match(t.Minute()) &&
		e.Hour.Match(t.Hour()) &&
		e.DayOfMonth.Match(t.Day()) &&
		e.Month.Match(int(t.Month())) &&
		e.DayOfWeek.Match(int(t.Weekday()))
}

// IsDSTNonexistentTime checks if a time falls in a DST gap.
func IsDSTNonexistentTime(t time.Time) bool {
	loc := t.Location()
	if loc == nil {
		return false
	}

	before := t.Add(-time.Minute).In(loc)
	after := t.Add(time.Minute).In(loc)
	return after.Sub(before) > 2*time.Minute
}

// IsDSTRepeatedTime checks if a time falls in a DST overlap.
func IsDSTRepeatedTime(t time.Time) bool {
	loc := t.Location()
	if loc == nil {
		return false
	}

	_, offset := t.Zone()
	_, offsetHourLater := t.Add(time.Hour).In(loc).Zone()
	return offsetHourLater < offset
}

func scheduleKey(schedule *CronSchedule) string {
	if schedule.ScheduleID != "" {
		return schedule.ScheduleID
	}
	return schedule.Expr + ":" + schedule.AgentName + ":" + schedule.AgentVersion
}

func sameWallMinute(a, b time.Time) bool {
	return a.Location().String() == b.Location().String() &&
		a.Year() == b.Year() &&
		a.Month() == b.Month() &&
		a.Day() == b.Day() &&
		a.Hour() == b.Hour() &&
		a.Minute() == b.Minute()
}

func cronIdempotencyKey(schedule *CronSchedule, now time.Time) string {
	if schedule.IdempotencyKey == "" {
		return ""
	}
	return schedule.IdempotencyKey + ":" + now.UTC().Format("200601021504")
}
