# OWA Record Template (Per Subtask)

Written by the Orchestrator after each subtask is merged. This is the
durable audit trail. Without it, the subtask has no evidence of
verification.

```markdown
# OWA Record: <BLOCK>-<SUBTASK>

## Subtask
- **Title**: <SUBTASK_TITLE>
- **Scope**: <ONE_SENTENCE_SCOPE>
- **Branch**: <BRANCH_NAME>
- **Merge Commit**: <SHA>
- **Date**: <YYYY-MM-DD>

## Worker
- **Model**: <WORKER_MODEL>
- **Status**: done | blocked | partial
- **Files Changed**: <LIST>
- **Tests Written**: <N>
- **Tests Passing**: <N>
- **Acceptance Criteria**:
  - [ ] <criterion 1>: met | not_met
  - [ ] <criterion 2>: met | not_met
- **Worker Notes**: <any context from the worker>

## Gate
- **Build**: PASS | FAIL
- **Test**: PASS | FAIL (<N> tests)
- **Race**: PASS | FAIL
- **Lint**: PASS | FAIL
- **Security Scan**: PASS | FAIL
- **Fix Cycles**: <N> (if >0, list what was fixed and why)
- **Cache Cleared**: yes (MANDATORY — false greens from stale cache
  are a known pitfall)

## Adversary
- **Model**: <ADVERSARY_MODEL>
- **Breaks Found**: <N>
  - **HIGH**: <N> — <list each with one-line description>
  - **MEDIUM**: <N> — <list each>
  - **LOW**: <N> — <list each>
- **Confirmed-Safe Tests**: <N>
- **All Breaks Resolved**: yes | no
  - If yes: <list each break + fix commit SHA + verification>
  - If no: BLOCKED — do not merge (but this record is for a merged
    subtask, so this should never be "no")

## Verifier
Deferred to block-end. See: docs/owa-records/<block>-block-end.md

## Orchestrator Decision
- **Verdict**: ACCEPT | REJECT
- **Rationale**: <one sentence: gate green + adversary breaks resolved
  + acceptance met>
- **Merged At**: <timestamp>
- **Merged By**: <orchestrator model>

## Cost
- **Worker Dispatches**: <N>
- **Adversary Dispatches**: <N>
- **Fix Workers Dispatched**: <N>
- **Estimated Token Cost**: $<estimate> (if tracked)

## Post-Build Audit
- [ ] Fix commits on remote (not just local)
- [ ] CI green on remote main
- [ ] E2E tests pass on merged code
- [ ] Security suppressions verified
- [ ] CI workflow runs current block gate
```

## Block-End Verifier Record Template

Written after the block-end Verifier passes. Per-subtask records
reference this file.

```markdown
# Block-End Verifier Report: Block <BLOCK_NUMBER>

## Verifier
- **Model**: <VERIFIER_MODEL>
- **Session**: fresh (no prior context)
- **Date**: <YYYY-MM-DD>

## Scope
- **Subtasks Verified**: <N> (<list: T01, T02, ...>)
- **Merged Commit**: <SHA on local main>

## Criteria Results
### Criterion 1: Full Block Gate
- build: PASS
- test: PASS (<N> tests)
- race: PASS
- lint: PASS
- secscan: PASS (<N> suppressions, all verified)
- RESULT: PASS

### Criterion 2: Adversary Tests on Merged Main
- tests run: <N>
- tests passed: <N>
- RESULT: PASS | FAIL

### Criterion 3: Cross-Subtask Integration
- review: <summary>
- findings: <list or "none">
- RESULT: PASS | FAIL

### Criterion 4: Security Suppression Review
- suppressions: <N>
- verified: <list>
- RESULT: PASS | FAIL

## Verdict
VERIFY PASS | VERIFY FAIL

## Fix History (if any)
- <finding ID>: <description> → fix commit <SHA> → re-verified PASS
```
