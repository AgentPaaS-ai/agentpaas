// Package identity provides the local CA, agent keys, and SVID issuance.
//
// This package defines narrow interfaces for KeyStore and IdentityIssuer,
// along with an in-memory fake implementation for testing. The KeyStore
// manages distinct local identities:
//   - Local CA key
//   - Daemon audit signing key
//   - Per-agent package identity keys
//   - Per-run workload key/cert
//
// The IdentityIssuer issues SPIFFE-style workload certificates; real CA
// logic is implemented in a later task (B3-T03).
package identity