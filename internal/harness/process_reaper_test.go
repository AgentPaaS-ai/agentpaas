package harness

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestChildReaperReapsUnwaitedChild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc")
	}

	reaper := startChildReaper()
	t.Cleanup(reaper.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := commandContext(ctx, "sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	assertProcessGone(t, cmd.Process.Pid, 2*time.Second)
}

func TestWorkerExitStatusNotStolenWithReaperActive(t *testing.T) {
	reaper := startChildReaper()
	t.Cleanup(reaper.Stop)

	for _, tc := range []struct {
		name string
		code int
	}{
		{name: "zero", code: 0},
		{name: "one", code: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			cmd := commandContext(ctx, "sh", "-c", "exit "+strconv.Itoa(tc.code))
			if err := cmd.Start(); err != nil {
				t.Fatalf("start child: %v", err)
			}
			reaper.Track(cmd.Process.Pid)
			defer reaper.Untrack(cmd.Process.Pid)

			err := cmd.Wait()
			if tc.code == 0 {
				if err != nil {
					t.Fatalf("wait error = %v, want nil", err)
				}
				return
			}
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				t.Fatalf("wait error = %T %[1]v, want ExitError", err)
			}
			if got := exitErr.ExitCode(); got != tc.code {
				t.Fatalf("exit code = %d, want %d", got, tc.code)
			}
		})
	}
}

func TestChildReaperStopDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	reaper := startChildReaper()
	reaper.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutines after stop = %d, before = %d", runtime.NumGoroutine(), before)
}

func TestProcessGroupKillKillsGrandchildOnBudgetTimeout(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc")
	}

	pidFile := filepathInTemp(t, "grandchild.pid")
	srv := newReadyServerWithConfig(t, Config{TerminateGrace: 100 * time.Millisecond}, `import os
import subprocess
import time

def invoke(payload):
    child = subprocess.Popen(["sleep", "60"])
    with open(payload["pid_file"], "w") as f:
        f.write(str(child.pid))
    while True:
        time.sleep(0.05)
`)

	invoke := invokeInGoroutine(t, srv, `{"pid_file":`+strconv.Quote(pidFile)+`,"budget":{"wall_clock_seconds":1}}`)
	grandchildPID := waitForPIDFile(t, pidFile)
	got := <-invoke
	if got.status != StatusBudgetExceeded {
		t.Fatalf("invoke status = %q, want %q; body %s", got.status, StatusBudgetExceeded, got.body)
	}
	assertProcessGone(t, grandchildPID, 2*time.Second)
}

func TestCancelPathKillsGrandchildProcessGroup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("requires /proc")
	}

	pidFile := filepathInTemp(t, "cancel-grandchild.pid")
	srv := newReadyServerWithConfig(t, Config{TerminateGrace: 100 * time.Millisecond}, `import os
import subprocess
import time

def invoke(payload):
    child = subprocess.Popen(["sleep", "60"])
    with open(payload["pid_file"], "w") as f:
        f.write(str(child.pid))
    while True:
        time.sleep(0.05)
`)

	ctx, cancel := context.WithCancel(context.Background())
	done := invokeWithContextInGoroutine(t, srv, ctx, `{"pid_file":`+strconv.Quote(pidFile)+`}`)
	grandchildPID := waitForPIDFile(t, pidFile)
	cancel()
	<-done
	assertProcessGone(t, grandchildPID, 2*time.Second)
}

func TestCancelPathSigtermIgnoreKilledByGraceDeadline(t *testing.T) {
	srv := newReadyServerWithConfig(t, Config{TerminateGrace: 2 * time.Second}, `import signal
import time

def ignore_term(signum, frame):
    pass

signal.signal(signal.SIGTERM, ignore_term)

def invoke(payload):
    while True:
        time.sleep(0.05)
`)

	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	done := invokeWithContextInGoroutine(t, srv, ctx, `{}`)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done

	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("cancel elapsed = %v, want worker dead within TerminateGrace + 2s", elapsed)
	}
}

func assertProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if runtime.GOOS == "linux" {
			state, readErr := processState(pid)
			if os.IsNotExist(readErr) || state == "" {
				return
			}
			if state != "Z" && readErr == nil {
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	state, _ := processState(pid)
	t.Fatalf("pid %d still exists after %v; state=%q", pid, timeout, state)
}

func processState(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "State:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1], nil
			}
		}
	}
	return "", nil
}

type invokeResult struct {
	status string
	body   string
}

func filepathInTemp(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(t.TempDir(), name)
}

func invokeInGoroutine(t *testing.T, srv *Server, body string) <-chan invokeResult {
	t.Helper()
	return invokeWithContextInGoroutine(t, srv, context.Background(), body)
}

func invokeWithContextInGoroutine(t *testing.T, srv *Server, ctx context.Context, body string) <-chan invokeResult {
	t.Helper()
	done := make(chan invokeResult, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/invoke", strings.NewReader(body)).WithContext(ctx)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		var errResp ErrorResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &errResp)
		done <- invokeResult{status: errResp.Status, body: rec.Body.String()}
	}()
	return done
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("parse pid file: %v", convErr)
			}
			return pid
		}
		if !os.IsNotExist(err) {
			t.Fatalf("read pid file: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("pid file %s was not written", path)
	return 0
}
