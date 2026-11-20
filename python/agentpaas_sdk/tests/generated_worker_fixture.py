"""B27-T06: Generated worker fixture template.

This fixture demonstrates the safe pattern for a generated AgentPaaS worker
that uses the progress API. Copy this as a starting point for new agents.

Key rules:
- Call agent.progress(phase="starting") first to check for resume_checkpoint.
- Only set safe_to_resume=True after side effects are committed.
- Never include secrets or raw artifact contents in progress calls.
- Treat resume_checkpoint as explicit input, not hidden memory.
- Do not add a second checkpoint API — agent.progress() is the only one.
"""

from __future__ import annotations

import json
import os
from typing import Any

from agentpaas_sdk import agent


def handle_invoke(payload: dict[str, Any]) -> dict[str, Any]:
    """Generated worker entry point."""

    # Step 1: Check for resume checkpoint.
    initial = agent.progress(phase="starting")
    resume = initial.get("resume_checkpoint")
    if resume:
        # Restore explicit state — do NOT replay completed work.
        _restore_state(resume)

    # Step 2: Do work in phases, committing after each.
    # Example: process data and write an artifact.
    data = payload.get("data", {})

    # Phase 1: Process.
    processed = _process(data)
    agent.progress(
        phase="processing_complete",
        completed_work=["processing"],
        remaining_work=["generate_report"],
        last_committed_action="processed input data",
        safe_to_resume=True,
    )

    # Phase 2: Write artifact.
    artifact_dir = os.environ.get("AGENTPAAS_ARTIFACT_DIR", "/tmp")
    artifact_path = os.path.join(artifact_dir, "report.json")
    os.makedirs(artifact_dir, exist_ok=True)
    with open(artifact_path, "w") as f:
        json.dump(processed, f)

    agent.progress(
        phase="report_complete",
        completed_work=["processing", "generate_report"],
        remaining_work=[],
        artifact_references=["report.json"],
        last_committed_action="wrote report.json",
        safe_to_resume=True,
    )

    return {"status": "OK", "result": processed}


def _process(data: dict[str, Any]) -> dict[str, Any]:
    """Process input data."""
    return {"processed": True, "items": len(data)}


def _restore_state(resume: dict[str, Any]) -> None:
    """Restore explicit state from a resume checkpoint.

    Only restore fields that are safe to re-apply. The checkpoint
    is a trusted hint from the daemon, not a replay instruction.
    """
    # The worker decides what to restore based on completed_work.
    # Example: if "processing" is in completed_work, skip processing.
    pass


# Register the handler.
agent.on_invoke(handle_invoke)
