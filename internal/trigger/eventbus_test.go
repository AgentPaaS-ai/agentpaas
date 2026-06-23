package trigger

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"google.golang.org/grpc/metadata"
)

func TestEventBusPublishSubscribersReceiveInOrder(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")

	ch, cancel := bus.Subscribe("run-1", 0)
	defer cancel()

	bus.Publish("run-1", EventRunCreated, nil)
	bus.Publish("run-1", EventRunStarted, nil)
	bus.Publish("run-1", EventRunSucceeded, nil)

	assertEvent(t, ch, 1, EventRunCreated)
	assertEvent(t, ch, 2, EventRunStarted)
	assertEvent(t, ch, 3, EventRunSucceeded)
}

func TestEventBusEventIDsMonotonicPerRun(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-a")
	bus.RegisterRun("run-b")

	firstA := bus.Publish("run-a", EventRunCreated, nil)
	firstB := bus.Publish("run-b", EventRunCreated, nil)
	secondA := bus.Publish("run-a", EventRunStarted, nil)

	if firstA.EventID != 1 || secondA.EventID != 2 {
		t.Fatalf("run-a event ids = %d, %d; want 1, 2", firstA.EventID, secondA.EventID)
	}
	if firstB.EventID != 1 {
		t.Fatalf("run-b first event id = %d; want 1", firstB.EventID)
	}
}

func TestEventBusTerminalEventClosesChannelAndIgnoresLaterPublishes(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")

	ch, cancel := bus.Subscribe("run-1", 0)
	defer cancel()

	if event := bus.Publish("run-1", EventRunSucceeded, nil); event == nil {
		t.Fatal("terminal publish returned nil")
	}
	if event := bus.Publish("run-1", EventRunFailed, nil); event != nil {
		t.Fatalf("publish after terminal returned event id %d; want nil", event.EventID)
	}

	assertEvent(t, ch, 1, EventRunSucceeded)
	select {
	case event, open := <-ch:
		if open {
			t.Fatalf("channel still open with event %v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal close")
	}

	events := bus.GetEvents("run-1")
	if len(events) != 1 {
		t.Fatalf("stored events = %d; want 1", len(events))
	}
}

func TestEventBusSubscribeReplaysFromEventID(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunCreated, nil)
	bus.Publish("run-1", EventRunStarted, nil)
	bus.Publish("run-1", EventRunProgress, nil)

	ch, cancel := bus.Subscribe("run-1", 1)
	defer cancel()

	assertEvent(t, ch, 2, EventRunStarted)
	assertEvent(t, ch, 3, EventRunProgress)
}

func TestEventBusLastEventIDReconnectGetsLaterEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunCreated, nil)
	bus.Publish("run-1", EventRunStarted, nil)
	bus.Publish("run-1", EventRunSucceeded, nil)

	ch, cancel := bus.Subscribe("run-1", 2)
	defer cancel()

	assertEvent(t, ch, 3, EventRunSucceeded)
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("channel remained open after replaying closed run")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed replay channel")
	}
}

func TestEventBusMultipleSubscribersReceiveSameRunEvents(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")

	first, cancelFirst := bus.Subscribe("run-1", 0)
	defer cancelFirst()
	second, cancelSecond := bus.Subscribe("run-1", 0)
	defer cancelSecond()

	bus.Publish("run-1", EventRunCreated, nil)

	assertEvent(t, first, 1, EventRunCreated)
	assertEvent(t, second, 1, EventRunCreated)
}

func TestEventBusUnregisterRunCleansUp(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunCreated, nil)
	bus.UnregisterRun("run-1")

	if events := bus.GetEvents("run-1"); events != nil {
		t.Fatalf("events after unregister = %v; want nil", events)
	}

	ch, cancel := bus.Subscribe("run-1", 0)
	defer cancel()
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("subscription to unregistered run stayed open")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}

func TestSSEHeartbeatSentOnIdleConnection(t *testing.T) {
	bus := NewEventBus()
	bus.RegisterRun("run-1")
	handler := NewSSEHandler(bus)

	originalInterval := heartbeatInterval
	heartbeatInterval = 20 * time.Millisecond
	defer func() { heartbeatInterval = originalInterval }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithCaller(r.Context(), CallerID("api_key:key-1"), AuthMethodAPIKey))
		handler.ServeSSE(w, r, "run-1")
	}))
	defer func() { server.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer test-key-123")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	line, err := readUntilLine(resp.Body, ": heartbeat")
	if err != nil {
		t.Fatalf("read heartbeat: %v", err)
	}
	if line != ": heartbeat" {
		t.Fatalf("line = %q; want heartbeat", line)
	}
}

func TestSSEHandlerRejectsUnauthenticatedRequest(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunSucceeded, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events?run_id=run-1", nil)
	NewSSEHandler(bus).ServeSSE(rec, req, "run-1")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

func TestSSEHandlerServesEventsOverHTTP(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunCreated, nil)
	bus.Publish("run-1", EventRunSucceeded, nil)

	rec := httptest.NewRecorder()
	req := authenticatedSSERequest("/events?run_id=run-1")
	NewSSEHandler(bus).ServeSSE(rec, req, "run-1")

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body=%s", rec.Code, body)
	}
	if got := rec.Header().Get("Content-Type"); got != sseContentType {
		t.Fatalf("content type = %q; want %q", got, sseContentType)
	}
	if !strings.Contains(body, "id: 1\nevent: run_created") {
		t.Fatalf("body missing created event: %s", body)
	}
	if !strings.Contains(body, "id: 2\nevent: run_succeeded") {
		t.Fatalf("body missing succeeded event: %s", body)
	}
}

func TestSSEHandlerReconnectUsesLastEventID(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	bus.RegisterRun("run-1")
	bus.Publish("run-1", EventRunCreated, nil)
	bus.Publish("run-1", EventRunStarted, nil)
	bus.Publish("run-1", EventRunSucceeded, nil)

	rec := httptest.NewRecorder()
	req := authenticatedSSERequest("/events?run_id=run-1")
	req.Header.Set("Last-Event-ID", "2")
	NewSSEHandler(bus).ServeSSE(rec, req, "run-1")

	body := rec.Body.String()
	if strings.Contains(body, "id: 1") || strings.Contains(body, "id: 2") {
		t.Fatalf("reconnect body included already-seen events: %s", body)
	}
	if !strings.Contains(body, "id: 3\nevent: run_succeeded") {
		t.Fatalf("reconnect body missing event 3: %s", body)
	}
}

func authenticatedSSERequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Authorization", "Bearer test-key-123")
	return req.WithContext(WithCaller(req.Context(), CallerID("api_key:key-1"), AuthMethodAPIKey))
}

func TestInvokeStreamSendsEventsToClient(t *testing.T) {
	t.Parallel()

	bus := NewEventBus()
	service := NewTriggerService(nil, DefaultMaxPayload, bus)
	stream := &captureInvokeStream{ctx: context.Background()}

	err := service.InvokeStream(&triggerv1.InvokeRequest{AgentName: "agent-1"}, stream)
	if err != nil {
		t.Fatalf("InvokeStream: %v", err)
	}

	if len(stream.responses) != 2 {
		t.Fatalf("responses = %d; want 2", len(stream.responses))
	}
	if got := stream.responses[0].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_PENDING {
		t.Fatalf("first status = %s; want pending", got)
	}
	if got := stream.responses[1].GetRun().GetStatus(); got != triggerv1.RunStatus_RUN_STATUS_SUCCEEDED {
		t.Fatalf("second status = %s; want succeeded", got)
	}
	if stream.responses[0].GetRun().GetRunId() == "" {
		t.Fatal("run id is empty")
	}
	if stream.responses[0].GetRun().GetRunId() != stream.responses[1].GetRun().GetRunId() {
		t.Fatalf("run ids differ: %q vs %q", stream.responses[0].GetRun().GetRunId(), stream.responses[1].GetRun().GetRunId())
	}
}

func assertEvent(t *testing.T, ch <-chan RunEvent, eventID int64, eventType EventType) {
	t.Helper()

	select {
	case event, open := <-ch:
		if !open {
			t.Fatalf("channel closed before event %d", eventID)
		}
		if event.EventID != eventID || event.Type != eventType {
			t.Fatalf("event = (%d, %s); want (%d, %s)", event.EventID, event.Type, eventID, eventType)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event %d", eventID)
	}
}

func readUntilLine(r io.Reader, want string) (string, error) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == want {
			return line, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", io.EOF
}

type captureInvokeStream struct {
	ctx       context.Context
	responses []*triggerv1.InvokeResponse
}

func (s *captureInvokeStream) Send(resp *triggerv1.InvokeResponse) error {
	s.responses = append(s.responses, resp)
	return nil
}

func (s *captureInvokeStream) SetHeader(metadata.MD) error {
	return nil
}

func (s *captureInvokeStream) SendHeader(metadata.MD) error {
	return nil
}

func (s *captureInvokeStream) SetTrailer(metadata.MD) {}

func (s *captureInvokeStream) Context() context.Context {
	return s.ctx
}

func (s *captureInvokeStream) SendMsg(any) error {
	return nil
}

func (s *captureInvokeStream) RecvMsg(any) error {
	return nil
}
