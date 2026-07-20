# Enterprise Follow-Up: Managed Vault and Remote Broker Patterns

Corporate employee machines behind a VPN need a stricter model than local
developer laptops. The P1 broker can deny future brokered credential requests
after revocation, but enterprise secrets should not permanently reside on the
employee laptop when the tenant requires central control.

## Patterns to Evaluate

### Managed Vault

Use a tenant-managed vault as the source of truth for enterprise secrets. The
local daemon would request a short-lived grant from the vault, scoped to the
agent run, destination, policy rule, and requested method. Secret material would
be returned only when device posture and tenant policy allow it, and the grant
would expire quickly.

This pattern keeps rotation, tenant ownership, and audit in existing enterprise
vault systems. It still risks exposing a secret value to the laptop for direct
leases unless those leases are disabled or converted into short-lived files with
strong cleanup and restart behavior.

### Remote Broker

Move credential injection to a tenant-controlled remote broker reachable over
the corporate network or VPN. The employee laptop sends the outbound request
metadata to the broker, and the broker injects credentials server-side or returns
a narrowly scoped signed request artifact. Long-lived enterprise secrets never
need to be stored on the laptop.

This pattern gives tenants stronger revocation and audit guarantees. It adds
availability and latency requirements: agent traffic depends on broker reachability
and must fail closed when the VPN, device posture service, or broker is unavailable.

## Required Controls

- Device posture: require a fresh device posture signal before issuing grants or
  accepting brokered requests. Inputs should include managed-device enrollment,
  disk encryption, OS security state, EDR status, and VPN or trusted network
  presence.
- Tenant policy to disable direct leases: allow tenants to turn off direct leases
  for enterprise credentials. When disabled, policy validation should reject
  file_lease and env_lease modes for those credentials.
- Short-lived credential grants: grants should be scoped to credential ID, run ID,
  policy rule, destination, method, and expiry. Default lifetimes should be short
  enough that revocation does not depend on laptop cleanup.
- Revocation: revocation should stop new brokered requests immediately, invalidate
  outstanding grants at the managed vault or remote broker, and identify affected
  runs for daemon restart. Direct leases cannot claw back a secret value already
  visible to agent code, so tenant policy should prefer remote brokered access
  for high-value secrets.
- Tenant-visible audit: tenants need audit events for grant issuance, injection,
  denial, revocation, affected-run identification, and daemon restart outcomes.
  Events should include tenant ID, device ID, user, run ID, policy rule,
  credential ID, destination, method, status, reason, posture decision, and
  whether the credential was visible to agent code.

## P2 Questions

- Should remote broker support request proxying, signed request artifacts, or
  both?
- What posture providers and cache lifetimes are acceptable for offline or
  intermittent VPN use?
- Which tenant policy level owns direct-lease disablement: tenant default,
  credential class, policy file, or all three?
- How should the daemon report affected-run restarts back to tenant audit when
  the laptop is offline during revocation?
