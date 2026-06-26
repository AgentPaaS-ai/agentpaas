# Task: Fix T05 Adversary HIGH — Genesis prev_hash Check in verifyHarnessChain

## Branch
You are on branch `feat/b14a-t05`. Do NOT create a new branch — commit directly to this one.

## Problem
In `internal/daemon/control_handlers.go`, the function `verifyHarnessChain()` (line 754) validates a hash chain of harness audit records. It checks:
1. Each record's `record_hash` matches a recomputed hash (line 759-766)
2. Each record's `prev_hash` matches the previous record's `record_hash` (line 767-772)

BUT the prev_hash check at line 767 is guarded by `if i > 0`, which means the FIRST record (genesis, i=0) is NEVER checked for `prev_hash == ""`. An attacker who tampers with the JSONL file post-container could set a non-empty `prev_hash` on the genesis record and verification would pass.

## Fix Required

### 1. Fix verifyHarnessChain() in internal/daemon/control_handlers.go

Change the prev_hash check logic so that:
- For i == 0 (genesis record): verify `rec.PrevHash == ""`. If not empty, return an error:
  `fmt.Errorf("harness chain: line %d: genesis record must have empty prev_hash, got %q", i+1, rec.PrevHash)`
- For i > 0: keep the existing prev_hash link check (prev_hash must match previous record's record_hash)

The current code structure:
```go
if i > 0 {
    if rec.PrevHash != records[i-1].RecordHash {
        return fmt.Errorf("harness chain: line %d: prev_hash mismatch: got %q, expected %q",
            i+1, rec.PrevHash, records[i-1].RecordHash)
    }
}
```

Change it to:
```go
if i == 0 {
    if rec.PrevHash != "" {
        return fmt.Errorf("harness chain: line %d: genesis record must have empty prev_hash, got %q", i+1, rec.PrevHash)
    }
} else {
    if rec.PrevHash != records[i-1].RecordHash {
        return fmt.Errorf("harness chain: line %d: prev_hash mismatch: got %q, expected %q",
            i+1, rec.PrevHash, records[i-1].RecordHash)
    }
}
```

Also update the doc comment above the function (line 749-753) to mention the genesis check. Add a third bullet:
```
// 3. The first (genesis) record has prev_hash == ""
```

### 2. Add test in internal/daemon/harness_audit_chain_test.go

Add a new test function `TestVerifyHarnessChain_GenesisNonEmptyPrevHash`:

```go
func TestVerifyHarnessChain_GenesisNonEmptyPrevHash(t *testing.T) {
    records := validHarnessChainRecords()
    path := filepath.Join(t.TempDir(), "harness-audit.jsonl")
    writeHarnessAuditChain(t, path, records)

    stored, err := readAuditJSONL(path)
    if err != nil {
        t.Fatalf("readAuditJSONL: %v", err)
    }

    // Tamper: set non-empty prev_hash on genesis record
    stored[0].PrevHash = "deadbeef"
    // Recompute record_hash so the record_hash check passes, isolating the genesis check
    recomputed, err := stored[0].ComputeRecordHash()
    if err != nil {
        t.Fatalf("ComputeRecordHash: %v", err)
    }
    stored[0].RecordHash = recomputed

    err = verifyHarnessChain(stored)
    if err == nil {
        t.Fatal("verifyHarnessChain() = nil, want genesis prev_hash error")
    }
    if !strings.Contains(err.Error(), "genesis record must have empty prev_hash") {
        t.Fatalf("verifyHarnessChain() error = %q, want genesis prev_hash error", err)
    }
}
```

## Verification

Run:
```bash
cd /Users/pms88/projects/agentpaas
go test ./internal/daemon/... -run TestVerifyHarnessChain -v -race
go test ./internal/daemon/... -run TestIngestHarnessAudit -v -race
go build ./...
```

All tests must pass. Also run:
```bash
golangci-lint run ./internal/daemon/...
```
Must have 0 issues.

## Constraints
- ONLY modify `internal/daemon/control_handlers.go` (verifyHarnessChain function + doc comment) and `internal/daemon/harness_audit_chain_test.go` (new test function)
- Do NOT touch any other files
- Commit with message: `fix(14a-t05): genesis prev_hash check in verifyHarnessChain per adversary HIGH finding`
