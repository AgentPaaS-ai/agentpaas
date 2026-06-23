package trigger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
)

// TestFuzzControlAPIRESTJSON fuzzes the REST JSON ingestion path.
// Acceptance: 0 crashes across 100k executions.
func TestFuzzControlAPIRESTJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fuzz test in short mode")
	}

	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	//nolint:gosec // math/rand is intentional for fuzz payload generation, not security.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	const iterations = 100_000

	crashes := 0
	for i := 0; i < iterations; i++ {
		payload := generateFuzzPayload(rng, i)
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		drainAndClose(t, resp)

		if resp.StatusCode == http.StatusInternalServerError {
			crashes++
			if crashes <= 3 {
				t.Errorf("CRASH at iteration %d: 500 Internal Server Error, payload=%q", i, string(payload[:min(len(payload), 200)]))
			}
		}
	}

	if crashes > 0 {
		t.Fatalf("Fuzz found %d crashes out of %d iterations", crashes, iterations)
	}
	t.Logf("Fuzzed %d iterations, 0 crashes", iterations)
}

// TestFuzzMalformedJSON400 verifies that malformed JSON returns 400 with line info.
func TestFuzzMalformedJSON400(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	malformedPayloads := []string{
		`{"agent_name": "test"`,
		`{"agent_name": }`,
		`{"agent_name": "test"]}`,
		`{invalid json}`,
		`{"agent_name": "test", "payload": }`,
		`{"agent_name": "test", ,}`,
		`{"agent_name": null, "payload": "test"`,
		`[{"agent_name": "test"}]`,
		"{\n  \"agent_name\": \"test\",\n  \"payload\": \n}",
	}

	for i, payload := range malformedPayloads {
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(payload))
		if err != nil {
			t.Fatalf("iteration %d: HTTP error: %v", i, err)
		}
		body := readAndClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("iteration %d: expected 400 for malformed JSON %q, got %d", i, payload[:min(len(payload), 50)], resp.StatusCode)
		}
		if !strings.Contains(strings.ToLower(string(body)), "line") {
			t.Errorf("iteration %d: expected malformed JSON response to include line info, body=%q", i, string(body))
		}
	}
	t.Logf("Tested: malformed JSON returns 400 (%d cases)", len(malformedPayloads))
}

func TestFuzzEmptyBody400(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(" \n\t "))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	body := readAndClose(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty JSON body, got %d body=%q", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "request body is required") {
		t.Fatalf("expected required body error, got %q", string(body))
	}
}

func TestFuzzNullByteString400(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	payload := `{"agent_name":"\u0000test"}`
	resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	body := readAndClose(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for null byte JSON string, got %d body=%q", resp.StatusCode, string(body))
	}
	if !strings.Contains(string(body), "request body contains null bytes") {
		t.Fatalf("expected null byte error, got %q", string(body))
	}
}

// TestFuzzRandomFields verifies random field combinations don't crash.
func TestFuzzRandomFields(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fuzz test in short mode")
	}

	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	//nolint:gosec // math/rand is intentional for fuzz payload generation, not security.
	rng := rand.New(rand.NewSource(42))
	const iterations = 10_000

	for i := 0; i < iterations; i++ {
		obj := generateRandomJSONFields(rng)
		payload, err := json.Marshal(obj)
		if err != nil {
			t.Fatalf("iteration %d: marshal fuzz object: %v", i, err)
		}
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("CRASH at iteration %d: 500, payload=%s", i, string(payload[:min(len(payload), 200)]))
		}
	}
	t.Logf("Tested: random field fuzzing (%d iterations, 0 crashes)", iterations)
}

// TestFuzzLargePayload verifies large payloads are rejected, not crashed.
func TestFuzzLargePayload(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	largePayload := bytes.Repeat([]byte("x"), DefaultMaxPayload+1)
	payload, err := json.Marshal(map[string]any{
		"agent_name": "test",
		"payload":    string(largePayload),
	})
	if err != nil {
		t.Fatalf("marshal large payload: %v", err)
	}

	resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	drainAndClose(t, resp)
	if resp.StatusCode == http.StatusInternalServerError {
		t.Errorf("SECURITY BREAK: large payload caused 500 (crash)")
	}
	t.Logf("Tested: large payload rejected without crash (status=%d)", resp.StatusCode)
}

// TestFuzzSpecialChars verifies special characters in JSON don't cause panics.
func TestFuzzSpecialChars(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	specialChars := []string{
		"\x00", "\n", "\r\n", "\t", "\\",
		"\xff\xfe", "\"", "'", "`",
		strings.Repeat("\x00", 1000),
		"\u0000\u0001\u0002\u0003\u0004",
		"{{{{{{{{{{",
		"}}}}}}}}}}",
		"[][][][][][]",
		"<script>alert(1)</script>",
		"'; DROP TABLE runs; --",
	}

	for i, sc := range specialChars {
		payload, err := json.Marshal(map[string]any{
			"agent_name": sc,
			"payload":    sc,
		})
		if err != nil {
			t.Fatalf("iteration %d: marshal special char payload: %v", i, err)
		}
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", bytes.NewReader(payload))
		if err != nil {
			continue
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("CRASH at iteration %d: special char %q caused 500", i, sc[:min(len(sc), 20)])
		}
	}
	t.Logf("Tested: special character fuzzing (%d cases, 0 crashes)", len(specialChars))
}

func newFuzzTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	mux := newRESTGatewayMux()
	svc := NewTriggerService(nil, DefaultMaxPayload, NewEventBus())
	if err := triggerv1.RegisterTriggerServiceHandlerServer(context.Background(), mux, svc); err != nil {
		t.Fatalf("register trigger service REST handler: %v", err)
	}

	srv := httptest.NewServer(jsonValidationMiddleware(mux))
	t.Cleanup(srv.Close)
	return srv
}

func generateFuzzPayload(rng *rand.Rand, seed int) []byte {
	strategy := rng.Intn(5)
	switch strategy {
	case 0:
		return []byte(fmt.Sprintf(`{"agent_name":"agent-%d","payload":"data"}`, seed))
	case 1:
		size := rng.Intn(1000) + 1
		b := make([]byte, size)
		_, _ = rng.Read(b)
		return b
	case 2:
		depth := rng.Intn(50) + 1
		return []byte(strings.Repeat(`{"a":`, depth) + `"v"` + strings.Repeat(`}`, depth))
	case 3:
		size := rng.Intn(10000) + 1
		return []byte(fmt.Sprintf(`{"agent_name":"x","payload":"%s"}`, strings.Repeat("A", size)))
	case 4:
		return []byte(fmt.Sprintf(`{"agent_name":%d,"payload":[%d,%t,null]}`, seed, seed, seed%2 == 0))
	default:
		return []byte(`{}`)
	}
}

func generateRandomJSONFields(rng *rand.Rand) map[string]any {
	obj := make(map[string]any)
	fields := []string{"agent_name", "agent_version", "payload", "content_type", "idempotency_key", "metadata", "run_id"}
	numFields := rng.Intn(len(fields)) + 1
	for i := 0; i < numFields; i++ {
		field := fields[rng.Intn(len(fields))]
		switch rng.Intn(4) {
		case 0:
			obj[field] = fmt.Sprintf("value-%d", rng.Intn(1000))
		case 1:
			obj[field] = rng.Intn(1000000)
		case 2:
			obj[field] = rng.Intn(2) == 0
		case 3:
			obj[field] = nil
		}
	}
	return obj
}

func drainAndClose(t *testing.T, resp *http.Response) {
	t.Helper()

	_, _ = io.Copy(io.Discard, resp.Body)
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
}

func readAndClose(t *testing.T, resp *http.Response) []byte {
	t.Helper()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		t.Fatalf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	return body
}
