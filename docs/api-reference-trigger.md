# Trigger API Reference

Package: `agentpaas.trigger.v1`  
Proto: `api/trigger/v1/trigger.proto`  
Implementation: `internal/trigger/server.go`, `internal/trigger/cancel.go`, `internal/trigger/apikey.go`  
Generated stubs: `api/trigger/v1/trigger.pb.go`, `trigger_grpc.pb.go`, `trigger.pb.gw.go`

---

## 1. Overview

The **TriggerService** is the external agent-invocation surface. Callers use it to start runs, stream lifecycle updates, query run status, list runs, and cancel in-flight work.

| Who calls it | How |
|---|---|
| External HTTP clients / webhooks | REST gateway (default `127.0.0.1:7717`) |
| gRPC clients / SDKs | gRPC (default `127.0.0.1:7718`) |
| AgentPaaS daemon | Wires TriggerService and delegates execution to Control `Run` via `SetInvokeFunc` |
| Cron scheduler | Indirectly via invoke path |

### Authentication model

| Mode | Behavior |
|---|---|
| **No API key configured** | Auth interceptor is not installed. Loopback-only access is accepted without a Bearer token (local-dev backward compatibility). |
| **`AGENTPAAS_TRIGGER_API_KEY` set** | Unary + stream gRPC interceptors require `authorization: Bearer <key>`. REST clients must send the same header. Invalid/missing key → `UNAUTHENTICATED`. |
| **`AGENTPAAS_TRIGGER_EXPOSE=1` / `true`** | Non-loopback bind allowed **only** when an API key authenticator is configured with at least one key. Otherwise server construction fails. |

Default bind addresses (overridable):

- `AGENTPAAS_TRIGGER_GRPC_ADDR` → default `127.0.0.1:7718`
- `AGENTPAAS_TRIGGER_REST_ADDR` → default `127.0.0.1:7717`

SSE event stream (REST-only, not a gRPC method): `GET /v1/trigger/events?run_id=...`  
When an authenticator is configured, the SSE path also accepts Bearer tokens.

### Idempotency semantics

`Invoke` and `InvokeStream` honor `idempotency_key` when an idempotency store is wired:

1. **Same key + same request hash** → replay original run (no duplicate execution).
2. **Same key + different payload/hash** → `ALREADY_EXISTS` (409) with message `idempotency key conflict: different payload`.
3. **Empty / different key** → new run every time.

Canonical request hash inputs (implementation): caller identity, `agent_name`, metadata `agent_lock_digest`, payload bytes, `content_type`, metadata `api_version` (default `"trigger.v1"`).

### Error conventions (`google.rpc.Status`)

| Code | HTTP | When |
|---|---|---|
| `INVALID_ARGUMENT` (3) | 400 | Missing required fields, payload over limit, invalid `page_token`, null bytes in REST body |
| `NOT_FOUND` (5) | 404 | Unknown `run_id` |
| `ALREADY_EXISTS` (6) | 409 | Idempotency key conflict with different payload |
| `FAILED_PRECONDITION` (9) | 412 | Cancel on terminal run / invalid cancel transition |
| `UNAUTHENTICATED` (16) | 401 | Missing/invalid API key when auth enabled |
| `RESOURCE_EXHAUSTED` (8) | 429 | Documented for budget exhaustion (may surface via control `Run` failures wrapped as `INTERNAL`) |
| `INTERNAL` (13) | 500 | Invoke path / store / audit failures |

Default max payload: **1 MiB** (`DefaultMaxPayload = 1 << 20`). Exceeding it returns `INVALID_ARGUMENT`.

---

## 2. Service definition

```
service TriggerService
package agentpaas.trigger.v1
full name: agentpaas.trigger.v1.TriggerService
```

| Transport | Default | Notes |
|---|---|---|
| **gRPC** | `127.0.0.1:7718` | Primary RPC surface |
| **REST / grpc-gateway** | `127.0.0.1:7717` | JSON mapping from `google.api.http` annotations; POST bodies validated (required non-empty body, rejects null bytes) |
| **SSE** | `GET /v1/trigger/events` | Extra REST handler (not in proto service) |

When the daemon starts TriggerService it:

1. Optionally installs API-key auth.
2. Sets `invokeFunc` to call Control `Run` with `agent_name` + `trigger_payload`.
3. Wires a durable EventStore under `~/.agentpaas/state/events/` so `InvokeStream` is replayable after restart. If that fails, falls back to in-memory EventBus (test/legacy path; logs a warning).

---

## 3. RPC methods

### 3.1 Invoke

Start a single agent run and return immediately with the admitted `Run`.

| | |
|---|---|
| **RPC** | `rpc Invoke(InvokeRequest) returns (InvokeResponse)` |
| **HTTP** | `POST /v1/trigger/invoke` body `*` |
| **Auth** | Bearer required when authenticator configured |

#### Behavior (implementation)

1. Reject payload if `len(payload) > maxPayload`.
2. Generate `run-<32 hex>` ID.
3. If `idempotency_key` set and store present: check/reserve; conflict → `AlreadyExists`; replay → reuse prior `run_id`.
4. If `invokeFunc` is wired (production daemon): call it; on success mark run **RUNNING** and return; on failure → `INTERNAL`.
5. If no `invokeFunc` (unit tests): register run as **PENDING** and return stub.

#### Request: `InvokeRequest`

| Field | Type | JSON | Required | Description |
|---|---|---|---|---|
| `agent_name` | string | `agentName` | recommended | Agent to invoke (passed through to Control `Run`) |
| `agent_version` | string | `agentVersion` | no | Version label (stored on Run when populated by caller/path) |
| `payload` | bytes | `payload` | no | Trigger body (base64 in JSON). Max 1 MiB. Must be valid JSON when Control `Run` validates it |
| `content_type` | string | `contentType` | no | MIME type of payload; part of idempotency hash |
| `idempotency_key` | string | `idempotencyKey` | no | Exactly-once key when store is enabled |
| `metadata` | map\<string,string\> | `metadata` | no | Free-form; `agent_lock_digest` and `api_version` affect idempotency hash |

#### Response: `InvokeResponse`

| Field | Type | JSON | Description |
|---|---|---|---|
| `run` | Run | `run` | Admitted run record |

#### Errors

| Condition | Code |
|---|---|
| Payload too large | `INVALID_ARGUMENT` |
| Idempotency conflict | `ALREADY_EXISTS` |
| Auth failure | `UNAUTHENTICATED` |
| invokeFunc / Control Run failure | `INTERNAL` (wraps underlying control error message) |
| Run ID generation failure | `INTERNAL` |

#### Example request

```http
POST /v1/trigger/invoke HTTP/1.1
Host: 127.0.0.1:7717
Authorization: Bearer <AGENTPAAS_TRIGGER_API_KEY>
Content-Type: application/json

{
  "agentName": "weather-agent",
  "agentVersion": "1.0.0",
  "payload": "eyJxdWVyeSI6IldoYXQgaXMgdGhlIHdlYXRoZXI/In0=",
  "contentType": "application/json",
  "idempotencyKey": "req-2026-04-01-001",
  "metadata": {
    "api_version": "trigger.v1"
  }
}
```

Decoded payload example: `{"query":"What is the weather?"}`

#### Example response

```json
{
  "run": {
    "runId": "run-a1b2c3d4e5f6789012345678abcdef01",
    "agentName": "weather-agent",
    "status": "RUN_STATUS_RUNNING",
    "createdAt": "2026-04-01T12:00:00Z"
  }
}
```

---

### 3.2 InvokeStream

Start a run and **stream** `InvokeResponse` updates until a terminal event.

| | |
|---|---|
| **RPC** | `rpc InvokeStream(InvokeRequest) returns (stream InvokeResponse)` |
| **HTTP** | `POST /v1/trigger/invoke/stream` body `*` |
| **Auth** | Bearer required when authenticator configured |

#### Behavior (implementation)

**Production path (durable EventStore wired):**

1. Same admission/idempotency checks as `Invoke`.
2. Register run; append durable `run_created` (skipped on idempotency replay).
3. Subscribe to durable store from cursor 0.
4. On fresh admission with `invokeFunc`: start real execution; bridge EventBus lifecycle events into the durable store.
5. Stream events until terminal (`run_succeeded` / `run_failed` / `run_cancelled`).
6. Never manufactures success when durable store is configured.

**Fallback path (no EventStore — tests only):**

- Synthetic EventBus path that immediately publishes created + succeeded (logs warning).

#### Request / response messages

Same as `Invoke` / streaming `InvokeResponse`.

#### Errors

Same as `Invoke`, plus stream context cancellation returns the context error. Durable append/subscribe failures → `INTERNAL`.

#### Example (conceptual stream frames)

```json
{"run":{"runId":"run-…","agentName":"weather-agent","status":"RUN_STATUS_PENDING","createdAt":"…"}}
{"run":{"runId":"run-…","agentName":"weather-agent","status":"RUN_STATUS_RUNNING"}}
{"run":{"runId":"run-…","agentName":"weather-agent","status":"RUN_STATUS_SUCCEEDED","finishedAt":"…"}}
```

---

### 3.3 GetRun

| | |
|---|---|
| **RPC** | `rpc GetRun(GetRunRequest) returns (Run)` |
| **HTTP** | `GET /v1/trigger/runs/{run_id}` |

#### Request: `GetRunRequest`

| Field | Type | JSON | Required | Description |
|---|---|---|---|---|
| `run_id` | string | `runId` | yes | Run identifier |

#### Response

Full `Run` message (see §4).

#### Errors

| Condition | Code |
|---|---|
| Empty `run_id` | `INVALID_ARGUMENT` |
| Unknown run | `NOT_FOUND` |

#### Example

```http
GET /v1/trigger/runs/run-a1b2c3d4e5f6789012345678abcdef01
Authorization: Bearer <key>
```

```json
{
  "runId": "run-a1b2c3d4e5f6789012345678abcdef01",
  "agentName": "weather-agent",
  "status": "RUN_STATUS_SUCCEEDED",
  "createdAt": "2026-04-01T12:00:00Z",
  "startedAt": "2026-04-01T12:00:01Z",
  "finishedAt": "2026-04-01T12:00:15Z"
}
```

---

### 3.4 CancelRun

| | |
|---|---|
| **RPC** | `rpc CancelRun(CancelRunRequest) returns (Run)` |
| **HTTP** | `POST /v1/trigger/runs/{run_id}:cancel` body `*` |

#### Behavior (implementation — `internal/trigger/cancel.go`)

1. Require non-empty `run_id` (max 256 chars).
2. Audit `cancel_requested`.
3. **Already cancelled** → return current run (idempotent).
4. **Terminal** (`SUCCEEDED` / `FAILED` / `BUDGET_EXCEEDED`) → `FAILED_PRECONDITION`.
5. **PENDING** → mark cancelled immediately; publish `run_cancelled`; audit graceful.
6. **RUNNING** → invoke cancel func once; wait up to **30s** grace period (`CancelGracePeriod`), polling for graceful completion; on timeout force-cancel and audit `cancel_timeout` + `cancel_forced`.

Note: Trigger cancel updates the trigger run store / event bus. When the daemon uses Control `Run` for execution, full container teardown is primarily driven by Control `Stop`; cancel coordinates via context cancellation when a cancel func is registered on the entry.

#### Request: `CancelRunRequest`

| Field | Type | JSON | Required | Description |
|---|---|---|---|---|
| `run_id` | string | `runId` | yes | Run to cancel |
| `reason` | string | `reason` | no | Human reason (audited) |

#### Response

Updated `Run` with `RUN_STATUS_CANCELLED` when cancel completes.

#### Errors

| Condition | Code |
|---|---|
| Missing/oversized `run_id` | `INVALID_ARGUMENT` |
| Unknown run | `NOT_FOUND` |
| Already terminal (not cancelled) | `FAILED_PRECONDITION` |
| Invalid status for cancel | `FAILED_PRECONDITION` |
| Audit append failure | `INTERNAL` |

#### Example

```json
POST /v1/trigger/runs/run-…:cancel
{
  "runId": "run-a1b2c3d4e5f6789012345678abcdef01",
  "reason": "user aborted"
}
```

```json
{
  "runId": "run-a1b2c3d4e5f6789012345678abcdef01",
  "agentName": "weather-agent",
  "status": "RUN_STATUS_CANCELLED",
  "createdAt": "2026-04-01T12:00:00Z",
  "finishedAt": "2026-04-01T12:00:20Z"
}
```

---

### 3.5 ListRuns

| | |
|---|---|
| **RPC** | `rpc ListRuns(ListRunsRequest) returns (ListRunsResponse)` |
| **HTTP** | `GET /v1/trigger/runs` |

#### Behavior

- Lists in-memory trigger run store entries.
- Optional filters: `agent_name`, `status` (UNSPECIFIED = no filter).
- Pagination: `page_token` is a decimal offset string; default `page_size` 100; hard cap 100.
- Sorted by `created_at` + `run_id`.

#### Request: `ListRunsRequest`

| Field | Type | JSON | Required | Description |
|---|---|---|---|---|
| `agent_name` | string | `agentName` | no | Filter |
| `status` | RunStatus | `status` | no | Filter |
| `page_size` | int32 | `pageSize` | no | Max results (default 100, max 100) |
| `page_token` | string | `pageToken` | no | Offset from prior response |

#### Response: `ListRunsResponse`

| Field | Type | JSON | Description |
|---|---|---|---|
| `runs` | repeated Run | `runs` | Page of runs |
| `next_page_token` | string | `nextPageToken` | Next offset, empty if done |

#### Errors

| Condition | Code |
|---|---|
| Non-integer / negative `page_token` | `INVALID_ARGUMENT` |

#### Example

```http
GET /v1/trigger/runs?agentName=weather-agent&pageSize=10
```

```json
{
  "runs": [
    {
      "runId": "run-…",
      "agentName": "weather-agent",
      "status": "RUN_STATUS_SUCCEEDED",
      "createdAt": "2026-04-01T12:00:00Z"
    }
  ],
  "nextPageToken": ""
}
```

---

## 4. Message types

### 4.1 Run

| Field | Type | JSON | Description |
|---|---|---|---|
| `run_id` | string | `runId` | Unique run ID (`run-` + 32 hex) |
| `agent_name` | string | `agentName` | Agent name |
| `agent_version` | string | `agentVersion` | Version (may be empty on trigger path) |
| `status` | RunStatus | `status` | Lifecycle state |
| `created_at` | Timestamp | `createdAt` | Admission time |
| `started_at` | Timestamp | `startedAt` | Execution start |
| `finished_at` | Timestamp | `finishedAt` | Terminal time |
| `error` | string | `error` | Error detail when failed |
| `budget_summary` | BudgetSummary | `budgetSummary` | Usage summary (when populated) |
| `policy_digest` | string | `policyDigest` | Policy digest at run time |
| `image_digest` | string | `imageDigest` | Image digest |
| `workflow_id` | string | `workflowId` | B26 hierarchy (additive) |
| `invocation_id` | string | `invocationId` | B26 hierarchy (additive) |
| `attempt_id` | string | `attemptId` | Present after async attempt claim; empty on immediate admission |

### 4.2 BudgetSummary

| Field | Type | JSON | Description |
|---|---|---|---|
| `token_count` | int64 | `tokenCount` | Tokens consumed |
| `cost_usd` | double | `costUsd` | Cost in USD |
| `wall_clock_ms` | int64 | `wallClockMs` | Wall time |
| `iterations` | int64 | `iterations` | Iteration count |

### 4.3 InvokeRequest / InvokeResponse / GetRunRequest / CancelRunRequest / ListRuns*

Documented with their RPCs above.

---

## 5. Enums

### RunStatus

| Value | Number | Meaning |
|---|---|---|
| `RUN_STATUS_UNSPECIFIED` | 0 | Unset / filter wildcard |
| `RUN_STATUS_PENDING` | 1 | Admitted, not started |
| `RUN_STATUS_RUNNING` | 2 | Executing |
| `RUN_STATUS_SUCCEEDED` | 3 | Terminal success |
| `RUN_STATUS_FAILED` | 4 | Terminal failure |
| `RUN_STATUS_CANCELLED` | 5 | Cancelled |
| `RUN_STATUS_BUDGET_EXCEEDED` | 6 | Budget terminal |
| `RUN_STATUS_PAUSE_REQUESTED` | 7 | B26 routed-run (additive) |
| `RUN_STATUS_PAUSED` | 8 | B26 routed-run |
| `RUN_STATUS_NEEDS_REPLAN` | 9 | B26 routed-run |
| `RUN_STATUS_EXPIRED` | 10 | B26 routed-run |

New values are additive; clients must ignore unknown enum numbers.

---

## 6. Security considerations

1. **Loopback by default.** Trigger binds `127.0.0.1` unless addresses are overridden. Treat non-loopback binds as a deliberate exposure.
2. **API key required for expose.** `AGENTPAAS_TRIGGER_EXPOSE` without `AGENTPAAS_TRIGGER_API_KEY` fails closed at daemon start.
3. **Bearer transport.** Prefer OS-local or TLS-terminated paths for keys. Keys are compared with constant-time equality against configured material; store hashes never log raw keys.
4. **Payload size cap (1 MiB)** limits DoS via large bodies.
5. **REST null-byte rejection** prevents truncated JSON / smuggling class issues on the gateway path.
6. **Idempotency** is caller+payload scoped — different callers with the same key do not collide (caller is part of the hash).
7. **Egress policy is not enforced by TriggerService itself.** Trigger only admits work; the Control `Run` path creates the dual-homed gateway and enforces `policy.yaml` egress. See [how-enforcement-works.md](how-enforcement-works.md).
8. **Secrets never appear in Trigger responses.** Credential brokering happens inside the control/runtime path ([secrets.md](secrets.md)).
9. **SSE auth.** When auth is enabled, `/v1/trigger/events` should send the same Bearer header; unauthenticated access depends on whether the authenticator rejects empty tokens (callers should always send the key when configured).
10. **No `runs:amend_limits` or admin scopes** on trigger credentials — trigger keys are intended for invoke-only authority (`scopes: ["trigger"]` when created from env key).

---

## 7. Related endpoints (non-proto)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/trigger/events?run_id=` | SSE stream of run events from EventBus |

---

## 8. Implementation notes / discrepancies

| Topic | Proto | Implementation |
|---|---|---|
| Auth | Not in proto | Enforced by interceptors when authenticator non-nil |
| `agent_version` on Run | Present | Often empty on trigger path unless filled elsewhere |
| Hierarchy IDs on Run | Documented as additive | Immediate trigger admission typically leaves them empty; Control Run may not populate trigger Run fields |
| `RESOURCE_EXHAUSTED` | Documented file-level | Concurrent limit is enforced in Control `Run` (`maxConcurrentRuns = 3`); Trigger surfaces it as wrapped `INTERNAL` from `invokeFunc` today |
| Invoke without invokeFunc | — | Returns PENDING stub (tests) |
| InvokeStream without EventStore | — | Synthetic success path (test-only; warned in logs) |
| Cancel vs Control Stop | Separate APIs | Cancel updates trigger store; full Docker cleanup is Control `Stop` / finalize path |
