# Manual Testing Guide

How AgentPaaS is tested, which suites exist, how to run them, and how to add
or debug tests. This is the **developer** testing map for the Go codebase —
not the product walkthrough (see [quickstart.md](quickstart.md) for that).

For the **release** end-to-end operator loop (clean install → build → share →
receive), see [execution/reference/e2e-test-plan.md](execution/reference/e2e-test-plan.md).

## Testing Philosophy

AgentPaaS security claims are only as strong as the tests that try to break
them. We test for three reasons:

1. **Correctness** — CLI, daemon, pack, policy, and runtime behave as
   documented for legitimate operators.
2. **Containment** — a compromised or hostile agent cannot escape the
   isolation boundary (network topology, credential brokering, non-root
   container, audit integrity).
3. **Regression** — once a bypass is fixed, an automated test keeps it fixed
   forever (`// ADVERSARY BREAK:` comments mark permanent regression pins).

Principles:

- Prefer **real pipeline** paths (pack → run → gateway → audit) over mocks
  when claiming security properties. Red-team and Docker e2e suites use the
  real stack.
- Unit tests stay fast and hermetic; they use `t.TempDir()`, fake stores, and
  never pop macOS Keychain GUI dialogs (`FakeKeyStore`, not
  `KeychainKeyStore`).
- Failures must be **actionable**: assert exact error classes, digests, deny
  reasons, or audit event types — not “something failed.”
- `make test` is the default commit gate. Heavier suites (Docker, redteam,
  golden-docker) run on explicit targets or CI lanes.

What we deliberately do **not** treat as a pass:

- Agent self-report (“I blocked exfil”) without harness/audit evidence
- Green unit tests alone as proof of network isolation
- Dev binaries (`0.1.0-dev`) as a substitute for the release cask in
  operator golden loops

## Test Categories

| Category | Where | What it proves |
|---|---|---|
| **Unit** | `internal/*/*_test.go` next to production code | Package contracts, parsers, validators, pure logic |
| **Integration** | Package tests that spin local daemons/sockets, plus `make e2e-network` under `internal/runtime` | Cross-package wiring; Docker topology when `AGENTPAAS_DOCKER_TESTS=1` |
| **Golden** | `test/golden/` (`TestGoldenSuite`) | Dataset of graded tasks with pass^k stability; tiers: fast / slow / docker |
| **Compat** | `test/compat/v0.2.3/` | v0.2.3 fixtures still parse and behave (policy, agent.yaml, lock, CLI shapes) |
| **Adversary** | `*_adversary*_test.go`, `adversary_*_test.go` across `internal/` and some `test/redteam/` | Negative tests that try to **break** security claims (symlink escape, PID reuse, injection, audit tamper, …) |
| **Redteam** | `test/redteam/` (`TestRedteamSmoke` + fixtures) | Six attack fixtures through the **real** pack → runtime → broker → operator path |

### Unit

Standard Go tests. Examples: policy parse, home layout, lock canonicalization,
CLI flag wiring. Run under `go test ./...` / `make test`.

### Integration

- Many packages start an in-process or subprocess daemon against a temp
  `AGENTPAAS_HOME`.
- Docker-backed network isolation lives behind `AGENTPAAS_DOCKER_TESTS=1`
  (see `make e2e-network`). Without that env var, Docker cases skip.

### Golden

Entry: `TestGoldenSuite` in `test/golden/golden_test.go`.

- Dataset: `test/golden/golden_tasks.yaml`
- Skips unless `GOLDEN_TIER` is set (so plain `go test ./...` does not run the
  full suite by accident)
- `GOLDEN_K` defaults to 3 (pass^k — every repetition must succeed)
- Makefile wrappers build binaries as needed (`golden-fast` runs `build-all`)

### Compat

Pinned fixtures under `test/compat/v0.2.3/fixtures/` (openrouter,
direct-provider, no-llm, full-policy, bundle). Tests ensure strict YAML parse
and behavioral compatibility with the v0.2.3 public surface.

```bash
go test ./test/compat/v0.2.3/... -count=1 -race
```

### Adversary

Named `TestAdversary...`. Intent: if the product is secure, the attack fails
and the test **passes**. If the product is vulnerable, the test **fails** with
a message starting with `ADVERSARY BREAK:`.

Common vectors covered across the tree:

- Symlink attacks on home, project paths, and parent components
- Path traversal / null-byte / empty / overlong paths
- PID and flock inode reuse races
- Config/unit-file injection (newlines, homoglyphs)
- Missing command timeouts
- Network bind gaps
- Redaction gaps
- Crypto/identity mismatches (SPIFFE, TTL, CA)
- Audit chain tamper (middle, reorder, truncation vs anchor, forgery)

Some packages require `-tags=adversary` (see block gates in the Makefile).

### Redteam

`make redteam-smoke` / `TestRedteamSmoke` runs fixtures such as:

- Network exfiltration
- Credential misuse
- Secret invisibility
- Host resource access
- Resource containment
- Operator injection

No synthetic enforcement shortcuts: real `pack.BuildImage`,
`runtime.DockerRuntime`, secrets broker/gateway, and operator handlers.
Target runtime is under ~10 minutes on a developer laptop with Docker.

## Running Tests

From the repo root. Requires **Go 1.26.5** (see `go.mod`). Docker suites need
a working daemon (Colima or Docker Desktop).

### Everyday (commit gate)

```bash
make test          # go test ./...
make lint          # golangci-lint run --timeout 5m
make race          # go test -race ./...
make build         # go build ./...
make build-all     # CLI + daemon + host harness + linux harness
```

Expected success:

```text
ok  	github.com/AgentPaaS-ai/agentpaas/internal/...
...
```

### Unit / package focus

```bash
go test ./internal/home/ -count=1 -v
go test ./internal/cli/ -count=1 -race -run 'TestInit'
go test ./internal/policy/ -count=1
```

### Adversary

```bash
# Typical adversary files (no extra build tag)
go test ./internal/home/ -count=1 -race -run 'Adversary' -v
go test ./internal/audit/ -count=1 -race -run 'Adversary' -v

# Packages that gate adversary tests behind a build tag (see Makefile block*-gate)
go test -tags=adversary -race -count=1 ./internal/pack/...
go test -tags=adversary -race -count=1 ./internal/trigger/...
go test -tags=adversary -race -count=1 ./internal/dashboard/... ./internal/otel/...
```

### Integration / Docker network

```bash
# Full B5 network + adversary matrix (long)
make e2e-network

# Equivalent pattern for a single class
AGENTPAAS_DOCKER_TESTS=1 go test -v -count=1 \
  -run 'TestE2E_Network_PositivePath' \
  ./internal/runtime/... -timeout 120s
```

If Docker is down, tests should skip or fail fast with a clear daemon error —
start Colima first: `colima start`.

### Golden dataset

```bash
make golden-fast      # every commit; timeout 120s
make golden-slow      # PRs; timeout 600s
make golden-docker    # main merge; needs AGENTPAAS_DOCKER_TESTS; timeout 1800s
make golden-eval      # all tiers

# Manual equivalent
GOLDEN_TIER=fast GOLDEN_K=3 go test -v -run TestGoldenSuite -count=1 \
  ./test/golden/ -timeout 120s
```

Without `GOLDEN_TIER`, the suite logs a skip:

```text
golden suite: set GOLDEN_TIER=fast|slow|docker|all to run (use make golden-fast etc.)
```

### Compat

```bash
go test ./test/compat/... -count=1 -race
go test ./test/compat/v0.2.3/... -count=1 -race
```

### Redteam smoke

```bash
make redteam-smoke
# expands to roughly:
# AGENTPAAS_DOCKER_TESTS=1 DOCKER_HOST="unix://$HOME/.colima/default/docker.sock" \
#   go test ./test/redteam/... -v -timeout 15m
```

Adjust `DOCKER_HOST` if your Docker socket is not Colima’s default.

### Block gates (CI-style aggregates)

The Makefile defines `blockN-gate` targets that combine build, test, race,
lint, osv, and package-specific adversary runs. Examples:

```bash
make block1-gate      # proto + build + test + lint
make block5-gate      # includes e2e-network
make block12-gate     # includes redteam-smoke
make block30-gate     # includes compat v0.2.3 + B30 adversary
```

Use the gate named in the block you are changing; do not invent partial
substitutes when the gate is the documented merge bar.

### Dependency vulnerability scan

```bash
make osv              # osv-scanner scan -r .
```

## Writing New Tests

### Conventions

1. **File names**
   - Normal: `foo_test.go` in the same package (or `foo_package_test.go` for
     external black-box tests).
   - Adversary: `adversary_tNN_test.go`, `adversary_bN_tNN_test.go`, or
     `*_adversary_test.go` in the package under attack.
2. **Test names** — `TestAdversary...` for negative security tests;
   `TestE2E_...` for Docker integration; keep `Test` + descriptive camel case.
3. **Comments** — every failing adversary assertion that encodes a break must
   include:

   ```go
   // ADVERSARY BREAK: <one-line description of the bypass>
   t.Fatal("ADVERSARY BREAK: ...")
   ```

   Do not delete or weaken adversary tests to go green. Fix production code
   (or file a defect) instead.
4. **Secrets** — use `FakeKeyStore` / in-memory fakes. Never call APIs that
   open the macOS Keychain prompt in automated tests.
5. **Cleanup** — `defer func() { _ = x.Close() }()` for all `Close()`-able
   resources; always check errors you care about (no silent `_ =` on security
   paths under test).
6. **Concurrency** — run sensitive suites with `-race`. Prefer
   `t.Parallel()` only when the test does not share process-global daemon
   sockets or fixed ports.
7. **Docker** — guard with env checks (`AGENTPAAS_DOCKER_TESTS=1`) and short
   skips when the daemon is unavailable so `make test` stays laptop-friendly.
8. **Time** — prefer fake clocks or short timeouts; production code under test
   should accept `context.Context` (hangs without deadlines are themselves
   defects).
9. **Fixtures** — put stable YAML/JSON under `testdata/` or
   `test/compat/v0.2.3/fixtures/`. Prefer real golden bytes over hand-wavy
   string contains when the surface is user-visible.

### Patterns that work

```go
func TestAdversarySymlinkHomeDir(t *testing.T) {
    target := t.TempDir()
    link := filepath.Join(t.TempDir(), ".agentpaas")
    if err := os.Symlink(target, link); err != nil {
        t.Fatal(err)
    }
    err := Ensure(link) // production API
    if err == nil {
        t.Fatal("ADVERSARY BREAK: Ensure accepted symlinked home directory")
    }
}
```

```go
func TestPackRejectsWildcardWithoutFlag(t *testing.T) {
    // arrange project with domain: '*'
    // act: CLI pack without --allow-wildcard
    // assert: error contains refusal text
}
```

### What to avoid

- Asserting only that `err != nil` without locking the **security reason**
- Sleep-based synchronization (`time.Sleep`) instead of conditions/channels
- Writing exploits or live attack payloads against non-local systems
- Reading or committing real API keys, `.env` files, or Keychain exports
- Skipping adversary tests with `t.Skip` to hide a known break (open a defect
  with severity instead)
- Depending on ambient `~/.agentpaas` — always inject a temp `--home` /
  `AGENTPAAS_HOME`
- Listing `agentpaas-sdk` in agent `requirements.txt` inside fixtures (SDK is
  pack-injected)
- Using deprecated APIs the linter already forbids

### Adding a golden task

1. Edit `test/golden/golden_tasks.yaml` with id, tier, grader, and threshold.
2. Implement or reuse a grader in `test/golden/graders.go` /
   `docker_graders.go`.
3. Run `make golden-fast` (or the tier you touched) with `GOLDEN_K=3`.

### Adding a redteam fixture

1. Implement the `Fixture` interface in `test/redteam/`.
2. Register it in `TestRedteamSmoke`.
3. Assert blocked/contained/refused **and** the expected audit signal.
4. Run `make redteam-smoke` with Docker up.

## Debugging Test Failures

### General workflow

```bash
# 1. Reproduce a single test verbosely
go test ./internal/home/ -count=1 -race -run 'TestAdversarySymlinkHomeDir' -v

# 2. See which packages failed in the full suite
go test ./... -count=1 2>&1 | rg 'FAIL|--- FAIL'

# 3. Confirm it is not flake
go test ./pkg/path -count=20 -run 'TestName'
```

### Common patterns and gotchas

| Symptom | Likely cause | What to check |
|---|---|---|
| `ADVERSARY BREAK: ...` | Security regression or intentional tighter test | Read the break comment; fix production code; do not delete the test |
| Hang / timeout in `go test` | `exec.Command` without context; Docker wait; deadlock | `-timeout 30s`; look for missing `CommandContext`; `-race` for lock cycles |
| Docker tests skip unexpectedly | `AGENTPAAS_DOCKER_TESTS` unset | Export `AGENTPAAS_DOCKER_TESTS=1`; verify `docker info` |
| Docker dial errors | Wrong `DOCKER_HOST` / Colima stopped | `colima status`; `echo $DOCKER_HOST`; default Colima socket `unix://$HOME/.colima/default/docker.sock` |
| Keychain / GUI password prompt during tests | Real `KeychainKeyStore` used | Switch to `FakeKeyStore`; never hit `security` in unit tests |
| Port already in use | Fixed trigger/dashboard port collision | Parallel package tests fighting `127.0.0.1:7717` (or similar); serialize or bind ephemeral ports in test helpers |
| `golden suite: set GOLDEN_TIER=...` | Ran golden package without Makefile env | Use `make golden-fast` or export `GOLDEN_TIER` |
| Pack / validate cannot connect | Daemon not running for CLI integration tests that expect it | Start test daemon against temp home, or use pure package APIs |
| Failures only with `-race` | Data race | Fix the race; do not drop `-race` from the gate |
| Compat fixture parse fail | Strict YAML / schema drift | Diff against `test/compat/v0.2.3/fixtures/`; intentional breaks need a new compat version dir, not silent edits |
| Redteam “not blocked” | Topology/policy regression | Inspect containment table in test log; `~/.agentpaas` pollution — use isolated home; confirm iptables/topology docs in [how-enforcement-works.md](how-enforcement-works.md) |
| Flaky adversary TOCTOU | Timing window too tight/loose | Prefer deterministic injection points over sleeps; re-run with `-count=50` |
| `exit 137` running pack tools | macOS quarantine on brew binaries | `xattr -cr` on `agentpaas`, `agentpaasd`, harness (operator env only) |

### Reading failure output

Adversary failures are intentional signal:

```text
--- FAIL: TestAdversarySymlinkHomeDir (0.01s)
    adversary_test.go:54: ADVERSARY BREAK: Ensure accepted symlinked home directory
```

Treat that as a **HIGH** severity product bug until proven otherwise.

Redteam smoke prints a containment table and writes a JSON report (path logged
by the test, often under the system temp dir as
`agentpaas-redteam-report.json`).

### Isolation checklist when “it fails on my machine”

1. `go version` → must be **go1.26.5** (module line in `go.mod`).
2. `docker info` succeeds.
3. No leftover `AGENTPAAS_HOME` pointing at a polluted tree.
4. Clean module cache only if checksum errors: `go clean -modcache` (last resort).
5. Re-run with `-count=1` to disable test cache.
6. For CLI binary tests, rebuild: `make build-all`.

### When to escalate

- Reproducible `ADVERSARY BREAK` on main → security defect; do not merge
  “fixes” that only skip the test.
- Redteam fixture flipped from BLOCKED to ALLOWED → stop release; see
  [threat-model.md](threat-model.md) and [how-enforcement-works.md](how-enforcement-works.md).
- Compat break without version policy decision → coordinate before changing
  fixtures.

## Quick reference

```bash
make test                 # unit + package tests
make race                 # full tree with -race
make lint                 # golangci-lint
make e2e-network          # Docker topology + runtime adversary
make golden-fast          # golden tier: fast
make redteam-smoke        # 6-fixture real-pipeline redteam
go test ./test/compat/... # v0.2.3 compatibility
go test ./internal/home/ -race -run Adversary -count=1 -v
```

Operator install and first agent: [quickstart.md](quickstart.md).
Release manual loop: [execution/reference/e2e-test-plan.md](execution/reference/e2e-test-plan.md).
