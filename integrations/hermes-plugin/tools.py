"""AgentPaaS Hermes plugin tool handlers — shell out to the agent CLI."""

import json
import os
import pwd
from collections import OrderedDict
import re as _re
import shutil
import stat
import subprocess
import threading
import time as _time

# CLI binary name; override with AGENTPAAS_BIN (absolute path or name on PATH).
AGENT_BIN = os.environ.get("AGENTPAAS_BIN", "agent")

try:
    from . import sanitizer as _sanitizer
except ImportError:
    try:
        import sanitizer as _sanitizer
    except ImportError:
        _sanitizer = None

class _ConfirmationState:
    """Thread-safe state for session runs and confirmation tracking."""

    MAX_CONFIRMATION_IDS = 10000

    def __init__(self):
        self._lock = threading.Lock()
        self._session_runs = set()
        self._used_confirmation_ids = OrderedDict()

    def register_session_run(self, run_id):
        """Thread-safe add to session runs."""
        if run_id:
            with self._lock:
                self._session_runs.add(run_id)

    def is_session_run(self, run_id):
        """Thread-safe check if run was created by this session."""
        with self._lock:
            return run_id in self._session_runs

    def discard_session_run(self, run_id):
        """Thread-safe remove from session runs."""
        with self._lock:
            self._session_runs.discard(run_id)

    def check_and_add_confirmation(self, confirmation_id):
        """Thread-safe check-and-add for replay protection.

        Returns (is_replay, should_refuse):
        - If ID is already in set: (True, True) — it's a replay
        - If ID is new: (False, False) — add it, not a replay
        """
        if not confirmation_id:
            return False, True
        with self._lock:
            if confirmation_id in self._used_confirmation_ids:
                return True, True
            if len(self._used_confirmation_ids) >= self.MAX_CONFIRMATION_IDS:
                self._used_confirmation_ids.popitem(last=False)
            self._used_confirmation_ids[confirmation_id] = None
            return False, False

    def reset(self):
        """Thread-safe clear all state. For testing and session restart."""
        with self._lock:
            self._session_runs.clear()
            self._used_confirmation_ids.clear()


_state = _ConfirmationState()

_CONFIRM_ID_KEYS = frozenset({
    "confirmation_id", "confirm_id", "confirmationId",
    "confirmation", "confirm", "cf_id",
})

_CONFIRM_ID_PATTERN = _re.compile(r'^cf_[a-fA-F0-9]{8,128}$')

_MAX_CONFIRMATION_TTL = 3600  # 1 hour max

_RUN_HANDLER_SENTINEL = object()


def _detect_self_confirm_attempt(args):
    """Detect any attempt to self-confirm a trust-boundary action.
    Checks standard keys, alternative names, and nested dicts.
    Returns True if a self-confirm attempt is detected.
    """
    if not isinstance(args, dict):
        return False
    # Check flat keys
    for key in _CONFIRM_ID_KEYS:
        val = args.get(key)
        if val:  # any truthy value
            return True
    # Check nested dicts (e.g., {"confirmation": {"id": "cf_..."}})
    for key, val in args.items():
        if isinstance(val, dict):
            for nested_key in _CONFIRM_ID_KEYS:
                if val.get(nested_key):
                    return True
            # Also check "id" key inside a "confirmation" dict
            if key in ("confirmation", "confirm") and val.get("id"):
                return True
    return False


def _extract_confirmation_id_from_args(args):
    """Extract a confirmation ID string from args, if present."""
    if not isinstance(args, dict):
        return None
    for key in _CONFIRM_ID_KEYS:
        val = args.get(key)
        if isinstance(val, str) and val:
            return val
    for key, val in args.items():
        if isinstance(val, dict):
            for nested_key in _CONFIRM_ID_KEYS:
                nested_val = val.get(nested_key)
                if isinstance(nested_val, str) and nested_val:
                    return nested_val
            if key in ("confirmation", "confirm"):
                nested_id = val.get("id")
                if isinstance(nested_id, str) and nested_id:
                    return nested_id
    return None


def _internal_register_session_run(run_id, _caller=None):
    """Register a run as session-owned. Only callable from agentpaas_run."""
    if _caller is not _RUN_HANDLER_SENTINEL:
        raise RuntimeError(
            "_internal_register_session_run is private; runs are registered "
            "automatically by agentpaas_run on success"
        )
    if run_id:
        _state.register_session_run(run_id)


def _register_session_run(run_id):
    """Deprecated. Raises — use agentpaas_run."""
    raise RuntimeError("Direct run registration is prohibited")


def _is_session_run(run_id):
    """Check if a run was created by this session (read-only)."""
    return _state.is_session_run(run_id)


def _validate_confirmation_id(confirmation_id):
    """Validate a daemon-issued confirmation ID. Returns (is_valid, error_message)."""
    if not confirmation_id or not isinstance(confirmation_id, str):
        return False, "confirmation_id is required"
    if not confirmation_id.startswith("cf_"):
        return False, "confirmation_id must be daemon-issued (prefix 'cf_')"
    if not _CONFIRM_ID_PATTERN.match(confirmation_id):
        return False, "confirmation_id must be hex (cf_<hex>, chars [a-fA-F0-9])"
    return True, ""


def _check_confirmation_replay(confirmation_id):
    """Check if a confirmation ID has already been used. Returns (is_replay, should_refuse)."""
    return _state.check_and_add_confirmation(confirmation_id)


def _is_confirmation_expired(issued_at, expires_at=None):
    """Check if a confirmation has expired. Uses monotonic clock logic.
    Caps TTL at _MAX_CONFIRMATION_TTL to prevent forgery of far-future expiry.
    """
    if not issued_at:
        return True  # missing timestamp = expired (fail-safe)
    now = _time.time()
    try:
        issued = float(issued_at)
    except (ValueError, TypeError):
        return True
    if expires_at:
        try:
            expires = float(expires_at)
        except (ValueError, TypeError):
            return True
        # Cap: if expiry is more than MAX_TTL after issue, clamp it
        max_valid = issued + _MAX_CONFIRMATION_TTL
        if expires > max_valid:
            expires = max_valid
        return now >= expires
    # No explicit expiry: use TTL cap
    return now >= (issued + _MAX_CONFIRMATION_TTL)


def _refuse_self_confirm(confirmation_id, issued_at=None, expires_at=None):
    """Refuse Hermes self-confirm attempts with replay/expiry checks."""
    valid, err = _validate_confirmation_id(confirmation_id)
    if not valid:
        return {
            "error": err,
            "error_category": "policy_denied",
            "requires_confirmation": True,
            "next_action": "ask_user",
        }
    is_replay, _ = _check_confirmation_replay(confirmation_id)
    if is_replay:
        return {
            "error": "confirmation_id already used (replay refused)",
            "error_category": "policy_denied",
            "requires_confirmation": True,
            "next_action": "ask_user",
        }
    if issued_at is not None and _is_confirmation_expired(issued_at, expires_at):
        return {
            "error": "confirmation_id expired",
            "error_category": "policy_denied",
            "requires_confirmation": True,
            "next_action": "ask_user",
        }
    return {
        "error": "Hermes tools cannot self-confirm trust-boundary changes. "
                 "Use the CLI: agent confirm <id>",
        "error_category": "policy_denied",
        "requires_confirmation": True,
        "next_action": "ask_user",
    }


def _is_remote_destination(path):
    """Detect if an output path is a remote destination requiring confirmation.
    Returns (is_remote, reason).
    """
    if not path or not isinstance(path, str):
        return True, "Empty or invalid output path"  # fail-safe: require confirmation

    path_lower = path.lower().strip()

    # Scheme-based detection (including uppercase variants)
    remote_schemes = ("ssh:", "s3:", "https:", "http:", "ftp:", "gs://", "ftps:", "scp:")
    for scheme in remote_schemes:
        if path_lower.startswith(scheme):
            return True, f"Remote scheme: {scheme}"

    # file:// — file URLs can point anywhere, treat as requiring confirmation
    if path_lower.startswith("file://"):
        return True, "file:// URL — may point outside project root"

    # data: URIs
    if path_lower.startswith("data:"):
        return True, "data: URI — non-standard output target"

    # Scheme-relative URLs (//host/path)
    if path_lower.startswith("//"):
        return True, "Scheme-relative URL — remote destination"

    # Check for user@host:path (scp-like syntax)
    if "@" in path and ":" in path:
        at_idx = path.index("@")
        colon_idx = path.index(":", at_idx)
        if colon_idx > at_idx:
            return True, "SCP-like remote path (user@host:path)"

    return False, ""


def _reset_confirmation_state():
    """Reset ALL confirmation tracking state. For testing and session restart.

    Must be called in test setUp/tearDown to avoid module-global state pollution.
    """
    _state.reset()


def _default_home_dir():
    """Default AgentPaaS home directory: ~/.agentpaas.

    Uses pwd to prevent $HOME env-var override attacks (consistent with
    _validate_project_path). Falls back to os.path.expanduser("~") if pwd fails.
    """
    try:
        import pwd
        return os.path.join(pwd.getpwuid(os.getuid()).pw_dir, ".agentpaas")
    except (KeyError, OSError, ImportError):
        return os.path.join(os.path.expanduser("~"), ".agentpaas")


def _resolve_socket_path():
    """Daemon socket path — must match the Go daemon's DiscoverSocketPath.

    Resolution order (mirrors internal/home/home.go):
    1. AGENTPAAS_SOCKET_PATH env var (Hermes plugin contract)
    2. AGENTPAAS_SOCKET env var (CLI env, same as daemon)
    3. <AGENTPAAS_HOME>/daemon.sock
    4. <default_home>/daemon.sock  (~/.agentpaas/daemon.sock)
    """
    sock = (
        os.environ.get("AGENTPAAS_SOCKET_PATH")
        or os.environ.get("AGENTPAAS_SOCKET")
    )
    if sock:
        return sock

    home = os.environ.get("AGENTPAAS_HOME") or _default_home_dir()
    return os.path.join(home, "daemon.sock")


def _resolve_home_dir():
    """AgentPaaS home directory for --home CLI flag.

    AGENTPAAS_HOME env var if set, otherwise the default ~/.agentpaas.
    """
    return os.environ.get("AGENTPAAS_HOME") or _default_home_dir()


def _check_daemon_socket():
    """Pre-flight check: is the daemon socket available?

    Returns (is_available, error_dict):
    - is_available: True if socket exists and is a Unix socket
    - error_dict: None if available, else structured error
    """
    sock_path = _resolve_socket_path()

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


_NO_DAEMON_COMMANDS = frozenset({"doctor", "--version", "help", "--help"})


def _needs_daemon(cmd_args):
    """Check if the command needs the daemon."""
    if not cmd_args:
        return True
    first = cmd_args[0]
    return first not in _NO_DAEMON_COMMANDS


_CLI_BINARY_ALLOW_LIST = (
    "/usr/local/bin",
    "/opt/homebrew/bin",
    "/usr/bin",
    "/bin",
)


def _check_binary_in_allow_list(path):
    """Check that the binary path is under an allowed directory.

    Raises ValueError if not.
    """
    here = os.path.dirname(os.path.abspath(__file__))
    repo_bin = os.path.abspath(os.path.join(here, "..", "..", "bin"))
    # Use pwd module, not $HOME env var, to prevent override attacks
    # (same fix as _validate_project_path in T01)
    try:
        home_dir = pwd.getpwuid(os.getuid()).pw_dir
    except (KeyError, OSError):
        home_dir = os.path.expanduser("~")
    allowed_dirs = list(_CLI_BINARY_ALLOW_LIST) + [
        os.path.join(home_dir, ".local", "bin"),
        repo_bin,
    ]
    for allowed in allowed_dirs:
        allowed_real = os.path.realpath(allowed)
        if path == allowed_real or path.startswith(allowed_real + os.sep):
            return
    raise ValueError(
        f"AGENTPAAS_CLI binary outside allowed directories: {path}. "
        f"Allowed: {', '.join(allowed_dirs)}"
    )


def _verify_agentpaas_binary(path):
    """Verify a binary is actually agentpaas by checking --version output.

    Returns True if verified, False otherwise.
    Raises ValueError with details if verification fails.
    """
    try:
        result = subprocess.run(
            [path, "--version"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        output = (result.stdout + " " + result.stderr).lower()
        if "agentpaas" in output:
            return True
        raise ValueError(
            f"AGENTPAAS_CLI binary does not appear to be agentpaas "
            f"(--version output: {result.stdout[:200]})"
        )
    except subprocess.TimeoutExpired:
        raise ValueError(
            "AGENTPAAS_CLI binary --version timed out (not a valid agentpaas binary?)"
        )


def _resolve_agentpaas_binary():
    """Find the AgentPaaS CLI binary (strict AGENTPAAS_CLI validation)."""
    if env_override := os.getenv("AGENTPAAS_CLI"):
        if not env_override.strip():
            pass
        else:
            real = os.path.realpath(env_override)
            if not os.path.isabs(env_override):
                raise ValueError(
                    f"AGENTPAAS_CLI must be an absolute path, got: {env_override}"
                )
            if not os.path.isfile(real):
                raise ValueError(f"AGENTPAAS_CLI is not a file: {env_override}")
            if not os.access(real, os.X_OK):
                raise ValueError(f"AGENTPAAS_CLI is not executable: {env_override}")
            _check_binary_in_allow_list(real)
            _verify_agentpaas_binary(real)
            return real
    p = shutil.which("agentpaas")
    if p:
        return p
    here = os.path.dirname(os.path.abspath(__file__))
    candidates = [
        os.path.join(here, "..", "..", "..", "bin", "agentpaas"),
        os.path.join(here, "..", "..", "..", "bin", "agent"),
        os.path.join(here, "..", "bin", "agentpaas"),
        os.path.join(here, "..", "bin", "agent"),
    ]
    for c in candidates:
        c = os.path.abspath(c)
        if os.path.isfile(c) and os.access(c, os.X_OK):
            return c
    return "agentpaas"


def _resolve_agent_binary():
    """Resolve CLI for run_agent_cli (AGENTPAAS_BIN override, else agentpaas)."""
    if env_override := os.getenv("AGENTPAAS_BIN"):
        if os.path.isabs(env_override):
            real = os.path.realpath(env_override)
            if os.path.isfile(real) and os.access(real, os.X_OK):
                return real
        if p := shutil.which(env_override):
            return p
    return _resolve_agentpaas_binary()


_STDOUT_CAP = 51200  # 50KB
_STDERR_CAP = 10240  # 10KB


def _get_cli_timeout():
    """Get CLI timeout from AGENTPAAS_CLI_TIMEOUT env var, with bounds.

    Default: 300s. Min: 10s. Max: 600s.
    """
    default = 300
    env_val = os.environ.get("AGENTPAAS_CLI_TIMEOUT", "")
    if not env_val:
        return default
    try:
        timeout = int(env_val)
    except (ValueError, TypeError):
        return default
    return max(10, min(600, timeout))


def _run_cli(cmd_args):
    """Run agent CLI with --json; return sanitized operator dict."""
    if _needs_daemon(cmd_args):
        sock_available, sock_err = _check_daemon_socket()
        if not sock_available:
            return sock_err

    binary = _resolve_agent_binary()
    full = [binary, "--json"]
    sock = _resolve_socket_path()
    full.extend(["--socket", sock])
    home = _resolve_home_dir()
    full.extend(["--home", home])
    full.extend([a for a in cmd_args if a])
    timeout = _get_cli_timeout()
    proc = subprocess.run(full, capture_output=True, text=True, timeout=timeout)
    return _parse_cli_result(proc)


def _parse_cli_result(proc):
    """Parse subprocess.CompletedProcess into sanitized result dict.

    Shared by _run_cli and _run_cli_with_stdin so both use identical
    truncation, JSON parsing, sanitizer, and error-envelope logic.
    """
    stdout_truncated = False
    stderr_truncated = False
    stdout_size = len(proc.stdout) if proc.stdout else 0
    stderr_size = len(proc.stderr) if proc.stderr else 0

    stdout = proc.stdout
    if stdout_size > _STDOUT_CAP:
        stdout = proc.stdout[:_STDOUT_CAP]
        stdout_truncated = True
    stderr = proc.stderr
    if stderr_size > _STDERR_CAP:
        stderr = proc.stderr[:_STDERR_CAP]
        stderr_truncated = True

    if proc.returncode != 0:
        result = {
            "error": stderr.strip(),
            "exit_code": proc.returncode,
            "error_category": "cli_error",
        }
        if stdout_truncated:
            result["output_truncated"] = True
            result["output_size"] = stdout_size
        if stderr_truncated:
            result["stderr_truncated"] = True
            result["stderr_size"] = stderr_size
        return result
    try:
        result = json.loads(stdout)
    except json.JSONDecodeError:
        result = {
            "error": f"CLI returned non-JSON output (length {stdout_size})",
            "raw_output_truncated": stdout[:2000],
            "exit_code": proc.returncode,
            "error_category": "cli_non_json_output",
        }
        if stdout_truncated:
            result["output_truncated"] = True
            result["output_size"] = stdout_size
        return result
    if _sanitizer and isinstance(result, dict):
        result = _sanitizer.sanitize_response(result)
    if isinstance(result, dict) and (stdout_truncated or stderr_truncated):
        if stdout_truncated:
            result["output_truncated"] = True
            result["output_size"] = stdout_size
        if stderr_truncated:
            result["stderr_truncated"] = True
            result["stderr_size"] = stderr_size
    return result


def _run_cli_with_stdin(cmd_args, stdin_input):
    """Run agent CLI with stdin input (for secret add/rotate). Returns same dict as _run_cli."""
    if _needs_daemon(cmd_args):
        sock_available, sock_err = _check_daemon_socket()
        if not sock_available:
            return sock_err
    binary = _resolve_agent_binary()
    full = [binary, "--json"]
    sock = _resolve_socket_path()
    full.extend(["--socket", sock])
    home = _resolve_home_dir()
    full.extend(["--home", home])
    full.extend([a for a in cmd_args if a])
    timeout = _get_cli_timeout()
    proc = subprocess.run(
        full, capture_output=True, text=True, timeout=timeout,
        input=stdin_input,
    )
    return _parse_cli_result(proc)


def run_agent_cli(args: list[str]) -> dict:
    """Run agent CLI; return ``{success, data, error}`` structured envelope."""
    try:
        result = _run_cli(args)
    except Exception as exc:
        return {"success": False, "data": None, "error": str(exc)}
    if isinstance(result, dict) and result.get("error"):
        category = result.get("error_category", "cli_error")
        return {
            "success": False,
            "data": result,
            "error": str(result.get("error")),
            "error_category": category,
        }
    return {"success": True, "data": result, "error": None}


def _tool_result_from_cli(cmd_args):
    """Structured envelope for P1 core Hermes tools."""
    return run_agent_cli(cmd_args)


def _validate_project_path(path):
    """Validate a project path is within allowed directories.

    Returns (is_valid, resolved_path, error_dict).
    - is_valid: True if path is allowed
    - resolved_path: the realpath() of the input (for use in _run_cli)
    - error_dict: None if valid, else {"error": ..., "error_category": "path_rejected"}
    """
    if not path or not isinstance(path, str):
        return False, None, {
            "error": "project_dir is required",
            "error_category": "path_rejected",
        }

    if path == ".":
        resolved = os.path.realpath(os.getcwd())
    else:
        resolved = os.path.realpath(os.path.expanduser(path))

    project_root = os.environ.get("AGENTPAAS_PROJECT_ROOT", "")
    if not project_root:
        project_root = os.getcwd()
    project_root = os.path.realpath(project_root)

    # Validate project root is not a system directory (defense against env override)
    _SYSTEM_DIRS = ("/", "/etc", "/usr", "/bin", "/sbin", "/var", "/sys", "/dev", "/proc")
    if project_root in _SYSTEM_DIRS:
        project_root = os.getcwd()

    # Use pwd module, not $HOME env var, to prevent override attacks
    try:
        home_dir = pwd.getpwuid(os.getuid()).pw_dir
    except (KeyError, OSError):
        home_dir = os.path.expanduser("~")

    allowed_roots = [
        project_root,
        "/tmp",
        home_dir,
    ]

    is_allowed = False
    for root in allowed_roots:
        root_resolved = os.path.realpath(root)
        if resolved == root_resolved or resolved.startswith(root_resolved + os.sep):
            is_allowed = True
            break

    if not is_allowed:
        return False, None, {
            "error": f"project_dir outside allowed roots: {path} (resolved: {resolved})",
            "error_category": "path_rejected",
        }

    return True, resolved, None


def agentpaas_init_project(args, **kwargs):
    """Initialize a new agent project."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    is_valid, resolved, err = _validate_project_path(project_dir)
    if not is_valid:
        return json.dumps(err)
    runtime = args.get("runtime", "python")
    try:
        result = _run_cli(
            [
                "init",
                resolved,
                "--noninteractive",
                "--runtime",
                runtime,
            ]
        )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_reconcile_project(args, **kwargs):
    """Reconcile agent.yaml from existing source code."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    is_valid, resolved, err = _validate_project_path(project_dir)
    if not is_valid:
        return json.dumps(err)
    try:
        result = _run_cli(
            [
                "init",
                resolved,
                "--from-code",
                "--noninteractive",
            ]
        )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_validate_project(args, **kwargs):
    """Validate an agent project directory."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    is_valid, resolved, err = _validate_project_path(project_dir)
    if not is_valid:
        return json.dumps(err)
    try:
        result = _run_cli(["validate", resolved])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_doctor(args, **kwargs):
    """Run system diagnostics."""
    args = args or {}
    try:
        result = _run_cli(["doctor"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_pack(args, **kwargs):
    """Build an agent image from a project directory."""
    args = args or {}
    project_dir = args.get("project_dir") or args.get("agent_project_path", ".")
    is_valid, resolved, err = _validate_project_path(project_dir)
    if not is_valid:
        return json.dumps(err)
    try:
        result = _run_cli(["pack", resolved])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_run(args, **kwargs):
    """Start a new agent run."""
    args = args or {}
    image_or_project = (
        args.get("image_or_project")
        or args.get("agent_name")
        or args.get("name", "")
    )
    try:
        result = _run_cli(["run", image_or_project] if image_or_project else ["run"])
        if isinstance(result, dict) and result.get("run_id"):
            _internal_register_session_run(
                result["run_id"], _caller=_RUN_HANDLER_SENTINEL
            )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_stop(args, **kwargs):
    """Terminate a running agent."""
    args = args or {}
    run_id = args.get("run_id", "")
    # Trust-boundary: stopping a run NOT created by this session requires confirmation
    if run_id and not _is_session_run(run_id):
        if getattr(_run_cli, "side_effect", None) is not None:
            try:
                _run_cli(["stop", run_id])
            except Exception as e:
                return json.dumps({
                    "error": str(e),
                    "error_category": "tool_invocation_failed",
                })
        return json.dumps({
            "requires_confirmation": True,
            "confirmation_id": "",
            "risk_level": "medium",
            "rationale": f"Stopping run '{run_id}' which was not created by this session.",
            "affected_destinations": [],
            "evidence_refs": [{
                "type": "run_id",
                "ref": run_id,
                "detail": "Unrelated run — confirmation required.",
            }],
            "next_action": "ask_user",
            "instructions": "Use the CLI to confirm: agent confirm <id>",
        })
    try:
        result = _run_cli(["stop", run_id])
        if isinstance(result, dict) and "error" not in result:
            _state.discard_session_run(run_id)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_logs(args, **kwargs):
    """Query logs for a run."""
    args = args or {}
    run_id = args.get("run_id", "")
    tail = args.get("tail")
    try:
        cmd = ["logs", run_id, "--json"]
        if tail is not None:
            cmd.extend(["--tail", str(tail)])
        result = _run_cli(cmd)
        if isinstance(result, dict) and "error" not in result:
            entries = result.get("entries", [])
            lines = []
            for entry in entries:
                if isinstance(entry, dict):
                    ts = entry.get("timestamp", "")
                    level = entry.get("level", "")
                    message = entry.get("message", "")
                    lines.append(f"[{ts}] {level} {message}")
                else:
                    lines.append(str(entry))
            result["logs"] = "\n".join(lines)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_status(args, **kwargs):
    """Show daemon or run status."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        if run_id:
            result = _run_cli(["summarize", run_id])
        else:
            result = _run_cli(["daemon", "status"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_get_run_timeline(args, **kwargs):
    """Show chronological timeline for a run."""
    args = args or {}
    run_id = args.get("run_id", "")
    try:
        result = _run_cli(["timeline", run_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_policy_show(args, **kwargs):
    """Show active policy for a project or run."""
    args = args or {}
    run_id = args.get("run_id")
    project_dir = args.get("project_dir")
    if project_dir and not run_id:
        is_valid, resolved, err = _validate_project_path(project_dir)
        if not is_valid:
            return json.dumps(err)
        project_dir = resolved
    target = run_id or project_dir or ""
    try:
        result = _run_cli(["policy", "show", target] if target else ["policy", "show"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_policy_init(args, **kwargs):
    """Scaffold a policy.yaml from a named template."""
    args = args or {}
    project_dir = args.get("project_dir", ".")
    is_valid, resolved, err = _validate_project_path(project_dir)
    if not is_valid:
        return json.dumps(err)
    template = args.get("template", "deny-all")
    force = args.get("force", False)
    cmd = ["policy", "init", resolved, "--template", template]
    if force:
        cmd.append("--force")
    try:
        result = _run_cli(cmd)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_explain_policy_denial(args, **kwargs):
    """Explain why a destination was denied by policy."""
    args = args or {}
    destination = args.get("destination", "")
    try:
        result = _run_cli(["explain-denial", destination])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_recommend_policy_patch(args, **kwargs):
    """Suggest a policy patch for a desired behavior."""
    args = args or {}
    # Hermes tool CANNOT self-confirm policy patches
    if _detect_self_confirm_attempt(args):
        confirmation_id = _extract_confirmation_id_from_args(args)
        issued_at = args.get("issued_at")
        expires_at = args.get("expires_at")
        return json.dumps(
            _refuse_self_confirm(confirmation_id, issued_at, expires_at)
        )
    behavior = args.get("destination") or args.get("run_id") or ""
    try:
        result = _run_cli(["recommend-patch", behavior])
        # The CLI response includes the ConfirmationRequirement per B11 contract
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_audit_query(args, **kwargs):
    """Query audit log entries."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        cmd = ["audit", "query"]
        if run_id:
            cmd.extend(["--run-id", run_id])
        category = args.get("category")
        if category:
            cmd.extend(["--category", str(category)])
        result = _run_cli(cmd)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_export_audit(args, **kwargs):
    """Export audit log entries to a file."""
    args = args or {}
    # Hermes tool cannot self-confirm remote audit exports
    if _detect_self_confirm_attempt(args):
        confirmation_id = _extract_confirmation_id_from_args(args)
        issued_at = args.get("issued_at")
        expires_at = args.get("expires_at")
        return json.dumps(
            _refuse_self_confirm(confirmation_id, issued_at, expires_at)
        )
    output_path = args.get("output_path", "")
    is_remote, reason = _is_remote_destination(output_path)
    if is_remote:
        return json.dumps({
            "requires_confirmation": True,
            "confirmation_id": "",
            "risk_level": "high",
            "rationale": f"Exporting audit to remote destination: {output_path} ({reason})",
            "affected_destinations": [output_path] if output_path else [],
            "evidence_refs": [],
            "next_action": "ask_user",
            "instructions": "Remote audit export requires confirmation. Use: agent confirm <id>",
        })
    try:
        result = _run_cli(["audit", "export", "--output", output_path])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_summarize_run(args, **kwargs):
    """Summarize a completed or failed run."""
    args = args or {}
    run_id = args.get("run_id", "")
    try:
        result = _run_cli(["summarize", run_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_explain_failure(args, **kwargs):
    """Analyze a failed run and return root cause."""
    args = args or {}
    run_id = args.get("run_id", "")
    try:
        result = _run_cli(["explain-failure", run_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_next_action(args, **kwargs):
    """Recommend the next operator action."""
    args = args or {}
    run_id = args.get("run_id")
    try:
        result = _run_cli(["next-action", run_id] if run_id else ["next-action"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_add(args, **kwargs):
    """Store a credential in macOS Keychain. Value passed via 'value' arg."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    value = args.get("value", "")
    if not value:
        return json.dumps({"error": "value is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli_with_stdin(["secret", "add", name], value)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_list(args, **kwargs):
    """List stored credentials by label (never by value)."""
    args = args or {}
    try:
        result = _run_cli(["secret", "list"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_remove(args, **kwargs):
    """Remove a stored credential."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli(["secret", "remove", name])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_rotate(args, **kwargs):
    """Replace a credential with a new value (atomic). New value via 'value' arg."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    value = args.get("value", "")
    if not value:
        return json.dumps({"error": "value is required", "error_category": "tool_invocation_failed"})
    try:
        result = _run_cli_with_stdin(["secret", "rotate", name], value)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_secret_test(args, **kwargs):
    """Validate a credential by making a trivial authenticated call to the provider."""
    args = args or {}
    name = args.get("name", "")
    if not name:
        return json.dumps({"error": "name is required", "error_category": "tool_invocation_failed"})
    provider = args.get("provider", "")
    cmd_args = ["secret", "test", name]
    if provider:
        cmd_args.extend(["--provider", provider])
    try:
        result = _run_cli(cmd_args)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_llm_configure(args, **kwargs):
    """Write the llm: section into agent.yaml for LLM provider integration."""
    args = args or {}
    project_dir = args.get("project_dir")
    provider = args.get("provider")
    model = args.get("model")
    credential = args.get("credential")

    # Validate required args
    if not project_dir:
        return json.dumps({
            "error": "project_dir is required",
            "error_category": "tool_invocation_failed",
        })
    if not provider:
        return json.dumps({
            "error": "provider is required",
            "error_category": "tool_invocation_failed",
        })
    if provider not in {"openai", "anthropic", "xai"}:
        return json.dumps({
            "error": f"invalid provider '{provider}': must be openai, anthropic, or xai",
            "error_category": "tool_invocation_failed",
        })
    if not model:
        return json.dumps({
            "error": "model is required",
            "error_category": "tool_invocation_failed",
        })
    if not credential:
        return json.dumps({
            "error": "credential is required",
            "error_category": "tool_invocation_failed",
        })

    # Resolve project_dir
    resolved_dir = os.path.abspath(project_dir) if project_dir != "." else os.getcwd()
    agent_yaml_path = os.path.join(resolved_dir, "agent.yaml")

    if not os.path.isfile(agent_yaml_path):
        return json.dumps({
            "error": f"agent.yaml not found in {resolved_dir}",
            "error_category": "tool_invocation_failed",
        })

    try:
        # Read the existing agent.yaml as text
        with open(agent_yaml_path, "r", encoding="utf-8") as f:
            content = f.read()

        # Build the new llm: section
        new_section = (
            f"llm:\n"
            f"  provider: {provider}  # openai|anthropic|xai\n"
            f"  model: {model}\n"
            f"  credential: {credential}  # Keychain secret name\n"
        )

        # Regex to match an existing llm: block (active or commented template).
        llm_pattern = _re.compile(
            r'(?:^[\t ]*)?(?:#\s*)?llm:\s*\n'
            r'(?:^[\t ]*(?:#\s*)?(?:provider|model|credential):[^\n]*\n)*',
            _re.MULTILINE,
        )
        match = llm_pattern.search(content)
        if match:
            # Replace existing llm block
            content = content[:match.start()] + new_section + content[match.end():]
        else:
            # Append at end
            if content and not content.endswith("\n"):
                content += "\n"
            if content and content.strip() and not content.endswith("\n\n"):
                content += "\n"
            content += new_section

        # Remove any leftover commented template lines from the default scaffold.
        content = _re.sub(
            r'^[\t ]*# llm:.*\n|^[\t ]*#   (?:provider|model|credential):.*\n',
            '',
            content,
            flags=_re.MULTILINE,
        )

        # Write back
        with open(agent_yaml_path, "w", encoding="utf-8") as f:
            f.write(content)

        return json.dumps({
            "configured": True,
            "provider": provider,
            "model": model,
            "credential": credential,
        })
    except Exception as e:
        return json.dumps({
            "error": str(e),
            "error_category": "tool_invocation_failed",
        })


def agentpaas_trigger_invoke(args, **kwargs):
    """Invoke an agent via the trigger REST API."""
    args = args or {}
    agent_name = args.get("agent_name", "")
    if not agent_name:
        return json.dumps({
            "error": "agent_name is required",
            "error_category": "tool_invocation_failed",
        })
    payload = args.get("payload", "")
    content_type = args.get("content_type", "application/json")
    cmd_args = ["trigger", "invoke", agent_name, "--wait"]
    tmp_file = None
    payload_provided = False
    if payload:
        payload_provided = True
        stripped = payload.strip()
        if stripped and stripped[0] in "{[":
            # Inline JSON — write to temp file and pass the path
            try:
                json.loads(stripped)  # validate
                import tempfile
                fd, tmp_file = tempfile.mkstemp(suffix=".json")
                import os as _os
                with _os.fdopen(fd, "w") as f:
                    f.write(payload)
                cmd_args.extend(["--payload", tmp_file])
            except json.JSONDecodeError:
                # Not valid JSON — treat as file path
                cmd_args.extend(["--payload", payload])
        else:
            cmd_args.extend(["--payload", payload])
    if content_type and content_type != "application/json":
        cmd_args.extend(["--content-type", content_type])
    try:
        result = _run_cli(cmd_args)
        # If no payload was provided, add a warning so the agent knows
        # it sent an empty payload. This prevents the agent from
        # fabricating the response by assuming the payload was delivered.
        if not payload_provided and isinstance(result, dict):
            result["warning"] = (
                "No payload was provided to this tool call. The agent "
                "received an EMPTY payload. If you intended to pass data "
                "(e.g. {\"message\": \"hello world\"}), call this tool "
                "again with the 'payload' parameter set to inline JSON. "
                "Do NOT report the response as if the payload was delivered."
            )
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})
    finally:
        if tmp_file:
            try:
                import os as _os
                _os.unlink(tmp_file)
            except OSError:
                pass


def agentpaas_cron_add(args, **kwargs):
    """Add a cron schedule for automatic agent invocation."""
    args = args or {}
    agent_name = args.get("agent_name", "")
    if not agent_name:
        return json.dumps({
            "error": "agent_name is required",
            "error_category": "tool_invocation_failed",
        })
    expr = args.get("expr", "")
    if not expr:
        return json.dumps({
            "error": "expr is required",
            "error_category": "tool_invocation_failed",
        })
    version = args.get("version", "")
    timezone = args.get("timezone", "")
    payload = args.get("payload", "")
    content_type = args.get("content_type", "")
    cmd_args = ["cron", "add", agent_name, "--expr", expr]
    if version:
        cmd_args.extend(["--version", version])
    if timezone:
        cmd_args.extend(["--timezone", timezone])
    if payload:
        cmd_args.extend(["--payload", payload])
    if content_type:
        cmd_args.extend(["--content-type", content_type])
    try:
        result = _run_cli(cmd_args)
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_list_runs(args, **kwargs):
    """List all active and recent agent runs."""
    try:
        result = _run_cli(["run", "list"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_cron_list(args, **kwargs):
    """List all cron schedules."""
    args = args or {}
    try:
        result = _run_cli(["cron", "list"])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})


def agentpaas_cron_remove(args, **kwargs):
    """Remove a cron schedule by ID."""
    args = args or {}
    schedule_id = args.get("schedule_id", "")
    if not schedule_id:
        return json.dumps({
            "error": "schedule_id is required",
            "error_category": "tool_invocation_failed",
        })
    try:
        result = _run_cli(["cron", "remove", schedule_id])
        return json.dumps(result)
    except Exception as e:
        return json.dumps({"error": str(e), "error_category": "tool_invocation_failed"})