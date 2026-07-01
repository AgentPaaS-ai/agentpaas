package trigger

import (
	"context"
	"path/filepath"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCronScheduler_AddSchedule_Success(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{})
	ctx := context.Background()

	schedule := &CronSchedule{
		Expr:      "*/5 * * * *",
		AgentName: "test-agent",
	}
	id, err := cs.AddSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("AddSchedule returned error: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ScheduleID")
	}
	if schedule.ScheduleID != id {
		t.Fatalf("ScheduleID = %q, want %q", schedule.ScheduleID, id)
	}

	schedules := cs.ListSchedules()
	if len(schedules) != 1 {
		t.Fatalf("ListSchedules returned %d schedules, want 1", len(schedules))
	}
}

func TestCronScheduler_AddSchedule_InvalidExpr(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{})
	ctx := context.Background()

	schedule := &CronSchedule{
		Expr:      "not-a-cron-expr",
		AgentName: "test-agent",
	}
	_, err := cs.AddSchedule(ctx, schedule)
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error code = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
}

func TestCronScheduler_AddSchedule_DuplicateID(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{})
	ctx := context.Background()

	schedule1 := &CronSchedule{
		ScheduleID: "dup-id",
		Expr:       "*/5 * * * *",
		AgentName:  "agent-1",
	}
	id1, err := cs.AddSchedule(ctx, schedule1)
	if err != nil {
		t.Fatalf("first AddSchedule returned error: %v", err)
	}
	if id1 != "dup-id" {
		t.Fatalf("expected ScheduleID %q, got %q", "dup-id", id1)
	}

	schedule2 := &CronSchedule{
		ScheduleID: "dup-id",
		Expr:       "*/10 * * * *",
		AgentName:  "agent-2",
	}
	_, err = cs.AddSchedule(ctx, schedule2)
	if err == nil {
		t.Fatal("expected error for duplicate ScheduleID")
	}
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("error code = %v, want %v", status.Code(err), codes.AlreadyExists)
	}
}

func TestCronScheduler_ListSchedules(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		s := &CronSchedule{
			Expr:      "*/5 * * * *",
			AgentName: "agent-" + string(rune('a'+i)),
		}
		if _, err := cs.AddSchedule(ctx, s); err != nil {
			t.Fatalf("AddSchedule %d returned error: %v", i, err)
		}
	}

	schedules := cs.ListSchedules()
	if len(schedules) != 3 {
		t.Fatalf("ListSchedules returned %d schedules, want 3", len(schedules))
	}
}

func TestCronScheduler_RemoveSchedule_Success(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{})
	ctx := context.Background()

	schedule := &CronSchedule{
		ScheduleID: "remove-me",
		Expr:       "*/5 * * * *",
		AgentName:  "test-agent",
	}
	id, err := cs.AddSchedule(ctx, schedule)
	if err != nil {
		t.Fatalf("AddSchedule returned error: %v", err)
	}

	schedules := cs.ListSchedules()
	if len(schedules) != 1 {
		t.Fatalf("ListSchedules before remove: %d, want 1", len(schedules))
	}

	if err := cs.RemoveSchedule(ctx, id); err != nil {
		t.Fatalf("RemoveSchedule returned error: %v", err)
	}

	schedules = cs.ListSchedules()
	if len(schedules) != 0 {
		t.Fatalf("ListSchedules after remove: %d, want 0", len(schedules))
	}
}

func TestCronScheduler_RemoveSchedule_NotFound(t *testing.T) {
	t.Parallel()

	cs := NewCronScheduler(CronConfig{})
	ctx := context.Background()

	err := cs.RemoveSchedule(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent schedule")
	}
	if status.Code(err) != codes.NotFound {
		t.Fatalf("error code = %v, want %v", status.Code(err), codes.NotFound)
	}
}

func TestCronScheduler_PersistAndLoad(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "cron-state.json")

	// Create first scheduler, add 2 schedules.
	cs1 := NewCronScheduler(CronConfig{
		StatePath: statePath,
	})
	ctx := context.Background()

	s1 := &CronSchedule{
		Expr:      "*/5 * * * *",
		AgentName: "agent-a",
	}
	id1, err := cs1.AddSchedule(ctx, s1)
	if err != nil {
		t.Fatalf("AddSchedule agent-a returned error: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty ScheduleID for agent-a")
	}

	s2 := &CronSchedule{
		Expr:      "0 9 * * *",
		AgentName: "agent-b",
	}
	id2, err := cs1.AddSchedule(ctx, s2)
	if err != nil {
		t.Fatalf("AddSchedule agent-b returned error: %v", err)
	}
	if id2 == "" {
		t.Fatal("expected non-empty ScheduleID for agent-b")
	}

	// Create a new scheduler pointing at the same state file.
	cs2 := NewCronScheduler(CronConfig{
		StatePath: statePath,
	})

	schedules := cs2.ListSchedules()
	if len(schedules) != 2 {
		t.Fatalf("ListSchedules after reload: %d, want 2", len(schedules))
	}

	found := map[string]bool{}
	for _, s := range schedules {
		found[s.AgentName] = true
	}
	if !found["agent-a"] || !found["agent-b"] {
		t.Fatalf("expected agent-a and agent-b in loaded schedules, got %v", found)
	}
}
