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
package audit