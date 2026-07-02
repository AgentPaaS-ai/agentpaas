# B14E Task: R18 — Wire trigger API key authentication into the daemon

## Repository
`~/projects/agentpaas` — on `main` branch. Commit directly to main.

## Context (read this first — saves you from going wrong)
The trigger server auth FRAMEWORK already exists:
- `internal/trigger/auth.go`: `APIKeyAuthenticator`, `AuthInterceptor`, `AuthStreamInterceptor`
- `internal/trigger/apikey.go`: `APIKeyStore` with hashed storage, CreateKey, ValidateKey, RevokeKey
- `internal/trigger/server.go`: `ServerConfig` has an `Authenticator Authenticator` field. When set,
  `grpc.UnaryInterceptor(AuthInterceptor(...))` is applied. When `Exposed=true` and no authenticator,
  `New()` returns an error.

**THE GAP (R18):** The daemon's `internal/daemon/server.go` (around line 316) creates the trigger
server WITHOUT passing an Authenticator:
```go
triggerSrv, err := trigger.New(trigger.ServerConfig{
    GRPCAddr: triggerGRPCAddr,
    RESTAddr: triggerRESTAddr,
    EventBus: d.eventBus,
    Audit:    auditWriter,
})
```
So even though the framework exists, authentication is never enforced. Any localhost process can
invoke agents.

## What to fix

### 1. Daemon reads AGENTPAAS_TRIGGER_API_KEY and wires the authenticator
In `internal/daemon/server.go`, in the trigger server setup block (starts ~line 307):

- Read `AGENTPAAS_TRIGGER_API_KEY` env var.
- If it is set AND non-empty:
  - Build an `*trigger.APIKeyAuthenticator` seeded with that key. Use `trigger.NewAPIKeyAuthenticator`
    with a map containing one `*trigger.APIKeyMeta`. Generate a stable key ID (e.g. "env-key" or a
    sha256 prefix of the key). Scopes can be `[]string{"trigger"}`.
  - Pass `Authenticator: auth` in the `trigger.ServerConfig`.
  - Log to stderr: `"daemon: trigger server API key authentication enabled\n"` (do NOT log the key).
- If it is unset or empty:
  - Pass NO Authenticator (current behavior — loopback-only, no auth). This is backward-compatible.
  - If `AGENTPAAS_TRIGGER_EXPOSE` env var is set to "1"/"true" AND no API key is configured, return
    an error from daemon Start (same as trigger.New does for Exposed). Print a clear message:
    `"--expose requires AGENTPAAS_TRIGGER_API_KEY to be set"`.

### 2. Add tests in `internal/daemon/server_trigger_test.go`
Add two tests:

**TestTriggerServer_APIKeyAuthRequired**:
- Set AGENTPAAS_TRIGGER_API_KEY to a test value, AGENTPAAS_TRIGGER_GRPC_ADDR/REST_ADDR to ephemeral ports.
- Start the daemon trigger server.
- Make a gRPC Invoke call WITHOUT the API key (no Authorization metadata) → must get
  `codes.Unauthenticated`.
- Make the same call WITH `Authorization: Bearer <key>` metadata → must succeed (or at least not
  get Unauthenticated — it may return a different error since no agent is packed, that's fine).
- Use the existing test helpers in server_trigger_test.go for address setup.

**TestTriggerServer_NoAuthWhenKeyUnset**:
- Do NOT set AGENTPAAS_TRIGGER_API_KEY.
- Start the daemon trigger server on ephemeral ports.
- Make an Invoke call with no auth → must NOT get Unauthenticated (backward compat). It may get a
  different error (e.g. NotFound for the agent), that's fine — assert it's not Unauthenticated.

## Constraints
- Go project. After changes: `go build ./...` must compile.
- `go test ./internal/daemon/... ./internal/trigger/... -count=1 -timeout 120s` must pass.
- `go vet ./internal/daemon/... ./internal/trigger/...` must be clean.
- Do NOT modify the trigger package's auth.go or apikey.go — they already work. Only wire them.
- Do NOT log the API key value anywhere.
- Commit: `git add -A && git commit -m "feat(daemon): R18 wire trigger API key auth via AGENTPAAS_TRIGGER_API_KEY (B14E)"`

## Report
Commit hash, files changed, test pass/fail counts, and the exact diff of the server.go trigger block.
