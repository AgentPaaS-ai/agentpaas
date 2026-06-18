# PR Template — OWA Loop Fields

## Model Routing
```yaml
orchestrator: <model>
worker: <model>
verifier: <model>
adversary: <model or null>
```

## Attempt Log Summary
```yaml
attempts:
  - attempt: 1
    role: worker
    model: <id>
    result: pass|fail
    commit: <sha>
  - attempt: 1
    role: verifier
    model: <id>
    result: pass|fail
    defects: <count>
  - attempt: 1
    role: orchestrator
    model: <id>
    result: accepted|refine|reject
```

## Gate Result
- Command: `make blockN-gate`
- Result: PASS / FAIL
- Evidence: <output>

## Adversary Status
- Invoked: true / false
- Reason: <if not invoked>
- Result: pass / break_found (if invoked)

## Orchestrator Decision
- Verdict: ACCEPT / REFINE / REJECT
- Evidence: <summary>

## Founder Gate
- Approved: [ ] Yes / [ ] No
- Date: <date>