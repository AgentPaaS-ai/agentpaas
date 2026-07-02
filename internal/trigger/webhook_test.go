package trigger

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

type fakeAuditAppender struct {
	mu      sync.Mutex
	records []audit.AuditRecord
}

func (f *fakeAuditAppender) Append(record audit.AuditRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, record)
	return nil
}

func (f *fakeAuditAppender) eventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	types := make([]string, len(f.records))
	for i, record := range f.records {
		types[i] = record.EventType
	}
	return types
}

func (f *fakeAuditAppender) recordsByType(eventType string) []audit.AuditRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	var records []audit.AuditRecord
	for _, record := range f.records {
		if record.EventType == eventType {
			records = append(records, record)
		}
	}
	return records
}

func testRunEvent() *RunEvent {
	return &RunEvent{
		EventID:   7,
		RunID:     "run-webhook-test",
		Type:      EventRunSucceeded,
		Timestamp: time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC),
		Data:      map[string]string{"agent": "unit-test"},
	}
}

func TestWebhookHMACComputationAndVerification(t *testing.T) {
	t.Parallel()

	timestamp := time.Date(2026, 6, 22, 12, 0, 0, 123, time.UTC)
	body := []byte(`{"run_id":"run-1"}`)
	signature := computeHMAC("secret", timestamp, body)

	if _, err := hex.DecodeString(signature); err != nil {
		t.Fatalf("signature is not hex: %v", err)
	}
	if !VerifyHMAC("secret", timestamp, body, signature) {
		t.Fatal("expected signature to verify")
	}
	if VerifyHMAC("wrong-secret", timestamp, body, signature) {
		t.Fatal("signature verified with invalid secret")
	}
	if VerifyHMAC("secret", timestamp, []byte(`{"run_id":"run-2"}`), signature) {
		t.Fatal("signature verified for tampered body")
	}
}

func TestWebhookTimestampReplayWindow(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	if !VerifyTimestamp(now.Add(-webhookReplayWindow), now) {
		t.Fatal("timestamp at replay window boundary should verify")
	}
	if VerifyTimestamp(now.Add(-webhookReplayWindow-time.Nanosecond), now) {
		t.Fatal("expired timestamp should be rejected")
	}
	if VerifyTimestamp(now.Add(webhookReplayWindow+time.Nanosecond), now) {
		t.Fatal("future timestamp outside replay window should be rejected")
	}
}

func TestVerifyDeliveryCompleteReceiverCheck(t *testing.T) {
	t.Parallel()

	timestamp := time.Now().UTC()
	body := []byte(`{"run_id":"run-1"}`)
	signature := "sha256=" + computeHMAC("secret", timestamp, body)

	if err := VerifyDelivery("secret", body, signature, timestamp.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("VerifyDelivery returned error: %v", err)
	}
	if err := VerifyDelivery("wrong-secret", body, signature, timestamp.Format(time.RFC3339Nano)); err == nil {
		t.Fatal("expected bad HMAC to be rejected")
	}
	expired := timestamp.Add(-webhookReplayWindow - time.Second)
	expiredSignature := "sha256=" + computeHMAC("secret", expired, body)
	if err := VerifyDelivery("secret", body, expiredSignature, expired.Format(time.RFC3339Nano)); err == nil {
		t.Fatal("expected expired timestamp to be rejected")
	}
	if err := VerifyDelivery("secret", body, "sha256=bad", "not-a-timestamp"); err == nil {
		t.Fatal("expected invalid timestamp to be rejected")
	}
}

func TestWebhookSuccessfulDeliveryToHTTPServer(t *testing.T) {
	t.Parallel()

	event := testRunEvent()
	auditAppender := &fakeAuditAppender{}
	received := make(chan struct{}, 1)
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		body := readRequestBody(t, r)
		receivedBody = body
		if err := VerifyDelivery("secret", body, r.Header.Get(webhookHMACHeader), r.Header.Get(webhookTimestampHeader)); err != nil {
			t.Errorf("VerifyDelivery failed: %v", err)
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get(webhookEventHeader); got != string(EventRunSucceeded) {
			t.Errorf("event header = %q, want %q", got, EventRunSucceeded)
		}
		w.WriteHeader(http.StatusAccepted)
		received <- struct{}{}
	}))
	t.Cleanup(server.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "success",
		URL:    server.URL,
		Secret: "secret",
	}}, auditAppender, NewEgressChecker([]policy.EgressRule{{Domain: "127.0.0.1"}}))
	deliverer.backoffBase = time.Millisecond

	results := deliverer.DeliverSync(context.Background(), event)

	if len(results) != 1 {
		t.Fatalf("results length = %d, want 1", len(results))
	}
	if results[0].Status != "delivered" || results[0].StatusCode != http.StatusAccepted {
		t.Fatalf("delivery = %+v, want delivered status 202", results[0])
	}
	select {
	case <-received:
	default:
		t.Fatal("server did not receive delivery")
	}
	var decoded RunEvent
	if err := json.Unmarshal(receivedBody, &decoded); err != nil {
		t.Fatalf("unmarshal delivery body: %v", err)
	}
	if decoded.RunID != event.RunID || decoded.Type != event.Type {
		t.Fatalf("delivered event = %+v, want run/type from %+v", decoded, event)
	}
	assertHasAuditEvent(t, auditAppender, "webhook_delivered")
}

func TestWebhookFailedDeliveryRetriesThreeTimesWithBackoff(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = readRequestBody(t, r)
		mu.Lock()
		attempts++
		mu.Unlock()
		http.Error(w, "try again", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "retry",
		URL:    server.URL,
		Secret: "secret",
	}}, nil, nil)
	deliverer.backoffBase = 2 * time.Millisecond

	start := time.Now()
	result := deliverer.DeliverSync(context.Background(), testRunEvent())[0]
	elapsed := time.Since(start)

	if result.Status != "dead_lettered" {
		t.Fatalf("status = %q, want dead_lettered", result.Status)
	}
	mu.Lock()
	gotAttempts := attempts
	mu.Unlock()
	if gotAttempts != webhookMaxRetries+1 {
		t.Fatalf("attempts = %d, want %d", gotAttempts, webhookMaxRetries+1)
	}
	if elapsed < 7*time.Millisecond {
		t.Fatalf("elapsed = %s, want retry backoff delay", elapsed)
	}
}

func TestWebhookRetriesExhaustedDeadLettersToAudit(t *testing.T) {
	t.Parallel()

	auditAppender := &fakeAuditAppender{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = readRequestBody(t, r)
		http.Error(w, "failed", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "dead-letter",
		URL:    server.URL,
		Secret: "secret",
	}}, auditAppender, nil)
	deliverer.backoffBase = time.Millisecond

	result := deliverer.DeliverSync(context.Background(), testRunEvent())[0]

	if result.Status != "dead_lettered" {
		t.Fatalf("status = %q, want dead_lettered", result.Status)
	}
	records := auditAppender.recordsByType("webhook_dead_lettered")
	if len(records) != 1 {
		t.Fatalf("dead-letter audit records = %d, want 1", len(records))
	}
	if got := records[0].Payload["reason"]; got != "HTTP 502" {
		t.Fatalf("dead-letter reason = %v, want HTTP 502", got)
	}
}

func TestWebhookDownTargetRetriesThenDeadLetters(t *testing.T) {
	t.Parallel()

	auditAppender := &fakeAuditAppender{}
	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "down",
		URL:    "http://127.0.0.1:1/webhook",
		Secret: "secret",
	}}, auditAppender, NewEgressChecker([]policy.EgressRule{{Domain: "127.0.0.1"}}))
	deliverer.backoffBase = time.Millisecond

	result := deliverer.DeliverSync(context.Background(), testRunEvent())[0]

	if result.Status != "dead_lettered" {
		t.Fatalf("status = %q, want dead_lettered", result.Status)
	}
	if !strings.Contains(result.Error, "connect") {
		t.Fatalf("error = %q, want connection failure", result.Error)
	}
	if got := len(deliverer.GetDeliveries()); got != webhookMaxRetries+1 {
		t.Fatalf("recorded deliveries = %d, want %d", got, webhookMaxRetries+1)
	}
	assertHasAuditEvent(t, auditAppender, "webhook_dead_lettered")
}

func TestWebhookNonAllowListedDomainBlockedByEgressChecker(t *testing.T) {
	t.Parallel()

	auditAppender := &fakeAuditAppender{}
	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "blocked",
		URL:    "https://blocked.example/webhook",
		Secret: "secret",
	}}, auditAppender, NewEgressChecker([]policy.EgressRule{{Domain: "allowed.example"}}))
	deliverer.backoffBase = time.Millisecond

	result := deliverer.DeliverSync(context.Background(), testRunEvent())[0]

	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}
	if got := len(deliverer.GetDeliveries()); got != 1 {
		t.Fatalf("recorded deliveries = %d, want 1", got)
	}
	assertHasAuditEvent(t, auditAppender, "webhook_dead_lettered")
}

func TestWebhookAllowListedDomainAllowedThrough(t *testing.T) {
	t.Parallel()

	event := testRunEvent()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = readRequestBody(t, r)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "allowed",
		URL:    server.URL,
		Secret: "secret",
	}}, nil, NewEgressChecker([]policy.EgressRule{{Domain: "127.0.0.1"}}))
	deliverer.backoffBase = time.Millisecond

	result := deliverer.DeliverSync(context.Background(), event)[0]

	if result.Status != "delivered" {
		t.Fatalf("status = %q, want delivered", result.Status)
	}
}

func TestWebhookAuditEventsDeliveredAndDeadLettered(t *testing.T) {
	t.Parallel()

	auditAppender := &fakeAuditAppender{}
	successServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = readRequestBody(t, r)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(successServer.Close)
	failServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = readRequestBody(t, r)
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	t.Cleanup(failServer.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{
		{Name: "ok", URL: successServer.URL, Secret: "secret"},
		{Name: "fail", URL: failServer.URL, Secret: "secret"},
	}, auditAppender, nil)
	deliverer.backoffBase = time.Millisecond

	results := deliverer.DeliverSync(context.Background(), testRunEvent())

	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	assertHasAuditEvent(t, auditAppender, "webhook_delivered")
	assertHasAuditEvent(t, auditAppender, "webhook_dead_lettered")
}

func TestWebhookMultipleHooksForSameEvent(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	received := make(map[string]int)
	newServer := func(name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { _ = r.Body.Close() }()
			_ = readRequestBody(t, r)
			mu.Lock()
			received[name]++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}))
	}
	serverA := newServer("a")
	t.Cleanup(serverA.Close)
	serverB := newServer("b")
	t.Cleanup(serverB.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{
		{Name: "a", URL: serverA.URL, Secret: "secret-a"},
		{Name: "b", URL: serverB.URL, Secret: "secret-b"},
	}, nil, nil)
	deliverer.backoffBase = time.Millisecond

	results := deliverer.DeliverSync(context.Background(), testRunEvent())

	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	mu.Lock()
	defer mu.Unlock()
	if received["a"] != 1 || received["b"] != 1 {
		t.Fatalf("received counts = %#v, want one delivery per hook", received)
	}
}

func TestWebhookHeaders(t *testing.T) {
	t.Parallel()

	event := testRunEvent()
	headersSeen := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _ = r.Body.Close() }()
		_ = readRequestBody(t, r)
		headersSeen <- r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	deliverer := NewWebhookDeliverer([]*WebhookConfig{{
		Name:   "headers",
		URL:    server.URL,
		Secret: "secret",
	}}, nil, nil)
	deliverer.backoffBase = time.Millisecond

	result := deliverer.DeliverSync(context.Background(), event)[0]
	if result.Status != "delivered" {
		t.Fatalf("status = %q, want delivered", result.Status)
	}

	var headers http.Header
	select {
	case headers = <-headersSeen:
	default:
		t.Fatal("server did not capture headers")
	}
	signature := headers.Get(webhookHMACHeader)
	if !strings.HasPrefix(signature, "sha256=") {
		t.Fatalf("signature header = %q, want sha256= prefix", signature)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256=")); err != nil {
		t.Fatalf("signature hex invalid: %v", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, headers.Get(webhookTimestampHeader)); err != nil {
		t.Fatalf("timestamp header is not RFC3339Nano: %v", err)
	}
	if got := headers.Get(webhookEventHeader); got != string(event.Type) {
		t.Fatalf("event header = %q, want %q", got, event.Type)
	}
}

func readRequestBody(t *testing.T, r *http.Request) []byte {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	return body
}

func assertHasAuditEvent(t *testing.T, auditAppender *fakeAuditAppender, eventType string) {
	t.Helper()
	for _, got := range auditAppender.eventTypes() {
		if got == eventType {
			return
		}
	}
	t.Fatalf("audit event %q not found in %v", eventType, auditAppender.eventTypes())
}
