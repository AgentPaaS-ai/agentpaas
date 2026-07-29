"""Fake-LLM multi-turn agent for B30 operator soak tests.

When run inside an AgentPaaS harness container, this agent runs a synthetic
multi-turn workload that emits progress and checkpoints through the AgentPaaS
harness RPC. It does NOT call any real LLM — every turn is a simulated operation.

Configuration (read from invoke payload first, then environment):
  payload.turns       — number of turns (default: 100, or 10 in short mode)
  payload.sleep_ms    — sleep per turn in ms (default: 100, or 50 in short mode)
  AGENTPAAS_SOAK_SHORT=1 — short mode (fewer turns, shorter sleeps)
"""
import os
import time

from agentpaas_sdk import agent

short_mode = os.environ.get("AGENTPAAS_SOAK_SHORT") == "1"
_default_turns = 10 if short_mode else 100
_default_sleep_ms = 50 if short_mode else 100
_checkpoint_interval = 5 if short_mode else 10


@agent.on_invoke
def handle_invoke(payload):
    """Synthetic multi-turn soak workload."""
    turns = _default_turns
    sleep_ms = _default_sleep_ms
    if isinstance(payload, dict):
        if "turns" in payload:
            turns = int(payload["turns"])
        if "sleep_ms" in payload:
            sleep_ms = int(payload["sleep_ms"])

    results = []
    for turn in range(1, turns + 1):
        phase = f"turn-{turn}"
        safe_to_resume = (turn % _checkpoint_interval == 0)
        last_action = f"completed-turn-{turn}"

        resp = agent.progress(
            phase=phase,
            completed_work=[f"turns-1-to-{turn}"],
            remaining_work=[f"turns-{turn+1}-to-{turns}"] if turn < turns else [],
            last_committed_action=last_action,
            safe_to_resume=safe_to_resume,
        )
        results.append({"turn": turn, "recorded": resp.get("recorded", False)})

        # Simulate work per turn
        time.sleep(sleep_ms / 1000.0)

    return {
        "status": "completed",
        "turns_completed": turns,
        "results": results,
    }
