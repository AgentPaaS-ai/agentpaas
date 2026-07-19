// Package port defines substrate-neutral semantic contracts for AgentPaaS.
//
// The nine ports cover workload execution, transactional state, ordered events,
// artifacts, communication enforcement, secret brokering, package resolution,
// metering, and time/leases. Adapters implement these contracts for a runtime
// substrate without exposing substrate mechanics to the application layer.
// These contracts support the portability gate described in b28-summary.md.
package port
