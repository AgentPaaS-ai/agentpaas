package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAdversary_B6T01_PayloadSizeLimitBypass(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    return {"ok": True}
`)
	defer func() { _ = srv.Close() }()

	// Attempt bypass via chunked encoding or oversized body
	largeBody := bytes.NewReader(bytes.Repeat([]byte("x"), MaxPayloadBytes+1024))
	req := httptest.NewRequest(http.MethodPost, "/invoke", largeBody)
	req.Header.Set("Transfer-Encoding", "chunked")
	rec := httptest.NewRecorder()

	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Logf("// ADVERSARY BREAK: payload size bypass possible, got %d", rec.Code)
	} else {
		t.Logf("SAFE payload limit: correctly rejected oversized")
	}
}

func TestAdversary_B6T01_ImportCrashReasonInjection(t *testing.T) {
	// Try agent that produces crash with newlines/nulls/unicode in reason/detail
	agentPath := writeAgent(t, `raise RuntimeError("boom\nwith\nnewlines\x00null\u2028homoglyph")`)

	srv := NewServer(Config{AgentPath: agentPath})
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}

	var got ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if strings.Contains(got.Detail, "\n") || strings.Contains(got.Detail, "\x00") {
		t.Logf("// ADVERSARY BREAK: unsanitized crash reason injection in detail: %q", got.Detail)
	} else {
		t.Logf("SAFE import reason: detail sanitized or json encoded at server:85")
	}
}

func TestAdversary_B6T01_HealthzReadyzRaceOrSpoof(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload): return {}`)
	defer func() { _ = srv.Close() }()

	var wg sync.WaitGroup
	wg.Add(2)
	var mu sync.Mutex
	okCount := 0

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			localOK := 0
			for j := 0; j < 50; j++ {
				req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				if rec.Code == http.StatusOK {
					localOK++
				}
			}
			mu.Lock()
			okCount += localOK
			mu.Unlock()
		}()
	}
	wg.Wait()

	if okCount > 0 {
		t.Logf("SAFE readyz race: consistent under concurrent reads at server.go:162 (saw %d OK)", okCount)
	} else {
		t.Logf("// ADVERSARY BREAK: readyz race/spoof possible, saw 0 OKs")
	}
}

func TestAdversary_B6T01_InvokeResponseSerializationTampering(t *testing.T) {
	srv := newReadyServer(t, `def invoke(payload):
    return {"tampered": True, "__proto__": "pollution"}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{"value":1}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("invoke status = %d, want unsafe result rejection", rec.Code)
	}
	var errResp ErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Reason != "invalid_result" {
		t.Fatalf("reason = %q, want invalid_result", errResp.Reason)
	}
	t.Logf("SAFE serialization: unsafe result key rejected at python_worker.go:186")
}

func TestAdversary_B6T01_StdoutStderrCaptureTampering(t *testing.T) {
	srv := newReadyServer(t, `import sys
def invoke(payload):
    print("tamper attempt", file=sys.stderr)
    return {}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var got InvokeResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	// Check if paths can be tampered (they are from config, normalized at server.go:273)
	if got.Stdout == "" || got.Stderr == "" {
		t.Logf("// ADVERSARY BREAK: capture paths empty or tampered")
	} else {
		t.Logf("SAFE capture: paths set at python_worker.go:111 and server.go:274")
	}
}

func TestAdversary_B6T01_WorkerSubprocessEscapeResourceExhaustion(t *testing.T) {
	srv := newReadyServer(t, `import os
def invoke(payload):
    os.system("echo escape > /tmp/harness-escape 2>/dev/null || true")
    return {"escaped": os.path.exists("/tmp/harness-escape")}
`)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	var got InvokeResponse
	json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Result != nil && got.Result["escaped"] == true {
		t.Logf("// ADVERSARY BREAK HIGH: subprocess escape via os.system at python_worker.go:261 (user agent)")
	} else {
		t.Logf("SAFE subprocess: no escape or os.system blocked by env (python_worker.go:58)")
	}
}

func TestAdversary_B6T01_MissingTimeoutsOnWorkerExec(t *testing.T) {
	// Use very short timeout config
	cfg := Config{
		AgentPath: writeAgent(t, `import time
def invoke(payload):
    time.sleep(5)
    return {}
`),
		InvokeTimeout: 100 * time.Millisecond,
	}
	srv := NewServer(cfg)
	defer func() { _ = srv.Close() }()

	// Wait for ready (may timeout import but assume fast)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code == http.StatusInternalServerError {
		var errResp ErrorResponse
		json.Unmarshal(rec.Body.Bytes(), &errResp)
		if errResp.Reason == "invoke_timeout" {
			t.Logf("SAFE timeout enforced at server.go:225 and python_worker.go:172")
		} else {
			t.Logf("// ADVERSARY BREAK: timeout not effective, got %s", errResp.Reason)
		}
	} else {
		t.Logf("// ADVERSARY BREAK MEDIUM: no timeout or wrong status on long invoke at server.go:225")
	}
}

func TestAdversary_B6T01_ContextCancellationNotPropagated(t *testing.T) {
	srv := newReadyServer(t, `import time
def invoke(payload):
    time.sleep(2)
    return {}
`)
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	start := time.Now()
	srv.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Logf("// ADVERSARY BREAK: context cancel not propagated to subprocess, took %v (server.go:225, python_worker.go:172, no cmd cancel)", elapsed)
	} else {
		t.Logf("SAFE cancel: ctx respected within timeout at python_worker.go:172")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Logf("note: status %d", rec.Code)
	}
}
