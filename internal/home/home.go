package home

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// HomePaths holds every path in the agentpaas home directory layout.
// All paths are absolute and derive from a single root (Home).
//
// Callers should obtain a HomePaths via NewHomePaths(DiscoverHome()) and
// then pass it to Ensure(), ValidatePermissions(), and CleanStale().
type HomePaths struct {
	// Home is the root agentpaas data directory (~/.agentpaas by default).
	Home string

	// Socket is the Unix domain socket path for daemon control.
	Socket string

	// PID is the path to the daemon's PID file.
	PID string

	// Lock is the path to the daemon's flock file.
	Lock string

	// Logs is the directory for daemon log output.
	Logs string

	// State is the directory for persistent runtime state.
	State string

	// Config is the directory for user-provided configuration files.
	Config string

	// Cache is the directory for cached data (templates, compiled rules, etc.).
	Cache string

	// Tmp is the directory for ephemeral scratch files.
	Tmp string
}

const (
	homeDirName   = ".agentpaas"
	socketName    = "daemon.sock"
	pidName       = "agentpaasd.pid"
	lockName      = "agentpaasd.lock"
	logsDirName   = "logs"
	stateDirName  = "state"
	configDirName = "config"
	cacheDirName  = "cache"
	tmpDirName    = "tmp"

	// homePerm is the required permission mode for the home directory.
	homePerm = os.FileMode(0700)

	// socketPerm is the required permission mode for the socket file.
	socketPerm = os.FileMode(0600)

	// subdirPerm is the required permission mode for all subdirectories.
	subdirPerm = os.FileMode(0700)

	// filePerm is the required permission mode for PID and lock files.
	filePerm = os.FileMode(0600)
)

// systemDirs is the list of system directories that ValidatePath rejects.
var systemDirs = []string{
	"/etc",
	"/var",
	"/usr",
	"/bin",
	"/sbin",
	"/dev",
	"/proc",
	"/sys",
	"/root",
	"/lib",
	"/lib64",
}

// ValidatePath checks that path is a safe, non-system directory location.
//
// It rejects:
//   - Non-absolute paths (must start with /)
//   - System directories: /etc, /var, /usr, /bin, /sbin, /dev, /proc, /sys,
//     /root, /lib, /lib64 — including any subdirectory of them
//   - Directories that are not candidates for agentpaas data
//
// It allows: user home directories, /tmp, /var/folders (macOS temp), and
// any custom path not in the blocklist.
func ValidatePath(path string) error {
	if path == "" {
		return fmt.Errorf("home: path is empty")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("home: path %q is not absolute (must start with /)", path)
	}

	// Clean the path to resolve any ".." or "." components.
	cleaned := filepath.Clean(path)

	// Allow macOS temp directory /var/folders.
	if cleaned == "/var/folders" || strings.HasPrefix(cleaned, "/var/folders/") {
		return nil
	}

	// Allow /tmp.
	if cleaned == "/tmp" || strings.HasPrefix(cleaned, "/tmp/") {
		return nil
	}

	// Check if the path is a system directory or a subdirectory of one.
	for _, sysDir := range systemDirs {
		if cleaned == sysDir || strings.HasPrefix(cleaned, sysDir+"/") {
			return fmt.Errorf("home: path %q is a system directory (%s) and is not allowed for agentpaas data", path, sysDir)
		}
	}

	return nil
}

// DiscoverHome returns the agentpaas home directory.
//
// It checks the AGENTPAAS_HOME environment variable first. If unset, it
// defaults to ~/.agentpaas (the user's home directory joined with
// ".agentpaas"). When AGENTPAAS_HOME is set, the path is validated to
// prevent dangerous system-directory locations.
func DiscoverHome() (string, error) {
	if h := os.Getenv("AGENTPAAS_HOME"); h != "" {
		if err := ValidatePath(h); err != nil {
			return "", err
		}
		return h, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home: cannot determine user home directory: %w", err)
	}
	return filepath.Join(userHome, homeDirName), nil
}

// DiscoverSocketPath returns the socket path for the given home directory.
//
// It checks the AGENTPAAS_SOCKET environment variable first. If unset, it
// defaults to <homeDir>/daemon.sock. When AGENTPAAS_SOCKET is set, the path
// is validated to prevent dangerous system-directory locations.
func DiscoverSocketPath(homeDir string) string {
	if s := os.Getenv("AGENTPAAS_SOCKET"); s != "" {
		// Validate the socket path — but don't error, just fall through to
		// the default if validation fails. The socket path can be anywhere,
		// but we still need to guard against system directories.
		if err := ValidatePath(s); err == nil {
			return s
		}
	}
	return filepath.Join(homeDir, socketName)
}

// NewHomePaths creates a HomePaths struct rooted at homeDir.
//
// All paths are derived deterministically from homeDir. Socket path respects
// the AGENTPAAS_SOCKET environment variable if set.
func NewHomePaths(homeDir string) *HomePaths {
	return &HomePaths{
		Home:   homeDir,
		Socket: DiscoverSocketPath(homeDir),
		PID:    filepath.Join(homeDir, pidName),
		Lock:   filepath.Join(homeDir, lockName),
		Logs:   filepath.Join(homeDir, logsDirName),
		State:  filepath.Join(homeDir, stateDirName),
		Config: filepath.Join(homeDir, configDirName),
		Cache:  filepath.Join(homeDir, cacheDirName),
		Tmp:    filepath.Join(homeDir, tmpDirName),
	}
}

// isSymlink checks whether the given path is a symlink using os.Lstat.
// Returns false if the path does not exist.
func isSymlink(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return fi.Mode()&os.ModeSymlink != 0, nil
}

// Ensure creates the home directory and all subdirectories and runtime files
// with the required secure permissions.
//
// It creates:
//   - Home directory (mode 0700)
//   - logs/, state/, config/, cache/, tmp/ subdirectories (mode 0700)
//   - Empty daemon.sock, agentpaasd.pid, and agentpaasd.lock files (mode 0600)
//
// Ensure is idempotent – calling it multiple times is safe. If a path
// already exists with correct permissions, it is left unchanged.
//
// Ensure refuses to operate on symlinks within the home tree to prevent
// symlink-escape attacks where an attacker replaces a subdirectory with a
// symlink pointing outside the home directory.
func Ensure(paths *HomePaths) error {
	// Refuse if the home root path itself is a symlink (prevents symlink
	// escape). This check must happen BEFORE any MkdirAll or Chmod calls
	// on paths.Home, because os.MkdirAll follows symlinks and os.Chmod
	// would operate on the symlink target.
	sym, err := isSymlink(paths.Home)
	if err != nil {
		return fmt.Errorf("home: cannot stat home path %s: %w", paths.Home, err)
	}
	if sym {
		return fmt.Errorf("home: refusing to follow symlink at home path %s", paths.Home)
	}

	// If the socket path has been overridden via AGENTPAAS_SOCKET (detected
	// by checking whether it differs from the default path under the home
	// dir), verify the socket path itself is not a symlink. This prevents
	// an escape where an attacker places a symlink at the overridden socket
	// location pointing outside the home tree.
	defaultSocket := filepath.Join(paths.Home, socketName)
	if paths.Socket != defaultSocket {
		sym, err := isSymlink(paths.Socket)
		if err != nil {
			return fmt.Errorf("home: cannot stat socket path %s: %w", paths.Socket, err)
		}
		if sym {
			return fmt.Errorf("home: refusing to follow symlink at socket path %s", paths.Socket)
		}
	}

	// Create home directory.
	if err := os.MkdirAll(paths.Home, homePerm); err != nil {
		return fmt.Errorf("home: cannot create home directory %s: %w", paths.Home, err)
	}

	// Ensure home directory has correct permissions (fix if needed).
	if err := os.Chmod(paths.Home, homePerm); err != nil {
		return fmt.Errorf("home: cannot set permissions on %s: %w", paths.Home, err)
	}

	// Create subdirectories.
	dirs := []string{
		paths.Logs,
		paths.State,
		paths.Config,
		paths.Cache,
		paths.Tmp,
	}
	for _, d := range dirs {
		// Refuse if the path is a symlink (prevents symlink escape).
		sym, err := isSymlink(d)
		if err != nil {
			return fmt.Errorf("home: cannot stat %s: %w", d, err)
		}
		if sym {
			return fmt.Errorf("home: %s is a symlink; refusing to follow", d)
		}

		if err := os.MkdirAll(d, subdirPerm); err != nil {
			return fmt.Errorf("home: cannot create directory %s: %w", d, err)
		}
		if err := os.Chmod(d, subdirPerm); err != nil {
			return fmt.Errorf("home: cannot set permissions on %s: %w", d, err)
		}
	}

	// Create runtime files (empty, with correct permissions).
	// NOTE: Do NOT pre-create the socket file. The daemon creates it via
	// net.Listen("unix", ...). A stale socket from a previous run causes
	// os.OpenFile to fail with "operation not supported on socket".
	// The daemon removes stale sockets before listening (server.go).
	runtimeFiles := []struct {
		path string
		mode os.FileMode
	}{
		{paths.PID, filePerm},
		{paths.Lock, filePerm},
	}
	for _, rf := range runtimeFiles {
		// Refuse if the path is a symlink.
		sym, err := isSymlink(rf.path)
		if err != nil {
			return fmt.Errorf("home: cannot stat %s: %w", rf.path, err)
		}
		if sym {
			return fmt.Errorf("home: %s is a symlink; refusing to follow", rf.path)
		}

		if err := touchFile(rf.path, rf.mode); err != nil {
			return fmt.Errorf("home: %w", err)
		}
	}

	return nil
}

// touchFile creates an empty file at path with the given mode if it does not
// already exist. If the file exists, it ensures the mode is correct.
func touchFile(path string, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, mode)
	if err != nil {
		return fmt.Errorf("cannot create %s: %w", path, err)
	}
	_ = f.Close() // best-effort close

	// Re-check with Lstat before Chmod to avoid following a symlink.
	sym, err := isSymlink(path)
	if err != nil {
		return fmt.Errorf("cannot check %s: %w", path, err)
	}
	if sym {
		return fmt.Errorf("%s is a symlink; refusing to set permissions on symlink target", path)
	}

	// Ensure correct permissions even if file existed with wrong ones.
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("cannot set permissions on %s: %w", path, err)
	}
	return nil
}

// ValidatePermissions checks that the home directory and socket file have
// secure permissions. It returns an actionable error if any check fails.
//
// The daemon MUST call this before serving. If permissions are too broad,
// ValidatePermissions refuses to initialize (fail-closed) and tells the
// user exactly how to fix each problem.
func ValidatePermissions(paths *HomePaths) error {
	// Check home directory.
	fi, err := os.Lstat(paths.Home)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("home directory %s does not exist — run 'agentpaasd init' or 'Ensure()'", paths.Home)
		}
		return fmt.Errorf("home: cannot stat home directory %s: %w", paths.Home, err)
	}
	if !fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("home: %s is not a directory", paths.Home)
	}
	// If the home path is a symlink, Stat the target to check its perms.
	if fi.Mode()&os.ModeSymlink != 0 {
		fi, err = os.Stat(paths.Home)
		if err != nil {
			return fmt.Errorf("home: cannot stat symlink target %s: %w", paths.Home, err)
		}
	}
	if !fi.IsDir() {
		return fmt.Errorf("home: %s is not a directory", paths.Home)
	}
	if fi.Mode().Perm()&^homePerm != 0 {
		return fmt.Errorf(
			"home directory %s has permissions %#o, which are too broad — "+
				"run 'chmod 0700 %s' to restrict access to the owning user only",
			paths.Home, fi.Mode().Perm(), paths.Home,
		)
	}

	// Check socket file (if it exists).
	if fi, err := os.Lstat(paths.Socket); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("home: socket %s is a symlink; refusing to validate", paths.Socket)
		}
		if fi.Mode().Perm()&^socketPerm != 0 {
			return fmt.Errorf(
				"socket file %s has permissions %#o, which are too broad — "+
					"run 'chmod 0600 %s' to restrict access to the owning user only",
				paths.Socket, fi.Mode().Perm(), paths.Socket,
			)
		}
	}
	// Socket file not existing yet is fine — the daemon will create it.

	return nil
}

// isAgentPaasProcess checks whether the given PID belongs to an agentpaasd
// process by examining the process command name. This prevents PID reuse
// attacks where a stale PID file matches a recycled PID belonging to a
// different (unrelated) process.
func isAgentPaasProcess(pid int) bool {
	// Try Linux /proc first.
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err == nil {
		cmdline := string(data)
		return strings.Contains(cmdline, "agentpaasd")
	}

	// macOS/BSD fallback: use `ps -p <pid> -o comm=`.
	out, err := exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(out))
	return strings.Contains(comm, "agentpaasd")
}

// IsStalePid checks whether the PID file at pidFile is stale.
//
// A PID file is stale if it exists but the process identified by the PID
// inside it is not running. This function reads the PID from the file,
// sends signal 0 to the process, and verifies the process command name
// matches "agentpaasd" to prevent PID reuse false negatives.
func IsStalePid(pidFile string) (bool, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("home: cannot read PID file %s: %w", pidFile, err)
	}

	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return true, nil // empty PID file is stale
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return true, nil // unparseable PID is stale
	}

	// Check if the PID file itself is world-writable (mode has 002 bit set).
	// If so, treat it as untrusted/stale because an attacker could have
	// written or overwritten it.
	if fi, err := os.Lstat(pidFile); err == nil {
		if fi.Mode().Perm()&0002 != 0 {
			return true, nil // world-writable PID file is untrusted
		}
	}

	// Signal 0 is a null check — it only checks if the process exists
	// without actually sending a signal.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true, nil // process not found
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return true, nil // process not running
	}

	// Process is alive — verify the command name to prevent PID reuse.
	if !isAgentPaasProcess(pid) {
		return true, nil // PID reused by a different process
	}

	return false, nil // process is running
}

// IsStaleSocket checks whether the socket file at sockFile is stale.
//
// A socket file is stale if it exists but nothing is listening on it.
// This function attempts to dial the Unix socket. If the connection
// succeeds, something is listening and the socket is live. If the
// connection fails, the socket is considered stale.
func IsStaleSocket(sockFile string) (bool, error) {
	if _, err := os.Stat(sockFile); os.IsNotExist(err) {
		return false, nil
	}

	conn, err := net.Dial("unix", sockFile)
	if err != nil {
		// Cannot connect — either no listener or file is not a real socket.
		return true, nil
	}
	_ = conn.Close() // best-effort close
	return false, nil
}

// IsStaleLock checks whether the lock file at lockFile is stale.
//
// A lock file is stale if it exists but no process holds an exclusive
// POSIX flock on it. This function opens the file and attempts to
// acquire an exclusive non-blocking flock. If the lock is acquired, no
// other process holds it, so the file is stale. If EWOULDBLOCK is
// returned, a live process holds the lock, so the file is not stale.
func IsStaleLock(lockFile string) (bool, error) {
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		return false, nil
	}

	f, err := os.OpenFile(lockFile, os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("home: cannot open lock file %s: %w", lockFile, err)
	}
	defer func() { _ = f.Close() }() // best-effort close

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if err == syscall.EWOULDBLOCK {
			// Another process holds the lock — not stale.
			return false, nil
		}
		return false, fmt.Errorf("home: flock error on %s: %w", lockFile, err)
	}

	// We acquired the lock — no one else holds it, so it's stale.
	// Release it immediately since we only needed to check.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) // intentionally ignored (reviewed)
	return true, nil
}

// CleanStale detects and removes stale runtime files from the home directory.
//
// It checks the PID file, socket file, and lock file. For each file that
// is provably stale (no live process owns it), CleanStale removes it.
//
// CleanStale is safe to call during daemon startup: it will never remove
// a file that a live daemon process might own, because each check
// positively proves the owning process is gone before removing the file.
//
// If a socket file exists and is held by a live process (cannot be removed),
// CleanStale returns an error explaining the situation so the caller can
// present it to the user.
func CleanStale(paths *HomePaths) error {
	// Check PID file.
	stale, err := IsStalePid(paths.PID)
	if err != nil {
		return fmt.Errorf("home: cannot check PID file staleness: %w", err)
	}
	if stale {
		if err := os.Remove(paths.PID); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("home: cannot remove stale PID file %s: %w", paths.PID, err)
		}
	}

	// Check socket file.
	stale, err = IsStaleSocket(paths.Socket)
	if err != nil {
		return fmt.Errorf("home: cannot check socket file staleness: %w", err)
	}
	if stale {
		if err := os.Remove(paths.Socket); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("home: cannot remove stale socket file %s: %w", paths.Socket, err)
		}
	} else {
		// Socket exists but is NOT stale — something is live on it.
		// This may be a legitimate daemon OR an attacker planting a
		// listener. Either way, we cannot clean it.
		if _, err := os.Lstat(paths.Socket); err == nil {
			return fmt.Errorf(
				"socket %s is held by a live process; cannot clean. "+
					"Stop the process or use a different socket path",
				paths.Socket,
			)
		}
	}

	// Check lock file.
	stale, err = IsStaleLock(paths.Lock)
	if err != nil {
		return fmt.Errorf("home: cannot check lock file staleness: %w", err)
	}
	if stale {
		if err := os.Remove(paths.Lock); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("home: cannot remove stale lock file %s: %w", paths.Lock, err)
		}
	}

	return nil
}