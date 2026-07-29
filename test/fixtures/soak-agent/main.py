"""Fake-LLM multi-turn agent for B30 operator soak tests.

When AGENTPAAS_TEST_FAKE_LLM=1 is set, this agent runs a synthetic multi-turn
workload that emits progress and checkpoints through the AgentPaaS harness RPC.
It does NOT call any real LLM — every turn is a simulated operation.

Environment:
  AGENTPAAS_TEST_FAKE_LLM=1   — enable fake-LLM mode (required)
  AGENTPAAS_FAKE_LLM_TURNS=N  — number of turns (default: 100, short: 10)
  AGENTPAAS_SOAK_SHORT=1      — short mode, fewer turns (10)
"""
import os
import sys
import time

# The AgentPaaS SDK is available at /app/python/agentpaas_sdk/ inside the
# harness container. The harness adds it to PYTHONPATH.
from agentpaas_sdk import agent


def main():
    if os.environ.get("AGENTPAAS_TEST_FAKE_LLM") != "1":
        print("AGENTPAAS_TEST_FAKE_LLM=1 is required for soak agent", file=sys.stderr)
        sys.exit(1)

    short_mode = os.environ.get("AGENTPAAS_SOAK_SHORT") == "1"
    turns = int(os.environ.get("AGENTPAAS_FAKE_LLM_TURNS", "10" if short_mode else "100"))
    checkpoint_interval = 5 if short_mode else 10

    agent_inst = agent.Agent()

    @agent_inst.on_invoke
    def handle_invoke(payload):
        results = []
        for turn in range(1, turns + 1):
            phase = f"turn-{turn}"
            safe_to_resume = (turn % checkpoint_interval == 0)

            last_action = f"completed-turn-{turn}"

            resp = agent_inst.progress(
                phase=phase,
                completed_work=[f"turns-1-to-{turn}"],
                remaining_work=[f"turns-{turn+1}-to-{turns}"] if turn < turns else [],
                last_committed_action=last_action,
                safe_to_resume=safe_to_resume,
            )
            results.append({"turn": turn, "recorded": resp.get("recorded", False)})

            # Simulate work per turn
            time.sleep(0.05 if short_mode else 0.1)

        return {
            "status": "completed",
            "turns_completed": turns,
            "results": results,
        }

    return agent_inst.invoke({})


if __name__ == "__main__":
    result = main()
    print(result)