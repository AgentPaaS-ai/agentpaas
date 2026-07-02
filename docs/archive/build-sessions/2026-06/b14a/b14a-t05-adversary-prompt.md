# Adversary Review: 14A-T05 — Hash-chained harness audit

You are a security adversary. Your job is to BREAK the hash-chained audit implementation.

## Target code

1. `internal/harness/file_appender.go` — FileAuditAppender now maintains a hash chain
2. `internal/daemon/control_handlers.go` — verifyHarnessChain() + ingestHarnessAudit()
3. `internal/audit/record.go` — ComputeRecordHash() exported wrapper

The implementation:
- Harness writes records with prev_hash + record_hash (SHA-256 chain)
- Daemon verifies the chain before ingesting records into its own audit trail
- If verification fails, daemon logs "harness_audit_chain_broken" and refuses ingestion

## Attack vectors to try

1. **Record insertion attack:** Can an attacker insert a record between two existing
   records and recompute the chain to be valid? (The daemon recomputes from canonical
   JSON — can the attacker produce valid canonical JSON?)
2. **Record deletion attack:** Can an attacker delete the last record? The chain would
   still be valid (it just ends earlier). Is this detected?
3. **Record reordering attack:** Can records be reordered?
4. **Genesis record attack:** What if the first record's prev_hash is set to something
   other than ""?
5. **Empty file attack:** What happens if the harness audit file is empty (0 bytes)?
6. **Malformed JSON attack:** What if a record has malformed JSON?
7. **Race condition:** Can the harness write records while the daemon is reading them?
   (The harness runs inside the container, the daemon reads after Stop — is there a
   timing window?)
8. **Hash collision:** SHA-256 collision — theoretical, but check the implementation
   doesn't weaken the hash (e.g., using a weaker algorithm or truncating)
9. **Canonical JSON manipulation:** Can an attacker produce a different canonical JSON
   that hashes to the same value? (Check the canonical marshal implementation)
10. **Concurrent append race:** Multiple goroutines in FileAuditAppender.Append — is
    the mutex held for the full compute + write + update cycle?

## Instructions

1. Read the target code
2. For each attack vector, analyze whether the code is vulnerable
3. If you find a real vulnerability, write a proof-of-concept test
4. Report findings with severity
