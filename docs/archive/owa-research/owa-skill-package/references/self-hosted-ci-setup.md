# Self-Hosted CI Setup

If you want CI as a safety net (not as the primary gate — the local gate
is primary in local-first mode), run it on your own machine via a
self-hosted runner.

## Why Self-Hosted

| Factor | Hosted CI (ubuntu-latest) | Self-Hosted |
|---|---|---|
| Billed minutes | metered (10x multiplier on macOS!) | 0 |
| Speed | baseline | 3-29x faster per job |
| Per-job install | ~3 min of each job | 0 (tools pre-installed) |
| Hardware match | generic VM | your actual dev/prod hardware |

The macOS billing multiplier is the hidden killer: GitHub Actions macOS
runners bill at 10x the Linux rate. In our measured run, this exhausted
a 3,000-minute monthly quota in ~290 actual minutes of compute — about
5 hours of real work consumed an entire month's allowance. Self-hosting
eliminated this entirely.

## Setup (GitHub Actions self-hosted runner)

1. On your dev machine, install the runner:
   ```bash
   # Go to: GitHub Repo → Settings → Actions → Runners → New self-hosted runner
   # Follow the instructions for your OS.
   ```

2. Pre-install all build tools so no per-job install step is needed:
   - Language toolchain (Go, Rust, Node, etc.)
   - Linter (golangci-lint, eslint, clippy, etc.)
   - Security scanner (osv-scanner, npm audit, cargo audit, etc.)
   - Container runtime (Docker, if you have containerized tests)
   - Proto/buf tools (if using gRPC)

3. In your workflow files, use `runs-on: self-hosted`:
   ```yaml
   jobs:
     block-gate:
       runs-on: self-hosted
       timeout-minutes: 5  # or 15 for Docker-gated tests
       steps:
         - uses: actions/checkout@v4
         - run: export PATH="/opt/homebrew/bin:$PATH:$(go env GOPATH)/bin"
         - run: make block${{ matrix.block }}-gate
   ```

4. For Docker jobs, set the Docker socket:
   ```yaml
       env:
         DOCKER_HOST: unix:///var/run/docker.sock
   ```

## Path Filters (Critical for Single Runner)

With a single self-hosted runner, every push triggers all gates
sequentially. A slow Docker gate blocks the runner and cancels
in-progress jobs on the next push (concurrency: cancel-in-progress).

Attach path filters so each block gate only runs when its own code
changes:

```yaml
   block5-gate:
     runs-on: self-hosted
     if: |
       contains(github.event.commits.*.modified, 'internal/runtime/') ||
       contains(github.event.commits.*.modified, 'go.mod') ||
       contains(github.event.commits.*.modified, 'Makefile')
     steps:
       - uses: actions/checkout@v4
       - run: make block5-gate
```

Shared files that trigger ALL gates: build config (go.mod, Makefile,
package.json), API definitions (proto, OpenAPI spec), workflow files.

## Trade-Offs

- **Single runner = sequential jobs.** 7 jobs take ~15 min total, not
  parallel. Register a second runner if you need parallelism.
- **Machine must be on.** If asleep, jobs queue until wake.
- **Resource contention.** Running CI while actively developing can slow
  both. For heavy Docker gates, let them finish before pushing again.
- **Preemption.** Pushing mid-run cancels in-progress jobs on a single
  runner. Let slow gates finish, or accept the cancel and re-run.

## When NOT to Self-Host

- You don't own a machine that matches your production environment.
- You need collaborative CI (multiple developers pushing simultaneously).
- Your build requires exotic hardware (GPU, special accelerators).
- Your team is distributed and the runner machine isn't always available.

In these cases, use hosted CI but be aware of the macOS 10x billing
multiplier. Prefer `ubuntu-latest` over `macos-latest` unless you
specifically need macOS.
