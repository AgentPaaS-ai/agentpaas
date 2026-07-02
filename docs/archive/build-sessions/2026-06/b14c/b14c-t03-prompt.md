# Block 14C-T03: Docs — Quickstart, Enforcement, Known Limitations

## Context

AgentPaaS is a Go project at /Users/pms88/projects/agentpaas. We need to create
the initial docs site content for the v0.1.0 release.

## What to Create

### 1. `docs/quickstart.md` — 15-minute path

A step-by-step guide from zero to a running governed agent:

```
## Prerequisites

- macOS (Apple Silicon or Intel)
- Homebrew
- Docker Desktop or Colima

## Step 1: Install

brew install agentpaas/tap/agentpaas

## Step 2: Verify

agent doctor

## Step 3: Start the daemon

agent daemon start

## Step 4: Create your first agent

agent init weather-agent
cd weather-agent

## Step 5: Write the agent

(Show a simple Python agent using the SDK)

## Step 6: Apply a policy

agent policy apply --file policy.yaml

## Step 7: Pack the agent

agent pack

## Step 8: Run it

agent run weather-agent

## Step 9: View the dashboard

open http://localhost:8090

## Step 10: Check the audit trail

agent audit list
```

### 2. `docs/how-enforcement-works.md` — Network topology deep dive

Explain the gateway topology:
- Agent container on internal-only Docker network
- Gateway container (agentgateway) dual-homed on internal + egress
- HTTP_PROXY env vars route agent traffic through gateway
- Gateway enforces frontendPolicies.networkAuthorization CEL rules
- Denied traffic gets 403/connection error
- All decisions audited

Include a diagram (ASCII art):
```
┌─────────────────────────────────────────────┐
│  Docker Host                                │
│                                             │
│  ┌──────────┐     ┌──────────┐             │
│  │  Agent   │─────│ Gateway  │──── Internet│
│  │ (internal│     │(dual-homed│            │
│  │  network)│     │ net)     │             │
│  └──────────┘     └──────────┘             │
│      │                  │                  │
│  internal net      egress net              │
│  (no internet)     (internet)              │
└─────────────────────────────────────────────┘
```

### 3. `docs/known-limitations.md` — P1 accepted limitations

List all P1 backlog items from the risk analyses:
- HTTP_PROXY only (no transparent proxy for non-HTTP)
- No real LLM integration (harness returns fake response)
- Cosign integration test opt-in
- Hash chain record deletion undetectable
- ReconcileAfterCrash doesn't clean gateways/networks
- Integer overflow in Stats() for very high CPU
- Trigger server has no auth
- No Linux support (macOS only)
- Volunteer gate (14C) not yet run

### 4. `docs/threat-model.md` — PRD §3 verbatim

Copy the threat model from the PRD. If you can find it in:
- agentpaas-prd-v4-master.md
- agentpaas-execution-plan-v1.md

Extract §3 (threat model) and put it in docs/threat-model.md.

### 5. `docs/policy-reference.md` — Policy YAML reference

Document the policy.yaml format:
- version
- agent.name
- egress[] (domain, action: allow/deny)
- credentials[] (id, type, header)
- ingress[] (path, port)

Show examples for common patterns:
- Allow one domain
- Allow multiple domains
- Default deny
- Credential injection

### 6. `docs/audit-export.md` — Audit verification guide

How to verify audit exports on a second machine:
- Export: `agent audit export --run <run-id> --output audit.jsonl`
- Verify hash chain: `agent audit verify --file audit.jsonl`
- The chain uses SHA-256 hash linking
- Genesis record has empty prev_hash
- Each record's record_hash is computed from canonical JSON

## Constraints

- All docs are markdown, terminal-readable.
- Link to each other where relevant.
- Keep each doc under 200 lines.
- Do NOT create a docs site builder config (static HTML is P2).
- These are content docs only — no code changes.
