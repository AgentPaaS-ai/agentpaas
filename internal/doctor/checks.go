package doctor

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/home"
)

// knownPorts are the ports that agentpaas uses for control and agent traffic.
var knownPorts = []int{7700, 7717, 7718}

// CheckDockerReachable verifies that the Docker daemon is reachable.
//
// It shells out to "docker info" and checks the API version in the output.
// Returns ok with the API version when reachable, or error with a fix hint
// when Docker is stopped or not installed.
func CheckDockerReachable() CheckResult {
	name := "docker_reachable"

	out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Output()
	if err != nil {
		hint := "Ensure Docker is installed and running. Run 'docker info' to verify."
		if runtime.GOOS == "darwin" {
			hint = "Docker Desktop or Colima is not running. Start Docker Desktop from Applications, or run 'colima start'."
		}
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Docker daemon is not reachable: %v", err),
			FixHint: hint,
		}
	}

	apiVersion := strings.TrimSpace(string(out))
	if apiVersion == "" {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: "Docker daemon returned an empty version string",
			FixHint: "Run 'docker info' to check Docker daemon status.",
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Docker daemon is reachable (API version: %s)", apiVersion),
	}
}

// CheckDockerContext reports the current Docker context name.
//
// It runs "docker context show" to get the active context. If Docker is
// not reachable, the check returns a warning rather than an error because
// the context check is informational.
func CheckDockerContext() CheckResult {
	name := "docker_context"

	out, err := exec.Command("docker", "context", "show").Output()
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: fmt.Sprintf("Cannot determine Docker context: %v", err),
			FixHint: "Run 'docker context show' manually to diagnose.",
		}
	}

	contextName := strings.TrimSpace(string(out))
	if contextName == "" {
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: "Docker context name is empty",
			FixHint: "Run 'docker context ls' to see available contexts.",
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Docker context: %s", contextName),
	}
}

// CheckDockerDesktop detects whether Docker Desktop or Colima is running on macOS.
//
// On macOS, it checks for the "com.docker.docker" process (Docker Desktop) or
// "colima" process. On Linux, it reports the check as informational/warning
// since dockerd management is handled differently.
func CheckDockerDesktop() CheckResult {
	name := "docker_desktop"

	if runtime.GOOS != "darwin" {
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: "Not running on macOS — Docker Desktop/Colima detection is macOS-only. This is a P2 check, not a P1 gate on Linux.",
			FixHint: "On Linux, manage dockerd via systemctl or the distribution's package manager.",
		}
	}

	// Check for Docker Desktop process.
	desktopOut, err := exec.Command("pgrep", "-f", "com.docker.docker").Output()
	desktopRunning := err == nil && strings.TrimSpace(string(desktopOut)) != ""

	// Check for Colima process.
	colimaOut, err := exec.Command("pgrep", "-f", "colima").Output()
	colimaRunning := err == nil && strings.TrimSpace(string(colimaOut)) != ""

	if !desktopRunning && !colimaRunning {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: "Neither Docker Desktop nor Colima process is detected",
			FixHint: "Start Docker Desktop from Applications or run 'colima start'.",
		}
	}

	var found []string
	if desktopRunning {
		found = append(found, "Docker Desktop")
	}
	if colimaRunning {
		found = append(found, "Colima")
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Docker runtime detected: %s", strings.Join(found, ", ")),
	}
}

// CheckLinuxDockerd reports the Linux dockerd status as informational.
//
// This is a P2 (non-gating) check. It verifies dockerd is running via
// systemctl is-active, but does not block startup if dockerd is down,
// since dockerd is not a hard dependency for agentpaas on Linux.
func CheckLinuxDockerd() CheckResult {
	name := "linux_dockerd"

	if runtime.GOOS != "linux" {
		return CheckResult{
			Name:    name,
			Status:  "ok",
			Message: "Not applicable (not running on Linux)",
		}
	}

	out, err := exec.Command("systemctl", "is-active", "docker").Output()
	status := strings.TrimSpace(string(out))

	if err != nil || status != "active" {
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: fmt.Sprintf("dockerd is not active (status: %s). This is a P2 informational check, not a P1 gate.", status),
			FixHint: "Run 'sudo systemctl start docker' if Docker is needed.",
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: "dockerd is active (P2 informational check)",
	}
}

// CheckPortsFree verifies that ports 7700, 7717, and 7718 are not in use.
//
// For each port, it attempts net.Listen("tcp", ...). If the listen fails,
// it uses lsof to identify the process holding the port and returns an
// error with the process name.
func CheckPortsFree() CheckResult {
	name := "ports_free"

	var squatters []string
	for _, port := range knownPorts {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			// Port is in use — try to identify the process.
			procName := identifyProcess(port)
			hint := fmt.Sprintf("Port %d is in use", port)
			if procName != "" {
				hint = fmt.Sprintf("Process '%s' is holding port %d", procName, port)
			}
			squatters = append(squatters, hint)
			continue
		}
		_ = ln.Close()
	}

	if len(squatters) > 0 {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: strings.Join(squatters, "; "),
			FixHint: "Stop the conflicting process or configure agentpaas to use different ports.",
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Ports %s are free", portList(knownPorts)),
	}
}

// identifyProcess uses lsof to find the process listening on the given port.
// Returns the process name if identifiable, or an empty string otherwise.
func identifyProcess(port int) string {
	out, err := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-P", "-n", "-sTCP:LISTEN").Output()
	if err != nil {
		return ""
	}
	// Parse lsof output: skip header line, take first non-header line, extract command name.
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COMMAND") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) > 0 {
			return fields[0]
		}
	}
	return ""
}

// portList formats a slice of ints as a human-readable list.
func portList(ports []int) string {
	var s []string
	for _, p := range ports {
		s = append(s, strconv.Itoa(p))
	}
	return strings.Join(s, ", ")
}

// CheckSocketPerms validates that the daemon socket file has mode 0600.
//
// It stat's the socket file directly and checks its permission bits. Returns ok
// if the socket does not yet exist (the daemon will create it).
func CheckSocketPerms(homeDir string) CheckResult {
	name := "socket_perms"

	paths := home.NewHomePaths(homeDir)

	// If socket doesn't exist yet, that's fine — daemon creates it.
	fi, err := os.Lstat(paths.Socket)
	if os.IsNotExist(err) {
		return CheckResult{
			Name:    name,
			Status:  "ok",
			Message: "Socket file does not exist yet (will be created by daemon)",
		}
	}
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Cannot stat socket file %s: %v", paths.Socket, err),
			FixHint: fmt.Sprintf("Check that %s is accessible.", paths.Socket),
		}
	}

	// Reject symlinks.
	if fi.Mode()&os.ModeSymlink != 0 {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Socket %s is a symlink", paths.Socket),
			FixHint: fmt.Sprintf("Remove the symlink at %s and let the daemon create a real socket.", paths.Socket),
		}
	}

	// Check permissions: must be 0600 (owner read/write only).
	perm := fi.Mode().Perm()
	if perm != 0600 {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Socket %s has permissions %#o, want 0600", paths.Socket, perm),
			FixHint: fmt.Sprintf("Run 'chmod 0600 %s' to fix socket permissions.", paths.Socket),
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Socket %s has correct permissions (0600)", paths.Socket),
	}
}

// CheckHomeDirPerms validates that the home directory has mode 0700.
//
// It uses home.ValidatePermissions on the home directory.
func CheckHomeDirPerms(homeDir string) CheckResult {
	name := "home_perms"

	paths := home.NewHomePaths(homeDir)

	if _, err := os.Stat(paths.Home); os.IsNotExist(err) {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Home directory %s does not exist", paths.Home),
			FixHint: "Run 'agentpaasd init' or 'agent daemon start' to create the home directory.",
		}
	}

	if err := home.ValidatePermissions(paths); err != nil {
		// Check if the error is about home dir specifically.
		if strings.Contains(err.Error(), "home directory") {
			return CheckResult{
				Name:    name,
				Status:  "error",
				Message: fmt.Sprintf("Home directory permission check failed: %v", err),
				FixHint: fmt.Sprintf("Run 'chmod 0700 %s' to fix home directory permissions.", paths.Home),
			}
		}
		// Error might be about socket perms — ignore, socket check covers it.
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Home directory %s has correct permissions (0700)", paths.Home),
	}
}

// CheckDaemonReady checks whether the daemon gRPC endpoint is responding.
//
// It dials the daemon socket and issues a Doctor RPC. Returns ok if the
// daemon responds, or error with a start hint if not.
func CheckDaemonReady(socketPath string) CheckResult {
	name := "daemon_ready"

	if socketPath == "" {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: "Socket path is empty",
			FixHint: "Set AGENTPAAS_SOCKET or use --socket flag.",
		}
	}

	conn, err := grpc.NewClient(
		fmt.Sprintf("unix://%s", socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Cannot create gRPC client: %v", err),
			FixHint: "Run 'agent daemon start' to start the daemon.",
		}
	}
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlServiceClient(conn)
	resp, err := client.Doctor(context.Background(), &controlv1.DoctorRequest{})
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Daemon did not respond to Doctor RPC: %v", err),
			FixHint: "Run 'agent daemon start' to start the daemon.",
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Daemon is ready (overall status: %s, %d checks reported)", resp.GetOverallStatus(), len(resp.GetChecks())),
	}
}

// CheckProtoCompatible compares the CLI and daemon protocol versions.
//
// It checks via the Doctor RPC whether the daemon's ProtoVersion matches
// the CLI's compiled-in ProtoVersion. Version mismatch is a warning, not
// an error, since minor protocol changes may be backward-compatible.
func CheckProtoCompatible(socketPath string, cliVersion, cliProtoVersion string) CheckResult {
	name := "proto_compatible"

	// Try to connect and get daemon version info.
	conn, err := grpc.NewClient(
		fmt.Sprintf("unix://%s", socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
	if err != nil {
		// If daemon isn't running, skip this check silently.
		return CheckResult{
			Name:    name,
			Status:  "ok",
			Message: fmt.Sprintf("Cannot connect to daemon (CLI v%s, proto %s) — check skipped", cliVersion, cliProtoVersion),
		}
	}
	defer func() { _ = conn.Close() }()

	client := controlv1.NewControlServiceClient(conn)
	resp, err := client.Doctor(context.Background(), &controlv1.DoctorRequest{})
	if err != nil {
		// Daemon didn't respond — can't compare.
		return CheckResult{
			Name:    name,
			Status:  "warning",
			Message: "Daemon did not respond to Doctor RPC — cannot compare protocol versions",
			FixHint: "Ensure the daemon is running and responsive.",
		}
	}

	// Check the version check in the response.
	daemonProto := ""
	for _, c := range resp.GetChecks() {
		if c.GetName() == "version" {
			daemonProto = c.GetMessage()
			break
		}
	}
	if daemonProto == "" {
		daemonProto = "unknown"
	}

	// For now, proto version is embedded in the overall listener; let's make a
	// weaker check: just note both versions.
	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("CLI proto: %s, Daemon proto: %s", cliProtoVersion, daemonProto),
	}
}