# Task: 14A-T04 — Thread-safe confirmation state (GAP-5, MEDIUM)

## Context

`integrations/hermes-plugin/tools.py` uses module-level sets that are NOT thread-safe:

```python
_session_runs = set()
_used_confirmation_ids = set()
```

If Hermes runs tool calls concurrently (delegate_task spawns subagents), concurrent
access to these sets can cause race conditions: a confirmation ID could be checked-and-added
non-atomically, allowing replay.

The functions that access these sets:
- `_internal_register_session_run(run_id, _caller)` — adds to `_session_runs`
- `_is_session_run(run_id)` — reads `_session_runs`
- `_check_confirmation_replay(confirmation_id)` — reads + writes `_used_confirmation_ids`
- `_reset_confirmation_state()` — clears both sets
- `agentpaas_stop` — reads `_session_runs` and discards from it
- `agentpaas_run` — calls `_internal_register_session_run`

## What to implement

### 1. Create a thread-safe state class

Replace the module-level sets with a class wrapping `threading.Lock`:

```python
import threading

class _ConfirmationState:
    """Thread-safe state for session runs and confirmation tracking."""
    
    def __init__(self):
        self._lock = threading.Lock()
        self._session_runs = set()
        self._used_confirmation_ids = set()
    
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
            self._used_confirmation_ids.add(confirmation_id)
            return False, False
    
    def reset(self):
        """Thread-safe clear all state. For testing and session restart."""
        with self._lock:
            self._session_runs.clear()
            self._used_confirmation_ids.clear()
```

### 2. Replace module-level sets

Replace:
```python
_session_runs = set()
_used_confirmation_ids = set()
```

With:
```python
_state = _ConfirmationState()
```

### 3. Update all functions that use the old sets

- `_internal_register_session_run`: use `_state.register_session_run(run_id)`
- `_is_session_run`: use `_state.is_session_run(run_id)`
- `_check_confirmation_replay`: use `_state.check_and_add_confirmation(confirmation_id)`
- `_reset_confirmation_state`: use `_state.reset()`
- In `agentpaas_stop`: replace `_session_runs.discard(run_id)` with `_state.discard_session_run(run_id)`

### 4. Keep the `_RUN_HANDLER_SENTINEL` pattern

`_internal_register_session_run` still checks `_caller is not _RUN_HANDLER_SENTINEL`
before calling `_state.register_session_run`. The sentinel guard stays — only the
set access becomes thread-safe.

## Tests to write

Create `integrations/hermes-plugin/tests/test_thread_safety.py`:

1. **test_concurrent_confirmation_replay:** Spawn 10 threads, each tries to
   replay the same confirmation_id. Exactly 1 succeeds (returns False, False),
   9 are rejected (returns True, True). Use `threading.Barrier` to ensure all
   threads start simultaneously.

2. **test_concurrent_session_run_registration:** Spawn 10 threads, each
   registers a different run_id. All 10 should be registered.

3. **test_concurrent_mixed_operations:** Multiple threads doing
   register/is_session_run/discard simultaneously. No exceptions, no deadlocks.

4. **test_check_and_add_atomicity:** Verify that between the check and the add,
   no other thread can insert the same ID. Use a mock to add a delay inside
   the check-and-add if needed (but the Lock should make this unnecessary).

5. **test_reset_is_thread_safe:** Call reset() from one thread while another
   thread is registering runs. No crash.

6. **test_discard_is_thread_safe:** Discard from one thread while another
   is checking. No crash.

7. **test_existing_api_unchanged:** Verify that _is_session_run, _check_confirmation_replay,
   _reset_confirmation_state, and _internal_register_session_run all still work
   as before from a single-threaded perspective.

## Example concurrent test pattern

```python
import threading
import unittest

class TestConcurrentConfirmationReplay(unittest.TestCase):
    def test_exactly_one_succeeds(self):
        plugin = _load_plugin_package()
        plugin.tools._reset_confirmation_state()
        confirmation_id = "cf_" + "a" * 16
        results = []
        barrier = threading.Barrier(10)
        
        def try_replay():
            barrier.wait()  # all threads start simultaneously
            is_replay, should_refuse = plugin.tools._state.check_and_add_confirmation(confirmation_id)
            results.append((is_replay, should_refuse))
        
        threads = [threading.Thread(target=try_replay) for _ in range(10)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()
        
        # Exactly 1 thread got (False, False) — the first to acquire the lock
        successes = [r for r in results if r == (False, False)]
        replays = [r for r in results if r == (True, True)]
        self.assertEqual(len(successes), 1, f"Expected 1 success, got {len(successes)}: {results}")
        self.assertEqual(len(replays), 9, f"Expected 9 replays, got {len(replays)}: {results}")
```

## Testing

```bash
cd /Users/pms88/projects/agentpaas
python3 -m unittest discover -s integrations/hermes-plugin/tests -t integrations/hermes-plugin -v
```

All 145 existing tests + 7 new tests must pass (152 total).

Run the thread safety tests multiple times to catch flaky races:
```bash
for i in 1 2 3 4 5; do
    python3 -m unittest tests.test_thread_safety -v 2>&1 | tail -3
done
```

## Commit message

```
feat(14a-t04): thread-safe confirmation state (GAP-5)

Replace module-level sets (_session_runs, _used_confirmation_ids) with
a _ConfirmationState class wrapping threading.Lock. check-and-add for
replay protection is now atomic. Prevents race conditions when Hermes
runs tool calls concurrently via delegate_task subagents.

7 new tests in test_thread_safety.py including concurrent replay test
(10 threads, exactly 1 succeeds).
```

## Branch

Create branch `feat/b14a-t04` from main.
