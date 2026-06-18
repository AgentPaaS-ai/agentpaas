// Package home provides the agentpaas home directory layout, discovery, and
// secure permission management.
//
// Home directory layout
//
// The agentpaas daemon (agentpaasd) and CLI (agent) share a local data
// directory ("home") that stores runtime artifacts and persistent state.
// The default path is ~/.agentpaas, but both callers can override it via
// the AGENTPAAS_HOME environment variable.
//
//	~/.agentpaas/         (directory, mode 0700)
//	├── daemon.sock       (Unix socket, mode 0600)
//	├── agentpaasd.pid    (PID file, mode 0600)
//	├── agentpaasd.lock   (flock file, mode 0600)
//	├── logs/             (directory, mode 0700)
//	├── state/            (directory, mode 0700)
//	├── config/           (directory, mode 0700)
//	├── cache/            (directory, mode 0700)
//	└── tmp/              (directory, mode 0700)
//
// Security invariants
//
//   - The home directory MUST be mode 0700 so that only the owning user
//     can read, write, or traverse it.
//   - The socket file MUST be mode 0600 so that only the daemon and root
//     may connect. Broader permissions create a local privilege-escalation
//     vector – any user on the system could send control commands to the
//     daemon.
//   - ValidatePermissions() checks these invariants and returns a clear,
//     actionable error when they are violated. The daemon MUST call this
//     before serving.
//
// Stale file recovery
//
// After an unclean shutdown (crash, SIGKILL, power loss) the daemon may
// find old socket, PID, or lock files in the home directory. These files
// block restart because:
//
//   - A stale socket file prevents net.Listen("unix", ...) from binding.
//   - A stale PID file would confuse monitoring scripts.
//   - A stale lock file occupies the flocker path.
//
// The IsStalePid, IsStaleSocket, and IsStaleLock functions detect each
// case without false positives. CleanStale() removes only those files
// that are provably stale. It NEVER removes a file that a live process
// might own – that would race with the other daemon and corrupt state.
//
// The lock file uses POSIX flock(2) for mutual exclusion. Two daemons
// racing to start: the first gets the lock, the second sees EWOULDBLOCK
// and exits.
//
// Environment variables
//
//	AGENTPAAS_HOME    – override the default home directory (~/.agentpaas)
//	AGENTPAAS_SOCKET  – override the default socket path (<home>/daemon.sock)
package home