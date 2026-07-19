"""B27-T01: Progress contract tests — SDK-level validation, forwarding, and error mapping."""

import unittest

from agentpaas_sdk import (
    Agent,
    ArtifactRejected,
    BudgetExceeded,
    CheckpointRejected,
    LeaseExpired,
    ProgressError,
    RPCError,
)


class FakeRPC:
    def __init__(self, result=None, error=None):
        self.calls = []
        self.result = result or {}
        self.error = error

    def call(self, method, params):
        self.calls.append((method, params))
        if self.error is not None:
            raise self.error
        return self.result


class ProgressForwardingTests(unittest.TestCase):
    """Full argument forwarding and keyword-only enforcement."""

    def test_minimal_heartbeat_forwards_correctly(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress("starting")
        method, params = rpc.calls[0]
        self.assertEqual(method, "progress")
        self.assertEqual(params["phase"], "starting")
        self.assertEqual(params["completed_work"], [])
        self.assertEqual(params["remaining_work"], [])
        self.assertEqual(params["artifact_references"], [])
        self.assertFalse(params["safe_to_resume"])
        self.assertIn("event_id", params)
        self.assertNotIn("last_committed_action", params)

    def test_full_checkpoint_forwards_all_fields(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress(
            "phase1",
            completed_work=["did A", "did B"],
            remaining_work=["do C"],
            artifact_references=["out.json", "charts/plot.json"],
            last_committed_action="wrote output",
            safe_to_resume=True,
        )
        _, params = rpc.calls[0]
        self.assertEqual(params["phase"], "phase1")
        self.assertEqual(params["completed_work"], ["did A", "did B"])
        self.assertEqual(params["remaining_work"], ["do C"])
        self.assertEqual(params["artifact_references"], ["out.json", "charts/plot.json"])
        self.assertEqual(params["last_committed_action"], "wrote output")
        self.assertTrue(params["safe_to_resume"])

    def test_phase_is_positional_keyword_only_others(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        # phase can be positional
        sdk.progress("p")
        # but other args must be keyword-only
        with self.assertRaises(TypeError):
            sdk.progress("p", "should_be_keyword")  # type: ignore[misc]

    def test_none_collections_become_empty_lists(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress("p")
        _, params = rpc.calls[0]
        self.assertEqual(params["completed_work"], [])
        self.assertEqual(params["remaining_work"], [])
        self.assertEqual(params["artifact_references"], [])

    def test_does_not_share_mutable_defaults(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress("p1", completed_work=["a"])
        sdk.progress("p2")
        _, params2 = rpc.calls[1]
        self.assertEqual(params2["completed_work"], [])

    def test_progress_event_id_uniqueness(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress("p1")
        sdk.progress("p2")
        _, p1 = rpc.calls[0]
        _, p2 = rpc.calls[1]
        self.assertNotEqual(p1["event_id"], p2["event_id"])
        self.assertEqual(len(p1["event_id"]), 32)


class ProgressValidationTests(unittest.TestCase):
    """Bounds, types, and negative cases."""

    def test_empty_phase_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress("")
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")

    def test_non_string_phase_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress(123)  # type: ignore[arg-type]

    def test_phase_too_long_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("x" * 129)

    def test_phase_exactly_128_ok(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress("x" * 128)
        self.assertEqual(len(rpc.calls), 1)

    def test_completed_work_too_many_entries(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("p", completed_work=["x"] * 51)

    def test_completed_work_entry_too_long(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("p", completed_work=["x" * 1025])

    def test_completed_work_non_string_entry(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("p", completed_work=[123])  # type: ignore[list-item]

    def test_remaining_work_too_many_entries(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("p", remaining_work=["x"] * 51)

    def test_artifact_references_too_many(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ArtifactRejected):
            sdk.progress("p", artifact_references=["f.json"] * 33)

    def test_artifact_reference_empty_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ArtifactRejected):
            sdk.progress("p", artifact_references=[""])

    def test_artifact_reference_absolute_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ArtifactRejected):
            sdk.progress("p", artifact_references=["/etc/passwd"])

    def test_artifact_reference_traversal_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ArtifactRejected):
            sdk.progress("p", artifact_references=["../secret"])

    def test_artifact_reference_backslash_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ArtifactRejected):
            sdk.progress("p", artifact_references=["foo\\bar"])

    def test_artifact_reference_too_many_segments(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ArtifactRejected):
            sdk.progress("p", artifact_references=["a/b/c/d/e/f/g/i/j"])

    def test_artifact_reference_valid_nested(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        sdk.progress("p", artifact_references=["charts/themes.json"])
        self.assertEqual(len(rpc.calls), 1)

    def test_last_committed_action_too_long(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("p", last_committed_action="x" * 1025)

    def test_phase_control_chars_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress("ev\x00il")
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")

    def test_last_committed_action_control_chars_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress("p", last_committed_action="bad\x01action")
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")

    def test_completed_work_control_chars_rejected(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress("p", completed_work=["bad\x02entry"])
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")


class SafeToResumeTests(unittest.TestCase):
    """safe_to_resume=True validation."""

    def test_safe_to_resume_requires_committed_action(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress("p", completed_work=["x"], safe_to_resume=True)
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")

    def test_safe_to_resume_requires_completed_work(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError):
            sdk.progress("p", last_committed_action="did", safe_to_resume=True)

    def test_safe_to_resume_rejects_empty_string_completed_work(self):
        """Spec: 'at least one non-empty completed_work entry'"""
        sdk = Agent()
        sdk.set_rpc(FakeRPC())
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress(
                "p",
                completed_work=[""],  # empty string, not non-empty
                last_committed_action="committed",
                safe_to_resume=True,
            )
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")

    def test_safe_to_resume_true_with_all_fields_ok(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True, "checkpoint_id": "cp1"})
        sdk.set_rpc(rpc)
        result = sdk.progress(
            "phase1",
            completed_work=["did it"],
            last_committed_action="committed",
            safe_to_resume=True,
        )
        self.assertTrue(result["recorded"])
        self.assertEqual(result["checkpoint_id"], "cp1")

    def test_safe_to_resume_false_ignores_missing_fields(self):
        sdk = Agent()
        rpc = FakeRPC({"recorded": True})
        sdk.set_rpc(rpc)
        # safe_to_resume=False is fine even with no completed_work/action
        sdk.progress("p")
        self.assertEqual(len(rpc.calls), 1)


class ProgressErrorMappingTests(unittest.TestCase):
    """Typed RPC error codes map to correct exceptions."""

    def test_invalid_progress_error_mapped(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC(error=ProgressError("bad", "INVALID_PROGRESS")))
        with self.assertRaises(ProgressError) as ctx:
            sdk.progress("p")
        self.assertEqual(ctx.exception.code, "INVALID_PROGRESS")

    def test_checkpoint_rejected_error_mapped(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC(error=CheckpointRejected("nope", "CHECKPOINT_REJECTED")))
        with self.assertRaises(CheckpointRejected) as ctx:
            sdk.progress(
                "p", completed_work=["x"], last_committed_action="a", safe_to_resume=True,
            )
        self.assertEqual(ctx.exception.code, "CHECKPOINT_REJECTED")

    def test_artifact_rejected_error_mapped(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC(error=ArtifactRejected("bad path", "ARTIFACT_REJECTED")))
        with self.assertRaises(ArtifactRejected) as ctx:
            sdk.progress("p", artifact_references=["ok.json"])
        self.assertEqual(ctx.exception.code, "ARTIFACT_REJECTED")

    def test_lease_expired_error_mapped(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC(error=LeaseExpired("expired", "LEASE_EXPIRED")))
        with self.assertRaises(LeaseExpired) as ctx:
            sdk.progress("p")
        self.assertEqual(ctx.exception.code, "LEASE_EXPIRED")

    def test_budget_exceeded_still_raised(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC(error=BudgetExceeded("over", "BUDGET_EXCEEDED")))
        with self.assertRaises(BudgetExceeded):
            sdk.progress("p")

    def test_generic_rpc_error_falls_through(self):
        sdk = Agent()
        sdk.set_rpc(FakeRPC(error=RPCError("boom", "INTERNAL")))
        with self.assertRaises(RPCError) as ctx:
            sdk.progress("p")
        self.assertEqual(ctx.exception.code, "INTERNAL")


class ResumeCheckpointResponseTests(unittest.TestCase):
    """Response shape includes expected fields."""

    def test_initial_attempt_no_resume_checkpoint(self):
        sdk = Agent()
        rpc = FakeRPC({
            "recorded": True,
            "run_id": "r1",
            "attempt_id": "a1",
            "checkpoint_id": None,
            "lease_expires_at": "2026-07-18T00:00:00Z",
        })
        sdk.set_rpc(rpc)
        result = sdk.progress("starting")
        self.assertTrue(result["recorded"])
        self.assertEqual(result["run_id"], "r1")
        self.assertEqual(result["attempt_id"], "a1")
        self.assertIsNone(result["checkpoint_id"])
        self.assertNotIn("resume_checkpoint", result)

    def test_resumed_attempt_returns_resume_checkpoint(self):
        sdk = Agent()
        rpc = FakeRPC({
            "recorded": True,
            "run_id": "r1",
            "attempt_id": "a2",
            "checkpoint_id": "cp_new",
            "lease_expires_at": "2026-07-18T01:00:00Z",
            "resume_checkpoint": {
                "checkpoint_id": "cp1",
                "phase": "themes_complete",
                "completed_work": ["theme analysis"],
            },
            "resume_reason": "failure_continuation",
        })
        sdk.set_rpc(rpc)
        result = sdk.progress("starting")
        self.assertIn("resume_checkpoint", result)
        self.assertEqual(result["resume_checkpoint"]["checkpoint_id"], "cp1")
        self.assertEqual(result["resume_reason"], "failure_continuation")


class ExistingSDKSuiteUnchangedTests(unittest.TestCase):
    """Progress additions must not break existing SDK behavior."""

    def test_llm_still_works(self):
        sdk = Agent()
        rpc = FakeRPC({"text": "ok"})
        sdk.set_rpc(rpc)
        result = sdk.llm("hello")
        self.assertEqual(result["text"], "ok")

    def test_http_still_works(self):
        sdk = Agent()
        rpc = FakeRPC({"status": 200, "body": "ok"})
        sdk.set_rpc(rpc)
        result = sdk.http("GET", "https://example.test")
        self.assertEqual(result["status"], 200)

    def test_on_invoke_still_works(self):
        sdk = Agent()

        @sdk.on_invoke
        def handle(payload):
            return {"ok": True}

        self.assertEqual(sdk.invoke({}), {"ok": True})

    def test_progress_without_rpc_raises_rpc_error(self):
        sdk = Agent()
        with self.assertRaises(RPCError):
            sdk.progress("p")


if __name__ == "__main__":
    unittest.main()
