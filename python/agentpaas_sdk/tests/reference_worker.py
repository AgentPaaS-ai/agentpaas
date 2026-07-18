"""B27-T06: Reference worker — demonstrates the agent.progress() API.

This worker is deterministic (no model calls) and proves the progress
contract is practical for a general phased worker. It:

1. Reads a local fixture.
2. Executes three phases.
3. Writes one artifact.
4. Calls agent.progress() after each committed phase.
5. Restores from a supplied resume checkpoint when present.
6. Returns a final structured result.

Run it as an AgentPaaS agent or directly for testing:

    python -m agentpaas_sdk.tests.reference_worker
"""

from __future__ import annotations

import json
import os
from typing import Any

from agentpaas_sdk import agent


def _restore_explicit_state(resume: dict[str, Any]) -> list[str]:
    """Restore completed work from a resume checkpoint.

    Returns the list of already-completed phases so the worker can skip them.
    """
    completed = resume.get("completed_work", [])
    # The worker uses phase names as completed markers.
    return [c.lower() for c in completed]


def handle_invoke(payload: dict[str, Any]) -> dict[str, Any]:
    """Reference worker entry point."""
    # Read fixture from payload or default.
    fixture = payload.get("fixture", "default")

    # Check for resume checkpoint from initial progress call.
    initial = agent.progress(phase="starting")
    resume = initial.get("resume_checkpoint")

    completed_phases: list[str] = []
    if resume:
        completed_phases = _restore_explicit_state(resume)

    results: dict[str, Any] = {}

    # --- Phase 1: Parse ---
    if "parse" not in completed_phases:
        # Simulate parsing a fixture.
        data = {"fixture": fixture, "items": ["a", "b", "c"]}
        results["parsed"] = data
        agent.progress(
            phase="parse_complete",
            completed_work=["parse"],
            remaining_work=["transform", "output"],
            last_committed_action="parsed fixture data",
            safe_to_resume=True,
        )
    else:
        results["parsed"] = {"restored": True}

    # --- Phase 2: Transform ---
    if "transform" not in completed_phases:
        items = results.get("parsed", {}).get("items", [])
        transformed = [item.upper() for item in items]
        results["transformed"] = transformed
        agent.progress(
            phase="transform_complete",
            completed_work=["parse", "transform"],
            remaining_work=["output"],
            last_committed_action="transformed items",
            safe_to_resume=True,
        )
    else:
        results["transformed"] = {"restored": True}

    # --- Phase 3: Write artifact + output ---
    if "output" not in completed_phases:
        artifact_dir = os.environ.get("AGENTPAAS_ARTIFACT_DIR", "/tmp")
        artifact_path = os.path.join(artifact_dir, "results.json")
        output_data = {"results": results.get("transformed", [])}
        os.makedirs(artifact_dir, exist_ok=True)
        with open(artifact_path, "w") as f:
            json.dump(output_data, f)

        agent.progress(
            phase="output_complete",
            completed_work=["parse", "transform", "output"],
            remaining_work=[],
            artifact_references=["results.json"],
            last_committed_action="wrote results.json",
            safe_to_resume=True,
        )
    else:
        results["output"] = {"restored": True}

    return {
        "status": "OK",
        "phase": "complete",
        "results": results,
    }


# Register the handler.
agent.on_invoke(handle_invoke)


if __name__ == "__main__":
    # Direct execution for testing without the harness.
    result = handle_invoke({"fixture": "test"})
    print(json.dumps(result, indent=2))
