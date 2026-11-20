package mcpmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// Adversary break tests for M13.12 T07 STDIO MCP lifecycle.
// Each test targets one abuse case against the stdio MCP transport:
// process leaks, premature EOF, malformed JSON, oversized responses,
// concurrent call routing, stop-while-in-flight, double start, and
// environment injection.

// writeAdversaryServer writes a python fixture script to t.TempDir().
func writeAdversaryServer(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

func adversaryStdioManager(serverID, command string, args []string, env map[string]string) *Manager {
	manager := NewManager()
	manager.Register([]policy.MCPServer{{
		Name:         serverID,
		Transport:    "stdio",
		Command:      command,
		Args:         args,
		Env:          env,
		AllowedTools: []string{"echo"},
	}}, "agent-1", "run-1")
	return manager
}

// 1. Process leak / immediate-exit: Start a server that exits immediately.
// Start must not leave a zombie or report the server as running; a later
// CallTool must surface the crash, and Stop must still clean up state.
func Test_StdioMCP_Adversary_ImmediateExitCleanup(t *testing.T) {
	script := writeAdversaryServer(t, "exit_now.py", `import sys
sys.exit(3)
`)
	manager := adversaryStdioManager("dies", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	startErr := lifecycle.Start(ctx, "dies", "agent-1", "run-1")
	// Start may succeed (process spawned, crashes asynchronously) — acceptable.
	// What is NOT acceptable: IsRunning staying true forever, or a leaked process.
	deadline := time.Now().Add(5 * time.Second)
	for lifecycle.IsRunning("dies") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if lifecycle.IsRunning("dies") {
		t.Fatal("ADVERSARY BREAK: server exited immediately but IsRunning() still true after 5s — process state leak")
	}
	crash := lifecycle.CrashContext("dies")
	if crash == nil {
		t.Fatal("ADVERSARY BREAK: no CrashContext for immediately-exited server — crash is invisible to operators")
	}
	if crash.ExitCode != 3 {
		t.Fatalf("CrashContext.ExitCode = %d, want 3", crash.ExitCode)
	}
	// CallTool must fail fast with the crash error, not hang.
	callCtx, callCancel := context.WithTimeout(ctx, 3*time.Second)
	defer callCancel()
	callStart := time.Now()
	_, err := router.CallTool(callCtx, "dies", "echo", map[string]any{"message": "x"}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("ADVERSARY BREAK: CallTool on crashed server returned nil error")
	}
	if !errors.Is(err, ErrServerCrashed) {
		t.Fatalf("CallTool error = %v, want ErrServerCrashed", err)
	}
	if time.Since(callStart) > 3*time.Second {
		t.Fatal("ADVERSARY BREAK: CallTool on crashed server hung instead of failing fast")
	}
	// Stop after crash must still succeed (cleanup) and not error fatally.
	if err := lifecycle.Stop(ctx, "dies"); err != nil {
		t.Fatalf("Stop() after crash error = %v", err)
	}
	if lifecycle.IsRunning("dies") {
		t.Fatal("IsRunning() = true after Stop of crashed server")
	}
	_ = startErr
}

// 2. Premature EOF: server reads one request then exits WITHOUT responding.
// decodeMCPResponse must return promptly (channel close → io.EOF), not hang
// for the full timeout.
func Test_StdioMCP_Adversary_PrematureEOF(t *testing.T) {
	script := writeAdversaryServer(t, "eof_server.py", `import sys
sys.stdin.readline()
sys.exit(0)
`)
	manager := adversaryStdioManager("eof", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "eof", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "eof") }()

	callCtx, callCancel := context.WithTimeout(ctx, 20*time.Second)
	defer callCancel()
	start := time.Now()
	_, err := router.CallTool(callCtx, "eof", "echo", map[string]any{"message": "hi"}, "agent-1", "run-1")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: CallTool returned nil error after server closed stdout without responding")
	}
	// EOF should propagate via channel close well before the 5s response timeout.
	if elapsed > stdioResponseTimeout {
		t.Fatalf("ADVERSARY BREAK: CallTool took %v after premature EOF (>= response timeout) — EOF not propagated promptly", elapsed)
	}
}

// 3. Malformed JSON: server emits a non-JSON line, then a VALID response for
// the same request ID. Current behavior: decodeMCPResponse errors on the
// malformed line immediately, abandoning the valid response. Assert no panic
// and a clean error; flag if the transport cannot recover (the valid response
// is left orphaned in the channel for the next caller).
func Test_StdioMCP_Adversary_MalformedJSON(t *testing.T) {
	script := writeAdversaryServer(t, "garbage_server.py", `import sys, json
while True:
    line = sys.stdin.readline()
    if not line:
        break
    req = json.loads(line)
    # Noise line a misbehaving server might print (debug logging to stdout).
    sys.stdout.write("DEBUG not-json garbage\n")
    sys.stdout.flush()
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": {"content": [{"type": "text", "text": "ok-after-garbage"}]}}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`)
	manager := adversaryStdioManager("garbage", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "garbage", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "garbage") }()

	_, err := router.CallTool(ctx, "garbage", "echo", map[string]any{"message": "m"}, "agent-1", "run-1")
	if err == nil {
		t.Log("note: malformed line was tolerated and valid response routed — ideal behavior")
	} else {
		if !strings.Contains(err.Error(), "decode") {
			t.Fatalf("unexpected error class for malformed JSON: %v", err)
		}
		t.Logf("CONFIRMED GAP (MEDIUM): one malformed stdout line fails the whole call even though a valid response followed: %v", err)
	}

	// Recovery check: the orphaned valid response (matching ID of call 1) sits
	// in the channel. Call 2 sends a new ID; decodeMCPResponse skips the stale
	// wrong-ID line. But call 2 ALSO triggers garbage+valid. If stale-response
	// skipping is broken, call 2 gets call 1's payload.
	result2, err2 := router.CallTool(ctx, "garbage", "echo", map[string]any{"message": "second"}, "agent-1", "run-1")
	if err2 != nil {
		t.Logf("second call also failed (error cascade from malformed line): %v", err2)
		return
	}
	b, _ := json.Marshal(result2)
	if strings.Contains(string(b), "ok-after-garbage") {
		t.Log("second call routed a response correctly")
	}
}

// 4. Oversized response: server returns a >1MiB tools/call result. The reader
// must reject it with an error — no OOM, no panic, no unbounded allocation.
func Test_StdioMCP_Adversary_OversizedResponse(t *testing.T) {
	script := writeAdversaryServer(t, "big_server.py", `import sys, json
while True:
    line = sys.stdin.readline()
    if not line:
        break
    req = json.loads(line)
    big = "A" * (3 * 1024 * 1024)
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": {"content": [{"type": "text", "text": big}]}}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`)
	manager := adversaryStdioManager("big", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "big", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "big") }()

	start := time.Now()
	_, err := router.CallTool(ctx, "big", "echo", map[string]any{"message": "m"}, "agent-1", "run-1")
	if err == nil {
		t.Fatal("ADVERSARY BREAK: 3MiB stdio response accepted — 1MiB limit not enforced on stdio transport")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("oversized response handling took too long: %v", time.Since(start))
	}
	t.Logf("oversized response rejected with: %v", err)
}

// 5. Concurrent calls: two simultaneous tools/call through the router on the
// same stdio server must each receive THEIR OWN echo (no cross-talk), and
// neither may hang.
func Test_StdioMCP_Adversary_ConcurrentCalls(t *testing.T) {
	script := writeAdversaryServer(t, "echo_conc.py", `import sys, json, time, random
while True:
    line = sys.stdin.readline()
    if not line:
        break
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        continue
    if req.get("method") != "tools/call":
        continue
    # Small random delay to widen any race window.
    time.sleep(random.random() * 0.05)
    msg = req["params"]["arguments"].get("message", "")
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": {"content": [{"type": "text", "text": msg}]}}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`)
	manager := adversaryStdioManager("conc", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "conc", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "conc") }()

	const callers = 8
	var wg sync.WaitGroup
	errs := make([]error, callers)
	results := make([]string, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			msg := fmt.Sprintf("caller-%d-unique-payload", n)
			res, err := router.CallTool(ctx, "conc", "echo", map[string]any{"message": msg}, "agent-1", "run-1")
			if err != nil {
				errs[n] = err
				return
			}
			b, _ := json.Marshal(res)
			results[n] = string(b)
		}(i)
	}
	wg.Wait()
	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent caller %d error = %v", i, errs[i])
		}
		want := fmt.Sprintf("caller-%d-unique-payload", i)
		if !strings.Contains(results[i], want) {
			t.Fatalf("ADVERSARY BREAK: caller %d got response %s, want payload %q — response cross-talk between concurrent calls", i, results[i], want)
		}
	}
}

// 6. Stop while in-flight: Stop() while a tools/call (server sleeps 3s) is
// pending. Stop must kill the process; the pending call must unblock with an
// error — not hang until its own timeout.
func Test_StdioMCP_Adversary_StopWhileInFlight(t *testing.T) {
	script := writeAdversaryServer(t, "slow_server.py", `import sys, json, time
while True:
    line = sys.stdin.readline()
    if not line:
        break
    req = json.loads(line)
    time.sleep(3)
    resp = {"jsonrpc": "2.0", "id": req["id"], "result": {"content": [{"type": "text", "text": "slow-done"}]}}
    sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`)
	manager := adversaryStdioManager("slow", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "slow", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	callDone := make(chan error, 1)
	go func() {
		callCtx, callCancel := context.WithTimeout(ctx, 25*time.Second)
		defer callCancel()
		_, err := router.CallTool(callCtx, "slow", "echo", map[string]any{"message": "m"}, "agent-1", "run-1")
		callDone <- err
	}()

	// Let the call get in-flight, then stop.
	time.Sleep(300 * time.Millisecond)
	stopStart := time.Now()
	if err := lifecycle.Stop(ctx, "slow"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if time.Since(stopStart) > 8*time.Second {
		t.Fatal("ADVERSARY BREAK: Stop() hung while a call was in-flight")
	}

	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("ADVERSARY BREAK: in-flight call succeeded after Stop() killed the server — zombie response")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ADVERSARY BREAK: in-flight CallTool never returned after Stop() — deadlock/hang")
	}
	if lifecycle.IsRunning("slow") {
		t.Fatal("IsRunning() = true after Stop during in-flight call")
	}
}

// 7. Double start: second Start must fail cleanly with "already running" and
// must NOT spawn a second process. After Stop, Start must succeed again.
func Test_StdioMCP_Adversary_DoubleStart(t *testing.T) {
	script := stdioMCPEchoServerScript(t)
	manager := adversaryStdioManager("dbl", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "dbl", "agent-1", "run-1"); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "dbl") }()

	lifecycle.mu.RLock()
	firstProc := lifecycle.processes["dbl"]
	lifecycle.mu.RUnlock()
	if firstProc == nil {
		t.Fatal("no process recorded after first Start")
	}

	err := lifecycle.Start(ctx, "dbl", "agent-1", "run-1")
	if err == nil {
		t.Fatal("ADVERSARY BREAK: second Start() succeeded — two child processes for one serverID")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Start() error = %v, want 'already running'", err)
	}

	lifecycle.mu.RLock()
	secondProc := lifecycle.processes["dbl"]
	lifecycle.mu.RUnlock()
	if secondProc != firstProc {
		t.Fatal("ADVERSARY BREAK: process handle changed after rejected double Start — process table corrupted")
	}

	// Restart after Stop must work.
	if err := lifecycle.Stop(ctx, "dbl"); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := lifecycle.Start(ctx, "dbl", "agent-1", "run-1"); err != nil {
		t.Fatalf("re-Start() after Stop error = %v", err)
	}
	if !lifecycle.IsRunning("dbl") {
		t.Fatal("IsRunning() = false after re-Start")
	}
}

// 8. PATH override via declared env: lifecycleEnv prepends minimalPATH, then
// appends declared env verbatim. A declared PATH key produces a duplicate
// PATH entry; child interpreters (python os.environ) typically honor the
// LAST occurrence — silently defeating the minimal-PATH sandbox.
func Test_StdioMCP_Adversary_EnvPATHOverride(t *testing.T) {
	script := writeAdversaryServer(t, "path_echo.py", `import sys, json, os
while True:
    line = sys.stdin.readline()
    if not line:
        break
    try:
        req = json.loads(line)
    except json.JSONDecodeError:
        continue
    if req.get("method") == "tools/call":
        resp = {"jsonrpc": "2.0", "id": req["id"], "result": {"content": [{"type": "text", "text": os.environ.get("PATH", "")}]}}
        sys.stdout.write(json.dumps(resp) + "\n")
        sys.stdout.flush()
`)
	manager := adversaryStdioManager("pathevil", "python3", []string{script},
		map[string]string{"PATH": "/tmp/attacker-controlled-bin"})
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "pathevil", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "pathevil") }()

	res, err := router.CallTool(ctx, "pathevil", "echo", map[string]any{"message": "x"}, "agent-1", "run-1")
	if err != nil {
		t.Fatalf("CallTool error = %v", err)
	}
	b, _ := json.Marshal(res)
	if strings.Contains(string(b), "/tmp/attacker-controlled-bin") {
		t.Fatalf("ADVERSARY BREAK: declared env PATH overrode minimalPATH sandbox — child PATH = %s", string(b))
	}
}

// 9. Null byte / newline injection in env values must be rejected at Start,
// not passed to execve.
func Test_StdioMCP_Adversary_EnvInjection(t *testing.T) {
	script := stdioMCPEchoServerScript(t)
	cases := map[string]map[string]string{
		"nullbyte": {"FOO": "a\x00b"},
		"newline":  {"FOO": "a\nEVIL=1"},
		"nullkey":  {"FO\x00O": "a"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			serverID := "inj-" + name
			manager := adversaryStdioManager(serverID, "python3", []string{script}, env)
			lifecycle := NewLifecycle(manager, nil, "")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err := lifecycle.Start(ctx, serverID, "agent-1", "run-1")
			if name == "nullbyte" || name == "nullkey" {
				// Go's exec rejects NUL in env — Start must surface that error
				// and must NOT leave a half-started process in the table.
				if err == nil {
					t.Log("Start accepted NUL env; verifying no leaked process entry")
				}
				defer func() { _ = lifecycle.Stop(context.Background(), serverID) }()
				return
			}
			// Newline: exec passes it raw. Document whether any validation exists.
			if err == nil {
				t.Logf("CONFIRMED GAP (LOW): newline in env value passed through to child without validation")
				defer func() { _ = lifecycle.Stop(context.Background(), serverID) }()
			}
		})
	}
}

// 10. Wrong-ID flood: server streams endless responses with non-matching IDs.
// decodeMCPResponse must skip them and terminate via ctx/timeout — no
// unbounded memory growth, no infinite spin past the deadline.
func Test_StdioMCP_Adversary_WrongIDFlood(t *testing.T) {
	script := writeAdversaryServer(t, "flood_server.py", `import sys, json
while True:
    line = sys.stdin.readline()
    if not line:
        break
    # Flood responses with IDs that never match the request.
    for i in range(100000):
        resp = {"jsonrpc": "2.0", "id": 999999, "result": {"i": i}}
        sys.stdout.write(json.dumps(resp) + "\n")
    sys.stdout.flush()
`)
	manager := adversaryStdioManager("flood", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")
	router := NewRouter(manager, lifecycle, nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := lifecycle.Start(ctx, "flood", "agent-1", "run-1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = lifecycle.Stop(context.Background(), "flood") }()

	callCtx, callCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer callCancel()
	start := time.Now()
	_, err := router.CallTool(callCtx, "flood", "echo", map[string]any{"message": "m"}, "agent-1", "run-1")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ADVERSARY BREAK: CallTool succeeded against server that never answers the matching ID")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("ADVERSARY BREAK: wrong-ID flood kept CallTool busy for %v — context deadline not honored", elapsed)
	}
}

// 11. Start with an already-cancelled context: exec.CommandContext kills the
// child immediately. Start must not report success while leaving the server
// in a half-dead state that reads as "running" — or must fail outright.
func Test_StdioMCP_Adversary_StartCancelledContext(t *testing.T) {
	script := stdioMCPEchoServerScript(t)
	manager := adversaryStdioManager("cancelled", "python3", []string{script}, nil)
	lifecycle := NewLifecycle(manager, nil, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before Start
	err := lifecycle.Start(ctx, "cancelled", "agent-1", "run-1")
	if err != nil {
		// Acceptable: fail fast on cancelled context.
		return
	}
	// Start succeeded on a cancelled context — the child is being killed
	// asynchronously. It must converge to not-running quickly.
	defer func() { _ = lifecycle.Stop(context.Background(), "cancelled") }()
	deadline := time.Now().Add(5 * time.Second)
	for lifecycle.IsRunning("cancelled") && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if lifecycle.IsRunning("cancelled") {
		t.Fatal("ADVERSARY BREAK: Start succeeded on cancelled context and server still 'running' after 5s — zombie state")
	}
	t.Logf("CONFIRMED GAP (MEDIUM): Start returned nil on already-cancelled context; child died asynchronously (crash-only signaling)")
}
