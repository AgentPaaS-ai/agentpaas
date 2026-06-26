# Adversary Review: 14A-T04 — Thread-safe confirmation state

You are a security adversary. Your job is to BREAK the thread-safe state implementation.

## Target code

File: `integrations/hermes-plugin/tools.py`
Class: `_ConfirmationState` (around line 22-60)
Wiring: `_state` global instance, used by `_internal_register_session_run`,
`_is_session_run`, `_check_confirmation_replay`, `_reset_confirmation_state`, `agentpaas_stop`

The implementation uses `threading.Lock` to wrap all set operations.

## Attack vectors to try

1. **Lock contention DoS:** Many threads spamming check_and_add — does the lock cause
   unacceptable contention or deadlock?
2. **Re-entrant lock:** Does any function call another function that also acquires the lock?
   (e.g., does _check_confirmation_replay call _is_session_run?) If so, a regular Lock
   would deadlock. Check for this.
3. **Race between reset and check:** Thread A calls reset() while Thread B is mid-check.
   Could a confirmation ID be "un-replayed" by reset being called at the right moment?
4. **Memory leak:** Are confirmation IDs ever cleaned up? The set grows unboundedly.
5. **Lock not held for entire operation:** Is there any window between check and add
   where another thread can insert?
6. **Exception during lock hold:** If an exception occurs while the lock is held, is it
   released? (Python's `with` statement handles this, but check for raw acquire/release)
7. **TOCTOU in agentpaas_stop:** The function checks `_state.is_session_run(run_id)` then
   later calls `_state.discard_session_run(run_id)`. Is there a race where another thread
   can register the same run_id between check and discard?

## Instructions

1. Read the target code in `integrations/hermes-plugin/tools.py`
2. Read the tests in `integrations/hermes-plugin/tests/test_thread_safety.py`
3. For each attack vector, analyze whether the code is vulnerable
4. If you find a real vulnerability, write a proof-of-concept test
5. Report findings with severity
