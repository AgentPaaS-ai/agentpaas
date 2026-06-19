// Package audit provides hash-chain log, export, and verification for the
// AgentPaaS daemon. It implements a canonical JSONL audit chain where each
// record is cryptographically linked to its predecessor via SHA-256 hashes,
// ensuring tamper-evident logging of security-relevant actions.
//
// The core types are AuditRecord (the canonical record schema with
// deterministic JSON serialization) and AuditWriter (a daemon-owned
// append-only writer with fsync durability).
//
// # Record chain
//
// Each record carries seq, prev_hash, record_hash, and metadata fields.
//   - record_hash = SHA-256(canonical JSON of the record, omitting record_hash)
//   - prev_hash for seq=1 is the empty string (genesis)
//   - prev_hash for seq>1 is the record_hash of the preceding record
//
// # Writer guarantees
//
//   - Append-only: records are appended to a JSONL file, one per line.
//   - Serialized: a mutex ensures only one Append executes at a time.
//   - Durable: each line is fsynced immediately after writing.
//   - Fail-closed: if the underlying file write or fsync fails, Append
//     returns an error so the caller can abort the guarded operation.
//   - Head reconstruction: on open, the writer replays the file to find the
//     latest seq and record_hash.
//
// # Checkpoints
//
// Checkpoints are signed snapshots of the audit chain head created at a
// configurable cadence or on demand. Each checkpoint records the head anchor
// (seq + record_hash) at a point in time, is cryptographically linked to the
// previous checkpoint, and is signed with the daemon's audit signing key.
// The checkpoint chain provides a separate signed attestation that can be
// used to verify the integrity of the audit log at the checkpointed point.
//
// # Audit Export Bundle
//
// ExportAuditBundle creates a portable bundle containing a copy of the audit
// JSONL, checkpoints JSONL, the daemon public key (PEM), and a signed
// manifest tying them together. The manifest is signed with the daemon's
// audit signing key and includes metadata (record count, head anchor,
// checkpoint count, public key fingerprint).
//
// VerifyAuditBundle performs offline verification of a bundle using only the
// bundle contents and the expected daemon audit public key fingerprint.
// Verification proves:
//  1. The bundled audit chain is internally consistent (hash chain intact).
//  2. The bundled checkpoint chain is internally consistent.
//  3. Checkpoint signatures are valid against the bundled public key.
//  4. The manifest signature is valid for the expected daemon audit key.
//
// Offline verification does NOT prove global transparency-log anchoring.
// It proves bundle integrity at export time only.
package audit