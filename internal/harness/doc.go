// Package harness runs agent workloads inside the container as PID 1 style
// supervision: a local HTTP lifecycle API, a Python worker, resource budgets,
// process reaping, egress firewall hooks, guardrails, streaming adapters, and
// authenticated progress journal integration.
//
// The harness is embedded into packed images and invoked by the runtime. It
// exposes lifecycle endpoints (health/import/invoke/terminate) on loopback,
// spawns the Python agent worker, applies rlimits derived from policy or
// legacy defaults, and records audit/progress events for the daemon to tail.
//
// # Durable vs legacy paths
//
// On the durable InvokeDeployment path, CPU/PID ceilings and wall-clock budget
// are policy- and TimeEnvelope-derived. On the legacy v0.2.3 path, fixed
// compatibility constants apply. Progress journals use HMAC-authenticated
// records keyed by attempt/lease identity loaded from sidecars.
//
// # Isolation responsibilities
//
// The harness is defense-in-depth inside an already sandboxed container: payload
// size limits, worker timeouts, process-group kill on cancel, zombie reaping,
// optional Linux capability-aware firewall helpers, and structured failure
// envelopes for the control plane.
package harness
