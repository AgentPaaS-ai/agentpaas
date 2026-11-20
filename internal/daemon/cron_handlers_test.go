package daemon

import (
	"context"
	"testing"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/trigger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCronAdd_RequiresAgentName(t *testing.T) {
	s := &controlServer{}
	_, err := s.CronAdd(context.Background(), &controlv1.CronAddRequest{
		Expr: "*/5 * * * *",
	})
	if err == nil {
		t.Fatal("CronAdd() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CronAdd() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestCronAdd_RequiresExpr(t *testing.T) {
	s := &controlServer{}
	_, err := s.CronAdd(context.Background(), &controlv1.CronAddRequest{
		AgentName: "test-agent",
	})
	if err == nil {
		t.Fatal("CronAdd() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CronAdd() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestCronAdd_NoScheduler(t *testing.T) {
	s := &controlServer{}
	_, err := s.CronAdd(context.Background(), &controlv1.CronAddRequest{
		AgentName: "test-agent",
		Expr:      "*/5 * * * *",
	})
	if err == nil {
		t.Fatal("CronAdd() error = nil, want FailedPrecondition")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CronAdd() code = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestCronList_NoScheduler(t *testing.T) {
	s := &controlServer{}
	_, err := s.CronList(context.Background(), &controlv1.CronListRequest{})
	if err == nil {
		t.Fatal("CronList() error = nil, want FailedPrecondition")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CronList() code = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestCronRemove_RequiresScheduleId(t *testing.T) {
	s := &controlServer{}
	_, err := s.CronRemove(context.Background(), &controlv1.CronRemoveRequest{})
	if err == nil {
		t.Fatal("CronRemove() error = nil, want InvalidArgument")
	}
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("CronRemove() code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestCronRemove_NoScheduler(t *testing.T) {
	s := &controlServer{}
	_, err := s.CronRemove(context.Background(), &controlv1.CronRemoveRequest{
		ScheduleId: "sched-123",
	})
	if err == nil {
		t.Fatal("CronRemove() error = nil, want FailedPrecondition")
	}
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CronRemove() code = %v, want FailedPrecondition", status.Code(err))
	}
}

func TestCronAdd_List_Remove_Success(t *testing.T) {
	cs := trigger.NewCronScheduler(trigger.CronConfig{})
	server := &controlServer{cronScheduler: cs}

	// Add a schedule.
	resp, err := server.CronAdd(context.Background(), &controlv1.CronAddRequest{
		AgentName: "test-agent",
		Expr:      "*/5 * * * *",
	})
	if err != nil {
		t.Fatalf("CronAdd() unexpected error: %v", err)
	}
	if resp.GetSchedule() == nil {
		t.Fatal("CronAdd() response schedule is nil")
	}
	scheduleID := resp.GetSchedule().GetScheduleId()
	if scheduleID == "" {
		t.Fatal("CronAdd() returned empty schedule_id")
	}

	// List should contain the schedule.
	listResp, err := server.CronList(context.Background(), &controlv1.CronListRequest{})
	if err != nil {
		t.Fatalf("CronList() unexpected error: %v", err)
	}
	if len(listResp.GetSchedules()) != 1 {
		t.Fatalf("CronList() schedules count = %d, want 1", len(listResp.GetSchedules()))
	}

	// Remove the schedule.
	removeResp, err := server.CronRemove(context.Background(), &controlv1.CronRemoveRequest{
		ScheduleId: scheduleID,
	})
	if err != nil {
		t.Fatalf("CronRemove() unexpected error: %v", err)
	}
	if !removeResp.GetRemoved() {
		t.Fatal("CronRemove() removed = false, want true")
	}

	// List should be empty after removal.
	listResp, err = server.CronList(context.Background(), &controlv1.CronListRequest{})
	if err != nil {
		t.Fatalf("CronList() after remove unexpected error: %v", err)
	}
	if len(listResp.GetSchedules()) != 0 {
		t.Fatalf("CronList() after remove schedules count = %d, want 0", len(listResp.GetSchedules()))
	}
}
