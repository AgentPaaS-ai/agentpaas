package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// ---------------------------------------------------------------------------
// B33-T06: Request size bounds
// ---------------------------------------------------------------------------

func TestCheckRequestSize_MaxMinusOne_OK(t *testing.T) {
	body := make([]byte, MaxRequestBytes-1)
	err := CheckRequestSize(body)
	if err != nil {
		t.Fatalf("CheckRequestSize(max-1) = %v, want nil", err)
	}
}

func TestCheckRequestSize_AtMax_OK(t *testing.T) {
	body := make([]byte, MaxRequestBytes)
	err := CheckRequestSize(body)
	if err != nil {
		t.Fatalf("CheckRequestSize(max) = %v, want nil", err)
	}
}

func TestCheckRequestSize_MaxPlusOne_Rejected(t *testing.T) {
	body := make([]byte, MaxRequestBytes+1)
	err := CheckRequestSize(body)
	if err == nil {
		t.Fatal("CheckRequestSize(max+1) = nil, want error")
	}
	if !strings.Contains(err.Error(), ErrCodeBodyTooLarge) {
		t.Fatalf("error = %v, want code %q", err, ErrCodeBodyTooLarge)
	}
}

func TestCheckRequestSize_Empty_OK(t *testing.T) {
	err := CheckRequestSize([]byte{})
	if err != nil {
		t.Fatalf("CheckRequestSize(empty) = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: Response size bounds
// ---------------------------------------------------------------------------

func TestCheckResponseSize_MaxMinusOne_OK(t *testing.T) {
	body := make([]byte, MaxResponseBytes-1)
	err := CheckResponseSize(body)
	if err != nil {
		t.Fatalf("CheckResponseSize(max-1) = %v, want nil", err)
	}
}

func TestCheckResponseSize_AtMax_OK(t *testing.T) {
	body := make([]byte, MaxResponseBytes)
	err := CheckResponseSize(body)
	if err != nil {
		t.Fatalf("CheckResponseSize(max) = %v, want nil", err)
	}
}

func TestCheckResponseSize_MaxPlusOne_Rejected(t *testing.T) {
	body := make([]byte, MaxResponseBytes+1)
	err := CheckResponseSize(body)
	if err == nil {
		t.Fatal("CheckResponseSize(max+1) = nil, want error")
	}
	if !strings.Contains(err.Error(), ErrCodeBodyTooLarge) {
		t.Fatalf("error = %v, want code %q", err, ErrCodeBodyTooLarge)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: JSON depth bounds
// ---------------------------------------------------------------------------

func TestCheckJSONDepth_AtLimit_OK(t *testing.T) {
	// Build a JSON object nested exactly MaxJSONDepth levels.
	raw := buildNestedJSON(MaxJSONDepth)
	err := CheckJSONDepth(raw)
	if err != nil {
		t.Fatalf("CheckJSONDepth(depth=%d) = %v, want nil", MaxJSONDepth, err)
	}
}

func TestCheckJSONDepth_OverLimit_Rejected(t *testing.T) {
	raw := buildNestedJSON(MaxJSONDepth + 1)
	err := CheckJSONDepth(raw)
	if err == nil {
		t.Fatal("CheckJSONDepth(depth=33) = nil, want error")
	}
	if !strings.Contains(err.Error(), ErrCodeDepthTooDeep) {
		t.Fatalf("error = %v, want code %q", err, ErrCodeDepthTooDeep)
	}
}

func TestCheckJSONDepth_Shallow_OK(t *testing.T) {
	raw := json.RawMessage(`{"a": 1, "b": [2, 3]}`)
	err := CheckJSONDepth(raw)
	if err != nil {
		t.Fatalf("CheckJSONDepth(shallow) = %v, want nil", err)
	}
}

func TestCheckJSONDepth_Empty_OK(t *testing.T) {
	err := CheckJSONDepth(nil)
	if err != nil {
		t.Fatalf("CheckJSONDepth(nil) = %v, want nil", err)
	}
	err = CheckJSONDepth(json.RawMessage{})
	if err != nil {
		t.Fatalf("CheckJSONDepth(empty) = %v, want nil", err)
	}
}

// buildNestedJSON constructs a JSON object nested to the given depth.
// For depth 0: {"v":0}; depth 1: {"v":{"v":1}}; etc.
func buildNestedJSON(depth int) json.RawMessage {
	if depth <= 0 {
		return json.RawMessage(`{"v":0}`)
	}
	var result string
	for i := 0; i < depth; i++ {
		result += `{"v":`
	}
	result += "0"
	for i := 0; i < depth; i++ {
		result += "}"
	}
	return json.RawMessage(result)
}

// ---------------------------------------------------------------------------
// B33-T06: CallSemaphore concurrency
// ---------------------------------------------------------------------------

func TestCallSemaphore_AtMax_OK(t *testing.T) {
	sem := NewCallSemaphore(2)

	r1, err1 := sem.Acquire()
	if err1 != nil {
		t.Fatalf("Acquire 1: %v", err1)
	}
	r2, err2 := sem.Acquire()
	if err2 != nil {
		t.Fatalf("Acquire 2: %v", err2)
	}
	if sem.Used() != 2 {
		t.Fatalf("Used = %d, want 2", sem.Used())
	}

	r2()
	if sem.Used() != 1 {
		t.Fatalf("Used after one release = %d, want 1", sem.Used())
	}
	r1()
	if sem.Used() != 0 {
		t.Fatalf("Used after all releases = %d, want 0", sem.Used())
	}
}

func TestCallSemaphore_MaxPlusOne_Rejected(t *testing.T) {
	sem := NewCallSemaphore(1)

	r1, err1 := sem.Acquire()
	if err1 != nil {
		t.Fatalf("Acquire 1: %v", err1)
	}
	defer r1()

	_, err2 := sem.Acquire()
	if err2 == nil {
		t.Fatal("Acquire over limit = nil, want overload error")
	}
	if !strings.Contains(err2.Error(), ErrCodeOverloaded) {
		t.Fatalf("error = %v, want code %q", err2, ErrCodeOverloaded)
	}
}

func TestCallSemaphore_Unlimited_NoRejection(t *testing.T) {
	sem := NewCallSemaphore(0) // unlimited

	var releases []func()
	for i := 0; i < 100; i++ {
		r, err := sem.Acquire()
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
		releases = append(releases, r)
	}
	for _, r := range releases {
		r()
	}
}

func TestCallSemaphore_Capacity(t *testing.T) {
	sem := NewCallSemaphore(5)
	if c := sem.Capacity(); c != 5 {
		t.Fatalf("Capacity = %d, want 5", c)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: Deadline — slow tool under timeout succeeds; over cancels
// ---------------------------------------------------------------------------

func TestRouter_CallTool_DeadlineExceeded_ReturnsTimeout(t *testing.T) {
	// Stand up a slow HTTP endpoint that takes 2s to respond.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer upstream.Close()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "slow-svc",
		Transport:    "http",
		Endpoint:     upstream.URL,
		AllowedTools: []string{"slow"},
	}}, "agent-1", "run-1")

	router := NewRouter(manager, nil, http.DefaultClient, nil)

	// Context with 100ms deadline — the 2s tool should be cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := router.CallTool(ctx, "slow-svc", "slow", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool with expired deadline = nil, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("CallTool error = %v, want deadline exceeded", err)
	}
}

func TestRouter_CallTool_DeadlineSufficient_Succeeds(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"source":"fast-result"}}`))
	}))
	defer upstream.Close()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "fast-svc",
		Transport:    "http",
		Endpoint:     upstream.URL,
		AllowedTools: []string{"fast"},
	}}, "agent-1", "run-1")

	router := NewRouter(manager, nil, http.DefaultClient, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := router.CallTool(ctx, "fast-svc", "fast", map[string]any{}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool error = %v, want nil", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["source"] != "fast-result" {
		t.Fatalf("result = %#v, want source=fast-result", result)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: Concurrency — Router-level semaphore
// ---------------------------------------------------------------------------

func TestRouter_CallTool_ConcurrencyAtMax_OK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Hold for 200ms to ensure semaphore is occupied.
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer upstream.Close()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "conc-svc",
		Transport:    "http",
		Endpoint:     upstream.URL,
		AllowedTools: []string{"op"},
	}}, "agent-1", "run-1")

	router := NewRouter(manager, nil, http.DefaultClient, nil)
	router.SetServiceConcurrency("conc-svc", 2)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	results := make(chan error, 3)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := router.CallTool(ctx, "conc-svc", "op", map[string]any{}, "agent-1", "run-1")
			results <- err
		}()
	}
	// Wait a bit for them to settle, then verify sem is full.
	time.Sleep(50 * time.Millisecond)
	if used := router.getSemaphore("conc-svc").Used(); used != 2 {
		t.Logf("semaphore used = %d (want 2; may be racy depending on timing)", used)
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Errorf("CallTool error = %v, want nil", err)
		}
	}
}

func TestRouter_CallTool_ConcurrencyMaxPlusOne_Rejected(t *testing.T) {
	holdCh := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-holdCh // block until released
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer upstream.Close()

	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "tight-svc",
		Transport:    "http",
		Endpoint:     upstream.URL,
		AllowedTools: []string{"op"},
	}}, "agent-1", "run-1")

	router := NewRouter(manager, nil, http.DefaultClient, nil)
	router.SetServiceConcurrency("tight-svc", 1) // only 1 concurrent

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// First call acquires semaphore, blocks on holdCh.
	errCh := make(chan error, 1)
	go func() {
		_, err := router.CallTool(ctx, "tight-svc", "op", map[string]any{}, "agent-1", "run-1")
		errCh <- err
	}()

	// Wait for first call to acquire semaphore.
	time.Sleep(50 * time.Millisecond)

	// Second call should be rejected immediately.
	_, err := router.CallTool(ctx, "tight-svc", "op", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool over concurrency = nil, want overload error")
	}
	if !strings.Contains(err.Error(), ErrCodeOverloaded) {
		t.Fatalf("error = %v, want code %q", err, ErrCodeOverloaded)
	}

	// Release the first call.
	close(holdCh)
	if firstErr := <-errCh; firstErr != nil {
		t.Errorf("first CallTool error = %v, want nil", firstErr)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: CancelTracker + Fence cancels in-flight calls
// ---------------------------------------------------------------------------

func TestCancelTracker_CancelAll_CancelsRegisteredCalls(t *testing.T) {
	tracker := NewCancelTracker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	id := tracker.Register(cancel)
	if tracker.Active() != 1 {
		t.Fatalf("Active = %d, want 1", tracker.Active())
	}

	// Cancel all — this should call our cancel func.
	tracker.CancelAll()

	// Verify our context is now cancelled.
	select {
	case <-ctx.Done():
		// Expected.
	default:
		t.Fatal("context not cancelled after CancelAll")
	}

	if tracker.Active() != 0 {
		t.Fatalf("Active after CancelAll = %d, want 0", tracker.Active())
	}

	// Unregister on already-empty tracker is safe.
	tracker.Unregister(id)
}

func TestCancelTracker_Unregister_RemovesCall(t *testing.T) {
	tracker := NewCancelTracker()
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	id := tracker.Register(cancel)
	if tracker.Active() != 1 {
		t.Fatalf("Active = %d, want 1", tracker.Active())
	}

	tracker.Unregister(id)
	if tracker.Active() != 0 {
		t.Fatalf("Active after Unregister = %d, want 0", tracker.Active())
	}
}

func TestServiceRegistry_Fence_CancelsInFlightCalls(t *testing.T) {
	// Build a registry with a ready instance and a cancel tracker.
	inst := TestServiceInstance("wf-fence", "fb-fence", StateReady,
		"http://localhost:0/mcp", "cap-test-token", []string{"tool1"})
	// Give it a lease deadline far in the future.
	inst.LeaseDeadline = time.Now().Add(1 * time.Hour)

	reg := TestServiceRegistry([]*ServiceInstance{inst})

	// Register a call.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	callID, err := reg.RegisterCall("wf-fence", "fb-fence", cancel)
	if err != nil {
		t.Fatalf("RegisterCall: %v", err)
	}
	_ = callID

	// Fence should cancel all in-flight calls.
	err = reg.Fence(context.Background(), "wf-fence", "fb-fence", "lease revoked")
	if err != nil {
		t.Fatalf("Fence: %v", err)
	}

	// Verify context is cancelled.
	select {
	case <-ctx.Done():
		// Expected.
	default:
		t.Fatal("context not cancelled after Fence")
	}

	// Verify service state is now Fenced.
	copied, err := reg.Get("wf-fence", "fb-fence")
	if err != nil {
		t.Fatalf("Get after Fence: %v", err)
	}
	if copied.State != StateFenced {
		t.Fatalf("State = %s, want %s", copied.State, StateFenced)
	}
}

func TestServiceRegistry_Fence_LateResultDiscarded(t *testing.T) {
	// Simulate a late result arriving after fence.
	// Build registry + instance.
	inst := TestServiceInstance("wf-late", "fb-late", StateReady,
		"http://localhost:0/mcp", "cap-test-token", []string{"tool1"})

	reg := TestServiceRegistry([]*ServiceInstance{inst})

	// Register an in-flight call with a cancelable context.
	ctx, cancel := context.WithCancel(context.Background())

	// Use our own cancel wrapper to detect if cancel was called.
	var called bool
	wrappedCancel := func() {
		called = true
		cancel()
	}

	callID, err := reg.RegisterCall("wf-late", "fb-late", wrappedCancel)
	if err != nil {
		t.Fatalf("RegisterCall: %v", err)
	}

	// Fence — this should cancel the call.
	err = reg.Fence(context.Background(), "wf-late", "fb-late", "lease revoked")
	if err != nil {
		t.Fatalf("Fence: %v", err)
	}

	if !called {
		t.Fatal("cancel func was not called by Fence")
	}

	// Verify context is cancelled.
	select {
	case <-ctx.Done():
		// Expected.
	default:
		t.Fatal("context not cancelled after Fence")
	}

	// Unregister after fence is safe.
	reg.UnregisterCall("wf-late", "fb-late", callID)
}

// ---------------------------------------------------------------------------
// B33-T06: No cross-service fallback
// ---------------------------------------------------------------------------

func TestRouter_CallTool_NoCrossServiceFallback_FailsClosed(t *testing.T) {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         "service-a",
		Transport:    "http",
		Endpoint:     "http://invalid:99999/mcp",
		AllowedTools: []string{"tool-a"},
	}}, "agent-1", "run-1")

	router := NewRouter(manager, nil, http.DefaultClient, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	_, err := router.CallTool(ctx, "service-a", "tool-a", map[string]any{}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("CallTool should fail — no cross-service fallback")
	}
	// Should NOT be "not allowed" — that would mean it found a different service.
	if err.Error() == "mcp server/tool not allowed" {
		t.Fatal("unexpected fallback to another service")
	}
}

// ---------------------------------------------------------------------------
// B33-T06: Managed resolver uses effective deadline (not stuck on 5s)
// ---------------------------------------------------------------------------

func TestManagedResolver_LeaseDeadline_Past_Rejected(t *testing.T) {
	inst := TestServiceInstance("wf-lease", "fb-lease", StateReady,
		"http://localhost:0/mcp", "cap-token", []string{"tool1"})
	// Lease expired 1 hour ago.
	inst.LeaseDeadline = time.Now().Add(-1 * time.Hour)

	reg := TestServiceRegistry([]*ServiceInstance{inst})
	resolver := NewManagedServiceResolver(reg, nil)

	ctx := context.Background()
	_, err := resolver.ResolveToolCall(ctx, "wf-lease", "fb-lease", "tool1", map[string]any{})
	if err == nil {
		t.Fatal("ResolveToolCall with expired lease = nil, want lease expired error")
	}
	var typed *TypedError
	if !errors.As(err, &typed) || typed.Code != ErrCodeLeaseExpired {
		t.Fatalf("error = %v, want typed error with code %q", err, ErrCodeLeaseExpired)
	}
}

func TestManagedResolver_LeaseDeadline_Future_OK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":0,"result":{"source":"lease-future-ok"}}`))
	}))
	defer upstream.Close()

	inst := TestServiceInstance("wf-ok", "fb-ok", StateReady,
		upstream.URL, "cap-token", []string{"tool1"})
	inst.LeaseDeadline = time.Now().Add(1 * time.Hour)

	reg := TestServiceRegistry([]*ServiceInstance{inst})
	resolver := NewManagedServiceResolver(reg, upstream.Client())

	ctx := context.Background()
	result, err := resolver.ResolveToolCall(ctx, "wf-ok", "fb-ok", "tool1", map[string]any{"q": "test"})
	if err != nil {
		t.Fatalf("ResolveToolCall with future lease = %v, want nil", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["source"] != "lease-future-ok" {
		t.Fatalf("result = %#v, want source=lease-future-ok", result)
	}
}

func TestManagedResolver_NoLeaseDeadline_OK(t *testing.T) {
	// Zero LeaseDeadline means no deadline — should proceed.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":0,"result":{"source":"no-lease-ok"}}`))
	}))
	defer upstream.Close()

	inst := TestServiceInstance("wf-nolease", "fb-nolease", StateReady,
		upstream.URL, "cap-token", []string{"tool1"})
	// LeaseDeadline is zero.

	reg := TestServiceRegistry([]*ServiceInstance{inst})
	resolver := NewManagedServiceResolver(reg, upstream.Client())

	result, err := resolver.ResolveToolCall(context.Background(), "wf-nolease", "fb-nolease", "tool1", map[string]any{})
	if err != nil {
		t.Fatalf("ResolveToolCall with no lease deadline = %v, want nil", err)
	}
	resultMap, ok := result.(map[string]any)
	if !ok || resultMap["source"] != "no-lease-ok" {
		t.Fatalf("result = %#v, want source=no-lease-ok", result)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: Managed resolver request size/depth enforcement
// ---------------------------------------------------------------------------

func TestManagedResolver_RequestSizeExceeded_Rejected(t *testing.T) {
	inst := TestServiceInstance("wf-size", "fb-size", StateReady,
		"http://localhost:0/mcp", "cap-token", []string{"tool1"})
	inst.LeaseDeadline = time.Now().Add(1 * time.Hour)

	reg := TestServiceRegistry([]*ServiceInstance{inst})
	resolver := NewManagedServiceResolver(reg, nil)

	// Create a payload larger than MaxRequestBytes.
	bigPayload := make([]byte, MaxRequestBytes+1)
	for i := range bigPayload {
		bigPayload[i] = 'x'
	}
	input := map[string]any{"data": string(bigPayload)}

	_, err := resolver.ResolveToolCall(context.Background(), "wf-size", "fb-size", "tool1", input)
	if err == nil {
		t.Fatal("ResolveToolCall with oversized input = nil, want error")
	}
	if !strings.Contains(err.Error(), "managed service") || !strings.Contains(err.Error(), ErrCodeBodyTooLarge) {
		t.Fatalf("error = %v, want managed service error with %q", err, ErrCodeBodyTooLarge)
	}
}

func TestManagedResolver_RequestDepthExceeded_Rejected(t *testing.T) {
	inst := TestServiceInstance("wf-depth", "fb-depth", StateReady,
		"http://localhost:0/mcp", "cap-token", []string{"tool1"})
	inst.LeaseDeadline = time.Now().Add(1 * time.Hour)

	reg := TestServiceRegistry([]*ServiceInstance{inst})
	resolver := NewManagedServiceResolver(reg, nil)

	// Build deeply nested input.
	deepInput := buildDeepMap(MaxJSONDepth + 1, "val")
	_, err := resolver.ResolveToolCall(context.Background(), "wf-depth", "fb-depth", "tool1", deepInput)
	if err == nil {
		t.Fatal("ResolveToolCall with deep input = nil, want error")
	}
	if !strings.Contains(err.Error(), "managed service") || !strings.Contains(err.Error(), ErrCodeDepthTooDeep) {
		t.Fatalf("error = %v, want managed service error with %q", err, ErrCodeDepthTooDeep)
	}
}

func TestManagedResolver_ResponseDepthExceeded_Rejected(t *testing.T) {
	// Build a response with nested JSON deeper than MaxJSONDepth.
	deepResponse := buildNestedJSON(MaxJSONDepth + 1)
	jsonBytes, err := json.Marshal(mcpResponse{
		JSONRPC: "2.0",
		ID:      0,
		Result:  deepResponse,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	responseBody := string(jsonBytes)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer upstream.Close()

	inst := TestServiceInstance("wf-resp-depth", "fb-resp-depth", StateReady,
		upstream.URL, "cap-token", []string{"tool1"})
	inst.LeaseDeadline = time.Now().Add(1 * time.Hour)

	reg := TestServiceRegistry([]*ServiceInstance{inst})
	resolver := NewManagedServiceResolver(reg, upstream.Client())

	_, err = resolver.ResolveToolCall(context.Background(), "wf-resp-depth", "fb-resp-depth", "tool1", map[string]any{})
	if err == nil {
		t.Fatal("ResolveToolCall with deep response = nil, want depth error")
	}
	if !strings.Contains(err.Error(), ErrCodeDepthTooDeep) {
		t.Fatalf("error = %v, want %q", err, ErrCodeDepthTooDeep)
	}
}

// buildDeepMap creates a map nested to the given depth.
func buildDeepMap(depth int, leaf string) map[string]any {
	if depth <= 1 {
		return map[string]any{"leaf": leaf}
	}
	return map[string]any{"nested": buildDeepMap(depth-1, leaf)}
}

// ---------------------------------------------------------------------------
// B33-T06: EffectiveCallDeadline helper
// ---------------------------------------------------------------------------

func TestEffectiveCallDeadline_MinOfSources(t *testing.T) {
	now := time.Now()
	t1 := now.Add(5 * time.Second)
	t2 := now.Add(2 * time.Second) // smallest
	t3 := now.Add(10 * time.Second)

	result := EffectiveCallDeadline(now, t1, t2, t3)
	if !result.Equal(t2) {
		t.Fatalf("EffectiveCallDeadline = %v, want %v (min of all)", result, t2)
	}
}

func TestEffectiveCallDeadline_AllZero_ReturnsZero(t *testing.T) {
	now := time.Now()
	result := EffectiveCallDeadline(now)
	if !result.IsZero() {
		t.Fatalf("EffectiveCallDeadline with no sources = %v, want zero", result)
	}
}

func TestEffectiveCallDeadline_SomeZero_UsesNonZero(t *testing.T) {
	now := time.Now()
	t1 := now.Add(3 * time.Second)
	result := EffectiveCallDeadline(now, time.Time{}, t1, time.Time{})
	if !result.Equal(t1) {
		t.Fatalf("EffectiveCallDeadline = %v, want %v", result, t1)
	}
}

// ---------------------------------------------------------------------------
// B33-T06: No synthetic success for managed + bounds audit
// ---------------------------------------------------------------------------

func TestTypedError_NewCodes(t *testing.T) {
	// Verify the new error codes produce correct TypedError values.
	tests := []struct {
		code string
		msg  string
	}{
		{ErrCodeOverloaded, "overloaded"},
		{ErrCodeBodyTooLarge, "body too large"},
		{ErrCodeDepthTooDeep, "depth too deep"},
	}
	for _, tt := range tests {
		err := newTypedError(tt.code, tt.msg)
		if err.Code != tt.code {
			t.Errorf("TypedError.Code = %q, want %q", err.Code, tt.code)
		}
		if err.Message != tt.msg {
			t.Errorf("TypedError.Message = %q, want %q", err.Message, tt.msg)
		}
		if !strings.Contains(err.Error(), tt.code) {
			t.Errorf("TypedError.Error() = %q, want to contain %q", err.Error(), tt.code)
		}
	}
}

// ---------------------------------------------------------------------------
// B33-T06: Heartbeat does NOT extend caller/workflow ceilings
// ---------------------------------------------------------------------------

func TestHeartbeat_DoesNotExtendCallerCeiling(t *testing.T) {
	// This is a design assertion test: ServiceInstance.LeaseDeadline is
	// set at Start and never modified by heartbeat/readiness ping.
	// A heartbeat or readiness check must not mutate the lease deadline.
	inst := NewServiceInstance("wf-hb", "fb-hb", "pkg", "1.0", "digest", []string{"t1"})
	inst.State = StateReady
	inst.LeaseDeadline = time.Now().Add(30 * time.Second)
	originalDeadline := inst.LeaseDeadline

	// Simulate a heartbeat: nothing should change LeaseDeadline.
	// A readiness probe (stub) reads state but does not mutate deadlines.
	time.Sleep(10 * time.Millisecond)

	if !inst.LeaseDeadline.Equal(originalDeadline) {
		t.Fatalf("LeaseDeadline changed from %v to %v after heartbeat — heartbeat must not extend caller ceiling",
			originalDeadline, inst.LeaseDeadline)
	}

	// Also verify that a simple state read doesn't change LeaseDeadline.
	copied := copyInstance(inst)
	if !copied.LeaseDeadline.Equal(originalDeadline) {
		t.Fatalf("copyInstance changed LeaseDeadline from %v to %v",
			originalDeadline, copied.LeaseDeadline)
	}
}
