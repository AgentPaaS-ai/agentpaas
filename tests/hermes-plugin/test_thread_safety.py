"""Thread-safety tests for confirmation state (14A-T04)."""

import threading
import unittest

from test_plugin_skeleton import _load_plugin_package


class ThreadSafetyTestBase(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.plugin = _load_plugin_package()
        cls.tools = cls.plugin.tools

    def setUp(self):
        self.tools._reset_confirmation_state()

    def tearDown(self):
        self.tools._reset_confirmation_state()


class TestConcurrentConfirmationReplay(ThreadSafetyTestBase):
    def test_concurrent_confirmation_replay(self):
        confirmation_id = "cf_" + "a" * 16
        results = []
        barrier = threading.Barrier(10)
        errors = []

        def try_replay():
            try:
                barrier.wait()
                result = self.tools._state.check_and_add_confirmation(confirmation_id)
                results.append(result)
            except Exception as exc:
                errors.append(exc)

        threads = [threading.Thread(target=try_replay) for _ in range(10)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        self.assertEqual(errors, [])
        successes = [r for r in results if r == (False, False)]
        replays = [r for r in results if r == (True, True)]
        self.assertEqual(len(successes), 1, f"Expected 1 success, got {len(successes)}: {results}")
        self.assertEqual(len(replays), 9, f"Expected 9 replays, got {len(replays)}: {results}")


class TestConcurrentSessionRunRegistration(ThreadSafetyTestBase):
    def test_concurrent_session_run_registration(self):
        run_ids = [f"run_{i}" for i in range(10)]
        barrier = threading.Barrier(10)
        errors = []

        def register(run_id):
            try:
                barrier.wait()
                self.tools._state.register_session_run(run_id)
            except Exception as exc:
                errors.append(exc)

        threads = [
            threading.Thread(target=register, args=(run_id,))
            for run_id in run_ids
        ]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        self.assertEqual(errors, [])
        for run_id in run_ids:
            self.assertTrue(
                self.tools._is_session_run(run_id),
                f"{run_id} should be registered",
            )


class TestConcurrentMixedOperations(ThreadSafetyTestBase):
    def test_concurrent_mixed_operations(self):
        barrier = threading.Barrier(12)
        errors = []

        def mixed_ops(thread_idx):
            try:
                barrier.wait()
                run_id = f"run_mixed_{thread_idx}"
                self.tools._state.register_session_run(run_id)
                self.tools._is_session_run(run_id)
                if thread_idx % 2 == 0:
                    self.tools._state.discard_session_run(run_id)
                self.tools._state.check_and_add_confirmation(f"cf_{thread_idx:016x}")
            except Exception as exc:
                errors.append(exc)

        threads = [
            threading.Thread(target=mixed_ops, args=(i,))
            for i in range(12)
        ]
        for t in threads:
            t.start()
        for t in threads:
            t.join(timeout=10)

        self.assertEqual(errors, [])
        for t in threads:
            self.assertFalse(t.is_alive(), "Thread deadlock detected")


class TestCheckAndAddAtomicity(ThreadSafetyTestBase):
    def test_check_and_add_atomicity(self):
        confirmation_id = "cf_" + "b" * 16
        results = []
        barrier = threading.Barrier(20)
        errors = []

        def try_add():
            try:
                barrier.wait()
                results.append(
                    self.tools._state.check_and_add_confirmation(confirmation_id)
                )
            except Exception as exc:
                errors.append(exc)

        threads = [threading.Thread(target=try_add) for _ in range(20)]
        for t in threads:
            t.start()
        for t in threads:
            t.join()

        self.assertEqual(errors, [])
        successes = [r for r in results if r == (False, False)]
        self.assertEqual(len(successes), 1)
        with self.tools._state._lock:
            self.assertIn(confirmation_id, self.tools._state._used_confirmation_ids)
        is_replay, should_refuse = self.tools._state.check_and_add_confirmation(
            confirmation_id
        )
        self.assertEqual((is_replay, should_refuse), (True, True))


class TestResetIsThreadSafe(ThreadSafetyTestBase):
    def test_reset_is_thread_safe(self):
        errors = []
        stop = threading.Event()

        def register_runs():
            try:
                i = 0
                while not stop.is_set():
                    self.tools._state.register_session_run(f"run_reset_{i}")
                    i += 1
            except Exception as exc:
                errors.append(exc)

        def reset_state():
            try:
                for _ in range(50):
                    self.tools._state.reset()
            except Exception as exc:
                errors.append(exc)

        register_thread = threading.Thread(target=register_runs)
        reset_thread = threading.Thread(target=reset_state)
        register_thread.start()
        reset_thread.start()
        reset_thread.join(timeout=5)
        stop.set()
        register_thread.join(timeout=5)

        self.assertEqual(errors, [])
        self.assertFalse(register_thread.is_alive())
        self.assertFalse(reset_thread.is_alive())


class TestDiscardIsThreadSafe(ThreadSafetyTestBase):
    def test_discard_is_thread_safe(self):
        run_id = "run_discard_test"
        self.tools._state.register_session_run(run_id)
        errors = []
        stop = threading.Event()
        barrier = threading.Barrier(2)

        def check_run():
            try:
                barrier.wait()
                while not stop.is_set():
                    self.tools._is_session_run(run_id)
            except Exception as exc:
                errors.append(exc)

        def discard_run():
            try:
                barrier.wait()
                for _ in range(100):
                    self.tools._state.discard_session_run(run_id)
            except Exception as exc:
                errors.append(exc)

        check_thread = threading.Thread(target=check_run)
        discard_thread = threading.Thread(target=discard_run)
        check_thread.start()
        discard_thread.start()
        discard_thread.join(timeout=5)
        stop.set()
        check_thread.join(timeout=5)

        self.assertEqual(errors, [])
        self.assertFalse(check_thread.is_alive())
        self.assertFalse(discard_thread.is_alive())


class TestExistingApiUnchanged(ThreadSafetyTestBase):
    def test_existing_api_unchanged(self):
        sentinel = self.tools._RUN_HANDLER_SENTINEL

        with self.assertRaises(RuntimeError):
            self.tools._internal_register_session_run("run_direct")

        self.tools._internal_register_session_run("run_api", _caller=sentinel)
        self.assertTrue(self.tools._is_session_run("run_api"))
        self.assertFalse(self.tools._is_session_run("run_other"))

        is_replay, should_refuse = self.tools._check_confirmation_replay("")
        self.assertEqual((is_replay, should_refuse), (False, True))

        confirmation_id = "cf_abcdef123456"
        is_replay, should_refuse = self.tools._check_confirmation_replay(confirmation_id)
        self.assertEqual((is_replay, should_refuse), (False, False))

        is_replay, should_refuse = self.tools._check_confirmation_replay(confirmation_id)
        self.assertEqual((is_replay, should_refuse), (True, True))

        self.tools._reset_confirmation_state()
        self.assertFalse(self.tools._is_session_run("run_api"))
        is_replay, should_refuse = self.tools._check_confirmation_replay(confirmation_id)
        self.assertEqual((is_replay, should_refuse), (False, False))


if __name__ == "__main__":
    unittest.main()