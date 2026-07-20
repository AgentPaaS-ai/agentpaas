// Package pack implements the agent project pack pipeline: runtime detection,
// project scaffolding, deterministic OCI image builds, dependency locking,
// SBOM generation, secret scanning, advisory scanning, lockfile signing, and
// provenance/lineage helpers.
//
// A packed agent is summarized by a signed AgentLock (agent.lock) that pins
// image digest, build input digest, policy digest, SBOM digest, package
// identity, and related metadata. The lock is the review unit consumed by
// deploy/run and by bundle export.
//
// # Build determinism
//
// Build contexts are collected with sorted paths, SOURCE_DATE_EPOCH timestamps,
// symlink rejection, and .agentpaasignore filtering. Multi-stage Docker builds
// embed the harness as PID 1 on a digest-pinned distroless base and install
// Python deps via uv lock output.
//
// # Safety
//
// Pack-time checks include LLM egress policy validation (provider domains must
// appear in policy egress), secret scanning of the project tree, optional OSV
// advisory gating, and path/symlink hardening on project directories.
package pack
