# package cli

## Purpose

`cli` implements the `agent` command-line interface: cobra command tree, output
formatting, daemon dial helpers, and user-facing workflows for pack/run/policy/
bundle/deploy/identity and operator diagnostics.

## Key Types

| Type | Role |
|------|------|
| Root `agent` command (`AgentCmd`) | Command tree entry |
| Per-file command constructors | `newPackCmd`, `newRunCmd`, `newDaemonCmd`, … |
| Output helpers | Human vs `--json` rendering |

(Most symbols are unexported command constructors; the public surface is
`AgentCmd` plus shared helpers used by `cmd/agent`.)

## Key Functions

| Symbol | Role |
|--------|------|
| `AgentCmd` | Build/return the root cobra command |
| Socket/home resolution | `--socket` / `--home` / env / defaults |
| `resolveRunTarget` | Map path/digest/name to deployed agent |
| Dial helpers | gRPC client to control daemon |
| Subcommand groups | daemon, pack, run, policy, secrets, audit, bundle, deploy, cron, trust, identity, … |

## Architecture

```
cmd/agent
    |
    v
cli.AgentCmd()
    |
    +-- local-only commands (init, bundle inspect, some identity)
    +-- daemon RPCs over Unix socket
            |
            v
        internal/daemon ControlService
```

PersistentPreRun validates explicit `--home`/`--socket` paths. Operational
errors suppress cobra usage spam (`SilenceUsage`).

## Usage

```go
cmd := cli.AgentCmd()
if err := cmd.Execute(); err != nil {
    os.Exit(1)
}
```

```
agent --home ~/.agentpaas daemon start
agent pack ./my-agent
agent run ./my-agent
agent deploy create my-agent:1.0.0
```
