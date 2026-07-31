# AgentPaaS Cloud golden path

This is the shortest path from a signed project to a scheduled Cloud run and its
customer-visible output. Replace `<...>` placeholders. Do not commit tokens,
secrets, or signed artifact URLs.

## Before you start

You need the AgentPaaS CLI, Docker, a running daemon, and a project containing
`agent.yaml` and `policy.yaml`. Create a publisher identity once per machine:

```bash
agentpaas identity init --name my-org
agentpaas daemon start
```

Cloud requires a `linux/amd64` image. On macOS, the pack command below builds
that target.

## 1. Provision a tenant token

A Cloud operator provisions a trial tenant through the Cloud control plane. The
OSS CLI has no `provision-token` command. The operator-only API call is:

```bash
export CLOUD_API=https://cloud.agentpaas.ai
curl -fsS -X POST "$CLOUD_API/v1/admin/tenants" \
  -H "X-Admin-Secret: $ADMIN_SECRET" \
  -H "Content-Type: application/json" \
  -d '{"name":"<customer-name>","email":"<customer-email>","tier":"trial"}'
```

Give the returned `token` (`apc_...`) to the customer once through a secure
channel. Keep it in a protected shell variable when API calls are needed:

```bash
export AGENTPAAS_CLOUD_API_TOKEN='apc_...'
```

## 2. Log in

Interactive login opens a browser and stores the token in the macOS Keychain:

```bash
agentpaas cloud login
agentpaas cloud whoami
```

For a token received out of band or for CI, use the help-listed stdin path:

```bash
printf '%s\n' "$AGENTPAAS_CLOUD_API_TOKEN" | agentpaas cloud login --token-stdin
agentpaas cloud whoami
```

## 3. Pack

From the project directory, build and sign the Cloud image:

```bash
cd ~/my-agent
agentpaas pack . --target linux/amd64
```

This writes `agent.lock`. The lock must be signed; Cloud rejects unsigned locks.

## 4. Push and admit

The normal push also sends the image to the Cloudflare Container Registry. The
registry credential is separate from the tenant token:

```bash
export CLOUDFLARE_API_TOKEN='<registry-token>'
agentpaas cloud push --lock agent.lock
```

Copy the `Digest: sha256:...` value from the output. `--skip-registry` is
admission-only and is not the full deploy path.

## 5. Push secrets

Store a value locally without displaying it, then sync its label and value to
Cloud over TLS:

```bash
agentpaas secret add openai-key
agentpaas cloud secrets push openai-key
agentpaas cloud secrets list
```

Use the name declared by the agent policy. Secret values are never printed.

## 6. Deploy

Use the exact 64-hex digest printed by `cloud push`:

```bash
export IMAGE_DIGEST='sha256:<64-hex-digest>'
agentpaas cloud deploy "$IMAGE_DIGEST"
```

Copy the `Deployment created: dep_...` ID into `DEPLOYMENT_ID`:

```bash
export DEPLOYMENT_ID='dep_...'
```

## 7. Configure completion and final-output delivery

The OSS CLI does not yet expose these Cloud configuration routes. Configure
both destinations with the tenant token. Each URL must be public HTTPS; do not
use localhost or a URL that redirects. If `secret` is omitted, Cloud generates
one and returns it once—save it in the receiver.

```bash
export COMPLETION_URL='https://<public-receiver>/agentpaas/completion'
export DELIVERY_URL='https://<public-receiver>/agentpaas/delivery'
export WEBHOOK_SECRET='<single-line-secret>'

curl -fsS -X PUT "$CLOUD_API/v1/deployments/$DEPLOYMENT_ID/completion-webhook" \
  -H "Authorization: Bearer $AGENTPAAS_CLOUD_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"url\":\"$COMPLETION_URL\",\"enabled\":true,\"secret\":\"$WEBHOOK_SECRET\"}"

curl -fsS -X PUT "$CLOUD_API/v1/deployments/$DEPLOYMENT_ID/delivery-webhook" \
  -H "Authorization: Bearer $AGENTPAAS_CLOUD_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"url\":\"$DELIVERY_URL\",\"enabled\":true,\"secret\":\"$WEBHOOK_SECRET\"}"
```

Both delivered POST requests include `X-Agentpaas-Timestamp` and
`X-Agentpaas-Signature`. Verify `HMAC-SHA256(secret,
"<timestamp>.<raw-json-body>")`. Completion contains the run status and signed
result links. Delivery has `kind: "final_output"` and only the declared final
output—not logs, secrets, artifacts, or intermediate events.

## 8. Invoke

Mint a deployment-scoped invoke token. The CLI stores it in
`~/.agentpaas/invoke-tokens.json` and displays it only once:

```bash
agentpaas cloud invoke-token "$DEPLOYMENT_ID"
agentpaas cloud invoke "$DEPLOYMENT_ID" --body '{"name":"Ada"}'
```

Copy the returned `Run ID: run_...`. `cloud invoke` uses the stored deployment
token; it does not use the tenant login token.

## 9. Schedule a Cloud run

Cloud cron is configured through the Cloud API; `agentpaas cron add` schedules
a local daemon agent and is not this Cloud schedule. The supported Cloud
interval names are `every_1m`, `every_5m`, `every_15m`, and `every_1h`:

```bash
curl -fsS -X PUT "$CLOUD_API/v1/deployments/$DEPLOYMENT_ID/cron" \
  -H "Authorization: Bearer $AGENTPAAS_CLOUD_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expr":"every_5m","enabled":true}'
```

## 10. Read the result

Wait until the run is terminal (`succeeded`, `failed`, or `cancelled`), then
fetch the customer-facing result package:

```bash
agentpaas cloud status <run_id>
agentpaas cloud result <run_id>
agentpaas cloud logs <run_id>
```

`cloud result` shows final output, failure details, and signed artifact URLs
with an expiry. The completion webhook carries the same result links; the
final-output delivery is intentionally smaller.

## 11. Check usage

```bash
agentpaas cloud usage
```

This shows the tier, concurrency, agent count, CPU minutes, remaining trial or
quota information, and the metering formula.
