package trigger

// B29-T01 CHARACTERIZATION TEST — freezes current behavior; B29
// replacement tasks are expected to update or fail these tests.
//
// Observation 5: Cold-per-run lifecycle — no warm pool. Each invocation
// creates a fresh run with a new runID. There is no warm sandbox
// retention, no activation policy on TriggerService. Terminal cleanup
// marks the run finished in-memory.

import (
	"context"
	"reflect"
	"testing"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// TestEachInvocationGeneratesFreshRunID proves that every InvokeStream
// call produces a unique runID — no warm pool reuse.
func TestEachInvocationGeneratesFreshRunID(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)

	runIDs := make(map[string]bool)
	for i := 0; i < 5; i++ {
		stream := &captureInvokeStream{ctx: context.Background()}
		err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "agent-cold"}, stream)
		if err != nil {
			t.Fatalf("InvokeStream %d: %v", i, err)
		}
		if len(stream.responses) == 0 {
			t.Fatalf("InvokeStream %d: no responses", i)
		}
		runID := stream.responses[0].GetRun().GetRunId()
		if runID == "" {
			t.Fatalf("InvokeStream %d: empty runID", i)
		}
		if runIDs[runID] {
			t.Fatalf("runID %q reused across invocations — should be unique", runID)
		}
		runIDs[runID] = true
	}

	if len(runIDs) != 5 {
		t.Fatalf("got %d unique runIDs from 5 invocations; want 5", len(runIDs))
	}
}

// TestInvokeAlsoGeneratesFreshRunID confirms Invoke also generates
// fresh runIDs (not reusing a warm pool).
func TestInvokeAlsoGeneratesFreshRunID(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)

	runIDs := make(map[string]bool)
	for i := 0; i < 5; i++ {
		resp, err := service.Invoke(context.Background(),
			&triggerv1.InvokeRequest{AgentName: "agent-invoke"})
		if err != nil {
			t.Fatalf("Invoke %d: %v", i, err)
		}
		runID := resp.GetRun().GetRunId()
		if runID == "" {
			t.Fatalf("Invoke %d: empty runID", i)
		}
		if runIDs[runID] {
			t.Fatalf("runID %q reused — should be unique", runID)
		}
		runIDs[runID] = true
	}

	if len(runIDs) != 5 {
		t.Fatalf("got %d unique runIDs from 5 Invoke calls; want 5", len(runIDs))
	}
}

// TestNoWarmPoolOnTriggerService proves there is no warm pool or
// activation policy field on TriggerService or ServerConfig.
func TestNoWarmPoolOnTriggerService(t *testing.T) {
	t.Parallel()

	// TriggerService must not have warmPool, activationPolicy, or similar fields.
	tsType := reflect.TypeOf(TriggerService{})
	forbidden := map[string]bool{
		"WarmPool":          true,
		"warmPool":          true,
		"ActivationPolicy":  true,
		"activationPolicy":  true,
		"IdleSandbox":       true,
		"idleSandbox":       true,
		"SandboxPool":       true,
		"sandboxPool":       true,
		"WarmSandboxes":     true,
		"warmSandboxes":     true,
		"PreAllocatedRuns":  true,
		"preAllocatedRuns":  true,
	}
	for i := 0; i < tsType.NumField(); i++ {
		name := tsType.Field(i).Name
		if forbidden[name] {
			t.Fatalf("TriggerService has field %q — warm pool detected", name)
		}
	}

	// ServerConfig must not have warm pool fields either.
	scType := reflect.TypeOf(ServerConfig{})
	for i := 0; i < scType.NumField(); i++ {
		name := scType.Field(i).Name
		if forbidden[name] {
			t.Fatalf("ServerConfig has field %q — warm pool detected", name)
		}
	}
}

// TestActivationPolicyExistsInPortOnly proves that ActivationPolicy is
// defined in internal/port but is NOT consumed by the trigger path.
func TestActivationPolicyExistsInPortOnly(t *testing.T) {
	t.Parallel()

	// ActivationPolicy type exists in port package (B28 addition).
	apType := reflect.TypeOf(port.ActivationPolicy{})
	if apType.NumField() == 0 {
		t.Fatal("ActivationPolicy has no fields — expected Mode and IdleTimeoutS")
	}

	// Verify ActivationPolicy fields.
	foundMode := false
	foundIdleTimeout := false
	for i := 0; i < apType.NumField(); i++ {
		switch apType.Field(i).Name {
		case "Mode":
			foundMode = true
		case "IdleTimeoutS":
			foundIdleTimeout = true
		}
	}
	if !foundMode {
		t.Fatal("ActivationPolicy missing Mode field")
	}
	if !foundIdleTimeout {
		t.Fatal("ActivationPolicy missing IdleTimeoutS field")
	}

	// TriggerService does NOT consume ActivationPolicy.
	tsType := reflect.TypeOf(TriggerService{})
	for i := 0; i < tsType.NumField(); i++ {
		fieldType := tsType.Field(i).Type
		if fieldType == reflect.TypeOf(port.ActivationPolicy{}) {
			t.Fatalf("TriggerService has ActivationPolicy field — it consumes the port type")
		}
		if fieldType.Kind() == reflect.Ptr && fieldType.Elem() == reflect.TypeOf(port.ActivationPolicy{}) {
			t.Fatalf("TriggerService has *ActivationPolicy field — it consumes the port type")
		}
	}
}

// TestRunStoreIsInMemory proves RunStore holds runs in an in-memory map.
// There is no persistence, no warm pool retention across process restart.
func TestRunStoreIsInMemory(t *testing.T) {
	t.Parallel()

	rs := NewRunStore()
	entry := rs.Register("run-cold", "agent-cold")
	if entry == nil {
		t.Fatal("Register returned nil")
	}

	// New RunStore does not see previous runs.
	rs2 := NewRunStore()
	if _, ok := rs2.Get("run-cold"); ok {
		t.Fatal("run survived new RunStore creation — RunStore is not in-memory-only?")
	}
}

// TestTerminalCleanupMarksRunFinished characterizes that the current
// cleanup behavior is in-memory: MarkFinished sets the status.
// InvokeStream immediately calls MarkFinished with SUCCEEDED.
func TestTerminalCleanupMarksRunFinished(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)
	stream := &captureInvokeStream{ctx: context.Background()}

	err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "agent-cleanup"}, stream)
	if err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}

	runID := stream.responses[0].GetRun().GetRunId()
	entry, ok := service.runStore.Get(runID)
	if !ok {
		t.Fatalf("run %q not found in store", runID)
	}
	if entry.Status != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("status = %s; want SUCCEEDED", entry.Status)
	}

	// The run is immediately terminal — no lingering in STARTED state.
	if entry.StartedAt.IsZero() {
		t.Log("RunStarted was never set — InvokeStream skips RunStarted")
	}
}
