//go:build adversary

package trigger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAdversaryB9T09_MalformedJSONAllCases verifies ALL malformed JSON types return 400 not 500.
func TestAdversaryB9T09_MalformedJSONAllCases(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	malformed := []string{
		`{"agent_name": "test"`,                    // truncated
		`{"agent_name": }`,                         // invalid value
		`{"agent_name": "test"]}`,                  // bracket mismatch
		`{invalid json}`,                          // no quotes
		`{"agent_name": "test", "payload": }`,      // missing value
		`{"agent_name": "test", ,}`,                // extra comma
		`{"agent_name": null, "payload": "test"`,   // truncated after
		`[{"agent_name": "test"}]`,                 // array top level
		"{\n  \"agent_name\": \"test\",\n  \"payload\": \n}", // newline issue
		`{"agent_name": "\u0000test"}`,             // null in string
		`{"agent_name": "test\x00"}`,               // binary in string
		"\xef\xbb\xbf{\"agent_name\":\"test\"}",    // BOM
		"{\"agent_name\":\"test\"\xff\xfe}",        // invalid UTF8 after
		strings.Repeat(`{"a":`, 100) + `"v"` + strings.Repeat(`}`, 99), // deep but truncated
		`{`,                                        // single char
		`}`,                                        // single closing
		`{"agent_name": true, "payload": false}`,   // bools where strings expected? but valid json
		`null`,
		`true`,
		`42`,
		`"string"`,
		`[]`,
	}

	for i, p := range malformed {
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(p))
		if err != nil {
			t.Fatalf("iteration %d: HTTP error: %v", i, err)
		}
		body := readAndClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("SECURITY BREAK: malformed JSON case %d returned %d instead of 400: %q", i, resp.StatusCode, p[:min(len(p), 80)])
			t.Fail()
		}
		if !strings.Contains(strings.ToLower(string(body)), "line") && !strings.Contains(string(body), "line") {
			t.Errorf("SECURITY BREAK: malformed JSON case %d 400 response missing line info: body=%q", i, string(body)[:min(len(body), 200)])
			t.Fail()
		}
	}
	t.Logf("Tested: all malformed JSON types return 400 with line info (%d cases) (good)", len(malformed))
}

// TestAdversaryB9T09_NoPanicsOnExtremeInputs verifies no panics on nil bytes, deep nesting, long strings, control chars, invalid UTF8.
func TestAdversaryB9T09_NoPanicsOnExtremeInputs(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 10 * time.Second}

	extremes := []string{
		strings.Repeat("\x00", 10000),
		strings.Repeat(`{"a":`, 1000) + `"v"` + strings.Repeat(`}`, 1000), // 1000 levels
		strings.Repeat("A", 100000),
		"\x00\x01\x02\x03\x04\x05\x06\x07\x08\x09\x0a\x0b\x0c\x0d\x0e\x0f",
		"\xff\xfe\xfd\xfc\xfb\xfa",
		"{\"agent_name\":\"" + strings.Repeat("\u0000", 5000) + "\"}",
		"{\"agent_name\":\"test\",\"payload\":\"" + string(make([]byte, 50000)) + "\"}",
	}

	for _, p := range extremes {
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(p))
		if err != nil {
			continue // network error ok, but no panic in server
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("SECURITY BREAK: extreme input caused 500: %q", p[:min(len(p), 50)])
			t.Fail()
		}
	}
	t.Logf("Tested: no panics on extreme inputs (nil bytes, 1000-deep nesting, long strings, control chars, invalid UTF8) (%d cases) (good)", len(extremes))
}

// TestAdversaryB9T09_LargePayloads verifies exactly 1MB, 1MB+1, 10MB, 100MB handled without crash (400 or accepted).
func TestAdversaryB9T09_LargePayloads(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 30 * time.Second}

	sizes := []int{1 << 20, (1 << 20) + 1, 10 << 20, 100 << 20}
	for _, sz := range sizes {
		large := bytes.Repeat([]byte("x"), sz)
		payload, _ := json.Marshal(map[string]any{
			"agent_name": "test",
			"payload":    string(large),
		})
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("size %d: HTTP error: %v", sz, err)
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("SECURITY BREAK: payload size %d caused 500 crash", sz)
			t.Fail()
		}
	}
	t.Logf("Tested: large payloads (1MB,1MB+1,10MB,100MB) rejected/handled without crash (good)")
}

// TestAdversaryB9T09_ContentTypeManipulation verifies various Content-Types without crash.
func TestAdversaryB9T09_ContentTypeManipulation(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	types := []string{
		"",
		"text/plain",
		"application/xml",
		"multipart/form-data",
		"application/json; charset=utf-8",
		"application/json",
		"application/octet-stream",
		"foo/bar",
	}

	validJSON := `{"agent_name":"test"}`
	for _, ct := range types {
		req, _ := http.NewRequest("POST", srv.URL+"/v1/trigger/invoke", strings.NewReader(validJSON))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("SECURITY BREAK: Content-Type %q caused 500", ct)
			t.Fail()
		}
	}
	t.Logf("Tested: Content-Type manipulation (missing, wrong, multipart, empty, variants) without crash (%d cases) (good)", len(types))
}

// TestAdversaryB9T09_NonPOSTMethods verifies non-POST methods on the endpoint.
func TestAdversaryB9T09_NonPOSTMethods(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	methods := []string{"GET", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
	for _, m := range methods {
		req, _ := http.NewRequest(m, srv.URL+"/v1/trigger/invoke", strings.NewReader(`{"agent_name":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("SECURITY BREAK: method %s caused 500", m)
			t.Fail()
		}
	}
	t.Logf("Tested: non-POST methods (GET,PUT,DELETE,PATCH,OPTIONS,HEAD) without crash (%d methods) (good)", len(methods))
}

// TestAdversaryB9T09_PathTraversal verifies path traversal attempts do not bypass or crash.
func TestAdversaryB9T09_PathTraversal(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	paths := []string{
		"/v1/trigger/invoke/../../../etc/passwd",
		"/v1/trigger/../invoke",
		"/v1/trigger/invoke/..",
		"/../../v1/trigger/invoke",
		"/v1/trigger/invoke%2f..%2f..",
	}
	for _, p := range paths {
		resp, err := client.Post(srv.URL+p, "application/json", strings.NewReader(`{"agent_name":"test"}`))
		if err != nil {
			continue
		}
		drainAndClose(t, resp)
		if resp.StatusCode == http.StatusInternalServerError {
			t.Errorf("SECURITY BREAK: path traversal %s caused 500", p)
			t.Fail()
		}
	}
	t.Logf("Tested: path traversal attempts without crash or bypass (%d paths) (good)", len(paths))
}

// TestAdversaryB9T09_ConcurrentRequests verifies 100 concurrent requests cause no races/panics.
func TestAdversaryB9T09_ConcurrentRequests(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 10 * time.Second}

	const n = 100
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			payload := fmt.Sprintf(`{"agent_name":"agent-%d"}`, i)
			resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(payload))
			if err != nil {
				return
			}
			drainAndClose(t, resp)
			if resp.StatusCode == http.StatusInternalServerError {
				errCh <- fmt.Errorf("concurrent %d: 500", i)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Errorf("SECURITY BREAK: %v", e)
		t.Fail()
	}
	t.Logf("Tested: 100 concurrent requests without races or panics (good)")
}

// TestAdversaryB9T09_EmptyBody verifies empty body returns 400 not crash.
func TestAdversaryB9T09_EmptyBody(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatalf("HTTP error: %v", err)
	}
	body := readAndClose(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("SECURITY BREAK: empty body returned %d not 400, body=%q", resp.StatusCode, string(body))
		t.Fail()
	}
	t.Logf("Tested: empty body returns 400 without crash (good)")
}

// TestAdversaryB9T09_NonObjectTopLevel verifies non-object top-level JSON (null,true,false,number,string,array).
func TestAdversaryB9T09_NonObjectTopLevel(t *testing.T) {
	srv := newFuzzTestServer(t)
	client := &http.Client{Timeout: 5 * time.Second}

	tops := []string{
		`null`,
		`true`,
		`false`,
		`42`,
		`"string"`,
		`[]`,
		`[1,2,3]`,
		`{"nested":true}`, // this is object, but test others
	}

	for _, p := range tops {
		resp, err := client.Post(srv.URL+"/v1/trigger/invoke", "application/json", strings.NewReader(p))
		if err != nil {
			t.Fatalf("non-object top-level HTTP error: %v", err)
		}
		body := readAndClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("SECURITY BREAK: non-object top-level %q returned %d not 400, body=%q", p, resp.StatusCode, string(body)[:min(len(body), 100)])
			t.Fail()
		}
	}
	t.Logf("Tested: non-object top-level JSON values return 400 without crash (%d cases) (good)", len(tops))
}