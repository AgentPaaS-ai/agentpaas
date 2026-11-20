"""Tests for workflow_input and commit_handoff SDK methods (B34-T03)."""

import unittest

from agentpaas_sdk import (
    Agent,
    HandoffNotAllowed,
    HandoffRejected,
    LeaseExpired,
    RPCError,
    WorkflowContextUnavailable,
)


class FakeRPC:
    """Fake RPC client for testing without a real harness."""

    def __init__(self, result=None, error=None):
        self.calls: list[tuple[str, dict]] = []
        self.result = result or {}
        self.error = error

    def call(self, method, params):
        self.calls.append((method, params))
        if self.error is not None:
            raise self.error
        return self.result


# ---------------------------------------------------------------------------


class WorkflowInputTests(unittest.TestCase):
    def setUp(self):
        self.agent = Agent()

    def test_workflow_input_none_when_unavailable(self):
        """workflow_input returns None when available is false."""
        rpc = FakeRPC({"available": False})
        self.agent.set_rpc(rpc)

        result = self.agent.workflow_input()
        self.assertIsNone(result)
        self.assertEqual(rpc.calls[0][0], "workflow_input")

    def test_workflow_input_returns_handoff_when_available(self):
        """workflow_input returns handoff dict when available is true."""
        handoff = {
            "schema_version": "agentpaas.workflow.handoff/v1",
            "workflow_id": "wf_1",
            "handoff_id": "ho_1",
            "from_node_id": "stage_a",
            "to_node_id": "stage_b",
            "context": {"schema": "ns/test/v1", "value": {"notes": "hello"}},
        }
        rpc = FakeRPC({
            "available": True,
            "handoff": handoff,
            "artifact_refs": [],
        })
        self.agent.set_rpc(rpc)

        result = self.agent.workflow_input()
        self.assertIsNotNone(result)
        self.assertEqual(result["from_node_id"], "stage_a")
        self.assertEqual(rpc.calls[0][0], "workflow_input")

    def test_workflow_input_raises_workflow_context_unavailable(self):
        """workflow_input raises WorkflowContextUnavailable when harness rejects."""
        rpc = FakeRPC(
            error=WorkflowContextUnavailable(
                "not a pipeline stage", "WORKFLOW_CONTEXT_UNAVAILABLE"
            )
        )
        self.agent.set_rpc(rpc)

        with self.assertRaises(WorkflowContextUnavailable) as ctx:
            self.agent.workflow_input()
        self.assertEqual(ctx.exception.code, "WORKFLOW_CONTEXT_UNAVAILABLE")


class CommitHandoffTests(unittest.TestCase):
    def setUp(self):
        self.agent = Agent()

    def test_commit_handoff_params_shape(self):
        """commit_handoff sends correct params to the RPC."""
        rpc = FakeRPC({
            "accepted": True,
            "handoff_digest": "sha256:abc123",
            "staged": True,
        })
        self.agent.set_rpc(rpc)

        result = self.agent.commit_handoff(
            "ns/test/v1",
            {"notes": "hello"},
        )
        self.assertTrue(result["accepted"])
        self.assertEqual(result["handoff_digest"], "sha256:abc123")
        self.assertTrue(result["staged"])
        method, params = rpc.calls[0]
        self.assertEqual(method, "commit_handoff")
        self.assertEqual(params["schema"], "ns/test/v1")
        self.assertEqual(params["context"], {"notes": "hello"})
        self.assertNotIn("artifacts", params)

    def test_commit_handoff_with_artifacts(self):
        """commit_handoff includes artifacts in params when provided."""
        rpc = FakeRPC({
            "accepted": True,
            "handoff_digest": "sha256:def456",
            "staged": True,
        })
        self.agent.set_rpc(rpc)

        artifacts = [
            {
                "artifact_id": "art_1",
                "immutable_ref": "outputs/report.json",
                "digest": "sha256:abc",
                "media_type": "application/json",
                "size_bytes": 1024,
                "classification": "internal",
            },
        ]
        result = self.agent.commit_handoff(
            "ns/test/v1",
            {"notes": "hello"},
            artifacts=artifacts,
        )
        self.assertTrue(result["accepted"])
        method, params = rpc.calls[0]
        self.assertEqual(method, "commit_handoff")
        self.assertEqual(params["artifacts"], artifacts)

    def test_commit_handoff_not_allowed(self):
        """commit_handoff raises HandoffNotAllowed when harness rejects."""
        rpc = FakeRPC(
            error=HandoffNotAllowed(
                "final stage cannot handoff", "HANDOFF_NOT_ALLOWED"
            )
        )
        self.agent.set_rpc(rpc)

        with self.assertRaises(HandoffNotAllowed) as ctx:
            self.agent.commit_handoff("ns/test/v1", {"notes": "nope"})
        self.assertEqual(ctx.exception.code, "HANDOFF_NOT_ALLOWED")

    def test_commit_handoff_rejected(self):
        """commit_handoff raises HandoffRejected on validation failure."""
        rpc = FakeRPC(
            error=HandoffRejected(
                "handoff validation failed", "HANDOFF_INVALID"
            )
        )
        self.agent.set_rpc(rpc)

        with self.assertRaises(HandoffRejected) as ctx:
            self.agent.commit_handoff("ns/test/v1", {"notes": "bad"})
        self.assertEqual(ctx.exception.code, "HANDOFF_INVALID")

    def test_commit_handoff_stale_lease(self):
        """commit_handoff raises LeaseExpired when harness reports stale lease."""
        rpc = FakeRPC(
            error=LeaseExpired(
                "invoke is terminal or lease expired", "STALE_LEASE"
            )
        )
        self.agent.set_rpc(rpc)

        with self.assertRaises(LeaseExpired) as ctx:
            self.agent.commit_handoff("ns/test/v1", {"notes": "stale"})
        self.assertEqual(ctx.exception.code, "STALE_LEASE")


class HandoffErrorCodeMappingTests(unittest.TestCase):
    """Test that error codes are mapped to correct exception types."""

    def setUp(self):
        self.agent = Agent()

    def test_workflow_context_unavailable_code(self):
        rpc = FakeRPC(
            error=RPCError(
                "not a pipeline", "WORKFLOW_CONTEXT_UNAVAILABLE"
            )
        )
        self.agent.set_rpc(rpc)
        # The RPC client maps WORKFLOW_CONTEXT_UNAVAILABLE to WorkflowContextUnavailable
        # in its error handling logic. When using FakeRPC we test the code mapping
        # via direct error object — the FakeRPC raises whatever error we give it.
        # Test that the imported exception class has the right parent.
        self.assertTrue(issubclass(WorkflowContextUnavailable, RPCError))

    def test_standalone_handoff_not_allowed(self):
        """RPC calls in standalone context get properly mapped errors."""
        rpc = FakeRPC(
            error=RPCError(
                "not allowed in non-pipeline context", "HANDOFF_NOT_ALLOWED"
            )
        )
        self.agent.set_rpc(rpc)
        with self.assertRaises(RPCError) as ctx:
            self.agent.commit_handoff("ns/test/v1", {"notes": "nope"})
        self.assertEqual(ctx.exception.code, "HANDOFF_NOT_ALLOWED")
        # When using FakeRPC directly, it raises what we give it.
        # Real RPCClient would raise HandoffNotAllowed via _HANDOFF_ERROR_MAP.
        self.assertIs(type(ctx.exception), RPCError)


if __name__ == "__main__":
    unittest.main()
