package harness

import (
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/routedrun"
)

// TestMCP_CallContext_LongEnvelopeNotCappedAt30s verifies that an active
// TimeEnvelope with a 90s remaining deadline is used as the MCP call timeout.
// Hosted MCP tools/call may admit+start a container, so long envelopes must
// not fall through to the 30s legacy fallback.
func TestMCP_CallContext_LongEnvelopeNotCappedAt30s(t *testing.T) {
	// active=90s, lease=90s, stall=90s → EffectiveOperationDeadlineMs = 90s.
	env, ok := routedrun.TimeEnvelopeFromCeilings(90_000, 90_000, 90_000, 90_000)
	if !ok {
		t.Fatal("expected envelope")
	}
	nowMs := routedrun.NowMonotonicMs(nil)
	s := &harnessRPCServer{
		nowMonotonicMs: func() int64 { return nowMs },
	}
	state := &rpcInvokeState{
		payload:      map[string]any{},
		timeEnvelope: &env,
	}

	ctx, cancel := s.mcpCallContext(state)
	defer cancel()

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		t.Fatal("mcpCallContext returned context without deadline")
	}
	got := time.Until(deadline)
	want := 90 * time.Second
	const slack = 200 * time.Millisecond
	if got < want-slack || got > want+slack {
		t.Fatalf("mcpCallContext timeout = %v, want %v (envelope 90s, not 30s legacy cap)", got, want)
	}
}
