# Founder walkthrough fixes — OSS — 2026-08-06

Scope: batch D from `/tmp/founder-bugs-2026-08-06.md`. OSS only; no cloud repository changes.

## Implemented

- Local and cloud walkthrough guidance now has an explicit single-invoke invariant. Status/result checks are reads; a second invoke is only allowed after the user asks another city/question.
- `agentpaas cloud login` prints the approve URL and same-browser instruction without opening a browser by default. `--open-browser` is opt-in. Hermes cloud-login schema/tool copy matches.
- Registry Authentication error 10000 coaching says `whoami` success means the session is OK, identifies the platform registry credential failure, recommends retrying push, and says to contact support if persistent; it does not recommend a cloud-login loop.
- `cloud whoami` now displays Tenant, Tier, Agent limit, and CPU minutes used/limit rather than leading with concurrency.
- Cold weather-demo provider guidance is limited to OpenRouter/OpenAI/Anthropic API-key providers, OpenRouter first, with two cheap DeepSeek-class OpenRouter model examples and `openrouter-key`. Nous token-exchange and xAI OAuth are excluded from that picker.

## Commits

- `e8910967891f79eba9400eaad6c864b6c1385d2f` — `fix: align founder walkthrough cloud UX`

## Verification

- `go test ./internal/cli ./internal/cloudclient -count=1` — PASS.
- `go build -o /tmp/agentpaas ./cmd/agentpaas` — PASS.
- `PYTHONPATH=integrations/hermes-plugin/tests uv run --with pytest --with pyyaml pytest --import-mode=importlib integrations/hermes-plugin/tests/test_cloud_tools.py -q` — PASS, 8 tests.
- `git diff --check` — PASS.
- The default pytest import mode collects `integrations/hermes-plugin/__init__.py` as a top-level module and fails on its pre-existing relative import; `--import-mode=importlib` is required for this plugin layout.

## Residuals

- The OSS client supports the new whoami fields when returned by the cloud API; no private cloud repository was modified.
- The pre-existing unrelated modification to `docs/owa-records/OPEN-BUGS-post-m7.5.md` remains untouched and uncommitted.
