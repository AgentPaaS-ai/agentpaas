# Golden Enterprise Demo — Governed Deal Rescue Agent

**Status:** TARGET FLOW — blocked on B27 capability closure
**Date:** 2026-07-10
**Execution gate:** `docs/execution/blocks/b27-summary.md`

> **Planned-command notice:** `auth`, `profile`, `approvals`,
> `installed bind-credential`, multi-channel LLM configuration, portable MCP
> components, and the full `verify` report flow below are B26/B27 targets.
> They are written as the desired demo contract and must not be presented as
> commands available in the current release until their block gates pass.

## 1. Demo thesis

The demo proves more than “an agent can call APIs.” It proves that a useful,
multi-system enterprise agent can be built and tested by one employee, signed
and shared without credentials, independently verified by a coworker, rebound
to the coworker’s own accounts and permissions, and run with consequential
writes still under explicit human control.

The audience should be able to repeat this sentence after the demo:

> Alice shared the agent, not her authority. Bob verified Alice’s signed
> artifact, approved its declared capabilities, connected his own accounts,
> and every read, model call, tool call, approval, and write was enforced and
> audited by AgentPaaS.

## 2. Scenario: Deal Rescue Agent

A sales operations engineer wants a governed agent that analyzes a late-stage
Salesforce opportunity, compares it with the current proposal in Google Drive,
produces an evidence-backed deal-risk assessment using two LLMs, validates the
assessment with a bundled deterministic MCP server, and—only after a human
approval—creates a Salesforce follow-up task and posts the action plan to a
Slack deal room.

This is deliberately a draft-then-act workflow. A realistic enterprise agent
must distinguish analysis from consequential mutation.

### Personas

| Persona | Demo role | Local authority |
|---|---|---|
| Alice | Builder and publisher | Alice’s Salesforce user, Drive file, Slack workspace/channel, and LLM keys |
| Bob | Coworker and receiver | Bob’s separate OAuth grants, Drive file, Slack channel, and LLM keys |
| Security reviewer | Observer | Reads policy, scopes, provenance, approval records, and signed evidence; receives no secrets |

Alice and Bob may use the same corporate SaaS tenants, but they must use
different user grants and separate local AgentPaaS homes/Keychain namespaces.
The demo is invalid if Bob reuses Alice’s tokens or secret-store entries.

## 3. Enterprise components

### SaaS systems

1. **Salesforce sandbox / Developer Edition**
   - OAuth authorization-code flow with PKCE through an External Client App.
   - Read: Opportunity, Account, selected Contacts, open Tasks/Events.
   - Write after approval: create one Task and optionally update
     `Opportunity.NextStep`; do not demonstrate broad arbitrary object writes.
   - The Salesforce user/permission set restricts objects and fields in
     addition to AgentPaaS route/path policy.

2. **Google Drive test account**
   - OAuth with the narrow `drive.file` scope where feasible.
   - Read one proposal file explicitly selected/created for the app.
   - Optional approved write: create one action-plan document owned by the
     current receiver.
   - Do not request full `drive` or `drive.readonly` access for the golden
     path unless a documented product requirement makes it unavoidable.

3. **Slack development workspace**
   - Slack OAuth v2, separate installation/grant for Alice and Bob.
   - `chat:write` only for the golden path.
   - Write after approval: one accessible summary message to a configured
     deal-room channel. Reading channel history is out of scope unless a
     later version demonstrates a separate need and scope review.

### LLM channels

Two named channels with separate providers, credentials, budgets, rate limits,
provider locks, and audit attribution:

| Channel | Suggested role | Data allowed |
|---|---|---|
| `extractor` | Low-cost structured extraction from already-minimized CRM/proposal fields | Redacted, field-limited deal evidence |
| `analyst` | Higher-quality risk reasoning and action-plan draft | MCP-normalized/redacted evidence only |

The exact providers can change. The security point is that each channel is an
independently governed route; `agent.http()` must not be usable to bypass its
LLM-specific gates.

### Bundled MCP server

Bundle a local, deterministic component named `deal-controls-mcp`. It has no
network egress and exposes only:

- `normalize_opportunity` — canonicalizes selected Salesforce fields;
- `redact_customer_data` — removes email, phone, and configured sensitive
  fields before any LLM call;
- `score_completeness` — calculates deterministic missing-field/risk signals;
- `validate_action_plan` — checks that the final draft cites input evidence
  and contains an owner and due date for every action.

Do not make the bundled MCP server a disguised SaaS proxy. Keeping it local
lets the demo separately prove signed component portability, per-tool access,
no ambient egress, and auditable tool calls.

## 4. Target project layout

```text
deal-rescue-agent/
├── agent.yaml
├── policy.yaml
├── main.py
├── requirements.txt
├── uv.lock
├── mcp/
│   └── deal-controls/
│       ├── component.yaml
│       ├── server.py
│       ├── requirements.txt
│       └── uv.lock
├── tests/
│   ├── test_workflow.py
│   ├── test_no_secret_visibility.py
│   ├── test_action_approval.py
│   └── fixtures/
└── README.md
```

The B27 target schema is expected to express named LLM channels, OAuth
requirements and scopes, signed MCP components, receiver-local resource
bindings, data-flow disclosures, and approval-required actions. Any YAML shown
in the demo before B27 is illustrative, not a promise that the current schema
already supports it.

## 5. Workflow contract

### Stage A — Read and minimize

Input:

```json
{
  "opportunity_id": "receiver-local Salesforce Opportunity ID",
  "proposal_file_id": "receiver-authorized Drive file ID",
  "dry_run": true
}
```

The agent:

1. GETs a path-constrained set of Salesforce Opportunity fields.
2. GETs related Account, selected Contact, and open activity fields.
3. GETs the explicitly authorized proposal file from Drive.
4. Sends the selected fields through `deal-controls-mcp` normalization and
   redaction.
5. Stores only hashes and redacted evidence references in audit; it does not
   persist raw customer payloads in the canonical audit chain.

### Stage B — Analyze through two governed LLM channels

1. `extractor` returns a strict structured evidence object.
2. Agent validates that object locally.
3. `analyst` produces a risk assessment and proposed actions from the redacted
   evidence.
4. `deal-controls-mcp.validate_action_plan` rejects unsupported claims or
   actions without owner/due-date fields.
5. The dry-run response contains the proposed Salesforce mutation and Slack
   message, but performs no write.

### Stage C — Human approval and execute

AgentPaaS, not agent code, creates an approval request bound to:

- run ID and installed agent digest;
- publisher and accepted policy digest;
- SaaS route, method, normalized path, and receiver account binding;
- hash of the exact outbound body;
- expiry and one-time-use nonce.

The user reviews a redacted action card and approves in the terminal. On
approval, AgentPaaS permits exactly the bound actions:

1. POST one Salesforce Task (and optionally PATCH the approved `NextStep`).
2. POST one Slack `chat.postMessage` body to the bound channel.
3. Optionally create one Drive action-plan file if that action was displayed
   and approved.

Changing the body, destination, object, channel, or file after approval must
invalidate the grant. Replaying the approval must fail.

### Stage D — Evidence

The final result links:

- Salesforce record/task IDs;
- Slack message timestamp/channel ID;
- Drive output file ID if used;
- LLM channel/provider/model/token/cost records;
- MCP tool names and input/output hashes;
- action approval ID and approved body hashes;
- policy, bundle, image, source, component, and publisher digests;
- signed Agent Approval Pack and audit export.

## 6. Demo environment preparation

Use non-production tenants and synthetic customer data only.

1. Create a Salesforce sandbox/Developer Edition dataset:
   - Account: `Northstar Manufacturing`;
   - Opportunity: `Northstar Renewal`, late stage, close date within 14 days;
   - deliberately missing next step and executive sponsor;
   - one open legal task and one stale customer activity.
2. Create Alice and Bob Salesforce users with different permission sets.
3. Configure a Salesforce External Client App for authorization-code + PKCE.
   Salesforce recommends External Client Apps for new integrations and can
   issue refresh tokens for long-lived API access.
4. Create one proposal document in each user’s Google Drive test area. Make
   the text intentionally disagree with one Salesforce amount/date so the
   agent has a real evidence conflict to identify.
5. Create `#deal-northstar-alice` and `#deal-northstar-bob` in a Slack
   development workspace. Install the app separately for both demo users.
6. Prepare two AgentPaaS homes and confirm their secret stores do not share
   service namespaces:

```bash
export ALICE_HOME="$HOME/.agentpaas-demo-alice"
export BOB_HOME="$HOME/.agentpaas-demo-bob"
```

7. Prepare distinct LLM credentials for the two users. Sentinel-tag test
   credentials so any accidental cross-user transfer is detectable without
   printing their values.
8. Record provider account, tenant/workspace, granted scopes, and token expiry
   metadata through AgentPaaS; never paste tokens into an agent conversation.

Authoritative provider setup references:

- Salesforce OAuth web server flow:
  https://help.salesforce.com/s/articleView?id=xcloud.remoteaccess_oauth_web_server_flow_ca.htm&type=5
- Slack OAuth v2:
  https://docs.slack.dev/authentication/installing-with-oauth/
- Google Drive scopes:
  https://developers.google.com/workspace/drive/api/guides/api-specific-auth

## 7. Live demo — Alice builds and proves the agent

### A1. Initialize publisher identity

```bash
agentpaas --home "$ALICE_HOME" identity init --name alice-sales-platform
agentpaas --home "$ALICE_HOME" identity show
```

Display the grouped fingerprint. Do not display any credential.

### A2. Connect Alice’s accounts outside the LLM conversation

Target B27 commands:

```bash
agentpaas --home "$ALICE_HOME" auth connect salesforce-prod
agentpaas --home "$ALICE_HOME" auth connect google-drive
agentpaas --home "$ALICE_HOME" auth connect slack-deal-room
agentpaas --home "$ALICE_HOME" secret add llm-extractor-alice
agentpaas --home "$ALICE_HOME" secret add llm-analyst-alice
```

Browser OAuth callbacks and token exchange remain local. Show account/tenant,
scope, and expiry metadata only:

```bash
agentpaas --home "$ALICE_HOME" auth list --json
```

### A3. Bind Alice’s receiver-local resources

```bash
agentpaas --home "$ALICE_HOME" profile create deal-rescue-alice
agentpaas --home "$ALICE_HOME" profile bind deal-rescue-alice \
  salesforce=salesforce-prod \
  drive=google-drive \
  slack=slack-deal-room \
  slack_channel=C_ALICE_DEAL_ROOM
```

The profile is local state. It is not added to the source bundle and cannot
widen the signed domains, methods, paths, scopes, or tool set.

### A4. Validate and pack

```bash
agentpaas validate ./deal-rescue-agent --json
agentpaas pack ./deal-rescue-agent --json
agentpaas verify ./deal-rescue-agent \
  --report /tmp/alice-deal-rescue-report.html \
  --evidence /tmp/alice-deal-rescue-evidence.agentpaas
```

Show:

- three SaaS destinations and exact method/path constraints;
- OAuth scope requirements, not tokens;
- two separately governed LLM channels;
- signed `deal-controls-mcp` component and allowed tools;
- data-flow disclosure showing which minimized fields reach each LLM;
- write actions marked `approval_required`;
- SBOM, source/policy/component digests, and publisher fingerprint.

### A5. Run mandatory security breaks before the happy path

Run and visibly prove each denial:

1. Agent attempts `https://attacker.example` → network denied.
2. Agent asks for a raw credential or prints its payload → sentinel absent.
3. Agent uses the Salesforce token on Slack → route/credential binding denied.
4. Agent accesses an undeclared Salesforce REST path/object → path denied.
5. Agent calls an undeclared MCP tool → tool denied.
6. Agent routes the `analyst` request through `extractor` or direct HTTP →
   channel/provider gate denied.
7. Agent attempts Salesforce/Slack POST without approval → action denied.
8. Expired OAuth access token → refreshed from Alice’s local refresh grant;
   refresh token remains invisible and the refresh event is audited.

### A6. Execute Alice’s dry run

```bash
agentpaas --home "$ALICE_HOME" run deal-rescue-agent \
  --profile deal-rescue-alice --json
agentpaas --home "$ALICE_HOME" trigger invoke deal-rescue-agent \
  --payload alice-opportunity.json --idempotency-key demo-alice-001 --wait
```

Show the evidence conflict, risk assessment, action plan, proposed Salesforce
Task, proposed Slack message, and a pending approval. Confirm that Salesforce
and Slack are unchanged.

### A7. Approve and execute Alice’s exact actions

```bash
agentpaas --home "$ALICE_HOME" approvals show <approval-id>
agentpaas --home "$ALICE_HOME" approvals approve <approval-id>
```

Verify the Salesforce Task and Slack message exist once. Replay the same
approval and idempotency key; prove no duplicate write occurs.

### A8. Export Alice’s signed agent

```bash
agentpaas export ./deal-rescue-agent \
  -o /tmp/deal-rescue-agent-1.0.0.agentpaas --yes
agentpaas bundle inspect /tmp/deal-rescue-agent-1.0.0.agentpaas
```

Before transfer, scan the exact bundle and show:

- Alice’s OAuth access/refresh tokens absent;
- Alice’s LLM keys absent;
- Alice’s Slack channel and receiver resource profile absent;
- signed OAuth requirements/scopes present;
- signed MCP component source/image digest present;
- signed policy and data-flow/approval declarations present.

Transfer the single bundle file to Bob and communicate Alice’s publisher
fingerprint over a separate channel.

## 8. Live demo — Bob verifies, rebinds, and runs

### B1. Inspect before trust

```bash
agentpaas --home "$BOB_HOME" bundle inspect \
  /tmp/deal-rescue-agent-1.0.0.agentpaas
```

Bob reviews publisher fingerprint, provenance, source/SBOM, three SaaS
destinations, OAuth scopes, two LLM routes, MCP tools, data flows, and
approval-required writes. The UI says “signed by Alice and unmodified,” never
“safe.”

### B2. Negative tamper proof

Flip one byte in a copy. Install must fail before trust or policy consent and
must write nothing to Bob’s state.

### B3. Install and establish trust

```bash
agentpaas --home "$BOB_HOME" install \
  /tmp/deal-rescue-agent-1.0.0.agentpaas
```

Bob verifies Alice’s fingerprint out of band, types the trust confirmation,
and approves the exact signed policy. Hermes/Codex may explain the card but
cannot perform these confirmations.

### B4. Bind Bob’s own authority

```bash
agentpaas --home "$BOB_HOME" auth connect salesforce-prod
agentpaas --home "$BOB_HOME" auth connect google-drive
agentpaas --home "$BOB_HOME" auth connect slack-deal-room
agentpaas --home "$BOB_HOME" secret add llm-extractor-bob
agentpaas --home "$BOB_HOME" secret add llm-analyst-bob
agentpaas --home "$BOB_HOME" installed bind-credential <installed-ref> \
  salesforce-oauth=salesforce-prod \
  drive-oauth=google-drive \
  slack-oauth=slack-deal-room \
  llm-extractor=llm-extractor-bob \
  llm-analyst=llm-analyst-bob
```

AgentPaaS verifies that Bob granted at least—but not more than silently
accepted—the declared scopes. Missing or mismatched tenant/scope bindings fail
before runtime resources are created.

### B5. Bind Bob’s local resources

```bash
agentpaas --home "$BOB_HOME" profile create deal-rescue-bob
agentpaas --home "$BOB_HOME" profile bind deal-rescue-bob \
  salesforce=salesforce-prod \
  drive=google-drive \
  slack=slack-deal-room \
  slack_channel=C_BOB_DEAL_ROOM
```

Show that Alice’s Salesforce IDs, Drive IDs, and Slack channel are not used.

### B6. Run Bob’s dry run

```bash
agentpaas --home "$BOB_HOME" run <installed-ref> \
  --profile deal-rescue-bob --json
agentpaas --home "$BOB_HOME" trigger invoke <installed-ref> \
  --payload bob-opportunity.json --idempotency-key demo-bob-001 --wait
```

Prove all reads use Bob’s accounts, LLM calls use Bob’s keys, the MCP component
matches Alice’s signed digest, and no write occurs before Bob approves it.

### B7. Approve Bob’s actions

Bob reviews and approves the exact action card. Verify the resulting Task and
Slack message appear under Bob’s authorized identities and configured
resources—not Alice’s.

### B8. Compare signed evidence

Export Bob’s Agent Approval Pack and audit bundle. Show:

- same publisher/source/policy/component identity as Alice’s bundle;
- Bob’s distinct receiver/install/run/workload identity;
- Bob’s account/tenant identifiers and scopes, redacted to non-secret metadata;
- different local credential bindings with no raw values;
- separate LLM provider/account attribution and budgets;
- exact approval and SaaS write receipts.

## 9. Final adversary matrix

| Attack | Expected evidence |
|---|---|
| Tampered bundle | Rejected before consent; no Bob state |
| Publisher credential smuggled in source/bundle | Export fails; sentinel absent |
| Bob maps undeclared sixth credential | Mapping rejected |
| OAuth grant lacks required scope | Run fails before Docker resources |
| OAuth token is for wrong tenant/profile | Binding or preflight fails closed |
| Agent calls undeclared SaaS path/object | Gateway denies and audits rule/path |
| Agent uses Salesforce credential on Slack | Route-scoped credential denial |
| Agent bypasses named LLM channel via HTTP | Provider/channel gate denial |
| Agent calls undeclared MCP tool | MCP denial with input hash |
| Agent posts before approval | Consequential-action denial |
| Agent mutates body after approval | Approval body-hash mismatch denial |
| Agent replays action/approval | No duplicate SaaS mutation |
| Alice’s profile/resource IDs appear at Bob | Test fails; demo is invalid |

## 10. Recording script (12–15 minutes)

1. **Problem (45s):** AI-built agent needs CRM, documents, Slack, two models,
   and tools; sharing code must not share authority.
2. **Alice’s policy/evidence (90s):** show scopes, routes, MCP tools, data
   flows, approval-required writes, and no secret values.
3. **Security breaks (90s):** blocked exfiltration, wrong credential route,
   undeclared path/tool, and unapproved write.
4. **Alice happy path (2m):** read → MCP → two LLMs → draft → approve → one
   Salesforce Task and Slack message.
5. **Package (60s):** signed bundle, provenance, SBOM, component digest,
   sentinel-secret scan.
6. **Bob inspection/trust (90s):** offline inspect, fingerprint verification,
   policy/scope consent.
7. **Bob rebinding (90s):** separate OAuth grants, LLM keys, and resource
   profile; Alice’s authority absent.
8. **Bob happy path (2m):** same signed agent, Bob’s data and permissions,
   separate approval and write receipts.
9. **Close (60s):** compare Alice/Bob evidence and audit identities; repeat
   “the agent moved, the authority did not.”

## 11. Demo success gate

The golden demo is ready only when:

1. Alice and Bob complete the flow from fresh, isolated homes using real test
   SaaS accounts and real LLM responses.
2. All adversary rows pass through the actual pack/export/install/run path.
3. OAuth authorization, exchange, refresh, scope preflight, and revocation are
   demonstrated without exposing tokens to agent code or operator context.
4. The signed MCP component rebuilds/runs on Bob’s machine and its digest,
   tools, egress, and audit events verify.
5. Each LLM channel enforces its own provider, credential, budget, and rate
   limit.
6. Consequential SaaS writes cannot occur without a non-replayable,
   body-bound human approval.
7. No Alice secret, account binding, or receiver-local resource ID appears in
   the bundle or Bob’s runtime.
8. The final Agent Approval Pack truthfully distinguishes implemented checks,
   skipped checks, limitations, and external SaaS receipts.
9. The recorded demo can be repeated from its script without manual source or
   policy edits.
