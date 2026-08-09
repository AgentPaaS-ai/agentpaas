# Automated cold-start golden path

This is the CLI equivalent of a clean-machine first-run:

1. Install AgentPaaS from GitHub (the installed brew `agentpaas` binary is used).
2. Build a weather agent using an LLM with a friendly response.
3. Run it in AgentPaaS Cloud.
4. Undeploy it and prove the slot can be reused.

## Prerequisites

- The brew-installed `agentpaas` binary is on `PATH`.
- `agentpaas doctor` passes all checks (7/7).
- `agentpaas cloud whoami` succeeds with the tenant token already in Keychain.
  A Cloudflare token is not required or read by this customer path.
- The local Keychain contains `openrouter-key` (`agentpaas secret list`).
- `agentpaas identity show` succeeds.
- Docker or colima is running.
- The default demo project and its Python dependencies are available.

`AGENTPAAS_CLOUD_API_URL` is optional. When unset, the binary's default
`https://cloud.agentpaas.ai` is used. Do not set a Cloudflare token for this
flow.

## Run

From the repository root:

```bash
make golden-cold-start
make golden-edge
```

The default project is `demo/weather-agent`. Pass a project directory to either
script to use another agent:

```bash
bash scripts/golden-path-cold-start.sh /absolute/path/to/weather-agent
bash scripts/golden-path-edge-regressions.sh /absolute/path/to/weather-agent
```

The cold-start run writes a redacted GO/NO-GO record under
`docs/golden-records/golden-cold-start-YYYYMMDD-HHMM.md`. It checks the amd64
lock, tenant-only push, admitted image, non-empty bindings, successful invoke
and result output, logs/usage, undeploy cleanup, and optional slot reuse.
The edge script prints PASS/FAIL for unauthenticated HTTP, platform, invoke
authentication, undeploy idempotency, binding, registry, and slot regressions.
Neither script prints token or secret values.
