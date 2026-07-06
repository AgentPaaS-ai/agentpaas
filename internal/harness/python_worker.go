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
	"path/filepath"
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
	reaper     *childReaper
	rpc        *harnessRPCServer

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

func startPythonWorker(cfg Config, reaper *childReaper) (*pythonWorker, *ErrorResponse) {
	if cfg.AgentPath == "" {
		errResp := &ErrorResponse{Status: "FAILED", Reason: "missing_agent_path", Detail: "agent path is required"}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if !loopbackAddr(cfg.Addr) {
		errResp := &ErrorResponse{Status: "FAILED", Reason: "invalid_listen_addr", Detail: "harness must listen on a loopback address"}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}

	stdoutCapture, err := os.OpenFile(cfg.StdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		errResp := &ErrorResponse{Status: "FAILED", Reason: "stdout_capture_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	defer func() { _ = stdoutCapture.Close() }()

	stderrCapture, err := os.OpenFile(cfg.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		errResp := &ErrorResponse{Status: "FAILED", Reason: "stderr_capture_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}

	rpcServer, err := startHarnessRPCServer(cfg.Audit)
	if err != nil {
		_ = stderrCapture.Close()
		errResp := &ErrorResponse{Status: "FAILED", Reason: "rpc_start_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}

	// Load pre-resolved credentials from the sidecar file before starting
	// the Python worker, so credential values are in memory before agent
	// code begins executing.
	if cfg.CredentialsPath != "" {
		if err := rpcServer.LoadCredentials(cfg.CredentialsPath); err != nil {
			_ = rpcServer.Close()
			_ = stderrCapture.Close()
			errResp := &ErrorResponse{Status: "FAILED", Reason: "credential_load_failed", Detail: err.Error()}
			return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
		}
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	cmd := commandContext(workerCtx, cfg.Python, "-u", "-c", pythonRunner, cfg.AgentPath, cfg.StdoutPath)
	cmd.Env = workerEnv(os.Environ(), rpcServer.Addr())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = rpcServer.Close()
		_ = stderrCapture.Close()
		errResp := &ErrorResponse{Status: "FAILED", Reason: "worker_stdin_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = rpcServer.Close()
		_ = stdin.Close()
		_ = stderrCapture.Close()
		errResp := &ErrorResponse{Status: "FAILED", Reason: "worker_stdout_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	cmd.Stderr = stderrCapture

	if err := cmd.Start(); err != nil {
		cancel()
		_ = rpcServer.Close()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrCapture.Close()
		errResp := &ErrorResponse{Status: "FAILED", Reason: "worker_start_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if reaper != nil {
		reaper.Track(cmd.Process.Pid)
	}

	decoder := json.NewDecoder(bufio.NewReader(stdout))
	msg, errResp := waitForImport(cmd, reaper, cancel, stdin, stdout, stderrCapture, decoder, cfg.ImportTimeout)
	if errResp != nil {
		_ = rpcServer.Close()
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if msg.Type == "import_failed" {
		cancel()
		_ = rpcServer.Close()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrCapture.Close()
		_ = waitCommand(cmd, reaper)
		errResp := &ErrorResponse{Status: "FAILED", Reason: msg.Reason, Detail: msg.Detail}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if msg.Type != "ready" {
		cancel()
		_ = rpcServer.Close()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrCapture.Close()
		_ = waitCommand(cmd, reaper)
		errResp := &ErrorResponse{Status: "FAILED", Reason: "import_failed", Detail: fmt.Sprintf("unexpected worker message %q", msg.Type)}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}

	return &pythonWorker{
		cmd:        cmd,
		cancel:     cancel,
		stdin:      stdin,
		stdout:     stdout,
		stderrFile: stderrCapture,
		decoder:    decoder,
		reaper:     reaper,
		rpc:        rpcServer,
		stdoutPath: cfg.StdoutPath,
		stderrPath: cfg.StderrPath,
	}, nil
}

func waitForImport(cmd *exec.Cmd, reaper *childReaper, cancel context.CancelFunc, stdin io.Closer, stdout io.Closer, stderrFile io.Closer, decoder *json.Decoder, timeout time.Duration) (workerMessage, *ErrorResponse) {
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
		// best-effort kill; error is not actionable here.
		_ = killCommand(cmd)
		_ = waitCommand(cmd, reaper)
		return workerMessage{}, &ErrorResponse{Status: "FAILED", Reason: "import_failed", Detail: err.Error()}
	case <-time.After(timeout):
		cancel()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderrFile.Close()
		// best-effort kill; error is not actionable here.
		_ = killCommand(cmd)
		_ = waitCommand(cmd, reaper)
		return workerMessage{}, &ErrorResponse{Status: "FAILED", Reason: "import_timeout", Detail: "agent import timed out"}
	}
}

const defaultTerminateGrace = 10 * time.Second

func (w *pythonWorker) Invoke(ctx context.Context, payload map[string]any, budget *BudgetEnforcer, terminateGrace time.Duration) (*InvokeResponse, *ErrorResponse, *UpstreamEvidence) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil, &ErrorResponse{Status: "FAILED", Reason: "worker_closed", Detail: "python worker is closed"}, nil
	}
	if budget == nil {
		budget = NewBudgetEnforcer(BudgetConfig{})
	}
	if terminateGrace <= 0 {
		terminateGrace = defaultTerminateGrace
	}
	budget.Start()
	if w.rpc != nil {
		w.rpc.SetInvoke(payload, budget, func() { _ = killCommand(w.cmd) })
		defer w.rpc.ClearInvoke()
	}

	// Sanitize payload before writing to Python worker stdin.
	// Strips reserved platform keys (credentials, llm, mcp, mcp_servers,
	// __agentpaas_*) so agent code never sees raw credentials or platform config.
	sanitizedPayload := sanitizeAgentPayload(payload)

	done := make(chan workerMessage, 1)
	errCh := make(chan error, 1)
	go func() {
		if err := json.NewEncoder(w.stdin).Encode(sanitizedPayload); err != nil {
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

	wallTimer := time.NewTimer(budget.WallClockBudget())
	defer wallTimer.Stop()

	select {
	case <-ctx.Done():
		_ = w.terminateWithGraceLocked(terminateGrace)
		return nil, &ErrorResponse{Status: "FAILED", Reason: "invoke_timeout", Detail: ctx.Err().Error()}, w.rpcFailureEvidence()
	case <-wallTimer.C:
		terminateErr := w.terminateWithGraceLocked(terminateGrace)
		if terminateErr != nil {
			return nil, &ErrorResponse{Status: "FAILED", Reason: "worker_kill_failed", Detail: terminateErr.Error()}, w.rpcFailureEvidence()
		}
		if err := budget.MarkWallClockExceeded(budget.Elapsed()); !errors.Is(err, ErrBudgetExceeded) {
			return nil, &ErrorResponse{Status: "FAILED", Reason: "audit_failed", Detail: err.Error()}, w.rpcFailureEvidence()
		}
		return nil, &ErrorResponse{Status: StatusBudgetExceeded, Reason: "wall_clock_budget_exceeded", Detail: "wall-clock budget exceeded"}, w.rpcFailureEvidence()
	case err := <-errCh:
		if budgetErr := budget.RecordTokens(0); errors.Is(budgetErr, ErrBudgetExceeded) {
			_ = w.terminateWithGraceLocked(terminateGrace)
			return nil, &ErrorResponse{Status: StatusBudgetExceeded, Reason: "budget_exceeded", Detail: budgetErr.Error()}, w.rpcFailureEvidence()
		}
		return nil, &ErrorResponse{Status: "FAILED", Reason: "invoke_failed", Detail: err.Error()}, w.rpcFailureEvidence()
	case msg := <-done:
		if msg.Type != "ok" {
			if budgetErr := budget.RecordTokens(0); errors.Is(budgetErr, ErrBudgetExceeded) {
				_ = w.terminateWithGraceLocked(terminateGrace)
				return nil, &ErrorResponse{Status: StatusBudgetExceeded, Reason: "budget_exceeded", Detail: budgetErr.Error()}, w.rpcFailureEvidence()
			}
			reason := msg.Reason
			if reason == "" {
				reason = "invoke_failed"
			}
			return nil, &ErrorResponse{Status: "FAILED", Reason: reason, Detail: msg.Detail}, w.rpcFailureEvidence()
		}
		if err := validateResultKeys(msg.Result); err != nil {
			return nil, &ErrorResponse{Status: "FAILED", Reason: "invalid_result", Detail: err.Error()}, w.rpcFailureEvidence()
		}
		return &InvokeResponse{
			Status: "OK",
			Result: msg.Result,
			Stdout: w.stdoutPath,
			Stderr: w.stderrPath,
		}, nil, nil
	}
}

func (w *pythonWorker) rpcFailureEvidence() *UpstreamEvidence {
	if w.rpc == nil {
		return nil
	}
	return w.rpc.FailureEvidence()
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

func (w *pythonWorker) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.closed
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
		joined = errors.Join(joined, waitCommand(w.cmd, w.reaper))
	}
	if w.stderrFile != nil {
		joined = errors.Join(joined, w.stderrFile.Close())
	}
	if w.rpc != nil {
		joined = errors.Join(joined, w.rpc.Close())
	}
	return joined
}

func (w *pythonWorker) terminateWithGraceLocked(grace time.Duration) error {
	w.closed = true

	var joined error
	if w.stdin != nil {
		joined = errors.Join(joined, w.stdin.Close())
	}
	if w.stdout != nil {
		joined = errors.Join(joined, w.stdout.Close())
	}
	if w.cmd != nil && w.cmd.Process != nil {
		waitCh := make(chan error, 1)
		if err := terminateCommand(w.cmd); err != nil {
			joined = errors.Join(joined, err)
		}
		go func() {
			waitCh <- waitCommand(w.cmd, w.reaper)
		}()

		timer := time.NewTimer(grace)
		select {
		case <-waitCh:
			timer.Stop()
		case <-timer.C:
			joined = errors.Join(joined, killCommand(w.cmd))
			<-waitCh
		}
	}
	if w.cancel != nil {
		w.cancel()
	}
	if w.stderrFile != nil {
		joined = errors.Join(joined, w.stderrFile.Close())
	}
	if w.rpc != nil {
		joined = errors.Join(joined, w.rpc.Close())
	}
	return joined
}

func terminateCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err == nil {
		return nil
	} else if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return cmd.Process.Signal(syscall.SIGTERM)
}

func killCommand(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil {
		return nil
	} else if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return cmd.Process.Kill()
}

func waitCommand(cmd *exec.Cmd, reaper *childReaper) error {
	if cmd == nil {
		return nil
	}
	err := cmd.Wait()
	if cmd.Process != nil && reaper != nil {
		reaper.Untrack(cmd.Process.Pid)
	}
	return err
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
    repo_python = os.path.join(os.getcwd(), "python")
    if repo_python not in sys.path:
        sys.path.insert(0, repo_python)
    os.environ["AGENTPAAS_AGENT_PATH"] = agent_path
    os.environ["AGENTPAAS_STDOUT_PATH"] = stdout_path
    try:
        from agentpaas_sdk import run
    except ModuleNotFoundError:
        run = None
    if run is not None:
        run()
        sys.exit(0)
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

_RESERVED = frozenset({"credentials", "llm", "mcp", "mcp_servers"})

for line in sys.stdin:
    try:
        payload = json.loads(line)
        sanitized = {k: v for k, v in payload.items() if k not in _RESERVED and not k.startswith("__agentpaas_")}
        result = invoke(sanitized)
        send({"type": "ok", "result": result})
    except Exception:
        send({"type": "failed", "reason": "invoke_failed", "detail": traceback.format_exc()})
`

func workerEnv(base []string, rpcAddr string) []string {
	env := make([]string, 0, len(base)+2)
	pythonPath := pythonPackagePath()
	var sawPythonPath bool
	for _, item := range base {
		if strings.HasPrefix(item, "AGENTPAAS_RPC_ADDR=") {
			continue
		}
		if strings.HasPrefix(item, "PYTHONPATH=") {
			sawPythonPath = true
			env = append(env, item+string(os.PathListSeparator)+pythonPath)
			continue
		}
		env = append(env, item)
	}
	if !sawPythonPath {
		env = append(env, "PYTHONPATH="+pythonPath)
	}
	env = append(env, "AGENTPAAS_RPC_ADDR="+rpcAddr)
	return env
}

func pythonPackagePath() string {
	wd, err := os.Getwd()
	if err != nil {
		return filepath.Join(".", "python")
	}
	for {
		candidate := filepath.Join(wd, "python")
		if info, statErr := os.Stat(filepath.Join(candidate, "agentpaas_sdk")); statErr == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return filepath.Join(".", "python")
		}
		wd = parent
	}
}

// sanitizeAgentPayload strips reserved platform keys from the invoke payload
// before it reaches the Python agent handler. This is defense-in-depth: the
// daemon's buildInvokePayload already excludes credential values, but the
// harness also strips all reserved keys so agent code never sees credentials,
// llm config, or mcp config even if they somehow leak through transport.
func sanitizeAgentPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	sanitized := make(map[string]any, len(payload))
	for k, v := range payload {
		if isReservedAgentKey(k) {
			continue
		}
		sanitized[k] = v
	}
	return sanitized
}

// isReservedAgentKey returns true if the key is a platform-internal key that
// must never reach agent code.
func isReservedAgentKey(key string) bool {
	switch key {
	case "credentials", "llm", "mcp", "mcp_servers":
		return true
	}
	return strings.HasPrefix(key, "__agentpaas_")
}
