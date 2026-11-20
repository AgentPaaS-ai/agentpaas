"""B27-T06: Reference worker tests — verifies the progress contract is practical."""

import json
import os
import tempfile
import unittest
from unittest.mock import patch, MagicMock

from agentpaas_sdk import agent
from agentpaas_sdk.agent import Agent
from agentpaas_sdk.tests import reference_worker


class FakeProgressRPC:
    """Fake RPC that simulates the harness progress responses."""

    def __init__(self):
        self.calls = []
        self.call_count = 0
        self.resume_checkpoint = None

    def call(self, method, params):
        self.calls.append((method, params))
        if method == "progress":
            self.call_count += 1
            resp = {
                "recorded": True,
                "run_id": "r1",
                "attempt_id": "a1",
                "checkpoint_id": None,
                "lease_expires_at": "2026-07-18T01:00:00Z",
            }
            if params.get("safe_to_resume"):
                resp["checkpoint_id"] = f"cp-a1-{self.call_count}"
            if self.resume_checkpoint and self.call_count == 1:
                resp["resume_checkpoint"] = self.resume_checkpoint
                resp["resume_reason"] = "failure_continuation"
            return resp
        return {}


class ReferenceWorkerTests(unittest.TestCase):
    def setUp(self):
        # Save and clear the global agent's state.
        self._orig_rpc = agent._rpc
        self._orig_handler = agent._invoke_handler

    def tearDown(self):
        agent._rpc = self._orig_rpc
        agent._invoke_handler = self._orig_handler

    def test_completes_from_scratch(self):
        """Reference worker completes all 3 phases from scratch."""
        rpc = FakeProgressRPC()
        agent.set_rpc(rpc)

        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ["AGENTPAAS_ARTIFACT_DIR"] = tmpdir
            result = reference_worker.handle_invoke({"fixture": "test"})

        self.assertEqual(result["status"], "OK")
        self.assertEqual(result["phase"], "complete")
        self.assertIn("parsed", result["results"])
        self.assertIn("transformed", result["results"])

        # Should have 4 progress calls: starting + 3 phases.
        progress_calls = [c for c in rpc.calls if c[0] == "progress"]
        self.assertEqual(len(progress_calls), 4)

        # First call is a heartbeat (starting).
        _, p0 = progress_calls[0]
        self.assertEqual(p0["phase"], "starting")
        self.assertFalse(p0["safe_to_resume"])

        # Phases 1-3 are safe checkpoints.
        for i in range(1, 4):
            _, p = progress_calls[i]
            self.assertTrue(p["safe_to_resume"])
            self.assertTrue(p["last_committed_action"])

    def test_writes_artifact(self):
        """Reference worker writes results.json artifact."""
        rpc = FakeProgressRPC()
        agent.set_rpc(rpc)

        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ["AGENTPAAS_ARTIFACT_DIR"] = tmpdir
            result = reference_worker.handle_invoke({"fixture": "test"})

            artifact_path = os.path.join(tmpdir, "results.json")
            self.assertTrue(os.path.exists(artifact_path))
            with open(artifact_path) as f:
                data = json.load(f)
            self.assertIn("results", data)

    def test_resume_skips_completed_work(self):
        """Simulated second invocation skips committed work."""
        rpc = FakeProgressRPC()
        # Set resume checkpoint with all phases completed.
        rpc.resume_checkpoint = {
            "checkpoint_id": "cp-a1-3",
            "phase": "output_complete",
            "completed_work": ["parse", "transform", "output"],
        }
        agent.set_rpc(rpc)

        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ["AGENTPAAS_ARTIFACT_DIR"] = tmpdir
            result = reference_worker.handle_invoke({"fixture": "test"})

        self.assertEqual(result["status"], "OK")
        # Should only have 1 progress call (starting), not 4.
        progress_calls = [c for c in rpc.calls if c[0] == "progress"]
        self.assertEqual(len(progress_calls), 1)

    def test_partial_resume_continues(self):
        """Resume with only 'parse' completed — should skip parse and continue."""
        rpc = FakeProgressRPC()
        rpc.resume_checkpoint = {
            "checkpoint_id": "cp-a1-1",
            "phase": "parse_complete",
            "completed_work": ["parse"],
        }
        agent.set_rpc(rpc)

        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ["AGENTPAAS_ARTIFACT_DIR"] = tmpdir
            result = reference_worker.handle_invoke({"fixture": "test"})

        self.assertEqual(result["status"], "OK")
        # Should have 3 progress calls: starting + transform + output.
        progress_calls = [c for c in rpc.calls if c[0] == "progress"]
        self.assertEqual(len(progress_calls), 3)

    def test_no_resume_checkpoint(self):
        """Worker handles no resume checkpoint gracefully."""
        rpc = FakeProgressRPC()
        # No resume_checkpoint set — initial attempt.
        agent.set_rpc(rpc)

        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ["AGENTPAAS_ARTIFACT_DIR"] = tmpdir
            result = reference_worker.handle_invoke({})

        self.assertEqual(result["status"], "OK")

    def test_uses_only_approved_sdk_method(self):
        """Generated fixture uses only agent.progress(), no second API."""
        rpc = FakeProgressRPC()
        agent.set_rpc(rpc)

        with tempfile.TemporaryDirectory() as tmpdir:
            os.environ["AGENTPAAS_ARTIFACT_DIR"] = tmpdir
            reference_worker.handle_invoke({})

        # All calls should be to "progress" method only.
        methods = set(c[0] for c in rpc.calls)
        self.assertEqual(methods, {"progress"})


if __name__ == "__main__":
    unittest.main()
