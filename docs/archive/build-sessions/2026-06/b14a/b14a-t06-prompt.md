# Task: 14A-T06 — Pre-flight daemon socket check (GAP-8, LOW)

## Context

`integrations/hermes-plugin/tools.py` `_run_cli()` spawns the CLI via subprocess.run()
with a 300s timeout. If the daemon is down, the CLI process hangs trying to connect to
the socket, and the plugin hangs for the full timeout duration (300s).

The plan says "daemon down → tool returns actionable agentpaas_doctor hints, not a hang."

## What to implement

In `integrations/hermes-plugin/tools.py`, add a pre-flight socket check in `_run_cli`:

### 1. Add socket check function

```python
import stat

def _check_daemon_socket():
    """Pre-flight check: is the daemon socket available?
    
    Returns (is_available, error_dict):
    - is_available: True if socket exists and is a Unix socket
    - error_dict: None if available, else structured error
    """
    sock_path = _resolve_socket_path()
    if not sock_path:
        # Try deriving from AGENTPAAS_HOME
        home = os.environ.get("AGENTPAAS_HOME", "")
        if home:
            sock_path = os.path.join(home, "run", "agentpaas.sock")
    
    if not sock_path:
        return False, {
            "error": "daemon socket not configured (AGENTPAAS_SOCKET or AGENTPAAS_HOME)",
            "error_category": "daemon_unavailable",
            "next_action": "start_docker",
            "hint": "Run: agentpaas daemon start",
        }
    
    if not os.path.exists(sock_path):
        return False, {
            "error": f"daemon socket not found: {sock_path}",
            "error_category": "daemon_unavailable",
            "next_action": "start_docker",
            "hint": "Run: agentpaas daemon start",
        }
    
    try:
        st = os.stat(sock_path)
        if not stat.S_ISSOCK(st.st_mode):
            return False, {
                "error": f"socket path is not a Unix socket: {sock_path}",
                "error_category": "daemon_unavailable",
                "next_action": "start_docker",
                "hint": "Run: agentpaas daemon start",
            }
    except OSError as e:
        return False, {
            "error": f"cannot stat socket: {e}",
            "error_category": "daemon_unavailable",
            "next_action": "start_docker",
            "hint": "Run: agentpaas daemon start",
        }
    
    return True, None
```

### 2. Wire into _run_cli

At the top of `_run_cli`, before spawning the subprocess:

```python
def _run_cli(cmd_args):
    """Run agent CLI with --json; return sanitized operator dict."""
    # Pre-flight: check daemon socket is available (avoid 300s hang)
    sock_available, sock_err = _check_daemon_socket()
    if not sock_available:
        return sock_err
    
    binary = _resolve_agent_binary()
    # ... rest of function unchanged ...
```

### 3. Skip socket check for non-daemon commands

Some CLI commands don't need the daemon — `doctor`, `--version`, `init` (which creates
a project, not contacts daemon). Add a skip list:

```python
_NO_DAEMON_COMMANDS = frozenset({"doctor", "--version", "help", "--help"})

def _needs_daemon(cmd_args):
    """Check if the command needs the daemon."""
    if not cmd_args:
        return True
    first = cmd_args[0]
    return first not in _NO_DAEMON_COMMANDS
```

In `_run_cli`:
```python
def _run_cli(cmd_args):
    # Pre-flight: check daemon socket for commands that need it
    if _needs_daemon(cmd_args):
        sock_available, sock_err = _check_daemon_socket()
        if not sock_available:
            return sock_err
    # ... rest ...
```

## Tests to write

Create `integrations/hermes-plugin/tests/test_socket_check.py`:

1. **test_socket_available:** Create a temp Unix socket, set AGENTPAAS_SOCKET to it,
   _check_daemon_socket returns (True, None)
2. **test_socket_not_found:** Set AGENTPAAS_SOCKET to /tmp/nonexistent.sock,
   returns (False, {"error_category": "daemon_unavailable"})
3. **test_socket_not_configured:** No AGENTPAAS_SOCKET or AGENTPAAS_HOME,
   returns (False, {"error_category": "daemon_unavailable"})
4. **test_socket_path_is_not_socket:** Create a regular file at the socket path,
   returns (False, {"error_category": "daemon_unavailable"})
5. **test_socket_from_home:** Set AGENTPAAS_HOME, create socket at
   $HOME/run/agentpaas.sock, returns (True, None)
6. **test_run_cli_returns_daemon_unavailable:** Mock _resolve_agent_binary,
   set AGENTPAAS_SOCKET to nonexistent path, call _run_cli(["status"]),
   result has error_category: "daemon_unavailable"
7. **test_doctor_skips_socket_check:** Set AGENTPAAS_SOCKET to nonexistent,
   call _run_cli(["doctor"]), mock subprocess.run — the subprocess IS called
   (doctor doesn't need daemon)
8. **test_init_skips_socket_check:** Same for "init" command... wait, actually
   "init" DOES need the daemon (it contacts the daemon to validate). Check the
   existing code — does init work without daemon? Actually, looking at the code,
   `init` calls the CLI which may or may not need the daemon. Keep init in the
   needs-daemon list for now. Only skip for doctor, --version, help.

## Testing

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All existing tests + new tests must pass.

## Commit message

```
feat(14a-t06): pre-flight daemon socket check (GAP-8)

Add _check_daemon_socket() that verifies the Unix socket exists before
spawning the CLI. Returns structured daemon_unavailable error with
next_action: start_docker hint. Skipped for non-daemon commands
(doctor, --version, help). Prevents 300s hang on dead daemon.

7 new tests in test_socket_check.py.
```

## Branch

Create branch `feat/b14a-t06` from main.
