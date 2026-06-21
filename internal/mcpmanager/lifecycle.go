package mcpmanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/parvezsyed/agentpaas/internal/policy"
	"github.com/parvezsyed/agentpaas/internal/runtime"
)

const minimalPATH = "PATH=/usr/local/bin:/usr/bin:/bin"

// Lifecycle manages the start/stop/readiness of declared MCP servers.
// Stdio servers run as child processes; HTTP servers run as Docker sidecar
// containers. No host networking is used for MCP sidecars.
type Lifecycle struct {
	mu         sync.RWMutex
	manager    *Manager
	driver     runtime.RuntimeDriver
	netID      string
	processes  map[string]*os.Process
	containers map[string]runtime.ContainerID
	procState  map[string]*stdioState
}

type stdioState struct {
	done     chan struct{}
	exitCode int
	exitTime time.Time
	err      error
}

// CrashContext is structured failure context for a crashed MCP server.
type CrashContext struct {
	ServerID    string
	Transport   string
	ExitCode    int
	ExitTime    time.Time
	Error       string
	Recoverable bool
}

// NewLifecycle creates a Lifecycle bound to the given Manager.
// driver may be nil if only stdio MCP servers will be used.
// netID is the Docker network ID for MCP sidecars (must NOT be host network).
func NewLifecycle(manager *Manager, driver runtime.RuntimeDriver, netID string) *Lifecycle {
	return &Lifecycle{
		manager:    manager,
		driver:     driver,
		netID:      netID,
		processes:  make(map[string]*os.Process),
		containers: make(map[string]runtime.ContainerID),
		procState:  make(map[string]*stdioState),
	}
}

// Start launches a declared MCP server.
func (lc *Lifecycle) Start(ctx context.Context, serverID, agentID, runID string) error {
	if lc.manager == nil {
		return errors.New("mcp lifecycle: manager is nil")
	}
	server, ok := lc.manager.server(serverID)
	if !ok {
		return fmt.Errorf("mcp server %q is undeclared", serverID)
	}

	lc.mu.Lock()
	if lc.isRunningLocked(ctx, serverID) {
		lc.mu.Unlock()
		return fmt.Errorf("mcp server %q is already running", serverID)
	}
	lc.mu.Unlock()

	switch server.Transport {
	case "stdio":
		return lc.startStdio(ctx, serverID, server)
	case "http":
		return lc.startHTTP(ctx, serverID, agentID, runID, server)
	default:
		return fmt.Errorf("mcp server %q has unsupported transport %q", serverID, server.Transport)
	}
}

func (lc *Lifecycle) startStdio(ctx context.Context, serverID string, server policy.MCPServer) error {
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	cmd.Env = lifecycleEnv(server.Env)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start stdio MCP server %q: %w", serverID, err)
	}

	state := &stdioState{
		done:     make(chan struct{}),
		exitCode: -1,
	}

	lc.mu.Lock()
	lc.processes[serverID] = cmd.Process
	lc.procState[serverID] = state
	lc.mu.Unlock()
	lc.manager.setReadiness(serverID, ReadinessStarting)

	go lc.waitForStdio(serverID, cmd, state)
	return nil
}

func (lc *Lifecycle) waitForStdio(serverID string, cmd *exec.Cmd, state *stdioState) {
	err := cmd.Wait()
	lc.mu.Lock()
	defer lc.mu.Unlock()
	state.err = err
	state.exitTime = time.Now().UTC()
	state.exitCode = exitCode(err)
	close(state.done)
	if err != nil {
		lc.manager.setFailure(serverID, ReadinessUnhealthy, err.Error())
	}
}

func (lc *Lifecycle) startHTTP(ctx context.Context, serverID, _ string, runID string, server policy.MCPServer) error {
	if lc.driver == nil {
		return errors.New("http MCP server requires Docker runtime")
	}
	if lc.netID == "" || lc.netID == "host" {
		return fmt.Errorf("http MCP server %q requires non-host Docker network", serverID)
	}

	labels := runtime.Labels(runtime.ResourceTypeMCP, runID)
	labels[runtime.LabelMCPServerID] = serverID
	spec := runtime.ContainerSpec{
		Image:      sidecarImage(server),
		Command:    []string{"sleep", "infinity"},
		Env:        lifecycleEnv(server.Env),
		Labels:     labels,
		NetworkIDs: []string{lc.netID},
	}

	containerID, err := lc.driver.Create(ctx, spec)
	if err != nil {
		return fmt.Errorf("create MCP sidecar %q: %w", serverID, err)
	}
	if err := lc.driver.Start(ctx, containerID); err != nil {
		return fmt.Errorf("start MCP sidecar %q: %w", serverID, err)
	}

	lc.mu.Lock()
	lc.containers[serverID] = containerID
	lc.mu.Unlock()
	lc.manager.setReadiness(serverID, ReadinessStarting)
	return nil
}

// CheckReadiness polls the MCP server until it is ready or timeout.
func (lc *Lifecycle) CheckReadiness(ctx context.Context, serverID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ready, err := lc.checkOnce(ctx, serverID)
		if ready {
			lc.manager.setReadiness(serverID, ReadinessReady)
			return nil
		}
		if time.Now().After(deadline) {
			if err == nil {
				err = fmt.Errorf("mcp server %q not ready before timeout", serverID)
			}
			lc.manager.setFailure(serverID, ReadinessUnhealthy, err.Error())
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (lc *Lifecycle) checkOnce(ctx context.Context, serverID string) (bool, error) {
	lc.mu.RLock()
	process, hasProcess := lc.processes[serverID]
	state := lc.procState[serverID]
	containerID, hasContainer := lc.containers[serverID]
	lc.mu.RUnlock()

	if hasProcess {
		if state != nil && isDone(state.done) {
			return false, fmt.Errorf("stdio MCP server %q crashed: %s", serverID, crashError(state))
		}
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return false, fmt.Errorf("stdio MCP server %q is not alive: %w", serverID, err)
		}
		return true, nil
	}
	if hasContainer {
		if lc.driver == nil {
			return false, errors.New("http MCP server requires Docker runtime")
		}
		status, err := lc.driver.Status(ctx, containerID)
		if err != nil {
			return false, fmt.Errorf("status MCP sidecar %q: %w", serverID, err)
		}
		if status == runtime.ContainerStatusRunning {
			return true, nil
		}
		return false, fmt.Errorf("MCP sidecar %q status is %s", serverID, status.String())
	}
	return false, fmt.Errorf("mcp server %q is not running", serverID)
}

// Stop terminates a running MCP server.
func (lc *Lifecycle) Stop(ctx context.Context, serverID string) error {
	lc.mu.RLock()
	_, hasProcess := lc.processes[serverID]
	_, hasContainer := lc.containers[serverID]
	lc.mu.RUnlock()

	switch {
	case hasProcess:
		return lc.stopStdio(serverID)
	case hasContainer:
		return lc.stopHTTP(ctx, serverID)
	default:
		return fmt.Errorf("mcp server %q is not running", serverID)
	}
}

func (lc *Lifecycle) stopStdio(serverID string) error {
	lc.mu.RLock()
	process := lc.processes[serverID]
	state := lc.procState[serverID]
	lc.mu.RUnlock()
	if process == nil || state == nil {
		return fmt.Errorf("stdio MCP server %q is not running", serverID)
	}

	if !isDone(state.done) {
		if err := signalProcessGroup(process, syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return fmt.Errorf("terminate stdio MCP server %q: %w", serverID, err)
		}
		select {
		case <-state.done:
		case <-time.After(5 * time.Second):
			if err := signalProcessGroup(process, syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
				return fmt.Errorf("kill stdio MCP server %q: %w", serverID, err)
			}
			<-state.done
		}
	}

	lc.mu.Lock()
	delete(lc.processes, serverID)
	delete(lc.procState, serverID)
	lc.mu.Unlock()
	lc.manager.setReadiness(serverID, ReadinessStopped)
	return nil
}

func (lc *Lifecycle) stopHTTP(ctx context.Context, serverID string) error {
	lc.mu.RLock()
	containerID := lc.containers[serverID]
	lc.mu.RUnlock()
	if containerID == "" {
		return fmt.Errorf("http MCP server %q is not running", serverID)
	}
	if lc.driver == nil {
		return errors.New("http MCP server requires Docker runtime")
	}

	if err := lc.driver.Stop(ctx, containerID, nil); err != nil {
		return fmt.Errorf("stop MCP sidecar %q: %w", serverID, err)
	}
	if err := lc.driver.Remove(ctx, containerID, true); err != nil {
		return fmt.Errorf("remove MCP sidecar %q: %w", serverID, err)
	}

	lc.mu.Lock()
	delete(lc.containers, serverID)
	lc.mu.Unlock()
	lc.manager.setReadiness(serverID, ReadinessStopped)
	return nil
}

// StopAll stops all running MCP servers.
func (lc *Lifecycle) StopAll(ctx context.Context) error {
	lc.mu.RLock()
	ids := make([]string, 0, len(lc.processes)+len(lc.containers))
	seen := make(map[string]bool, len(lc.processes)+len(lc.containers))
	for serverID := range lc.processes {
		ids = append(ids, serverID)
		seen[serverID] = true
	}
	for serverID := range lc.containers {
		if !seen[serverID] {
			ids = append(ids, serverID)
		}
	}
	lc.mu.RUnlock()

	var result error
	for _, serverID := range ids {
		if err := lc.Stop(ctx, serverID); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

// IsRunning returns true if the MCP server is currently running.
func (lc *Lifecycle) IsRunning(serverID string) bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.isRunningLocked(context.Background(), serverID)
}

func (lc *Lifecycle) isRunningLocked(ctx context.Context, serverID string) bool {
	if state, ok := lc.procState[serverID]; ok {
		return !isDone(state.done)
	}
	if containerID, ok := lc.containers[serverID]; ok {
		if lc.driver == nil {
			return false
		}
		status, err := lc.driver.Status(ctx, containerID)
		return err == nil && status == runtime.ContainerStatusRunning
	}
	return false
}

// CrashContext returns structured failure context for a crashed MCP server.
func (lc *Lifecycle) CrashContext(serverID string) *CrashContext {
	server, ok := lc.manager.server(serverID)
	if !ok {
		return nil
	}

	switch server.Transport {
	case "stdio":
		return lc.stdioCrashContext(serverID)
	case "http":
		return lc.httpCrashContext(serverID)
	default:
		return nil
	}
}

func (lc *Lifecycle) stdioCrashContext(serverID string) *CrashContext {
	lc.mu.RLock()
	state := lc.procState[serverID]
	lc.mu.RUnlock()
	if state == nil || !isDone(state.done) {
		return nil
	}
	crash := &CrashContext{
		ServerID:    serverID,
		Transport:   "stdio",
		ExitCode:    state.exitCode,
		ExitTime:    state.exitTime,
		Error:       crashError(state),
		Recoverable: false,
	}
	lc.manager.setFailure(serverID, ReadinessUnhealthy, crash.Error)
	return crash
}

func (lc *Lifecycle) httpCrashContext(serverID string) *CrashContext {
	lc.mu.RLock()
	containerID, ok := lc.containers[serverID]
	lc.mu.RUnlock()
	if !ok || lc.driver == nil {
		return nil
	}

	status, err := lc.driver.Status(context.Background(), containerID)
	if err != nil {
		crash := &CrashContext{
			ServerID:    serverID,
			Transport:   "http",
			ExitCode:    -1,
			ExitTime:    time.Now().UTC(),
			Error:       err.Error(),
			Recoverable: true,
		}
		lc.manager.setFailure(serverID, ReadinessUnhealthy, crash.Error)
		return crash
	}
	if status == runtime.ContainerStatusRunning {
		return nil
	}
	crash := &CrashContext{
		ServerID:    serverID,
		Transport:   "http",
		ExitCode:    -1,
		ExitTime:    time.Now().UTC(),
		Error:       "container status is " + status.String(),
		Recoverable: true,
	}
	lc.manager.setFailure(serverID, ReadinessUnhealthy, crash.Error)
	return crash
}

func lifecycleEnv(declared map[string]string) []string {
	env := []string{minimalPATH}
	for key, value := range declared {
		env = append(env, key+"="+value)
	}
	return env
}

func sidecarImage(server policy.MCPServer) string {
	if server.URL != "" {
		return server.URL
	}
	return "busybox:latest"
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func crashError(state *stdioState) string {
	if state.err != nil {
		return state.err.Error()
	}
	return "process exited with code " + strconv.Itoa(state.exitCode)
}

func isDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func signalProcessGroup(process *os.Process, signal syscall.Signal) error {
	if process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-process.Pid, signal)
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return process.Signal(signal)
}
