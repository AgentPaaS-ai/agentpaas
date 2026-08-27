package harness

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/delegation"
)

// TestAdversaryT25_Hop302MustNotSucceedTask
// ADVERSARY BREAK: performLiveCallHop treats 3xx as success
// (delegation_handlers.go:812-842). 200-299 succeed; >=400 fail;
// 301/302/307 fall through to succeedLiveCallTask. A hop 302 with
// Location: attacker host must not complete the phone-call.
func TestAdversaryT25_Hop302MustNotSucceedTask(t *testing.T) {
	t.Setenv("AGENTPAAS_AGENT_KIND", "tool")
	s := setupToolDelegationServer(t, phoneCallToolSnapshot(), false)
	s.liveCallHop = liveCallRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"Location":     []string{"https://evil.example/exfil"},
			},
			Body: io.NopCloser(strings.NewReader(`{"error":"redirect"}`)),
		}, nil
	})

	resp := s.handleRequest(rpcRequest{
		ID:     "req-adv-t25-302",
		Method: "delegate_task",
		Params: map[string]any{
			"capability":      "dep-agent-peer",
			"idempotency_key": "idem-adv-t25-302",
			"message":         map[string]any{"task": "lookup"},
		},
	})
	if resp.OK {
		t.Fatal("// ADVERSARY BREAK: hop 302 succeeded the live-call task as if wait+link completed")
	}

	dts := s.getDelegationTrustState()
	task, err := dts.Store.GetTaskByIdempotencyKey(context.Background(), dts.Snapshot.CallerDeploymentID, "idem-adv-t25-302")
	if err != nil || task == nil {
		t.Fatalf("GetTaskByIdempotencyKey: %v", err)
	}
	if task.Status == delegation.TaskStatusSucceeded {
		t.Fatal("// ADVERSARY BREAK: hop 302 CAS'd the task to SUCCEEDED")
	}
}
