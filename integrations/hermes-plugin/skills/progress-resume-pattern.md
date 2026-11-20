# Agent Progress and Resume Pattern (v0.3 — Internal)

> **Status:** Internal v0.3 contract. Not user-facing until B36 enables the
> user flow. Do not teach operators this pattern yet.

## When to Use

Use `agent.progress(...)` when your worker:

- Performs multiple phases of work (parse, analyze, generate, verify).
- Writes artifacts that should survive between attempts.
- Needs a safe resume point after a committed phase.
- Runs long enough that crash recovery matters.

Agents that do a single LLM call and return do NOT need progress calls.

## The Pattern

```python
from agentpaas_sdk import agent

@agent.on_invoke
def handle_invoke(payload):
    # 1. Check for resume checkpoint (initial call returns None).
    resume = agent.progress(phase="starting").get("resume_checkpoint")
    if resume:
        # Restore explicit state from the checkpoint.
        # Do NOT re-do committed work.
        completed = set(resume.get("completed_work", []))
    else:
        completed = set()

    # 2. Phase 1: Parse
    if "parse" not in completed:
        # ... do parsing work ...
        agent.progress(
            phase="parse_complete",
            completed_work=["parse"],
            remaining_work=["analyze", "generate"],
            last_committed_action="parsed input data",
            safe_to_resume=True,
        )

    # 3. Phase 2: Analyze
    if "analyze" not in completed:
        # ... do analysis ...
        agent.progress(
            phase="analyze_complete",
            completed_work=["parse", "analyze"],
            remaining_work=["generate"],
            artifact_references=["analysis.json"],
            last_committed_action="wrote analysis.json",
            safe_to_resume=True,
        )

    # 4. Phase 3: Generate
    # ... generate final output ...
    return {"status": "OK", "result": "..."}
```

## Rules

1. **Every call is a heartbeat.** A heartbeat with `safe_to_resume=False`
   never becomes a claimed safe resume point.

2. **`safe_to_resume=True` requires:**
   - Non-empty `last_committed_action`
   - At least one non-empty `completed_work` entry
   - The worker has actually committed the side effects described

3. **Never include:**
   - Secrets or API keys in any field
   - Raw artifact contents (use `artifact_references` with relative paths)
   - Hidden reasoning or provider response fragments

4. **`resume_checkpoint` is explicit input**, not hidden memory. If present,
   restore from it. If absent, start fresh. Never assume the runtime
   remembers your state.

5. **`resume_reason`** is set by the runtime, never by worker code. It is
   either `failure_continuation` or `operator_pause_resume`.

## Artifact References

Write artifacts to `$AGENTPAAS_ARTIFACT_DIR` (mounted at
`/workspace/artifacts`). Reference them by relative path:

```python
import os

artifact_dir = os.environ.get("AGENTPAAS_ARTIFACT_DIR", "/workspace/artifacts")
path = os.path.join(artifact_dir, "themes.json")
with open(path, "w") as f:
    f.write(json.dumps(themes))

agent.progress(
    phase="themes_complete",
    completed_work=["theme analysis"],
    artifact_references=["themes.json"],
    last_committed_action="wrote themes.json",
    safe_to_resume=True,
)
```

Artifact path rules:
- POSIX `/` separators only (no backslashes)
- At most 512 characters, 8 path segments
- Each segment: `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`
- No absolute paths, traversal (`..`), symlinks, or hard links
- Max 25 MiB per file, 100 MiB total per run

## Bounds

| Field | Limit |
|-------|-------|
| `phase` | 1–128 UTF-8 chars, no control chars |
| `completed_work` | Max 50 entries, 1024 chars each |
| `remaining_work` | Max 50 entries, 1024 chars each |
| `artifact_references` | Max 32 entries |
| `last_committed_action` | Max 1024 chars |
| Serialized checkpoint | Max 64 KiB |

## What This Does NOT Do

- `agent.progress(...)` does not mark the task complete. Returning from
  `@agent.on_invoke` is still successful completion.
- AgentPaaS does not infer semantic correctness from a checkpoint.
  `safe_to_resume` is a worker assertion, not proof of correctness.
- This does not implement automatic model failover (B28) or routed
  continuation (B35).
