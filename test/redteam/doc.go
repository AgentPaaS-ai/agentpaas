// Package redteam implements the AgentPaaS P1 red-team smoke gate.
//
// Each fixture exercises the REAL pipeline (pack.BuildImage,
// runtime.DockerRuntime, secrets.Broker/Gateway, Block 11 operator
// handlers) — no synthetic harnesses, direct daemon shortcuts, or
// test-only enforcement paths.
//
// The runner prints a 6-row containment table plus a signed
// audit-export verification summary. Gate: make block12-gate.
package redteam
