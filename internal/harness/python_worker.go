package harness

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

type pythonWorker struct {
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderrFile *os.File
	decoder    *json.Decoder

	stdoutPath string
	stderrPath string

	mu     sync.Mutex
	closed bool
}

type workerMessage struct {
	Type   string         `json:"type"`
	Result map[string]any `json:"result,omitempty"`
	Reason string         `json:"reason,omitempty"`
	Detail string         `json:"detail,omitempty"`
}

func startPythonWorker(cfg Config) (*pythonWorker, *ErrorResponse) {
	if cfg.AgentPath == "" {
		return nil, &ErrorResponse{Status: "FAILED", Reason: "missing_agent_path", Detail: "agent path is required"}
	}
	if !loopbackAddr(cfg.Addr) {
		return nil, &ErrorResponse{Status: "FAILED", Reason: "invalid_listen_addr", Detail: "harness must listen on a loopback address"}
	}

	stdoutCapture, err := os.OpenFile(cfg.StdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, &ErrorResponse{Status: "FAILED", Reason: "stdout_capture_failed", Detail: err.Error()}
	}
	defer func() { _ = stdoutCapture.Close() }()

	stderrCapture, err := os.OpenFile(cfg.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, &ErrorResponse{Status: "FAILED", Reason: "stderr_capture_failed", Detail: err.Error()}
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	cmd := commandContext(workerCtx, cfg.Python, "-u", "-c", pythonRunner, cfg.AgentPath, cfg.StdoutPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = stderrCapture.Close()
		return nil, &ErrorResponse{Status: "FAILED", Reason: "worker_stdin_failed", Detail: err.Error()}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = stdin.Close()
		_ = stderrCapture.Close()
		return nil, &ErrorResponse{Status: "FAILED", Reason: "worker_stdout_failed", Detail: err.Error()}
	}
	cmd.Stderr = stderrCapture

	if err := cmd.Start(); err != nil {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrCapture.Close()
		return nil, &ErrorResponse{Status: "FAILED", Reason: "worker_start_failed", Detail: err.Error()}
	}

	decoder := json.NewDecoder(bufio.NewReader(stdout))
	msg, errResp := waitForImport(cmd, cancel, stdin, stdout, stderrCapture, decoder, cfg.ImportTimeout)
	if errResp != nil {
		return nil, errResp
	}
	if msg.Type == "import_failed" {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrCapture.Close()
		_ = cmd.Wait()
		return nil, &ErrorResponse{Status: "FAILED", Reason: msg.Reason, Detail: msg.Detail}
	}
	if msg.Type != "ready" {
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrCapture.Close()
		_ = cmd.Wait()
		return nil, &ErrorResponse{Status: "FAILED", Reason: "import_failed", Detail: fmt.Sprintf("unexpected worker message %q", msg.Type)}
	}

	return &pythonWorker{
		cmd:        cmd,
		cancel:     cancel,
		stdin:      stdin,
		stdout:     stdout,
		stderrFile: stderrCapture,
		decoder:    decoder,
		stdoutPath: cfg.StdoutPath,
		stderrPath: cfg.StderrPath,
	}, nil
}

func waitForImport(cmd *exec.Cmd, cancel context.CancelFunc, stdin io.Closer, stdout io.Closer, stderrFile io.Closer, decoder *json.Decoder, timeout time.Duration) (workerMessage, *ErrorResponse) {
	msgCh := make(chan workerMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		var msg workerMessage
		if err := decoder.Decode(&msg); err != nil {
			errCh <- err
			return
		}
		msgCh <- msg
	}()

	select {
	case msg := <-msgCh:
		return msg, nil
	case err := <-errCh:
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrFile.Close()
		killCommand(cmd)
		_ = cmd.Wait()
		return workerMessage{}, &ErrorResponse{Status: "FAILED", Reason: "import_failed", Detail: err.Error()}
	case <-time.After(timeout):
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrFile.Close()
		killCommand(cmd)
		_ = cmd.Wait()
		return workerMessage{}, &ErrorResponse{Status: "FAILED", Reason: "import_timeout", Detail: "agent import timed out"}
	}
}

func (w *pythonWorker) Invoke(ctx context.Context, payload map[string]any) (*InvokeResponse, *ErrorResponse) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil, &ErrorResponse{Status: "FAILED", Reason: "worker_closed", Detail: "python worker is closed"}
	}

	done := make(chan workerMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		if err := json.NewEncoder(w.stdin).Encode(payload); err != nil {
			errCh <- err
			return
		}
		var msg workerMessage
		if err := w.decoder.Decode(&msg); err != nil {
			errCh <- err
			return
		}
		done <- msg
	}()

	select {
	case <-ctx.Done():
		_ = w.terminateLocked()
		return nil, &ErrorResponse{Status: "FAILED", Reason: "invoke_timeout", Detail: ctx.Err().Error()}
	case err := <-errCh:
		return nil, &ErrorResponse{Status: "FAILED", Reason: "invoke_failed", Detail: err.Error()}
	case msg := <-done:
		if msg.Type != "ok" {
			reason := msg.Reason
			if reason == "" {
				reason = "invoke_failed"
			}
			return nil, &ErrorResponse{Status: "FAILED", Reason: reason, Detail: msg.Detail}
		}
		if err := validateResultKeys(msg.Result); err != nil {
			return nil, &ErrorResponse{Status: "FAILED", Reason: "invalid_result", Detail: err.Error()}
		}
		return &InvokeResponse{
			Status: "OK",
			Result: msg.Result,
			Stdout: w.stdoutPath,
			Stderr: w.stderrPath,
		}, nil
	}
}

func validateResultKeys(value any) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if strings.HasPrefix(key, "__") {
				return fmt.Errorf("result key %q is reserved", key)
			}
			for _, r := range key {
				if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
					return fmt.Errorf("result key %q contains a control character", key)
				}
			}
			if err := validateResultKeys(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateResultKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *pythonWorker) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	joined := w.terminateLocked()
	w.mu.Unlock()

	return joined
}

func (w *pythonWorker) terminateLocked() error {
	w.closed = true

	var joined error
	if w.cancel != nil {
		w.cancel()
	}
	if w.stdin != nil {
		joined = errors.Join(joined, w.stdin.Close())
	}
	if w.stdout != nil {
		joined = errors.Join(joined, w.stdout.Close())
	}
	if w.cmd != nil && w.cmd.Process != nil {
		joined = errors.Join(joined, killCommand(w.cmd))
		joined = errors.Join(joined, w.cmd.Wait())
	}
	if w.stderrFile != nil {
		joined = errors.Join(joined, w.stderrFile.Close())
	}
	return joined
}

func killCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil || errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return cmd.Process.Kill()
}

const pythonRunner = `
import importlib.util
import json
import os
import resource
import sys
import traceback

agent_path = sys.argv[1]
stdout_path = sys.argv[2]
protocol = os.fdopen(os.dup(1), "w", buffering=1)
sys.stdout = open(stdout_path, "a", buffering=1)

def send(value):
    protocol.write(json.dumps(value, separators=(",", ":")) + "\n")
    protocol.flush()

def apply_resource_limits():
    # Full container sandboxing is deferred to a later block. These rlimits are
    # a best-effort first layer that bounds CPU, file growth, and child process
    # creation inherited by user agent code.
    limits = [
        ("RLIMIT_CPU", 30),
        ("RLIMIT_FSIZE", 64 * 1024 * 1024),
        ("RLIMIT_NPROC", 0),
    ]
    for name, soft in limits:
        if not hasattr(resource, name):
            continue
        kind = getattr(resource, name)
        try:
            _, hard = resource.getrlimit(kind)
            if hard != resource.RLIM_INFINITY:
                soft = min(soft, hard)
            resource.setrlimit(kind, (soft, hard))
        except (OSError, ValueError):
            pass

apply_resource_limits()

try:
    spec = importlib.util.spec_from_file_location("agentpaas_user_agent", agent_path)
    if spec is None or spec.loader is None:
        raise RuntimeError("unable to load agent module")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    invoke = getattr(module, "invoke", None)
    if not callable(invoke):
        raise RuntimeError("agent module must define callable invoke(payload)")
except Exception:
    send({"type": "import_failed", "reason": "import_failed", "detail": traceback.format_exc()})
    sys.exit(2)

send({"type": "ready"})

for line in sys.stdin:
    try:
        payload = json.loads(line)
        result = invoke(payload)
        send({"type": "ok", "result": result})
    except Exception:
        send({"type": "failed", "reason": "invoke_failed", "detail": traceback.format_exc()})
`
