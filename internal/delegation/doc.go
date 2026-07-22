// Package delegation defines the canonical durable contracts for Secure Task
// Delegation (B32): logical task delegation, messages/parts, terminal results,
// transferable artifact references (schema only), ordered task events, idempotency,
// content digests, and a pluggable Store interface.
//
// T01 delivers the schemas, validation, transitions, and store interface.
// T02 adds snapshot-based two-sided authorization.  T03 wires the SDK and
// gateway.  T04 implements the artifact broker.  T05 delivers event-driven
// wait/wake.  T06 is the adversary gate.
package delegation