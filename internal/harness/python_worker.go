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
	"strconv"
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
	defer func() { _ = stdoutCapture.Close() }() // best-effort close

	stderrCapture, err := os.OpenFile(cfg.StderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		errResp := &ErrorResponse{Status: "FAILED", Reason: "stderr_capture_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}

	rpcServer, err := startHarnessRPCServer(cfg.Audit)
	if err != nil {
		_ = stderrCapture.Close() // best-effort cleanup
		errResp := &ErrorResponse{Status: "FAILED", Reason: "rpc_start_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}

	// Load pre-resolved credentials from the sidecar file before starting
	// the Python worker, so credential values are in memory before agent
	// code begins executing.
	if cfg.CredentialsPath != "" {
		if err := rpcServer.LoadCredentials(cfg.CredentialsPath); err != nil {
			_ = rpcServer.Close()     // best-effort cleanup
			_ = stderrCapture.Close() // best-effort cleanup
			errResp := &ErrorResponse{Status: "FAILED", Reason: "credential_load_failed", Detail: err.Error()}
			return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
		}
	}

	// Load progress journal metadata from the sidecar key file before
	// starting the Python worker, so the journal writer is wired into
	// the RPC server before agent code begins executing. The daemon
	// writes the journal key to a 0600 file and bind-mounts it into the
	// harness container. The key file is deleted after loading to
	// prevent agent access (mirrors the credentials sidecar pattern).
	// If no journal key path is set, progress is disabled — handleProgress
	// returns INVALID_PROGRESS (nil journal guard).
	if cfg.JournalKeyPath != "" && cfg.JournalPath != "" && cfg.AttemptID != "" {
		if err := rpcServer.LoadProgressMetadata(cfg); err != nil {
			_ = rpcServer.Close()     // best-effort cleanup
			_ = stderrCapture.Close() // best-effort cleanup
			errResp := &ErrorResponse{Status: "FAILED", Reason: "progress_metadata_load_failed", Detail: err.Error()}
			return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
		}
	}

	// Load delegation trust state from the sidecar file before starting
	// the Python worker (BUG-040). The daemon writes the pre-built
	// CommunicationSnapshot and per-binding capability tokens to a JSON
	// file and bind-mounts it read-only. The harness reads it at startup
	// and injects it into the RPC server. If no delegation snapshot path
	// is set, delegation is disabled (backward compat).
	if cfg.DelegationSnapshotPath != "" {
		if err := rpcServer.LoadDelegationSnapshot(cfg.DelegationSnapshotPath); err != nil {
			_ = rpcServer.Close()     // best-effort cleanup
			_ = stderrCapture.Close() // best-effort cleanup
			errResp := &ErrorResponse{Status: "FAILED", Reason: "delegation_snapshot_load_failed", Detail: err.Error()}
			return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
		}
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	cmd := commandContext(workerCtx, cfg.Python, "-u", "-c", pythonRunner, cfg.AgentPath, cfg.StdoutPath)
	cmd.Env = appendPolicyResourceEnv(workerEnv(os.Environ(), rpcServer.Addr()), cfg.DurablePath, cfg.CPUQuotaSeconds, cfg.MaxPIDs)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		_ = rpcServer.Close()     // best-effort cleanup
		_ = stderrCapture.Close() // best-effort cleanup
		errResp := &ErrorResponse{Status: "FAILED", Reason: "worker_stdin_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = rpcServer.Close()     // best-effort cleanup
		_ = stdin.Close()         // best-effort cleanup
		_ = stderrCapture.Close() // best-effort cleanup
		errResp := &ErrorResponse{Status: "FAILED", Reason: "worker_stdout_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	cmd.Stderr = stderrCapture

	if err := cmd.Start(); err != nil {
		cancel()
		_ = rpcServer.Close()     // best-effort cleanup
		_ = stdin.Close()         // best-effort cleanup
		_ = stdout.Close()        // best-effort cleanup
		_ = stderrCapture.Close() // best-effort cleanup
		errResp := &ErrorResponse{Status: "FAILED", Reason: "worker_start_failed", Detail: err.Error()}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if reaper != nil {
		reaper.Track(cmd.Process.Pid)
	}

	decoder := json.NewDecoder(bufio.NewReader(stdout))
	msg, errResp := waitForImport(cmd, reaper, cancel, stdin, stdout, stderrCapture, decoder, cfg.ImportTimeout)
	if errResp != nil {
		_ = rpcServer.Close() // best-effort cleanup
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if msg.Type == "import_failed" {
		cancel()
		_ = rpcServer.Close()        // best-effort cleanup
		_ = stdin.Close()            // best-effort cleanup
		_ = stdout.Close()           // best-effort cleanup
		_ = stderrCapture.Close()    // best-effort cleanup
		_ = waitCommand(cmd, reaper) // best-effort cleanup
		errResp := &ErrorResponse{Status: "FAILED", Reason: msg.Reason, Detail: msg.Detail}
		return nil, attachFailureContext(errResp, newImportFailureContext(cfg, errResp.Reason, errResp.Detail), cfg.Audit)
	}
	if msg.Type != "ready" {
		cancel()
		_ = rpcServer.Close()        // best-effort cleanup
		_ = stdin.Close()            // best-effort cleanup
		_ = stdout.Close()           // best-effort cleanup
		_ = stderrCapture.Close()    // best-effort cleanup
		_ = waitCommand(cmd, reaper) // best-effort cleanup
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
		_ = stdin.Close()      // best-effort cleanup
		_ = stdout.Close()     // best-effort cleanup
		_ = stderrFile.Close() // best-effort cleanup
		// best-effort kill; error is not actionable here.
		_ = killCommand(cmd)         // best-effort cleanup
		_ = waitCommand(cmd, reaper) // best-effort cleanup
		return workerMessage{}, &ErrorResponse{Status: "FAILED", Reason: "import_failed", Detail: err.Error()}
	case <-time.After(timeout):
		cancel()
		_ = stdin.Close()      // best-effort cleanup
		_ = stdout.Close()     // best-effort cleanup
		_ = stderrFile.Close() // best-effort cleanup
		// best-effort kill; error is not actionable here.
		_ = killCommand(cmd)         // best-effort cleanup
		_ = waitCommand(cmd, reaper) // best-effort cleanup
		return workerMessage{}, &ErrorResponse{Status: "FAILED", Reason: "import_timeout", Detail: "agent import timed out"}
	}
}

const defaultTerminateGrace = 10 * time.Second

// pythonWorker.Invoke invokes python worker.
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
		w.rpc.SetInvoke(payload, budget, func() { _ = killCommand(w.cmd) }) // best-effort cleanup
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
		_ = w.terminateWithGraceLocked(terminateGrace) // best-effort cleanup
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
			_ = w.terminateWithGraceLocked(terminateGrace) // best-effort cleanup
			return nil, &ErrorResponse{Status: StatusBudgetExceeded, Reason: "budget_exceeded", Detail: budgetErr.Error()}, w.rpcFailureEvidence()
		}
		// B30-T04: if the worker was terminated by a resource-limit signal
		// (SIGXCPU from RLIMIT_CPU, or fork-bomb contained by RLIMIT_NPROC),
		// surface the signed-policy termination reason and observed value
		// so the attempt evidence records which limit was hit.
		if reason, detail, ok := w.resourceLimitExitReason(); ok {
			_ = w.terminateWithGraceLocked(terminateGrace) // best-effort cleanup
			return nil, &ErrorResponse{Status: "FAILED", Reason: reason, Detail: detail}, w.rpcFailureEvidence()
		}
		return nil, &ErrorResponse{Status: "FAILED", Reason: "invoke_failed", Detail: err.Error()}, w.rpcFailureEvidence()
	case msg := <-done:
		if msg.Type != "ok" {
			if budgetErr := budget.RecordTokens(0); errors.Is(budgetErr, ErrBudgetExceeded) {
				_ = w.terminateWithGraceLocked(terminateGrace) // best-effort cleanup
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

// resourceLimitExitReason inspects the worker process state for a
// resource-limit-induced termination and returns (reason, detail, true)
// when one is detected. Returns false when the worker exited normally or the
// exit signal is not a resource-limit signal (b30-summary.md:414).
//
//   - SIGXCPU (CPU time limit exceeded, RLIMIT_CPU / RLIMIT_CPU_2) →
//     "cpu_quota_exhausted" with detail "CPU quota exhausted: <N>s".
//   - RLIMIT_NPROC exhaustion surfaces as a Python exception (caught
//     elsewhere) or a non-resource-signal exit — reported via the normal
//     invoke_failed path; here we only classify signal exits.
func (w *pythonWorker) resourceLimitExitReason() (reason, detail string, ok bool) {
	if w.cmd == nil || w.cmd.ProcessState == nil {
		return "", "", false
	}
	ps := w.cmd.ProcessState
	if !ps.Exited() {
		return "", "", false
	}
	if ws, ok2 := ps.Sys().(syscall.WaitStatus); ok2 && ws.Signaled() {
		switch ws.Signal() {
		case syscall.SIGXCPU:
			return "cpu_quota_exhausted", "CPU quota exhausted: worker terminated by SIGXCPU", true
		case syscall.SIGXFSZ:
			return "file_size_limit_exhausted", "File-size limit exhausted: worker terminated by SIGXFSZ", true
		}
	}
	return "", "", false
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
				return fmt.Errorf("validate result keys: %w", err)
			}
		}
	case []any:
		for _, child := range v {
			if err := validateResultKeys(child); err != nil {
				return fmt.Errorf("validate result keys: %w", err)
			}
		}
	}
	return nil
}

// pythonWorker.Close closes python worker.
//
// It returns an error if the operation fails or inputs are invalid.
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
    # B30-T04: the durable path (InvokeDeployment) drives CPU and PID limits
    # from the deployment policy via AGENTPAAS_CPU_QUOTA_SECONDS and
    # AGENTPAAS_MAX_PIDS env vars. The legacy v0.2.3 path (no policy) keeps
    # the fixed RLIMIT_CPU=30 / RLIMIT_NPROC=0 constants with "legacy compat"
    # comments. RLIMIT_FSIZE (64MB) is policy-independent and always applied.
    #
    # T04.4 design note: child-agent creation is NOT done via os/exec — it
    # goes through the AgentPaaS control plane (B35) and separate containers.
    # The RLIMIT_NPROC limit applies to TOOL subprocesses (git, grep, awk),
    # NOT to child-agent creation.
    #
    # Full container sandboxing (memory, CFS quota, PID cgroup, disk) is
    # applied at the runtime driver container spec (T04.5). These rlimits
    # are a best-effort first layer inside the container.
    limits = []
    if "AGENTPAAS_CPU_QUOTA_SECONDS" in os.environ:
        # Durable path: policy-derived CPU quota. 0 means unlimited CPU
        # (bounded by the container CFS quota); do not set RLIMIT_CPU.
        cpu_quota = int(os.environ.get("AGENTPAAS_CPU_QUOTA_SECONDS", "0") or "0")
        if cpu_quota > 0:
            limits.append(("RLIMIT_CPU", cpu_quota))
        # else: unlimited CPU — no RLIMIT_CPU, CFS quota bounds the worker.
    else:
        # Legacy compat (v0.2.3): fixed 30s CPU ceiling.
        limits.append(("RLIMIT_CPU", 30))
    # RLIMIT_FSIZE is policy-independent and always applied.
    limits.append(("RLIMIT_FSIZE", 64 * 1024 * 1024))
    if "AGENTPAAS_MAX_PIDS" in os.environ:
        # Durable path: policy-derived PID limit. 0 means an explicit policy
        # decision to forbid ALL subprocesses; >0 allows that many processes
        # (sufficient for approved local tools: git, grep, awk). Default 64
        # is enough for approved local tools but not a fork bomb.
        max_pids = int(os.environ.get("AGENTPAAS_MAX_PIDS", "0") or "0")
        if max_pids > 0:
            limits.append(("RLIMIT_NPROC", max_pids))
        else:
            # Explicit policy decision to forbid subprocesses.
            limits.append(("RLIMIT_NPROC", 0))
    else:
        # Legacy compat (v0.2.3): forbid all subprocesses.
        limits.append(("RLIMIT_NPROC", 0))
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

// appendPolicyResourceEnv adds the B30-T04 policy-derived resource ceilings
// to the worker env. On the durable path (durable=true), BOTH env vars are
// always emitted so the Python runner applies policy-derived RLIMIT_CPU and
// RLIMIT_NPROC instead of the legacy fixed constants. On the legacy v0.2.3
// path (durable=false), neither env var is emitted and the runner falls back
// to RLIMIT_CPU=30 / RLIMIT_NPROC=0 with "legacy compat" comments.
//
// Durable path semantics:
//   - cpuQuotaSeconds > 0 → RLIMIT_CPU = cpuQuotaSeconds (explicit CPU budget).
//   - cpuQuotaSeconds == 0 → unlimited CPU (runner does NOT set RLIMIT_CPU;
//     bounded by the container CFS quota the runtime driver applies).
//   - maxPIDs > 0 → RLIMIT_NPROC = maxPIDs (approved local tools can spawn).
//   - maxPIDs == 0 → RLIMIT_NPROC = 0 (explicit policy decision to forbid all
//     subprocesses; e.g. a pure-LLM worker with no tool subprocesses).
//
// Child-agent creation is NOT affected by RLIMIT_NPROC: child agents are
// created via the AgentPaaS control plane (B35), never via os/exec. The
// RLIMIT_NPROC limit applies to TOOL subprocesses (git, grep, awk) only.
func appendPolicyResourceEnv(env []string, durable bool, cpuQuotaSeconds int64, maxPIDs int) []string {
	if !durable {
		return env
	}
	env = append(env, "AGENTPAAS_CPU_QUOTA_SECONDS="+strconv.FormatInt(cpuQuotaSeconds, 10))
	env = append(env, "AGENTPAAS_MAX_PIDS="+strconv.Itoa(maxPIDs))
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
