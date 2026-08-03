# AgentPaaS Cloud — short golden path (terminal)

Copy each block top to bottom. Replace nothing except secrets when prompted.
Do not commit tokens.

## 0. One-time shell setup

```bash
export PATH="/opt/homebrew/bin:$PATH"
export AGENTPAAS_CLOUD_API_URL='https://agentpaas-cloud-api.parvezsyed.workers.dev'
# Cloudflare registry push (not your apc_ tenant token):
export CLOUDFLARE_API_TOKEN="$(security find-generic-password -s agentpaas-cloudflare-api-token -w)"
```

Check:

```bash
agentpaas version          # expect 0.3.6
agentpaas doctor           # 7/7
echo "API=$AGENTPAAS_CLOUD_API_URL"
```

## 1. Identity + daemon (skip if already done)

```bash
agentpaas identity init --name my-org     # OK if "already exists"
agentpaas daemon start                   # OK if already running
```

## 2. Login (trial token once)

```bash
# paste apc_… token when prompted, or:
printf '%s\n' "$AGENTPAAS_CLOUD_API_TOKEN" | agentpaas cloud login --token-stdin
agentpaas cloud whoami
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
| unexpected exit code 1 | Need amd64 pack + container app instance_type dev |

## Hermes first-time user (full)

Use `docs/customer/golden-loop-hermes-e2e.md` — profile teardown → install →
local build → egress deny/allow → cloud path, all through Hermes.
