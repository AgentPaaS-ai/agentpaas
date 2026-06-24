package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/home"
	"github.com/spf13/cobra"
)

// newDaemonCmd creates the `agent daemon` umbrella command.
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the AgentPaaS control daemon",
		Long: `Control the lifecycle of the AgentPaaS daemon (agentpaasd).

Start, stop, restart the daemon process, or query its status.
Install and uninstall are stubs — real service installation comes
in a future release.`,
	}

	cmd.AddCommand(newDaemonStatusCmd())
	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonRestartCmd())
	cmd.AddCommand(newDaemonInstallCmd())
	cmd.AddCommand(newDaemonUninstallCmd())

	return cmd
}

// newDaemonStatusCmd creates the `agent daemon status` command.
func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Query daemon version and readiness",
		Long: `Connect to the running daemon and retrieve version information,
Docker context, and readiness state.

If the daemon is not running, a clear error is printed with
a hint to start it first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStatus(cmd)
		},
	}
}

func runDaemonStatus(cmd *cobra.Command) error {
	sock, err := socketPath(cmd)
	if err != nil {
		return fmt.Errorf("cannot determine socket path: %w", err)
	}

	if client, conn, err := ConnectToDaemon(sock); err != nil {
		// Daemon not reachable — format error according to --json flag.
		hint := "Run 'agent daemon start' to start the daemon"
		errMsg := fmt.Sprintf("daemon not reachable at %s", sock)
		if jsonOutput(cmd) {
			je := JSONError{
				Error:   errMsg,
				Message: err.Error(),
				Hint:    hint,
			}
			return printTextOrJSON(true, je, func(v interface{}) string {
				return v.(JSONError).Error
			})
		}
		// For text output, we return the error so cobra can display it.
		return fmt.Errorf("%s: %w\n%s", errMsg, err, hint)
	} else {
		defer func() { _ = conn.Close() }()

		// Call the Doctor RPC to get version info.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		resp, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
		if err != nil {
			return fmt.Errorf("daemon doctor call failed: %w", err)
		}

		// Extract version info from the doctor response.
		// For now, extract from the "version" check result.
		versionStr := ""
		for _, check := range resp.GetChecks() {
			if check.GetName() == "version" {
				versionStr = check.GetMessage()
			}
		}

		// Build output from what we know.
		output := DaemonStatusOutput{
			DaemonVersion:    daemonVersionFromString(versionStr),
			ProtoVersion:     "v1",
			GitCommit:        gitCommitFromString(versionStr),
			OsArch:           osArchFromString(versionStr),
			DockerContext:    "unknown",
			DockerAPIVersion: "unknown",
			Ready:            resp.GetOverallStatus() == "ok",
		}

		return printTextOrJSON(jsonOutput(cmd), output, func(v interface{}) string {
			return DaemonStatusText(v.(DaemonStatusOutput))
		})
	}
}

// daemonVersionFromString extracts the daemon version from a version string.
// The string is formatted as "CLI: 0.1.0-dev | Daemon: 0.1.0-dev | ..."
func daemonVersionFromString(s string) string {
	// Try to find "Daemon: <version>" in the string.
	parts := strings.Split(s, "|")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "Daemon:") {
			return strings.TrimSpace(strings.TrimPrefix(p, "Daemon:"))
		}
	}
	return "unknown"
}

// gitCommitFromString extracts the git commit from a version string.
func gitCommitFromString(s string) string {
	parts := strings.Split(s, "|")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "Commit:") {
			return strings.TrimSpace(strings.TrimPrefix(p, "Commit:"))
		}
	}
	return "unknown"
}

// osArchFromString extracts the OS/arch from a version string.
func osArchFromString(s string) string {
	parts := strings.Split(s, "|")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "OS/Arch:") {
			return strings.TrimSpace(strings.TrimPrefix(p, "OS/Arch:"))
		}
	}
	return "unknown"
}

// daemonBinaryResolver resolves the agentpaasd binary path. Tests may override it.
var daemonBinaryResolver = resolveDaemonBinary

func resolveDaemonBinary() (string, error) {
	// Find the agentpaasd binary. First check same directory as the agent binary.
	agentPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate agent binary: %w", err)
	}
	agentDir := filepath.Dir(agentPath)
	daemonBinary := filepath.Join(agentDir, "agentpaasd")

	// If not found next to the agent, try PATH.
	if _, err := os.Stat(daemonBinary); os.IsNotExist(err) {
		var err2 error
		daemonBinary, err2 = exec.LookPath("agentpaasd")
		if err2 != nil {
			return "", fmt.Errorf("cannot find agentpaasd binary (checked next to agent and in PATH): %w", err)
		}
	}
	return daemonBinary, nil
}

// newDaemonStartCmd creates the `agent daemon start` command.
func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the control daemon",
		Long: `Start the agentpaasd daemon as a subprocess.

The daemon binary must be built and available. It is expected
to be in the same directory as the agent CLI binary.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStart(cmd)
		},
	}
}

// buildDaemonStartCommand creates the exec.Cmd for starting the daemon subprocess.
// The caller must not close the returned logFile — the daemon subprocess inherits it.
func buildDaemonStartCommand(cmd *cobra.Command, daemonBinary string, paths *home.HomePaths) (*exec.Cmd, *os.File, error) {
	if err := os.MkdirAll(paths.Logs, 0700); err != nil {
		return nil, nil, fmt.Errorf("cannot create logs directory %s: %w", paths.Logs, err)
	}
	logPath := filepath.Join(paths.Logs, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot open daemon log file %s: %w", logPath, err)
	}

	cmdDaemon := exec.Command(daemonBinary)
	cmdDaemon.Stdout = logFile
	cmdDaemon.Stderr = logFile
	cmdDaemon.Stdin = nil

	homeDir, err := homeDirPath(cmd)
	if err != nil {
		_ = logFile.Close()
		return nil, nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	// Pass the AGENTPAAS_HOME and AGENTPAAS_SOCKET env vars if overridden.
	cmdDaemon.Env = os.Environ()
	if cmd.Root().PersistentFlags().Changed("home") {
		cmdDaemon.Env = append(cmdDaemon.Env, "AGENTPAAS_HOME="+homeDir)
	}
	if sock, _ := socketPath(cmd); sock != "" {
		cmdDaemon.Env = append(cmdDaemon.Env, "AGENTPAAS_SOCKET="+sock)
	}

	return cmdDaemon, logFile, nil
}

func runDaemonStart(cmd *cobra.Command) error {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	paths := home.NewHomePaths(homeDir)

	daemonBinary, err := daemonBinaryResolver()
	if err != nil {
		return err
	}

	// Ensure home directory exists.
	if err := home.Ensure(paths); err != nil {
		return fmt.Errorf("home directory setup failed: %w", err)
	}

	// Check if daemon is already running by looking for a non-stale PID.
	stale, err := home.IsStalePid(paths.PID)
	if err != nil {
		return fmt.Errorf("cannot check PID file: %w", err)
	}
	if !stale {
		return fmt.Errorf("daemon is already running (PID file %s is live)", paths.PID)
	}

	// Clean stale files.
	if err := home.CleanStale(paths); err != nil {
		return fmt.Errorf("stale file cleanup failed: %w", err)
	}

	// Start the daemon subprocess.
	cmdDaemon, _, err := buildDaemonStartCommand(cmd, daemonBinary, paths)
	if err != nil {
		return err
	}

	if err := cmdDaemon.Start(); err != nil {
		return fmt.Errorf("cannot start daemon: %w", err)
	}

	fmt.Printf("Daemon started (PID %d)\n", cmdDaemon.Process.Pid)

	// Wait for the daemon to either stay alive past the grace period or exit early.
	// We cannot use cmdDaemon.ProcessState here — it is nil until Wait() returns.
	waitCh := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case waitCh <- cmdDaemon.Wait():
		case <-done:
			// Parent is returning; best-effort reap without blocking.
			if cmdDaemon.Process != nil {
				_, _ = cmdDaemon.Process.Wait()
			}
		}
	}()

	select {
	case err := <-waitCh:
		// Process exited during the grace period.
		exitCode := -1
		if cmdDaemon.ProcessState != nil {
			exitCode = cmdDaemon.ProcessState.ExitCode()
		}
		return fmt.Errorf("daemon exited immediately (exit code %d) — check logs at %s: %w",
			exitCode, paths.Logs, err)
	case <-time.After(500 * time.Millisecond):
		// Daemon survived the grace period — consider it started.
		fmt.Println("Daemon is running. Use 'agent daemon status' to verify.")
		return nil
	}
}

// newDaemonStopCmd creates the `agent daemon stop` command.
func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the control daemon",
		Long: `Send SIGTERM to the running daemon process.

Uses the PID file in the home directory to locate the process.
If the daemon is not running, returns a clear error.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemonStop(cmd)
		},
	}
}

func runDaemonStop(cmd *cobra.Command) error {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return fmt.Errorf("cannot determine home directory: %w", err)
	}
	paths := home.NewHomePaths(homeDir)

	// Read PID file and send SIGTERM.
	pidData, err := os.ReadFile(paths.PID)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("daemon is not running (no PID file at %s)", paths.PID)
		}
		return fmt.Errorf("cannot read PID file %s: %w", paths.PID, err)
	}

	pidStr := strings.TrimSpace(string(pidData))
	if pidStr == "" {
		return fmt.Errorf("daemon is not running (PID file %s is empty)", paths.PID)
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return fmt.Errorf("invalid PID %q in %s: %w", pidStr, paths.PID, err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("cannot find process %d: %w", pid, err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("cannot send SIGTERM to PID %d: %w", pid, err)
	}

	// Wait for process to exit.
	done := make(chan struct{})
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Printf("Daemon (PID %d) stopped\n", pid)
	case <-time.After(10 * time.Second):
		fmt.Printf("Daemon (PID %d) did not exit within 10 seconds — sending SIGKILL\n", pid)
		_ = proc.Signal(syscall.SIGKILL)
	}

	return nil
}

// newDaemonRestartCmd creates the `agent daemon restart` command.
func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the control daemon",
		Long:  `Stop then start the control daemon.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Stop — ignore "not running" errors.
			_ = runDaemonStop(cmd)
			return runDaemonStart(cmd)
		},
	}
}

// newDaemonInstallCmd creates the `agent daemon install` command (stub).
func newDaemonInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the daemon as a system service (not yet implemented)",
		Long:  `Register the daemon as a system service (launchd on macOS, systemd on Linux).`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Service installation not yet implemented")
			return nil
		},
	}
}

// newDaemonUninstallCmd creates the `agent daemon uninstall` command (stub).
func newDaemonUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the daemon from system services (not yet implemented)",
		Long:  `Unregister the daemon from system services.`,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Service uninstallation not yet implemented")
			return nil
		},
	}
}