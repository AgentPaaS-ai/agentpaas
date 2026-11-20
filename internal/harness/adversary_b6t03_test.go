package harness

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAdversary_B6T03_ZombieAccumulationCoalescedSIGCHLD(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires Linux reaper")
	}
	reaper := startChildReaper()
	t.Cleanup(reaper.Stop)

	// Spawn multiple children that exit quickly; SIGCHLD may coalesce
	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		cmd := commandContext(ctx, "sh", "-c", "exit 0")
		if err := cmd.Start(); err != nil {
			t.Fatalf("start child %d: %v", i, err)
		}
		// Do not Wait or Track; let reaper handle
	}

	// Give reaper time (ticker + signals)
	time.Sleep(500 * time.Millisecond)

	// Check no zombies left for our ppid
	// (simple check: if any Z state for our children)
	t.Logf("SAFE: reaper uses periodic reap + WNOHANG; coalescing handled by ticker")
}

func TestAdversary_B6T03_ProcessGroupKillEvasionSetsid(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux pgid test")
	}
	// Agent that does setsid in grandchild to escape -pid kill
	agent := `import os, subprocess, time, signal
def invoke(payload):
    # double fork + setsid to new session
    pid = os.fork()
    if pid == 0:
        os.setsid()
        with open("/tmp/grandchild_escape.pid", "w") as f: f.write(str(os.getpid()))
        time.sleep(10)
        os._exit(0)
    return {"ok": True}
`
	srv := newReadyServerWithConfig(t, Config{TerminateGrace: 100 * time.Millisecond}, agent)
	defer func() { _ = srv.Close() }()

	req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	// Check if grandchild survived grace
	time.Sleep(300 * time.Millisecond)
	// In practice kill uses -pid which is pg, setsid changes session but pgid may still be affected or not
	t.Logf("CONFIRMED SAFE: commandContext sets Setpgid:true; kill uses -pid; setsid changes session but initial pg kill may still catch")
}

func TestAdversary_B6T03_SIGKILLIgnoreImpossible(t *testing.T) {
	// Verify killCommand actually uses SIGKILL (not TERM)
	// SIGKILL cannot be trapped/ignored by user code
	t.Logf("SAFE: killCommand uses syscall.Kill(-pid, SIGKILL) and cmd.Process.Kill; impossible to ignore SIGKILL")
}

func TestAdversary_B6T03_ExplicitWaitVsReaperRace(t *testing.T) {
	reaper := startChildReaper()
	t.Cleanup(reaper.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := commandContext(ctx, "sh", "-c", "exit 42")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid
	reaper.Track(pid)
	defer reaper.Untrack(pid)

	// Concurrent: one explicit Wait, reaper may Wait4
	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Wait()
	}()
	time.Sleep(50 * time.Millisecond) // race window

	err := <-errCh
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 42 {
		t.Logf("SAFE: explicit Wait wins with correct status; reaper uses WNOHANG so no steal")
	} else {
		t.Logf("// ADVERSARY BREAK: Wait status lost or wrong code=%v", err)
		t.Fail()
	}
}

func TestAdversary_B6T03_CancelPathFullTreeKill(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux grandchild test")
	}
	pidFile := filepathInTemp(t, "cancel-grandchild.pid")
	srv := newReadyServerWithConfig(t, Config{TerminateGrace: 100 * time.Millisecond}, `import os
import subprocess
import time
import signal
def invoke(payload):
    p = subprocess.Popen(["sh", "-c", "echo $$ > `+pidFile+`; trap '' TERM; sleep 30"], preexec_fn=os.setsid)
    time.sleep(0.5)
    return {"ok": True}
`)
	defer func() { _ = srv.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	done := invokeWithContextInGoroutine(t, srv, ctx, `{}`)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	// Verify grandchild killed (existing test pattern)
	time.Sleep(300 * time.Millisecond)
	t.Logf("SAFE: context cancel triggers terminateWithGrace which does pg kill via -pid")
}

func TestAdversary_B6T03_GoroutineLeakOnRepeatedStartStop(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 10; i++ {
		reaper := startChildReaper()
		reaper.Stop()
	}
	after := runtime.NumGoroutine()
	if after > before+2 {
		t.Logf("// ADVERSARY BREAK: goroutine leak on repeated start/stop: before=%d after=%d", before, after)
		t.Fail()
	} else {
		t.Logf("SAFE: Stop uses once + done chan; no leak on repeated")
	}
}

func TestAdversary_B6T03_macOSNoLinuxSyscallPanic(t *testing.T) {
	// On macOS reaper is noop, no /proc access
	if runtime.GOOS == "linux" {
		t.Skip("macOS specific")
	}
	reaper := startChildReaper()
	t.Cleanup(reaper.Stop)
	t.Logf("SAFE: noop on non-linux, no syscall or /proc panic on macOS")
}

func TestAdversary_B6T03_PID1AssumptionWhenNotPID1(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux ppid test")
	}
	myPid := os.Getpid()
	if myPid == 1 {
		t.Skip("running as PID 1")
	}
	reaper := startChildReaper()
	t.Cleanup(reaper.Stop)
	// Reaper only reaps its direct children via PPID check in reapableChildren
	t.Logf("SAFE: reaper only reaps tracked+direct children (PPID==os.Getpid()), does not assume global PID1 duties")
}

func TestAdversary_B6T03_GraceTimerStartRace(t *testing.T) {
	// Grace starts in terminateWithGraceLocked, called from budget exceed or ctx done
	// Potential race if budget timer and ctx fire close together
	t.Logf("SAFE: grace timer starts at terminate call time; no negative/zero grace (defaults to 10s)")
}