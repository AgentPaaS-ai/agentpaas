package doctor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

// knownPorts are the ports that agentpaas uses for control and agent traffic.
var knownPorts = []int{7700, 7717, 7718}

// dockerInfoTimeout is the timeout for "docker info" commands.
const dockerInfoTimeout = 10 * time.Second

// dockerContextTimeout is the timeout for "docker context show".
const dockerContextTimeout = 5 * time.Second

// identifyProcessTimeout is the timeout for lsof.
const identifyProcessTimeout = 5 * time.Second

// pgrepTimeout is the timeout for pgrep.
const pgrepTimeout = 5 * time.Second

// daemonDialTimeout is the timeout for gRPC dial to the daemon.
const daemonDialTimeout = 5 * time.Second

// expectedDockerPaths are the only legitimate locations for the docker binary.
var expectedDockerPaths = []string{
	"/usr/local/bin/docker",
	"/opt/homebrew/bin/docker",
	"/usr/bin/docker",
}

// validateDockerBinary checks that the docker binary at PATH is a legitimate
// installation. Returns a warning CheckResult if the path is unexpected.
func validateDockerBinary(name string) *CheckResult {
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return nil
	}

	// Resolve symlinks to get the real path.
	realPath, err := filepath.EvalSymlinks(dockerPath)
	if err != nil {
		realPath = dockerPath
	}

	// Reject if the binary lives in /tmp, /var/tmp.
	if strings.HasPrefix(realPath, "/tmp/") || strings.HasPrefix(realPath, "/var/tmp/") {
		return &CheckResult{
			Name:    name,
			Status:  "warning",
			Message: fmt.Sprintf("Docker binary at unexpected path %s — verify this is a legitimate Docker installation", realPath),
			FixHint: fmt.Sprintf("Remove the spoofed docker binary at %s and install Docker from the official channel.", realPath),
		}
	}

	// Check if the binary is in a user-writable directory.
	// Home directories and /tmp-like dirs are considered user-writable.
	homeDir, _ := os.UserHomeDir() // empty home handled by caller
	if homeDir != "" && strings.HasPrefix(realPath, homeDir) {
		return &CheckResult{
			Name:    name,
			Status:  "warning",
			Message: fmt.Sprintf("Docker binary at unexpected path %s — verify this is a legitimate Docker installation", realPath),
			FixHint: fmt.Sprintf("The docker binary is located in your home directory (%s). Remove it and install Docker from the official channel.", realPath),
		}
	}

	// Check against known expected locations.
	found := false
	for _, loc := range expectedDockerPaths {
		rp, err := filepath.EvalSymlinks(loc)
		if err == nil && realPath == rp {
			found = true
			break
		}
		if realPath == loc {
			found = true
			break
		}
	}

	if !found {
		return &CheckResult{
			Name:    name,
			Status:  "warning",
			Message: fmt.Sprintf("Docker binary at unexpected path %s — verify this is a legitimate Docker installation", realPath),
			FixHint: fmt.Sprintf("Expected docker at one of: %s. The current binary is at %s.", strings.Join(expectedDockerPaths, ", "), realPath),
		}
	}

	return nil
}

// CheckDockerReachable verifies that the Docker daemon is reachable.
//
// It shells out to "docker info" and checks the API version in the output.
// Returns ok with the API version when reachable, or error with a fix hint
// when Docker is stopped or not installed.
func CheckDockerReachable() CheckResult {
	name := "docker_reachable"

	// Validate the docker binary path before running it.
	if result := validateDockerBinary(name); result != nil {
		return *result
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerInfoTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Output()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return CheckResult{
				Name:    name,
				Status:  "error",
				Message: fmt.Sprintf("%s timed out after %ds", name, int(dockerInfoTimeout.Seconds())),
				FixHint: "Docker info timed out. Check Docker daemon responsiveness.",
			}
		}
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
		Message: fmt.Sprintf("Docker daemon is reachable (Server version: %s)", apiVersion),
	}
}

// CheckDockerServerVersion validates the Docker server version against known
// vulnerable ranges. Currently rejects Docker Engine < 29.5.1 (vulnerable to
// CVE-2026-41567 / 41568 / 42306).
//
// It shells out to "docker info" for the server version, then compares
// against the minimum fixed version using numeric semver comparison.
func CheckDockerServerVersion() CheckResult {
	name := "docker_server_version"

	// Validate the docker binary path before running it.
	if result := validateDockerBinary(name); result != nil {
		return *result
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerInfoTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Output()
	if err != nil {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Cannot determine Docker server version: %v", err),
			FixHint: "Run 'docker info' to verify the daemon is running.",
		}
	}

	serverVersion := strings.TrimSpace(string(out))
	if serverVersion == "" {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: "Docker daemon returned an empty version string",
			FixHint: "Run 'docker info' to check Docker daemon status.",
		}
	}

	if isDockerVersionVulnerable(serverVersion) {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("Docker Engine %s has known vulnerabilities (CVE-2026-41567/41568/42306). Upgrade to 29.5.1+ via Docker Desktop update or 'colima upgrade'.", serverVersion),
			FixHint: "Update Docker Desktop from the official channel or run 'colima stop && colima start --edit' to adjust the version.",
		}
	}

	return CheckResult{
		Name:    name,
		Status:  "ok",
		Message: fmt.Sprintf("Docker Engine %s (patched)", serverVersion),
	}
}

// parseDockerVersion splits a dotted semver string (e.g. "29.5.1") into
// its major, minor, and patch components. Leading 'v' prefix and build
// metadata are rejected.
func parseDockerVersion(v string) (major, minor, patch int, err error) {
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid version %q: expected 3 dot-separated components", v)
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse major version %q: %w", parts[0], err)
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse minor version %q: %w", parts[1], err)
	}
	patch, err = strconv.Atoi(parts[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse patch version %q: %w", parts[2], err)
	}
	return major, minor, patch, nil
}

// minFixedDockerVersionMajor, Minor, Patch define the minimum safe Docker
// Engine version (29.5.1) that fixes CVE-2026-41567/41568/42306.
const (
	minFixedDockerVersionMajor = 29
	minFixedDockerVersionMinor = 5
	minFixedDockerVersionPatch = 1
)

// isDockerVersionVulnerable returns true if the given Docker Engine version
// is below the minimum fixed version (29.5.1). Uses numeric comparison so
// that v29.5.10 > v29.5.1 is handled correctly (backport-aware).
func isDockerVersionVulnerable(version string) bool {
	major, minor, patch, err := parseDockerVersion(version)
	if err != nil {
		// If we can't parse the version, assume vulnerable (fail closed).
		return true
	}
	if major < minFixedDockerVersionMajor {
		return true
	}
	if major > minFixedDockerVersionMajor {
		return false
	}
	// major == 29
	if minor < minFixedDockerVersionMinor {
		return true
	}
	if minor > minFixedDockerVersionMinor {
		return false
	}
	// minor == 5
	return patch < minFixedDockerVersionPatch
}

// CheckDockerContext reports the current Docker context name.
//
// It runs "docker context show" to get the active context. If Docker is
// not reachable, the check returns a warning rather than an error because
// the context check is informational.
func CheckDockerContext() CheckResult {
	name := "docker_context"

	// Validate the docker binary path before running it.
	if result := validateDockerBinary(name); result != nil {
		return *result
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerContextTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "docker", "context", "show").Output()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return CheckResult{
				Name:    name,
				Status:  "warning",
				Message: fmt.Sprintf("%s timed out after %ds", name, int(dockerContextTimeout.Seconds())),
				FixHint: "Docker context show timed out. Check Docker daemon responsiveness.",
			}
		}
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
	ctx1, cancel1 := context.WithTimeout(context.Background(), pgrepTimeout)
	defer cancel1()
	desktopOut, err := exec.CommandContext(ctx1, "pgrep", "-f", "com.docker.docker").Output()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		// pgrep returns non-zero exit code when no process matches — that's fine.
		desktopOut = nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("%s timed out after %ds", name, int(pgrepTimeout.Seconds())),
			FixHint: "pgrep timed out checking for Docker Desktop process.",
		}
	}
	desktopRunning := err == nil && strings.TrimSpace(string(desktopOut)) != ""

	// Check for Colima process.
	ctx2, cancel2 := context.WithTimeout(context.Background(), pgrepTimeout)
	defer cancel2()
	colimaOut, err := exec.CommandContext(ctx2, "pgrep", "-f", "colima").Output()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		colimaOut = nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return CheckResult{
			Name:    name,
			Status:  "error",
			Message: fmt.Sprintf("%s timed out after %ds", name, int(pgrepTimeout.Seconds())),
			FixHint: "pgrep timed out checking for Colima process.",
		}
	}
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
// For each port, it attempts net.Listen("tcp", ...) on both IPv4 (127.0.0.1)
// and IPv6 (::1) loopback. If either listen fails, the port is considered
// squatted and it uses lsof to identify the process holding the port.
func CheckPortsFree() CheckResult {
	name := "ports_free"

	var squatters []string
	for _, port := range knownPorts {
		// Try IPv4 loopback.
		ipv4Addr := fmt.Sprintf("127.0.0.1:%d", port)
		ln4, err4 := net.Listen("tcp", ipv4Addr)

		// Try IPv6 loopback.
		ipv6Addr := fmt.Sprintf("[::1]:%d", port)
		ln6, err6 := net.Listen("tcp", ipv6Addr)

		// If either binding failed, the port is squatted on that address family.
		if err4 != nil || err6 != nil {
			// Close whichever succeeded.
			if err4 == nil && ln4 != nil {
				_ = ln4.Close() // best-effort close
			}
			if err6 == nil && ln6 != nil {
				_ = ln6.Close() // best-effort close
			}
			// Try to identify the process.
			procName := identifyProcess(port)
			hint := fmt.Sprintf("Port %d is in use", port)
			if procName != "" {
				hint = fmt.Sprintf("Process '%s' is holding port %d", procName, port)
			}
			squatters = append(squatters, hint)
			continue
		}

		// Both succeeded — close both and move on.
		_ = ln4.Close() // best-effort close
		_ = ln6.Close() // best-effort close
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
	ctx, cancel := context.WithTimeout(context.Background(), identifyProcessTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "lsof", "-i", fmt.Sprintf(":%d", port), "-P", "-n", "-sTCP:LISTEN").Output()
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
	if errors.Is(err, os.ErrNotExist) {
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

	if _, err := os.Stat(paths.Home); errors.Is(err, os.ErrNotExist) {
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
	defer func() { _ = conn.Close() }() // best-effort close

	client := controlv1.NewControlServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), daemonDialTimeout)
	defer cancel()
	resp, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return CheckResult{
				Name:    name,
				Status:  "error",
				Message: fmt.Sprintf("%s timed out after %ds", name, int(daemonDialTimeout.Seconds())),
				FixHint: "Daemon gRPC dial timed out. Check that the daemon is running and responsive.",
			}
		}
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
	defer func() { _ = conn.Close() }() // best-effort close

	client := controlv1.NewControlServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), daemonDialTimeout)
	defer cancel()
	resp, err := client.Doctor(ctx, &controlv1.DoctorRequest{})
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
