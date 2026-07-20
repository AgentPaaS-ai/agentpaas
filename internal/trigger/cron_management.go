package trigger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// cronScheduleState is the JSON-serializable schedule representation for
// state persistence. It omits the Payload field to keep the file compact.
type cronScheduleState struct {
	ScheduleID        string `json:"schedule_id"`
	Expr              string `json:"expr"`
	AgentName         string `json:"agent_name"`
	AgentVersion      string `json:"agent_version"`
	ContentType       string `json:"content_type"`
	Timezone          string `json:"timezone"`
	MissedRunPolicy   string `json:"missed_run_policy"`
	ConcurrencyPolicy string `json:"concurrency_policy"`
	Payload           []byte `json:"payload,omitempty"`
	IdempotencyKey    string `json:"idempotency_key"`
}

func toState(s *CronSchedule) cronScheduleState {
	return cronScheduleState{
		ScheduleID:        s.ScheduleID,
		Expr:              s.Expr,
		AgentName:         s.AgentName,
		AgentVersion:      s.AgentVersion,
		ContentType:       s.ContentType,
		Timezone:          s.Timezone,
		MissedRunPolicy:   s.MissedRunPolicy,
		ConcurrencyPolicy: s.ConcurrencyPolicy,
		Payload:           s.Payload,
		IdempotencyKey:    s.IdempotencyKey,
	}
}

func fromState(st cronScheduleState) *CronSchedule {
	return &CronSchedule{
		ScheduleID:        st.ScheduleID,
		Expr:              st.Expr,
		AgentName:         st.AgentName,
		AgentVersion:      st.AgentVersion,
		ContentType:       st.ContentType,
		Timezone:          st.Timezone,
		MissedRunPolicy:   st.MissedRunPolicy,
		ConcurrencyPolicy: st.ConcurrencyPolicy,
		Payload:           st.Payload,
		IdempotencyKey:    st.IdempotencyKey,
	}
}

// AddSchedule adds a schedule to the scheduler at runtime.
// It validates the cron expression, generates a ScheduleID if one is not
// provided, and persists the updated list when StatePath is configured.
func (cs *CronScheduler) AddSchedule(ctx context.Context, schedule *CronSchedule) (string, error) {
	if _, err := ParseCron(schedule.Expr); err != nil {
		return "", status.Error(codes.InvalidArgument, fmt.Sprintf("invalid cron expression: %v", err))
	}

	if schedule.ScheduleID == "" {
		id, err := generateScheduleID()
		if err != nil {
			return "", fmt.Errorf("generate schedule ID: %w", err)
		}
		schedule.ScheduleID = id
	}

	if schedule.MissedRunPolicy == "" {
		schedule.MissedRunPolicy = "skip"
	}
	if schedule.ConcurrencyPolicy == "" {
		schedule.ConcurrencyPolicy = "forbid"
	}

	cs.mu.Lock()
	for _, existing := range cs.schedules {
		if existing.ScheduleID == schedule.ScheduleID {
			cs.mu.Unlock()
			return "", status.Errorf(codes.AlreadyExists, "schedule %q already exists", schedule.ScheduleID)
		}
	}
	cs.schedules = append(cs.schedules, schedule)
	cs.mu.Unlock()

	if cs.statePath != "" {
		if err := cs.persistSchedules(); err != nil {
			log.Printf("trigger: persistSchedules failed: %v", err)
		}
	}

	return schedule.ScheduleID, nil
}

// ListSchedules returns a copy of all currently registered schedules.
func (cs *CronScheduler) ListSchedules() []*CronSchedule {
	cs.mu.Lock()
	schedules := append([]*CronSchedule(nil), cs.schedules...)
	cs.mu.Unlock()
	return schedules
}

// RemoveSchedule removes a schedule by its ScheduleID.
// Returns a NotFound error if no schedule with the given ID exists.
func (cs *CronScheduler) RemoveSchedule(ctx context.Context, scheduleID string) error {
	cs.mu.Lock()
	idx := -1
	for i, schedule := range cs.schedules {
		if schedule.ScheduleID == scheduleID {
			idx = i
			break
		}
	}
	if idx < 0 {
		cs.mu.Unlock()
		return status.Errorf(codes.NotFound, "schedule %q not found", scheduleID)
	}
	cs.schedules = append(cs.schedules[:idx], cs.schedules[idx+1:]...)
	cs.mu.Unlock()

	if cs.statePath != "" {
		if err := cs.persistSchedules(); err != nil {
			log.Printf("trigger: persistSchedules failed: %v", err)
		}
	}

	return nil
}

// persistSchedules writes the current schedules to statePath atomically.
// It copies the slice under the mutex, then marshals and writes outside the
// lock to avoid holding the mutex during I/O.
func (cs *CronScheduler) persistSchedules() error {
	cs.mu.Lock()
	schedules := append([]*CronSchedule(nil), cs.schedules...)
	cs.mu.Unlock()

	states := make([]cronScheduleState, 0, len(schedules))
	for _, s := range schedules {
		states = append(states, toState(s))
	}

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cron schedules: %w", err)
	}

	tmpPath := cs.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write cron state: %w", err)
	}
	if err := os.Rename(tmpPath, cs.statePath); err != nil {
		return fmt.Errorf("rename cron state: %w", err)
	}
	return nil
}

// loadSchedules reads schedules from statePath. It returns (true, nil) if
// the file was loaded, (false, nil) if the file does not exist, and
// (false, error) for any other problem. Callers must not hold cs.mu.
func (cs *CronScheduler) loadSchedules() (bool, error) {
	data, err := os.ReadFile(cs.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read cron state: %w", err)
	}

	var states []cronScheduleState
	if err := json.Unmarshal(data, &states); err != nil {
		return false, fmt.Errorf("unmarshal cron state: %w", err)
	}

	schedules := make([]*CronSchedule, 0, len(states))
	for _, st := range states {
		schedules = append(schedules, fromState(st))
	}
	cs.schedules = schedules
	return true, nil
}

// generateScheduleID creates a random 16-character hex string from 8 bytes
// of crypto/rand entropy.
func generateScheduleID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
