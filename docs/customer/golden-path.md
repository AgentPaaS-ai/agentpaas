# AgentPaaS Cloud — short golden path (terminal)

## Automated

For the founder-cold CLI equivalent of install → build a friendly LLM weather
agent → cloud deploy → invoke → undeploy, run `make golden-founder-cold`; run
`make golden-edge` for today's authentication, amd64, binding, undeploy, and
slot-reuse regressions. See `docs/customer/golden-path-automated.md` for
prerequisites and project override details.

Copy each block top to bottom. Replace nothing except secrets when prompted.
Do not commit tokens.

Hermes can run this same flow through its structured cloud tools, including login,
push, deploy, invoke, result, logs, usage, and label-only secret operations;
state-changing cloud calls still require explicit user confirmation.

## 0. One-time shell setup

```bash
export PATH="/opt/homebrew/bin:$PATH"
# AGENTPAAS_CLOUD_API_URL is optional; the binary defaults to
# https://cloud.agentpaas.ai.
# To override the API host, uncomment and set:
# export AGENTPAAS_CLOUD_API_URL='https://cloud.agentpaas.ai'
# No Cloudflare credential needed — push/deploy/invoke use only your tenant token.
```

Check:

```bash
agentpaas version          # expect 0.4.0
agentpaas doctor           # 7/7
echo "API=${AGENTPAAS_CLOUD_API_URL:-https://cloud.agentpaas.ai}"
```

## 1. Identity + daemon (skip if already done)

```bash
agentpaas identity init --name my-org     # OK if "already exists"
agentpaas daemon start                   # OK if already running
```

## 2. Login (once)

Primary path — browser claim link:

1. Ask your AgentPaaS provider/operator for your tenant claim link.
2. Open the link in a browser and complete sign-in (for example,
   `https://<api>/v1/auth/claim/<code>`). That sets a browser session cookie
   on the API host.
3. Optional: if your provider exposes a dashboard, use the URL they provide.
   The CLI defaults to `https://cloud.agentpaas.ai`; set
   `AGENTPAAS_CLOUD_API_URL` to a legacy workers.dev API host during the
   transition. The dashboard, when available, is on the API origin rather
   than a separate app domain.
4. In the same terminal, approve the CLI login:

```bash
agentpaas cloud login
agentpaas cloud whoami
```

Fallback for CI or scripted use — use the `apc_…` token your provider gave you:

```bash
printf '%s\n' "$AGENTPAAS_CLOUD_API_TOKEN" | agentpaas cloud login --token-stdin
agentpaas cloud whoami
```

If you don't have a claim link yet, your AgentPaaS provider/operator mints one
when they provision your tenant. Without it, use the token they gave you with
`--token-stdin`.

## Cloud registry and MCP deployments

Use the registry to see tenant-owned assets and the platform MCP catalog. The
`list` alias is equivalent to `registry`, and `--json` returns the structured
response for automation:

```bash
agentpaas cloud registry
agentpaas cloud list --json
```

Deployments are agents by default. To create an MCP deployment, pass
`--type mcp` (the request sends `kind: "mcp"`):

```bash
agentpaas cloud deploy latest --type mcp
```

## 3. Pack the demo weather agent

```bash
cd ~/projects/agentpaas/demo/weather-agent
agentpaas pack . --target linux/amd64
```

Copy the **Lock:** line (absolute path). Example:

```bash
LOCK="$HOME/.agentpaas/state/agents/weather-agent/agent.lock"
```

## 4. Push image to Cloud

```bash
agentpaas cloud push --lock "$LOCK"
agentpaas cloud images
```

## 5. Secret (local → cloud)

```bash
# Local Keychain (paste key in terminal; never in chat):
agentpaas secret add openrouter-key
agentpaas cloud secrets push openrouter-key
agentpaas cloud secrets list
```

## 6. Deploy

```bash
agentpaas cloud deploy latest
# note: Deployment created: dep_…
export DEPLOYMENT_ID='dep_…'
```

## 7. Bind secret on the deployment (required for LLM)

```bash
agentpaas cloud secrets bind "$DEPLOYMENT_ID" openrouter-key --as bearer --host openrouter.ai
agentpaas cloud secrets bindings "$DEPLOYMENT_ID"
```

## 8. Invoke + result

```bash
agentpaas cloud invoke-token "$DEPLOYMENT_ID"
agentpaas cloud invoke "$DEPLOYMENT_ID" --body '{"query":"What is the weather in Folsom?"}'
# note Run ID
agentpaas cloud result run_…
agentpaas cloud logs run_…
agentpaas cloud usage
```

**Pass:** Status succeeded, Final output is real weather prose (not empty).

## 9. Free capacity (undeploy) — required before more deploys

```bash
agentpaas cloud undeploy "$DEPLOYMENT_ID"     # frees the slot
agentpaas cloud deployments                    # list no longer shows it
agentpaas cloud deploy latest                  # deploy again succeeds (slot reused)
```

**Pass:** undeploy prints `Undeployed: dep_… (slot freed)`; second deploy creates a
new deployment instead of `no_slot_capacity`.

## If something fails

| Message | Fix |
|---------|-----|
| cannot reach cloud.agentpaas.ai / set AGENTPAAS_CLOUD_API_URL | Step 0 export |
| cf_bind_not_configured | Operator: Worker needs CF_API_TOKEN, CF_ACCOUNT_ID, CF_CONTAINER_APP_ID |
| secrets_misconfigured | Operator: SECRETS_MASTER_KEY on Worker |
| llm credential not declared | Step 7 bind |
| path must be absolute: agent.lock | Use Lock path from pack, not `./agent.lock` |
| unexpected exit code 1 | Need amd64 pack (`pack . --target linux/amd64`); container preset defaults to basic (1GiB) — do not force lite/dev |
| Run ID prints empty on invoke | Fixed in 0.3.7+ — upgrade: `brew upgrade agentpaas` or rebuild |

## Hermes first-time user (full)

Use `docs/customer/golden-loop-hermes-e2e.md` — profile teardown → install →
local build → egress deny/allow → cloud path, all through Hermes.


## Optional: compose a workflow

Weather is a single-agent run. After it works, you may compose a linear, fan-out, choice, or phone-call envelope in Hermes. A workflow is a recipe, not a deployment. Create children first, then the parent, then start once.
