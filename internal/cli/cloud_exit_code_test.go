package cli

import (
	"errors"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
)

func TestCloudExitCodeMapsAPIReasons(t *testing.T) {
	tests := []struct {
		reason string
		code   int
	}{
		{reason: "quota_exceeded", code: cloudExitQuota},
		{reason: "trial_expired", code: cloudExitQuota},
		{reason: "unauthorized", code: cloudExitAuth},
		{reason: "not_found", code: cloudExitNotFound},
		{reason: "no_slot_capacity", code: cloudExitCapacity},
		{reason: "already_running", code: cloudExitConflict},
		{reason: "container_start_failed", code: cloudExitOther},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			err := &cloudclient.HTTPStatusError{
				StatusCode: httpStatusForCloudReason(tt.reason),
				ErrorCode:  tt.reason,
				Reason:     tt.reason,
				Message:    tt.reason,
			}
			if got := CloudExitCode(err); got != tt.code {
				t.Fatalf("CloudExitCode(%q) = %d, want %d", tt.reason, got, tt.code)
			}
		})
	}

	if got := CloudExitCode(errors.New("ordinary failure")); got != cloudExitOther {
		t.Fatalf("ordinary failure exit code = %d, want %d", got, cloudExitOther)
	}
}

func httpStatusForCloudReason(reason string) int {
	switch reason {
	case "unauthorized":
		return 401
	case "not_found":
		return 404
	case "no_slot_capacity":
		return 503
	case "already_running":
		return 409
	case "quota_exceeded", "trial_expired":
		return 429
	default:
		return 500
	}
}
