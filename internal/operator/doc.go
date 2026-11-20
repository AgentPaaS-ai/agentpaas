// Package operator contains operator-facing soak and durability tests
// that prove the B30 multi-turn runtime with real Docker, real clock,
// daemon restart injection, SIGKILL failure injection, and active-time
// ledger checks.
//
// These tests are NOT library-only MemoryStore theater — they use real
// Docker containers, real filesystem stores, and real wall-clock time.
// The gate target is make block30-soak-gate.
package operator