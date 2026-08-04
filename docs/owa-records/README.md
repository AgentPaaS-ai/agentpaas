# OWA records (internal)

This directory holds **local orchestrator build history** (block notes, bug
ledgers, gate logs). It is **not** part of the customer documentation set.

## Public docs (go here instead)

| Need | Path |
|------|------|
| Cloud golden path | `docs/customer/golden-path.md` |
| Hermes cloud E2E | `docs/customer/golden-loop-hermes-e2e.md` |
| Honest product gaps | `docs/known-limitations.md` |
| Control API reference | `docs/api-reference-control.md` |

## M10 public-visibility audit (2026-08-03)

**Decision:** all historical files under `docs/owa-records/` are **internal**.
None are required for a stranger trial. Customer-facing content already lives
under `docs/customer/` and `docs/known-limitations.md`.

**Action:** track only this README in git. Everything else stays on disk for
builders but is gitignored (see root `.gitignore`).

| Class | Pattern | Disposition |
|-------|---------|-------------|
| keep-public | `README.md` (this file) | tracked |
| internal | `BUG-*`, `bNN-*`, `m*`, `OPEN-BUGS*`, `PROMPT-*`, `_e2e-*`, `nuclear-*`, `v0.3.*`, `golden-path-cloud-*` | untracked + gitignore |
| already-ignored | `docs/owa-records/archive/` | unchanged |

Do not re-add internal ledgers to the public OSS tree. If a note must be
customer-visible, rewrite a clean version under `docs/customer/` without
tenant IDs, pool sizes, admin verbs, or founder runbooks.
