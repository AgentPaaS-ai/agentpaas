# Path-Filtered CI

When using a self-hosted runner (or any single-runner setup), every push
triggers all configured gates. With path filters, each gate only runs
when its own code or shared dependencies change.

## Why Path Filters Matter

Without path filters:
- You push a one-line change to `internal/harness/`
- ALL block gates run (Block 1, Block 3, Block 5, Block 6, Block 7...)
- Block 5 Gate (Docker, ~10 min) blocks the runner
- You push again mid-run → concurrency cancels the in-progress Block 5 Gate
- The Docker gate never completes

With path filters:
- You push a one-line change to `internal/harness/`
- Only Block 6 Gate runs (~57s)
- Block 5 Gate (Docker, ~10 min) is skipped entirely

## Configuration (GitHub Actions)

```yaml
name: Block Gates

on:
  push:
    branches: [main]

jobs:
  block1-gate:
    runs-on: self-hosted
    if: |
      contains(github.event.commits.*.modified, 'api/') ||
      contains(github.event.commits.*.modified, 'Makefile') ||
      contains(github.event.commits.*.modified, 'go.mod') ||
      contains(github.event.commits.*.modified, 'go.sum') ||
      contains(github.event.commits.*.modified, '.github/workflows/')
    steps:
      - uses: actions/checkout@v4
      - run: make block1-gate

  block5-gate:
    runs-on: self-hosted
    timeout-minutes: 15
    if: |
      contains(github.event.commits.*.modified, 'internal/runtime/') ||
      contains(github.event.commits.*.modified, 'go.mod') ||
      contains(github.event.commits.*.modified, 'go.sum') ||
      contains(github.event.commits.*.modified, 'Makefile') ||
      contains(github.event.commits.*.modified, '.github/workflows/')
    env:
      DOCKER_TESTS: "1"
    steps:
      - uses: actions/checkout@v4
      - run: make block5-gate

  block6-gate:
    runs-on: self-hosted
    if: |
      contains(github.event.commits.*.modified, 'internal/harness/') ||
      contains(github.event.commits.*.modified, 'python/') ||
      contains(github.event.commits.*.modified, 'go.mod') ||
      contains(github.event.commits.*.modified, 'go.sum') ||
      contains(github.event.commits.*.modified, 'Makefile') ||
      contains(github.event.commits.*.modified, '.github/workflows/')
    steps:
      - uses: actions/checkout@v4
      - run: make block6-gate
```

## Shared Files (Trigger All Gates)

These files are shared across all blocks and should trigger every gate:
- Build config: `go.mod`, `go.sum`, `package.json`, `Cargo.toml`, `Makefile`
- API definitions: `api/` (proto, OpenAPI, GraphQL schema)
- CI configuration: `.github/workflows/`

## Path Filters Don't Apply To

- **Pull requests.** Path filters only check push commits. On PRs, all
  gates run. (This is usually fine — PRs are less frequent than pushes
  in local-first mode.)
- **Changes to shared files.** Touching `go.mod` or `Makefile` triggers
  every gate, by design.

## Adding a New Block Gate

1. Add a `blockN-gate` job with `runs-on: self-hosted`
2. Set `timeout-minutes` (5 for non-Docker, 15 for Docker)
3. Add a path filter (`if:` condition for the block's code + shared files)
4. Update your build system: `blockN-gate: build test race lint secscan`
5. For Docker blocks, set `DOCKER_TESTS: "1"` and `DOCKER_HOST` in env
