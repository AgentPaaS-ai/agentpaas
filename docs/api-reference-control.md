# Control Plane API Reference

Package: `agentpaas.control.v1`  
Proto: `api/control/v1/control.proto`  
Implementations:

- `internal/daemon/control_handlers.go` — pack, run, stop, logs, policy, secrets, audit, cron, list runs
- `internal/daemon/export_handlers.go` — export preview / export
- `internal/daemon/operator_handlers.go` — validate, summarize, explain, policy recommend, timeline, next action
- `internal/daemon/routed_handlers.go` — deployments, aliases, invoke deployment, workflows, amend limits
- `internal/daemon/stub_handlers.go` — server struct, Doctor stub, concurrency constant

Generated stubs: `control.pb.go`, `control_grpc.pb.go`, `control.pb.gw.go`

---

## 1. Overview

**ControlService** is the local operational control plane for AgentPaaS. It packs agent images, starts and stops governed runs, applies egress policy, manages secrets grants, queries the signed audit chain, runs operator diagnostics for coding-agent clients, schedules cron invocations, and exposes the B26+ deployment/workflow surface.

| Who calls it | How |
|---|---|
| CLI (`agentpaas`, plugin tools) | gRPC over Unix domain socket |
| Hermes / coding agents | Same socket via local tooling |
| Dashboard / internal components | May call handlers in-process or via socket |
| TriggerService (daemon-wired) | In-process `controlServer.Run` from trigger `invokeFunc` |

### Transport

| | |
|---|---|
| **Primary** | gRPC on Unix domain socket `~/.agentpaas/daemon.sock` (override home via env; socket name `daemon.sock`; mode **0600**, fail-closed if chmod fails) |
| **REST annotations** | Every RPC has `google.api.http` paths under `/v1/control/...` and a generated grpc-gateway file exists |
| **Daemon REST** | The production daemon **does not** currently mount a Control REST gateway listener; clients should use the Unix socket gRPC API unless a separate gateway process is introduced |

Socket discovery: `internal/home` (`DiscoverSocketPath` / `AGENTPAAS_SOCKET`).

### Authentication model

| Surface | Auth |
|---|---|
| Control UDS | **Filesystem permissions only** (socket `0600`, home `0700`). No Bearer/mTLS interceptor on ControlService today. Any local principal that can open the socket can call all control RPCs. |
| Readiness | Unary/stream interceptors return `UNAVAILABLE` until the daemon marks itself ready |
| Trigger (separate) | Optional API key on TCP loopback — see [api-reference-trigger.md](api-reference-trigger.md) |
| Authority scopes (B26) | Proto `AuthorityScope` documents `runs:control` and `runs:amend_limits`. Mutating workflow control RPCs currently **fail closed** with typed `FEATURE_NOT_ENABLED` rather than enforcing live scope checks |

### Error conventions

gRPC status codes used by handlers:

| Code | Typical meaning |
|---|---|
| `INVALID_ARGUMENT` | Missing/malformed fields, bad paths, invalid JSON payload |
| `NOT_FOUND` | Unknown run / deployment / workflow / invocation |
| `ALREADY_EXISTS` | Alias already exists (use CAS) |
| `FAILED_PRECONDITION` | Not packed/deployed, Docker unavailable, export unconfirmed, feature gated, missing credentials, verification failure |
| `RESOURCE_EXHAUSTED` | Concurrent run limit (`maxConcurrentRuns = 3`) |
| `UNAVAILABLE` | Audit index missing; daemon not ready |
| `INTERNAL` | Pack/runtime/store failures |
| `PERMISSION_DENIED` | Reserved in proto conventions; UDS relies on FS perms instead |

**Typed control errors:** many B26 responses embed `TypedControlError` in the **response body** (gRPC OK) so callers branch on `code` / `code_name` without string-matching. Admission outcomes use `AdmissionOutcomeCode` on `InvokeDeploymentResponse`.

### Operator contract

Operator methods (validate, summarize, explain, recommend, timeline, next-action) return structured fields with:

- `schema_version` = `operator.SchemaVersion` (**`1.1.0`**)
- Stable `error_category` / `next_action` enums
- `EvidenceRef` pointers into audit artifacts
- Confirmation protocol for trust-boundary policy patches

---

## 2. Service definition

```
service ControlService
package agentpaas.control.v1
full name: agentpaas.control.v1.ControlService
```

**43 RPC methods** grouped below.

---

## 3. Enums

### LogLevel

| Name | Number |
|---|---|
| `LOG_LEVEL_UNSPECIFIED` | 0 |
| `LOG_LEVEL_DEBUG` | 1 |
| `LOG_LEVEL_INFO` | 2 |
| `LOG_LEVEL_WARN` | 3 |
| `LOG_LEVEL_ERROR` | 4 |

### EventType (audit filter)

| Name | Number |
|---|---|
| `EVENT_TYPE_UNSPECIFIED` | 0 |
| `EVENT_TYPE_INVOKE` | 1 |
| `EVENT_TYPE_CANCEL` | 2 |
| `EVENT_TYPE_POLICY_APPLY` | 3 |
| `EVENT_TYPE_POLICY_DENIAL` | 4 |
| `EVENT_TYPE_SECRET_SET` | 5 |
| `EVENT_TYPE_SECRET_GRANT` | 6 |
| `EVENT_TYPE_SECRET_REVOKE` | 7 |
| `EVENT_TYPE_PACK` | 8 |
| `EVENT_TYPE_RUN` | 9 |
| `EVENT_TYPE_STOP` | 10 |

### SecretType

| Name | Number | Meaning |
|---|---|---|
| `SECRET_TYPE_UNSPECIFIED` | 0 | Unset |
| `SECRET_TYPE_ENV` | 1 | Environment injection (legacy model; runtime prefers gateway brokering) |
| `SECRET_TYPE_FILE` | 2 | File injection |
| `SECRET_TYPE_INLINE` | 3 | Inline |

### AuthorityScope

| Name | Number | Scope string |
|---|---|---|
| `AUTHORITY_SCOPE_UNSPECIFIED` | 0 | — |
| `AUTHORITY_SCOPE_DEFAULT` | 1 | default |
| `AUTHORITY_SCOPE_RUNS_CONTROL` | 2 | `runs:control` |
| `AUTHORITY_SCOPE_RUNS_AMEND_LIMITS` | 3 | `runs:amend_limits` (**must not** be granted to ordinary trigger credentials) |

### AdmissionOutcomeCode

| Name | Number |
|---|---|
| `ADMISSION_OUTCOME_UNSPECIFIED` | 0 |
| `ADMISSION_OUTCOME_ACCEPTED` | 1 |
| `ADMISSION_OUTCOME_IDEMPOTENT_REPLAY` | 2 |
| `ADMISSION_OUTCOME_ALREADY_RUNNING` | 3 |
| `ADMISSION_OUTCOME_IDEMPOTENCY_CONFLICT` | 4 |
| `ADMISSION_OUTCOME_DEPLOYMENT_INACTIVE` | 5 |

### TypedControlErrorCode

| Name | Number | Typical `code_name` |
|---|---|---|
| `TYPED_CONTROL_ERROR_UNSPECIFIED` | 0 | — |
| `TYPED_CONTROL_ERROR_IDEMPOTENT_REPLAY` | 1 | `IDEMPOTENT_REPLAY` |
| `TYPED_CONTROL_ERROR_ALREADY_RUNNING` | 2 | `ALREADY_RUNNING` |
| `TYPED_CONTROL_ERROR_IDEMPOTENCY_CONFLICT` | 3 | — |
| `TYPED_CONTROL_ERROR_DEPLOYMENT_INACTIVE` | 4 | `DEPLOYMENT_INACTIVE` / `DEPLOYMENT_NOT_FOUND` |
| `TYPED_CONTROL_ERROR_RUN_TERMINAL` | 5 | `RUN_TERMINAL` |
| `TYPED_CONTROL_ERROR_UNSAFE_PAUSE_BOUNDARY` | 6 | — |
| `TYPED_CONTROL_ERROR_CONCURRENCY_UNAVAILABLE` | 7 | — |
| `TYPED_CONTROL_ERROR_LIMIT_AMENDMENT_DENIED` | 8 | — |
| `TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED` | 9 | `FEATURE_NOT_ENABLED` / feature-specific names |
| `TYPED_CONTROL_ERROR_MISSING_SCOPE` | 10 | — |
| `TYPED_CONTROL_ERROR_NUMERIC_OVERFLOW` | 11 | — |
| `TYPED_CONTROL_ERROR_CHANGED_IDEMPOTENCY_PAYLOAD` | 12 | `CHANGED_IDEMPOTENCY_PAYLOAD` |

### ControlCommand

| Name | Number |
|---|---|
| `CONTROL_COMMAND_UNSPECIFIED` | 0 |
| `CONTROL_COMMAND_CANCEL` | 1 |
| `CONTROL_COMMAND_PAUSE` | 2 |
| `CONTROL_COMMAND_RESUME` | 3 |
| `CONTROL_COMMAND_RESTART` | 4 |
| `CONTROL_COMMAND_CONTINUE` | 5 |
| `CONTROL_COMMAND_AMEND_LIMITS` | 6 |

---

## 4. RPC methods — Packaging & lifecycle

### 4.1 Pack

Build a signed agent image from a project directory, push to the local registry, write agent lock + deployment record.

| | |
|---|---|
| **RPC** | `Pack(PackRequest) → PackResponse` |
| **HTTP** | `POST /v1/control/pack` |

#### Behavior

1. Require `agent_project_path`; resolve absolute path.
2. Detect project runtime; load `agent.yaml` (name/version defaults: yaml → `"default"` / `"latest"`).
3. Read optional `policy.yaml`.
4. Build image (`agentpaas/<name>:<version>`), push local registry.
5. Sign lock with package identity keystore + optional publisher Keychain store.
6. `RecordDeployment`; audit `pack`.

#### Request fields

| Field | Type | JSON | Req | Description |
|---|---|---|---|---|
| `agent_project_path` | string | `agentProjectPath` | yes | Project directory |
| `agent_name` | string | `agentName` | no | Override name |
| `agent_version` | string | `agentVersion` | no | Override version |
| `base_image` | string | `baseImage` | no | Base image override |

#### Response fields

| Field | Type | JSON | Description |
|---|---|---|---|
| `image_digest` | string | `imageDigest` | Built image digest |
| `build_log` | string | `buildLog` | Human build excerpt |

#### Errors

`INVALID_ARGUMENT` (path/project/yaml), `FAILED_PRECONDITION` (no home paths), `INTERNAL` (build/push/lock/SDK).

#### Example

```json
// Request
{
  "agentProjectPath": "/Users/me/agents/weather",
  "agentName": "weather-agent",
  "agentVersion": "1.0.0"
}

// Response
{
  "imageDigest": "sha256:abc…",
  "buildLog": "Built localhost:…/weather-agent:1.0.0, digest: sha256:abc…"
}
```

---

### 4.2 ExportPreview

| | |
|---|---|
| **RPC** | `ExportPreview(ExportPreviewRequest) → ExportPreviewResponse` |
| **HTTP** | `POST /v1/control/export:preview` |

Returns export file manifest after preconditions (publisher identity, pack state). Does not write a bundle.

| Field (req) | Type | JSON | Req | Description |
|---|---|---|---|---|
| `agent_project_path` | string | `agentProjectPath` | yes | Project path |
| `include_globs` | repeated string | `includeGlobs` | no | Extra include globs |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `agent_name` | string | `agentName` | Detected name |
| `agent_version` | string | `agentVersion` | Detected version |
| `files` | repeated ExportFileEntry | `files` | Manifest entries |

`ExportFileEntry`: `path`, `digest`, `bytes`, `extra`.

Errors: `INVALID_ARGUMENT`, `FAILED_PRECONDITION` (identity / not deployed / secrets / blocked), `INTERNAL`.

---

### 4.3 Export

| | |
|---|---|
| **RPC** | `Export(ExportRequest) → ExportResponse` |
| **HTTP** | `POST /v1/control/export` |

Writes a signed `.agentpaas` bundle. **Requires `confirmed=true`** after reviewing the preview manifest. Secret scan gate is fail-closed.

| Field (req) | Type | JSON | Req | Description |
|---|---|---|---|---|
| `agent_project_path` | string | `agentProjectPath` | yes | Project |
| `output_path` | string | `outputPath` | yes | Bundle output path |
| `with_image` | bool | `withImage` | no | Include image layers |
| `include_globs` | repeated string | `includeGlobs` | no | Extra files |
| `confirmed` | bool | `confirmed` | yes (must be true) | Explicit confirmation |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `bundle_digest` | string | `bundleDigest` | Bundle digest |
| `publisher_fingerprint` | string | `publisherFingerprint` | Publisher key FP |
| `file_count` | int32 | `fileCount` | Files packed |
| `total_bytes` | int64 | `totalBytes` | Size |
| `output_path` | string | `outputPath` | Written path |

Errors: `FAILED_PRECONDITION` if not confirmed or export blocked (secrets, source changed, no publisher, etc.).

```json
{
  "agentProjectPath": "/Users/me/agents/weather",
  "outputPath": "/tmp/weather.agentpaas",
  "withImage": true,
  "confirmed": true
}
```

---

### 4.4 Run

Start a governed agent run: verify lock/install, enforce credentials, create dual-homed network + gateway + agent container, invoke harness.

| | |
|---|---|
| **RPC** | `Run(RunRequest) → RunResponse` |
| **HTTP** | `POST /v1/control/run` |

#### Behavior (high level)

1. Require `agent_name`; home paths configured.
2. **Fail closed** if `continue_run_id`, `recovery_action`, or `requested_attempt_lease_ms` set → `FAILED_PRECONDITION` (`routed_run_continuation_not_enabled` until B35).
3. **Fail closed** if `deployment_ref` set → `FAILED_PRECONDITION` (`routed_run_invocation_not_enabled` until B28). Use `InvokeDeployment` instead.
4. `idempotency_key` on legacy Run is accepted but ignored.
5. Enforce **max 3 concurrent** active runs → `RESOURCE_EXHAUSTED`.
6. Installed vs packed agent verification (signatures, digests) **before** Docker.
7. Routed projects (`Route` / `workflow.yaml`) fail closed before Docker.
8. Validate trigger payload JSON if non-empty.
9. Validate all declared credentials exist in Keychain.
10. Require non-vulnerable Docker unless `AGENTPAAS_ALLOW_VULNERABLE_DOCKER=1`.
11. Create internal + egress networks, gateway (policy-compiled or default-deny), agent container with proxy env, credentials file for gateway only.
12. Track run; start async invoke; return `run_id` with status running/pending fields.

Egress enforcement: see [how-enforcement-works.md](how-enforcement-works.md). Secrets: [secrets.md](secrets.md).

#### Request: `RunRequest`

| Field | Type | JSON | Req | Description |
|---|---|---|---|---|
| `agent_name` | string | `agentName` | yes | Deployed or installed agent ref |
| `agent_version` | string | `agentVersion` | no | Version label |
| `trigger_payload` | bytes | `triggerPayload` | no | JSON payload to harness |
| `budget` | BudgetConfig | `budget` | no | Optional ceilings |
| `continue_run_id` | string | `continueRunId` | no | **Gated** B35 |
| `recovery_action` | string | `recoveryAction` | no | **Gated** B35 |
| `requested_attempt_lease_ms` | int64 | `requestedAttemptLeaseMs` | no | **Gated** B35 |
| `idempotency_key` | string | `idempotencyKey` | no | Ignored on legacy Run |
| `deployment_ref` | string | `deploymentRef` | no | **Gated** B28 |

#### `BudgetConfig`

| Field | Type | JSON | Description |
|---|---|---|---|
| `max_tokens` | int64 | `maxTokens` | Token ceiling |
| `max_cost_usd` | double | `maxCostUsd` | Cost ceiling |
| `max_wall_clock_ms` | int64 | `maxWallClockMs` | Wall clock |
| `max_iterations` | int32 | `maxIterations` | Iteration cap |

#### Response: `RunResponse`

| Field | Type | JSON | Description |
|---|---|---|---|
| `run_id` | string | `runId` | Started run |
| `invocation_id` | string | `invocationId` | B26 (may be empty on legacy path) |
| `workflow_id` | string | `workflowId` | B26 |
| `attempt_id` | string | `attemptId` | Empty until async claim |
| `status` | string | `status` | e.g. `running` |
| `requested_deployment_ref` | string | `requestedDeploymentRef` | Echo |
| `resolved_deployment_id` | string | `resolvedDeploymentId` | When resolved |
| `resolved_deployment_version` | string | `resolvedDeploymentVersion` | When resolved |

#### Errors

`INVALID_ARGUMENT`, `FAILED_PRECONDITION` (not deployed, verification, credentials, Docker, feature gates, routed project), `RESOURCE_EXHAUSTED`, `INTERNAL`.

```json
// Request
{
  "agentName": "weather-agent",
  "triggerPayload": "eyJxdWVyeSI6ImZvcmVjYXN0In0="
}

// Response
{
  "runId": "run-…",
  "status": "running"
}
```

---

### 4.5 Stop

| | |
|---|---|
| **RPC** | `Stop(StopRequest) → StopResponse` |
| **HTTP** | `POST /v1/control/stop` |

Claims the tracked run, cancels invoke context, stops agent (+ gateway) containers, finalizes audit ingestion, publishes terminal EventBus event.

| Field (req) | Type | JSON | Req | Description |
|---|---|---|---|---|
| `run_id` | string | `runId` | yes | Run to stop |
| `reason` | string | `reason` | no | Reason (audited indirectly via stop fields) |
| `force` | bool | `force` | no | Immediate kill (`timeout=0`); status forced to `cancelled` |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `acknowledged` | bool | `acknowledged` | Always true on success |

Idempotent for already-cleaned containers (`ErrContainerNotFound` treated as success).

Errors: `INVALID_ARGUMENT`, `NOT_FOUND`, `FAILED_PRECONDITION` (Docker), `INTERNAL`.

---

### 4.6 Logs (streaming)

| | |
|---|---|
| **RPC** | `Logs(LogsRequest) → stream LogEntry` |
| **HTTP** | `GET /v1/control/logs` |

Streams Docker container logs for a tracked run. **Implementation requires `run_id`** (proto marks it optional; handler does not).

| Field (req) | Type | JSON | Req | Description |
|---|---|---|---|---|
| `run_id` | string | `runId` | **yes (impl)** | Target run |
| `min_level` | LogLevel | `minLevel` | no | Not applied in current stream path |
| `follow` | bool | `follow` | no | Follow live logs |
| `tail` | int32 | `tail` | no | Historical lines (default **100**) |

`LogEntry`: `timestamp`, `run_id`, `level` (always `"info"` today), `message`, `fields`.

Errors: `INVALID_ARGUMENT`, `NOT_FOUND`, `FAILED_PRECONDITION`, `INTERNAL`.

---

### 4.7 ListRuns (control)

| | |
|---|---|
| **RPC** | `ListRuns(ListRunsRequest) → ListRunsResponse` |
| **HTTP** | `GET /v1/control/runs` |

Lists **currently tracked** daemon runs (in-memory map), not the full historical audit.

`ListRunsRequest`: empty.  
`ListRunsResponse.runs[]` → `RunInfo` (`run_id`, `agent_name`, `status`, `started_at`, `workflow_id`, `invocation_id`).

---

## 5. RPC methods — Policy & secrets

### 5.1 PolicyApply

| | |
|---|---|
| **RPC** | `PolicyApply(PolicyApplyRequest) → PolicyApplyResponse` |
| **HTTP** | `POST /v1/control/policy:apply` |

Parse/validate policy YAML; compute digest; if `dry_run`, return without writing. Else compile gateway config and write under daemon config paths.

| Field (req) | Type | JSON | Req | Description |
|---|---|---|---|---|
| `policy_yaml` | string | `policyYaml` | yes | Full policy document |
| `dry_run` | bool | `dryRun` | no | Validate only |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `policy_digest` | string | `policyDigest` | Canonical digest |
| `rules_applied` | int32 | `rulesApplied` | Rule count |
| `warnings` | repeated string | `warnings` | Validation warnings |

```json
{
  "policyYaml": "version: 1\negress:\n  - allow: api.example.com\n",
  "dryRun": true
}
```

---

### 5.2 SecretSet

| | |
|---|---|
| **RPC** | `SecretSet(SecretSetRequest) → SecretSetResponse` |
| **HTTP** | `POST /v1/control/secrets` |

#### Important implementation note

The proto **has no secret value field**. The handler opens Keychain scope `agentpaas-<scope>` (default scope `default`) and returns `created=true` if `Get(name)` fails (secret appears absent). **It does not write secret material.** Real secret writes are performed by CLI/keychain tooling outside this RPC. Treat this RPC as a **probe / placeholder**, not a credential upload API.

| Field (req) | Type | JSON | Req | Description |
|---|---|---|---|---|
| `name` | string | `name` | yes | Secret name |
| `scope` | string | `scope` | no | Keychain service suffix (default `default`) |
| `type` | SecretType | `type` | no | Injection type (unused by current handler) |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `created` | bool | `created` | true if Get failed (interpreted as create-needed) |

---

### 5.3 SecretGrant / SecretRevoke

| RPC | HTTP |
|---|---|
| `SecretGrant` | `POST /v1/control/secrets:grant` |
| `SecretRevoke` | `POST /v1/control/secrets:revoke` |

In-memory grant map: `run_id → set(secret_name)`. Does not move Keychain material; used for run-scoped grant bookkeeping.

Both require `run_id` + `secret_name`. Response: `{ "acknowledged": true }`.

---

## 6. RPC methods — Audit

### 6.1 AuditQuery

| | |
|---|---|
| **RPC** | `AuditQuery(AuditQueryRequest) → AuditQueryResponse` |
| **HTTP** | `POST /v1/control/audit:query` |

Queries SQLite audit index with optional filters; includes chain verification summary when available.

| Field (req) | Type | JSON | Description |
|---|---|---|---|
| `agent_name` | string | `agentName` | Filter |
| `run_id` | string | `runId` | Filter |
| `event_type` | EventType | `eventType` | Filter |
| `time_range` | TimeRange | `timeRange` | start/end timestamps |
| `page_size` | int32 | `pageSize` | Default 50 |
| `page_token` | string | `pageToken` | Pagination |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `entries` | repeated AuditEntry | `entries` | Results |
| `next_page_token` | string | `nextPageToken` | Next page |
| `total_count` | int32 | `totalCount` | Count |
| `chain_verification` | AuditChainVerification | `chainVerification` | Hash-chain check |

`AuditEntry`: `event_id`, `event_type`, `agent_name`, `run_id`, `timestamp`, `payload` (bytes).  
`AuditChainVerification`: `verified`, `audit_record_count`, `audit_head_seq`, `checkpoint_count`, `issues[]` (`type`, `message`, `seq`, `line`).

Errors: `UNAVAILABLE` if index nil; `INTERNAL`; `INVALID_ARGUMENT` on resolve failures.

---

### 6.2 AuditExport

| | |
|---|---|
| **RPC** | `AuditExport(AuditExportRequest) → AuditExportResponse` |
| **HTTP** | `POST /v1/control/audit:export` |

| Field (req) | Type | JSON | Description |
|---|---|---|---|
| `time_range` | TimeRange | `timeRange` | Export window |
| `format` | string | `format` | e.g. `json`, `csv`, `ndjson` |
| `include_payloads` | bool | `includePayloads` | Include payloads |

| Field (resp) | Type | JSON | Description |
|---|---|---|---|
| `data` | bytes | `data` | Formatted export |
| `entry_count` | int32 | `entryCount` | Rows |

Unsupported format → `INTERNAL` with clear message.

---

## 7. RPC methods — Diagnostics & operator

### 7.1 Doctor

| | |
|---|---|
| **RPC** | `Doctor(DoctorRequest) → DoctorResponse` |
| **HTTP** | `GET /v1/control/doctor` |

**Implementation is a stub:** returns version check + `daemon_skeleton` ok. Full host diagnostics (Docker, Keychain, harness) are exercised by the CLI `doctor` path, not this RPC body today.

```json
{
  "checks": [
    {"name": "version", "status": "ok", "message": "…"},
    {"name": "daemon_skeleton", "status": "ok", "message": "Daemon skeleton is running. Stub implementation — full methods pending."}
  ],
  "overallStatus": "ok"
}
```

---

### 7.2 ValidateAgentProject

| | |
|---|---|
| **RPC** | `ValidateAgentProject(ValidateAgentProjectRequest) → ValidateAgentProjectResponse` |
| **HTTP** | `POST /v1/control/validate` |

Validates project path (rejects system dirs), `agent.yaml`, `policy.yaml` (no symlinks), policy parse, entry path traversal, readiness issues.

Does **not** return gRPC error for validation failures — returns `valid=false` with `OperatorIssue` list (`schema_version` `1.1.0`).

| Field (req) | Type | JSON | Req |
|---|---|---|---|
| `project_path` | string | `projectPath` | yes |

Response includes legacy `validations[]` plus operator fields: `schema_version`, `ready`, `project_dir`, `runtime`, `issues[]`.

---

### 7.3 SummarizeRun

| | |
|---|---|
| **RPC** | `SummarizeRun(SummarizeRunRequest) → SummarizeRunResponse` |
| **HTTP** | `POST /v1/control/runs/{run_id}:summarize` |

Builds NL summary + key events + budget string from audit records; operator fields include status, timings, denials, optional `AttemptReport`.

Requires `run_id`. Errors: `INVALID_ARGUMENT`, `INTERNAL` (audit query).

---

### 7.4 ExplainFailure

| | |
|---|---|
| **RPC** | `ExplainFailure(ExplainFailureRequest) → ExplainFailureResponse` |
| **HTTP** | `POST /v1/control/runs/{run_id}:explain-failure` |

Root cause, contributing factors, suggested fixes, redacted excerpts, `next_action`, B26 `latest_reason` / `latest_action`.

---

### 7.5 ExplainPolicyDenial

| | |
|---|---|
| **RPC** | `ExplainPolicyDenial(ExplainPolicyDenialRequest) → ExplainPolicyDenialResponse` |
| **HTTP** | `POST /v1/control/policy:explain-denial` |

| Field (req) | Type | JSON | Description |
|---|---|---|---|
| `run_id` | string | `runId` | Optional run scope |
| `denied_destination` | string | `deniedDestination` | Optional destination filter |

Response includes matching rule, rationale, `policy_digest`, `blocking_rule_id`, `next_action` always oriented to `review_policy_patch`.

---

### 7.6 RecommendPolicyPatch

| | |
|---|---|
| **RPC** | `RecommendPolicyPatch(RecommendPolicyPatchRequest) → RecommendPolicyPatchResponse` |
| **HTTP** | `POST /v1/control/policy:recommend-patch` |

| Field (req) | Type | JSON | Description |
|---|---|---|---|
| `desired_behavior` | string | `desiredBehavior` | What should be allowed |

Returns proposed YAML patch, risk level, affected destinations, credential ids, and `ConfirmationRequirement` (trust-boundary confirmation protocol). Does not apply the patch.

---

### 7.7 GetRunTimeline

| | |
|---|---|
| **RPC** | `GetRunTimeline(GetRunTimelineRequest) → GetRunTimelineResponse` |
| **HTTP** | `GET /v1/control/runs/{run_id}/timeline` |

Chronological `TimelineEvent` list from audit (`timestamp`, `type`, `description`, `data`).

---

### 7.8 NextAction

| | |
|---|---|
| **RPC** | `NextAction(NextActionRequest) → NextActionResponse` |
| **HTTP** | `POST /v1/control/next-action` |

| Field (req) | Type | JSON | Description |
|---|---|---|---|
| `context` | string | `context` | Free-form / structured context (may encode confirmation decisions) |

Returns recommended `action` / `next_action`, rationale, evidence, optional confirmation. Can complete pending confirmation approvals when context encodes a valid decision.

---

## 8. RPC methods — Cron

### 8.1 CronAdd

| HTTP | `POST /v1/control/cron` |

Requires `agent_name`, `expr`. Optional: `agent_version`, `timezone`, `missed_run_policy`, `concurrency_policy`, `payload`, `content_type`.  
Fails with `FAILED_PRECONDITION` if cron scheduler unavailable.

### 8.2 CronList

| HTTP | `GET /v1/control/cron` |

Returns all `CronScheduleInfo` records.

### 8.3 CronRemove

| HTTP | `DELETE /v1/control/cron/{schedule_id}` |

Requires `schedule_id`. Response `{ "removed": true }`.

#### `CronScheduleInfo`

| Field | Type | JSON |
|---|---|---|
| `schedule_id` | string | `scheduleId` |
| `expr` | string | `expr` |
| `agent_name` | string | `agentName` |
| `agent_version` | string | `agentVersion` |
| `timezone` | string | `timezone` |
| `missed_run_policy` | string | `missedRunPolicy` |
| `concurrency_policy` | string | `concurrencyPolicy` |
| `payload` | bytes | `payload` |
| `content_type` | string | `contentType` |

---

## 9. RPC methods — Deployments & aliases (B26 state — enabled)

These mutate durable routed stores when initialized. Proto comments that say “FEATURE_NOT_ENABLED until B28/B35” apply to **workflow runtime control**, not basic deployment CRUD.

### 9.1 CreateDeployment

| HTTP | `POST /v1/control/deployments` |

Requires `package_name`, `package_version`, `bundle_digest`. Optional digests, `max_concurrent_runs` (default 1), nested package digests, `actor_identity` (default `"local"`).

### 9.2 GetDeployment

| HTTP | `GET /v1/control/deployments/{deployment_id}` |

### 9.3 ListDeployments

| HTTP | `GET /v1/control/deployments` |

Optional `package_name`, pagination.

### 9.4 DeactivateDeployment

| HTTP | `POST /v1/control/deployments/{deployment_id}:deactivate` |

Fields: `preserve_active_runs`, `actor_identity`, `idempotency_key`, `authority_scope`.

### 9.5 CreateDeploymentAlias

| HTTP | `POST /v1/control/deployment-aliases` |

Requires `alias`, `target_deployment_id`. Existing alias → `ALREADY_EXISTS` (use CAS).

### 9.6 GetDeploymentAlias / ListDeploymentAliases

| HTTP | `GET /v1/control/deployment-aliases/{alias}` |
| HTTP | `GET /v1/control/deployment-aliases` |

### 9.7 CasDeploymentAlias

| HTTP | `POST /v1/control/deployment-aliases/{alias}:cas` |

Compare-and-swap with `expected_generation`.

#### `DeploymentRecord` (response shape)

| Field | Type | JSON | Description |
|---|---|---|---|
| `schema_version` | string | `schemaVersion` | Record schema |
| `deployment_id` | string | `deploymentId` | ID |
| `package_name` / `package_version` | string | camelCase | Package identity |
| `generation` | int64 | `generation` | CAS generation |
| `status` | string | `status` | `ACTIVE` / `INACTIVE` |
| `max_concurrent_runs` | int32 | `maxConcurrentRuns` | Cap |
| `bundle_digest` … `provenance_digest` | string | digests | Artifact locks |
| `nested_package_digests` | map | nested | Nested locks |
| `created_at` / `activated_at` / `deactivated_at` | Timestamp | … | Lifecycle |
| `created_by` | string | `createdBy` | Actor |

#### `DeploymentAliasRecord`

`schema_version`, `alias`, `target_deployment_id`, `target_version`, `generation`, `updated_at`, `updated_by`.

Common errors: `FailedPrecondition` (`routed store not initialized`), `InvalidArgument`, `NotFound`, `AlreadyExists`.

```json
// CreateDeployment request
{
  "packageName": "weather-agent",
  "packageVersion": "1.0.0",
  "bundleDigest": "sha256:…",
  "policyDigest": "sha256:…",
  "maxConcurrentRuns": 2,
  "actorIdentity": "cli@local"
}
```

---

## 10. RPC methods — Invocation & run status (B26/B30)

### 10.1 InvokeDeployment

| HTTP | `POST /v1/control/deployments:invoke` |

**Fully implemented admission path** against `localStore.AdmitInvocation`.

Requires: `deployment_ref`, `idempotency_key`, `caller_identity`.  
Optional: `input_json`, initial duration/lease ms, `initial_max_cost_usd_decimal` (decimal string).

Returns gRPC OK with structured body:

| Field | Type | Description |
|---|---|---|
| `outcome` / `outcome_name` | AdmissionOutcomeCode / string | ACCEPTED, IDEMPOTENT_REPLAY, conflicts, … |
| `invocation_id`, `workflow_id`, `run_id` | string | Hierarchy IDs |
| `requested_deployment_ref`, `resolved_deployment_*` | string | Resolution |
| `ceilings` | AbsoluteCeilingsSnapshot | Original/current spend & duration |
| `error` | TypedControlError | Set on failure outcomes |
| `admitted_at` | Timestamp | Admission time |

**Admission failures are not always gRPC errors** — branch on `outcome` / `error.code`.

```json
// Success
{
  "outcome": "ADMISSION_OUTCOME_ACCEPTED",
  "outcomeName": "ADMISSION_OUTCOME_ACCEPTED",
  "invocationId": "inv-…",
  "workflowId": "wf-…",
  "runId": "run-…",
  "requestedDeploymentRef": "prod",
  "resolvedDeploymentId": "dep-…",
  "ceilings": {
    "originalMaxActiveDurationMs": 600000,
    "currentMaxActiveDurationMs": 600000,
    "authorityGeneration": 1
  }
}

// Conflict (still HTTP/gRPC success with error payload)
{
  "outcome": "ADMISSION_OUTCOME_IDEMPOTENCY_CONFLICT",
  "error": {
    "code": "TYPED_CONTROL_ERROR_CHANGED_IDEMPOTENCY_PAYLOAD",
    "codeName": "CHANGED_IDEMPOTENCY_PAYLOAD",
    "message": "…"
  }
}
```

### 10.2 GetInvocation

| HTTP | `GET /v1/control/invocations/{invocation_id}` |

Returns `InvocationRecord` or typed error / `NOT_FOUND`.

### 10.3 GetRunStatus

| HTTP | `GET /v1/control/runs/{run_id}/status` |

Durable run status: `run_id`, `workflow_id`, `status`, `run_kind`, `generation`.

### 10.4 GetRunResult

| HTTP | `GET /v1/control/runs/{run_id}/result` |

Returns terminal status envelope; **result content / attempt_id may be empty** until supervisor/result writers (T05/T08) populate them.

---

## 11. RPC methods — Workflow control (mostly gated)

| RPC | HTTP | Implementation status |
|---|---|---|
| `CreateWorkflow` | `POST /v1/control/workflows` | Validates `idempotency_key`; returns `FEATURE_NOT_ENABLED` in body (kind-dependent block) |
| `GetWorkflow` | `GET /v1/control/workflows/{workflow_id}` | **Read enabled** when store init; else typed not-enabled |
| `CancelWorkflow` | `POST /v1/control/workflows/{workflow_id}:cancel` | Validates ids; **B35** feature-not-enabled body |
| `SetWorkflowDesiredState` | `POST /v1/control/workflows/{workflow_id}:desired-state` | **B35** gated (pause/resume/etc.) |
| `RestartWorkflow` | `POST /v1/control/workflows/{source_workflow_id}:restart` | **B35** gated |
| `AmendLimits` | `POST /v1/control/workflows/{workflow_id}:amend-limits` | Requires `reason` + `idempotency_key`; **B35** gated (`runs:amend_limits` intended) |
| `GetWorkflowGraph` | `GET /v1/control/workflows/{workflow_id}/graph` | **Read enabled**: workflow + nodes/services/handoffs/child batches |

Gated responses look like:

```json
{
  "error": {
    "code": "TYPED_CONTROL_ERROR_FEATURE_NOT_ENABLED",
    "codeName": "routed_run_control_not_enabled",
    "message": "workflow_control is not enabled until B35",
    "details": {
      "feature": "workflow_control",
      "enabled_in_block": "B35"
    }
  }
}
```

#### Key request fields

**CreateWorkflowRequest:** `workflow_kind` (`standalone`|`pipeline`|`parent_child`), `deployment_ref`, `input_json`, `idempotency_key`, `caller_identity`, duration/lease/spend ceilings.

**SetWorkflowDesiredStateRequest:** `workflow_id`, `desired_command` (`ControlCommand`), `expected_generation`, `actor_identity`, `idempotency_key`, `authority_scope`, optional `target_deployment_ref`, `recovery_action`.

**AmendLimitsRequest:** `workflow_id`, `expected_authority_generation`, increase-only `new_max_active_duration_ms` / `new_current_attempt_lease_ms` / `new_max_llm_spend_decimal` (0/empty = unchanged), `reason`, `idempotency_key`, `actor_identity`, `authority_scope`.

---

## 12. Shared message catalog

### TypedControlError

| Field | Type | JSON | Description |
|---|---|---|---|
| `code` | TypedControlErrorCode | `code` | Enum |
| `code_name` | string | `codeName` | Stable machine string |
| `message` | string | `message` | Human detail (not sole control signal) |
| `run_id` / `workflow_id` / `invocation_id` / `deployment_id` | string | camelCase | Context |
| `idempotency_key` | string | `idempotencyKey` | When relevant |
| `required_scope` | string | `requiredScope` | e.g. `runs:control` |
| `details` | map\<string,string\> | `details` | Extra pairs |

### AbsoluteCeilingsSnapshot

Duration fields are **int64 ms**; spend fields are **decimal strings** (never float for money).

| Field | Type | JSON |
|---|---|---|
| `original_max_active_duration_ms` | int64 | `originalMaxActiveDurationMs` |
| `current_max_active_duration_ms` | int64 | `currentMaxActiveDurationMs` |
| `consumed_active_duration_ms` | int64 | `consumedActiveDurationMs` |
| `reserved_active_duration_ms` | int64 | `reservedActiveDurationMs` |
| `original_attempt_lease_ms` / `current_attempt_lease_ms` | int64 | camelCase |
| `original_max_llm_spend_decimal` / `current_…` / `consumed_…` / `reserved_…` | string | camelCase |
| `authority_generation` | int64 | `authorityGeneration` |

### Operator shared types

**OperatorIssue:** `category`, `message`, `evidence_refs[]`, `next_action`  
**EvidenceRef:** `type` (`audit_seq`|`run_id`|`policy_rule`|`span`|`log`|`redacted_excerpt`|`verification`), `ref`, `detail`  
**RedactedExcerpt:** `source`, `start_line`, `end_line`, `content`  
**ConfirmationRequirement:** `requires_confirmation`, `confirmation_id`, `risk_level` (`low`|`medium`|`high`), `rationale`, `affected_destinations[]`, `credential_ids[]`, `evidence_refs[]`

### Attempt / progress types (B26 portable report)

- `ProgressSummary` — model/tool call counters  
- `CheckpointSummary` — checkpoint metadata (**no host paths**)  
- `ArtifactRef` — logical artifact refs (**never host/container paths**)  
- `TimeBudgetSummary` — int64 ms budgets  
- `LLMBudgetSummary` — token ints + decimal spend strings  
- `RouteDecision` — per model-call routing record  
- `AttemptReport` — aggregates the above + `recommended_actions`, `evidence_refs`, timestamps  

### Workflow graph types

`WorkflowRecord`, `WorkflowNodeStatus`, `ServiceBindingStatus`, `HandoffMetadata`, `ChildBatchStatus`, `ChildResult` — see proto field comments; used by GetWorkflow / GetWorkflowGraph.

### TimeRange

`start`, `end` as `google.protobuf.Timestamp`.

---

## 13. Security considerations

1. **Local socket trust boundary.** Control is root-equivalent for the AgentPaaS home: pack, run, export, policy, audit. Protect `~/.agentpaas` (`0700`) and `daemon.sock` (`0600`). Do not relocate the socket to a world-writable directory.
2. **No application-layer auth on Control today.** Multi-user machines need OS user isolation.
3. **Egress fail-closed.** Runs compile policy into gateway network authorization; default-deny when no agent policy. Topology isolation (internal net without internet) is the primary control; see [how-enforcement-works.md](how-enforcement-works.md).
4. **Credentials are brokered.** Raw secrets are written to a per-run host file mounted **read-only into the gateway**, not the agent. Invoke payloads carry credential **metadata** (ids/headers), not values. See [secrets.md](secrets.md).
5. **Missing credentials fail before Docker** (`validateCredentialsExist`).
6. **Export secret gate + confirmation.** `confirmed=true` required; secret findings block export.
7. **Publisher / package signing.** Pack and export use identity keystores; install/run verify digests and signatures ([trust-model.md](trust-model.md)).
8. **Concurrent run limit** (3) reduces resource exhaustion.
9. **Docker vulnerability gate** unless explicitly overridden.
10. **Authority separation.** `runs:amend_limits` must never be on trigger API keys. AmendLimits is feature-gated but proto-documented as high privilege.
11. **Operator redaction.** Explain/summarize paths return redacted excerpts; still treat audit payloads as sensitive.
12. **Audit chain.** Prefer `AuditQuery.chain_verification` and signed checkpoints for integrity; see [audit-export.md](audit-export.md).
13. **Path safety.** ValidateAgentProject rejects system paths and policy symlinks; pack/export resolve absolute project paths.
14. **Typed errors over string matching.** Control clients must branch on `TypedControlErrorCode` / `AdmissionOutcomeCode`, not English messages.

---

## 14. Proto vs implementation discrepancies

| Item | Proto / docs intent | Implementation reality |
|---|---|---|
| Control REST gateway | Full `/v1/control/*` annotations | Daemon serves **UDS gRPC only**; REST requires external gateway |
| Control auth | Status codes include `PERMISSION_DENIED` | **FS permissions** only; no Bearer interceptor |
| `Doctor` | System diagnostics | **Stub** (version + skeleton) |
| `SecretSet` | “creates or updates a secret” | **No value field; does not write** Keychain material |
| `LogsRequest.run_id` | Optional | **Required** by handler |
| `LogsRequest.min_level` | Filter | **Ignored** (all lines as `info`) |
| B26 service comment “FEATURE_NOT_ENABLED until B28/B35” | Blanket on B26 RPCs | **Deployment CRUD + InvokeDeployment + reads are live**; workflow mutations gated |
| `Run` + `deployment_ref` / continuation fields | Present on RunRequest | **Rejected** with feature-not-enabled; use `InvokeDeployment` |
| `Run.idempotency_key` | Documented for deployments | **Ignored** on legacy Run |
| `GetRunResult` content | Structured result + artifacts | Often **empty envelope** until later blocks |
| `maxConcurrentRuns` on deployments | Per-deployment field | Legacy Run uses global const **3** |
| SecretGrant/Revoke | Run-scoped grants | **In-memory only**; not durable across restart |
| Operator schema | Response field | Constant **`1.1.0`** (`internal/operator`) |

---

## 15. Quick method index

| # | RPC | HTTP | Group |
|---|---|---|---|
| 1 | Pack | POST `/v1/control/pack` | Lifecycle |
| 2 | ExportPreview | POST `/v1/control/export:preview` | Lifecycle |
| 3 | Export | POST `/v1/control/export` | Lifecycle |
| 4 | Run | POST `/v1/control/run` | Lifecycle |
| 5 | Stop | POST `/v1/control/stop` | Lifecycle |
| 6 | Logs | GET `/v1/control/logs` | Lifecycle |
| 7 | PolicyApply | POST `/v1/control/policy:apply` | Policy |
| 8 | SecretSet | POST `/v1/control/secrets` | Secrets |
| 9 | SecretGrant | POST `/v1/control/secrets:grant` | Secrets |
| 10 | SecretRevoke | POST `/v1/control/secrets:revoke` | Secrets |
| 11 | AuditQuery | POST `/v1/control/audit:query` | Audit |
| 12 | AuditExport | POST `/v1/control/audit:export` | Audit |
| 13 | Doctor | GET `/v1/control/doctor` | Operator |
| 14 | ValidateAgentProject | POST `/v1/control/validate` | Operator |
| 15 | SummarizeRun | POST `/v1/control/runs/{run_id}:summarize` | Operator |
| 16 | ExplainFailure | POST `/v1/control/runs/{run_id}:explain-failure` | Operator |
| 17 | ExplainPolicyDenial | POST `/v1/control/policy:explain-denial` | Operator |
| 18 | RecommendPolicyPatch | POST `/v1/control/policy:recommend-patch` | Operator |
| 19 | GetRunTimeline | GET `/v1/control/runs/{run_id}/timeline` | Operator |
| 20 | NextAction | POST `/v1/control/next-action` | Operator |
| 21 | CronAdd | POST `/v1/control/cron` | Cron |
| 22 | CronList | GET `/v1/control/cron` | Cron |
| 23 | CronRemove | DELETE `/v1/control/cron/{schedule_id}` | Cron |
| 24 | ListRuns | GET `/v1/control/runs` | Lifecycle |
| 25 | CreateDeployment | POST `/v1/control/deployments` | Deploy |
| 26 | GetDeployment | GET `/v1/control/deployments/{deployment_id}` | Deploy |
| 27 | ListDeployments | GET `/v1/control/deployments` | Deploy |
| 28 | DeactivateDeployment | POST `/v1/control/deployments/{deployment_id}:deactivate` | Deploy |
| 29 | CreateDeploymentAlias | POST `/v1/control/deployment-aliases` | Deploy |
| 30 | GetDeploymentAlias | GET `/v1/control/deployment-aliases/{alias}` | Deploy |
| 31 | ListDeploymentAliases | GET `/v1/control/deployment-aliases` | Deploy |
| 32 | CasDeploymentAlias | POST `/v1/control/deployment-aliases/{alias}:cas` | Deploy |
| 33 | InvokeDeployment | POST `/v1/control/deployments:invoke` | Invoke |
| 34 | GetInvocation | GET `/v1/control/invocations/{invocation_id}` | Invoke |
| 35 | GetRunStatus | GET `/v1/control/runs/{run_id}/status` | Invoke |
| 36 | GetRunResult | GET `/v1/control/runs/{run_id}/result` | Invoke |
| 37 | CreateWorkflow | POST `/v1/control/workflows` | Workflow |
| 38 | GetWorkflow | GET `/v1/control/workflows/{workflow_id}` | Workflow |
| 39 | CancelWorkflow | POST `/v1/control/workflows/{workflow_id}:cancel` | Workflow |
| 40 | SetWorkflowDesiredState | POST `/v1/control/workflows/{workflow_id}:desired-state` | Workflow |
| 41 | RestartWorkflow | POST `/v1/control/workflows/{source_workflow_id}:restart` | Workflow |
| 42 | AmendLimits | POST `/v1/control/workflows/{workflow_id}:amend-limits` | Workflow |
| 43 | GetWorkflowGraph | GET `/v1/control/workflows/{workflow_id}/graph` | Workflow |

---

## 16. Related docs

- [api-reference-trigger.md](api-reference-trigger.md) — external invoke API  
- [how-enforcement-works.md](how-enforcement-works.md) — gateway topology  
- [policy-reference.md](policy-reference.md) — policy.yaml  
- [secrets.md](secrets.md) — credential brokering  
- [trust-model.md](trust-model.md) — publisher signatures  
- [threat-model.md](threat-model.md) — threats  
- [audit-export.md](audit-export.md) — audit export formats  
- [bundle-format.md](bundle-format.md) — export bundles  
