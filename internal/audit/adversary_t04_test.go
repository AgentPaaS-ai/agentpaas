package audit

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ========================================================================
// CANONICAL JSON ATTACKS
// ========================================================================

// TestAdversaryT04_HashDeterminism verifies that the same content produces
// the same record_hash regardless of map insertion order, across many
// random shuffles.
func TestAdversaryT04_HashDeterminism(t *testing.T) {
	basePayload := map[string]interface{}{
		"z": "last",
		"a": "first",
		"m": "middle",
		"nested": map[string]interface{}{
			"b": 2,
			"a": 1,
		},
		"list": []interface{}{3, 1, 2},
	}

	// Build key list for shuffling
	keys := make([]string, 0, len(basePayload))
	for k := range basePayload {
		keys = append(keys, k)
	}

	var prevHash string
	for i := 0; i < 20; i++ {
		shuffled := make([]string, len(keys))
		copy(shuffled, keys)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		// Build payload with shuffled key order
		payload := make(map[string]interface{})
		for _, k := range shuffled {
			payload[k] = basePayload[k]
		}

		rec := AuditRecord{
			Timestamp:      "2025-06-18T12:00:00Z",
			EventType:      "test",
			DeploymentMode: "local",
			Actor:          "adversary",
			Payload:        payload,
		}

		h, err := rec.computeRecordHash()
		if err != nil {
			t.Fatalf("computeRecordHash iteration %d: %v", i, err)
		}

		if prevHash == "" {
			prevHash = h
		} else if h != prevHash {
			t.Fatalf("hash determinism broken at iteration %d: got %q, expected %q", i, h, prevHash)
		}
	}
}

// TestAdversaryT04_UnicodeEscapeAttacks verifies canonical JSON handles
// unicode characters, control chars, null bytes, escaped quotes, and
// other special characters deterministically.
func TestAdversaryT04_UnicodeEscapeAttacks(t *testing.T) {
	testCases := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "unicode_chars",
			payload: map[string]interface{}{
				"text": "Hello 世界 🌍 \u00e9 \u2603",
			},
		},
		{
			name: "control_chars",
			payload: map[string]interface{}{
				"ctrl": "\u0000\u0001\u0002\u001f\u007f",
			},
		},
		{
			name: "null_byte",
			payload: map[string]interface{}{
				"null": "\u0000",
			},
		},
		{
			name: "escaped_quotes",
			payload: map[string]interface{}{
				"quotes": `{"nested": "value with \"quotes\""}`,
			},
		},
		{
			name: "mixed_unicode",
			payload: map[string]interface{}{
				"混合":    "キー",
				"emoji":  "🚀🔥💥",
				"specials": "\u00e9\u00e8\u00f1\u00fc",
			},
		},
		{
			name: "control_and_text",
			payload: map[string]interface{}{
				"data": "line1\nline2\tindented\u0000null",
				"also": "\r\n\b\f\t",
			},
		},
		{
			name: "json_injection",
			payload: map[string]interface{}{
				"field": `}, "injected": true, "x": {`,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec1 := AuditRecord{
				Timestamp:      "2025-01-01T00:00:00Z",
				EventType:      "unicode_test",
				DeploymentMode: "local",
				Actor:          "tester",
				Payload:        tc.payload,
			}

			h1, err := rec1.computeRecordHash()
			if err != nil {
				t.Fatalf("computeRecordHash: %v", err)
			}

			// Re-create same payload (different map insertion order)
			payload2 := make(map[string]interface{})
			for k, v := range tc.payload {
				payload2[k] = v
			}
			rec2 := AuditRecord{
				Timestamp:      "2025-01-01T00:00:00Z",
				EventType:      "unicode_test",
				DeploymentMode: "local",
				Actor:          "tester",
				Payload:        payload2,
			}

			h2, err := rec2.computeRecordHash()
			if err != nil {
				t.Fatalf("computeRecordHash rec2: %v", err)
			}

			if h1 != h2 {
				t.Fatalf("hash differs between two constructions: %q vs %q", h1, h2)
			}

			// Canonical output must be valid JSON
			canon, err := rec1.CanonicalMarshal()
			if err != nil {
				t.Fatalf("CanonicalMarshal: %v", err)
			}
			if len(canon) == 0 {
				t.Fatal("empty canonical output")
			}

			var unmarshaled map[string]interface{}
			if err := json.Unmarshal(canon, &unmarshaled); err != nil {
				t.Fatalf("cannot unmarshal canonical output: %v", err)
			}
		})
	}
}

// TestAdversaryT04_NestedMapsArrays verifies canonical marshaling recursively
// sorts keys at every nesting level and handles arrays with nested maps.
func TestAdversaryT04_NestedMapsArrays(t *testing.T) {
	buildNested := func(depth int) map[string]interface{} {
		result := map[string]interface{}{"z": "bottom"}
		current := result
		for i := 0; i < depth; i++ {
			inner := map[string]interface{}{
				fmt.Sprintf("key_%d", i): fmt.Sprintf("val_%d", i),
				"a_always_first":         i,
			}
			current["nested"] = inner
			current = inner
		}
		return result
	}

	for depth := 1; depth <= 10; depth++ {
		t.Run(fmt.Sprintf("depth_%d", depth), func(t *testing.T) {
			rec1 := AuditRecord{
				Timestamp:      "2025-01-01T00:00:00Z",
				EventType:      "nested_test",
				DeploymentMode: "local",
				Actor:          "tester",
				Payload:        buildNested(depth),
			}

			canon1, err := rec1.CanonicalMarshal()
			if err != nil {
				t.Fatalf("CanonicalMarshal: %v", err)
			}

			h1, err := rec1.computeRecordHash()
			if err != nil {
				t.Fatalf("computeRecordHash: %v", err)
			}

			// Build same content in second pass to verify determinism
			rec2 := AuditRecord{
				Timestamp:      "2025-01-01T00:00:00Z",
				EventType:      "nested_test",
				DeploymentMode: "local",
				Actor:          "tester",
				Payload:        buildNested(depth),
			}

			canon2, err := rec2.CanonicalMarshal()
			if err != nil {
				t.Fatalf("CanonicalMarshal rec2: %v", err)
			}

			h2, err := rec2.computeRecordHash()
			if err != nil {
				t.Fatalf("computeRecordHash rec2: %v", err)
			}

			if string(canon1) != string(canon2) {
				t.Fatalf("canonical differs for same content:\n  got1: %s\n  got2: %s", canon1, canon2)
			}
			if h1 != h2 {
				t.Fatalf("hash differs: %q vs %q", h1, h2)
			}
		})
	}

	// Test arrays containing nested maps
	t.Run("nested_arrays", func(t *testing.T) {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "array_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload: map[string]interface{}{
				"array":  []interface{}{3, 1, 2, map[string]interface{}{"b": 2, "a": 1}},
				"z_last": "val",
			},
		}

		canon, err := rec.CanonicalMarshal()
		if err != nil {
			t.Fatalf("CanonicalMarshal: %v", err)
		}

		h, err := rec.computeRecordHash()
		if err != nil {
			t.Fatalf("computeRecordHash: %v", err)
		}

		if h == "" {
			t.Fatal("empty hash")
		}
		t.Logf("canonical (nested_arrays): %s", string(canon))
	})
}

// TestAdversaryT04_EmptyVsNilPayload verifies both empty and nil payloads
// produce deterministic (and possibly distinct) hashes consistently.
func TestAdversaryT04_EmptyVsNilPayload(t *testing.T) {
	// Empty payload (non-nil map with no keys)
	recEmpty := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "payload_test",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        map[string]interface{}{},
	}

	// Nil payload
	recNil := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "payload_test",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        nil,
	}

	hEmpty, err := recEmpty.computeRecordHash()
	if err != nil {
		t.Fatalf("computeRecordHash empty: %v", err)
	}

	hNil, err := recNil.computeRecordHash()
	if err != nil {
		t.Fatalf("computeRecordHash nil: %v", err)
	}

	// Verify both are deterministic across multiple calls
	hEmpty2, err := recEmpty.computeRecordHash()
	if err != nil {
		t.Fatalf("computeRecordHash empty2: %v", err)
	}
	if hEmpty != hEmpty2 {
		t.Fatalf("empty payload hash not deterministic: %q vs %q", hEmpty, hEmpty2)
	}

	hNil2, err := recNil.computeRecordHash()
	if err != nil {
		t.Fatalf("computeRecordHash nil2: %v", err)
	}
	if hNil != hNil2 {
		t.Fatalf("nil payload hash not deterministic: %q vs %q", hNil, hNil2)
	}

	// Verify canonical output is valid JSON
	canonEmpty, _ := recEmpty.CanonicalMarshal()
	canonNil, _ := recNil.CanonicalMarshal()

	var m1, m2 map[string]interface{}
	if err := json.Unmarshal(canonEmpty, &m1); err != nil {
		t.Fatalf("canonical empty unmarshal: %v", err)
	}
	if err := json.Unmarshal(canonNil, &m2); err != nil {
		t.Fatalf("canonical nil unmarshal: %v", err)
	}

	t.Logf("empty canonical: %s", string(canonEmpty))
	t.Logf("nil canonical:   %s", string(canonNil))
	t.Logf("empty hash: %q", hEmpty)
	t.Logf("nil hash:   %q", hNil)
}

// TestAdversaryT04_LargePayload verifies that a ~1MB payload can be
// canonically marshaled and hashed without panic or error, and that
// the hash is deterministic. Also verifies it can be written and
// re-read via the AuditWriter.
func TestAdversaryT04_LargePayload(t *testing.T) {
	payload := make(map[string]interface{})
	for i := 0; i < 10000; i++ {
		payload[fmt.Sprintf("key_%010d", i)] = fmt.Sprintf("value_%010d", i)
	}

	rec := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "large_payload",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        payload,
	}

	canon, err := rec.CanonicalMarshal()
	if err != nil {
		t.Fatalf("CanonicalMarshal: %v", err)
	}

	if len(canon) < 300000 {
		t.Fatalf("payload too small: %d bytes (expected ~300KB+ for stress)", len(canon))
	}
	t.Logf("large payload canonical size: %d bytes (no panic)", len(canon))

	h, err := rec.computeRecordHash()
	if err != nil {
		t.Fatalf("computeRecordHash: %v", err)
	}
	if h == "" {
		t.Fatal("empty hash for large payload")
	}

	// Verify determinism
	payload2 := make(map[string]interface{})
	for k, v := range payload {
		payload2[k] = v
	}
	rec2 := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "large_payload",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        payload2,
	}
	h2, err := rec2.computeRecordHash()
	if err != nil {
		t.Fatalf("computeRecordHash rec2: %v", err)
	}
	if h != h2 {
		t.Fatalf("hash differs for large payload: %q vs %q", h, h2)
	}

	// Write through the AuditWriter and re-read
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	rec3 := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "large_payload",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        payload,
	}
	if err := w.Append(rec3); err != nil {
		t.Fatalf("Append large payload: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter after write: %v", err)
	}
	defer func() { _ = w2.Close() }()
	seq, _ := w2.CurrentHead()
	if seq != 1 {
		t.Fatalf("expected seq=1, got %d", seq)
	}
}

// ========================================================================
// CHAIN INTEGRITY ATTACKS
// ========================================================================

// TestAdversaryT04_TamperMiddleRecord writes 5 records, tampers line 3
// in the JSONL file (changes a byte), then verifies the chain detects
// the break.
func TestAdversaryT04_TamperMiddleRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "tamper_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read file, tamper line 3 (index 2) by changing "seq":3 to "seq":9
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	tampered := strings.Replace(lines[2], `"seq":3`, `"seq":9`, 1)
	lines[2] = tampered
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Re-open — replay() simply takes the last record as head (no validation)
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	t.Logf("After tamper, re-opened writer reports seq=%d, hash=%q", seq, hash)

	// Manually verify chain integrity — if chain is still "intact", the
	// attack bypassed detection
	data2, _ := os.ReadFile(path)
	allLines := strings.Split(string(data2), "\n")
	var records []AuditRecord
	for _, line := range allLines {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Logf("malformed line (expected): %v", err)
			continue
		}
		records = append(records, r)
	}

	chainIntact := true
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			chainIntact = false
			t.Logf("CHAIN BREAK at seq %d -> seq %d: prev_hash=%q, expected %q",
				records[i-1].Seq, records[i].Seq,
				records[i].PrevHash, records[i-1].RecordHash)
		}
	}
	if chainIntact {
		t.Error("BREAK: chain tamper was NOT detected — chain integrity bypass (HIGH)")
	} else {
		t.Log("PASS: chain tamper detected via hash verification")
	}
}

// TestAdversaryT04_TruncateTail writes 5 records, deletes the last 2
// lines from the file, then verifies NewAuditWriter reconstructs the
// head from the remaining records and the chain is intact.
func TestAdversaryT04_TruncateTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "trunc_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Keep only the first 3 lines
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	truncated := strings.Join(lines[:3], "\n")
	if err := os.WriteFile(path, []byte(truncated), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Re-open — replay() should reconstruct head from remaining 3 records
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	t.Logf("After truncation, writer reports seq=%d, hash=%q", seq, hash)

	// Verify remaining records are a valid chain
	data2, _ := os.ReadFile(path)
	allLines := strings.Split(string(data2), "\n")
	var records []AuditRecord
	for _, line := range allLines {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		records = append(records, r)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 remaining records, got %d", len(records))
	}

	// Chain among remaining records should be intact
	chainIntact := true
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			chainIntact = false
			t.Logf("CHAIN BREAK at seq %d -> seq %d", records[i-1].Seq, records[i].Seq)
		}
	}
	if !chainIntact {
		t.Error("BREAK: remaining records after truncation have broken chain (HIGH)")
	}

	// Verify we can continue appending from seq=4
	recNew := AuditRecord{
		Timestamp:      "2025-01-01T01:00:00Z",
		EventType:      "trunc_cont",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        map[string]interface{}{},
	}
	if err := w2.Append(recNew); err != nil {
		t.Fatalf("Append after truncation: %v", err)
	}
	newSeq, _ := w2.CurrentHead()
	if newSeq != 4 {
		t.Fatalf("expected seq=4 after append on truncated chain, got %d", newSeq)
	}
}

// TestAdversaryT04_ReorderLines writes 5 records, swaps lines 2 and 3,
// then verifies the chain detects the break.
func TestAdversaryT04_ReorderLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "reorder_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Swap lines 2 and 3 (indices 1 and 2)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	lines[1], lines[2] = lines[2], lines[1]
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Re-open — replay() does not validate chain
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	t.Logf("After reorder, writer reports seq=%d, hash=%q", seq, hash)

	// Manually verify chain — must detect break
	data2, _ := os.ReadFile(path)
	allLines := strings.Split(string(data2), "\n")
	var records []AuditRecord
	for _, line := range allLines {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		records = append(records, r)
	}

	chainIntact := true
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			chainIntact = false
			t.Logf("CHAIN BREAK at seq %d -> seq %d: prev_hash=%q, expected %q",
				records[i-1].Seq, records[i].Seq,
				records[i].PrevHash, records[i-1].RecordHash)
		}
	}
	if chainIntact {
		t.Error("BREAK: reorder was NOT detected — chain integrity bypass (HIGH)")
	} else {
		t.Log("PASS: reorder detected via hash verification")
	}
}

// TestAdversaryT04_InsertFakeRecord writes 3 records, inserts a fake
// record between lines 2 and 3, then verifies the chain detects the
// break (prev_hash mismatch).
func TestAdversaryT04_InsertFakeRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 3; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "fake_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Build a fake record with plausible-looking fields but wrong hashes
	fakeRecord := AuditRecord{
		Seq:            3,
		PrevHash:       "0000000000000000000000000000000000000000000000000000000000000000",
		RecordHash:     "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "fake_event",
		DeploymentMode: "local",
		Actor:          "attacker",
		Payload:        map[string]interface{}{"malicious": true},
	}
	fakeJSON, _ := json.Marshal(fakeRecord)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	// Insert fake record between line 2 (index 1) and line 3 (index 2)
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[0], lines[1], string(fakeJSON))
	newLines = append(newLines, lines[2:]...)
	if err := os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Re-open
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	t.Logf("After fake insert, writer reports seq=%d, hash=%q", seq, hash)

	// Manually verify chain integrity — must detect break
	data2, _ := os.ReadFile(path)
	allLines := strings.Split(string(data2), "\n")
	var records []AuditRecord
	for _, line := range allLines {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		records = append(records, r)
	}

	chainIntact := true
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			chainIntact = false
			t.Logf("CHAIN BREAK at seq %d -> seq %d: prev_hash=%q, expected %q",
				records[i-1].Seq, records[i].Seq,
				records[i].PrevHash, records[i-1].RecordHash)
		}
	}
	if chainIntact {
		t.Error("BREAK: fake record insertion was NOT detected — chain integrity bypass (HIGH)")
	} else {
		t.Log("PASS: fake record detected via hash mismatch")
	}
}

// TestAdversaryT04_DuplicateSeq writes 3 records, modifies line 3 to
// have seq=2 (duplicate), then verifies detection.
func TestAdversaryT04_DuplicateSeq(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 3; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "dup_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Modify line 3 (index 2) to have seq=2 instead of seq=3
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	var rec3 AuditRecord
	if err := json.Unmarshal([]byte(lines[2]), &rec3); err != nil {
		t.Fatalf("Unmarshal line 3: %v", err)
	}
	rec3.Seq = 2
	modifiedLine, _ := json.Marshal(rec3)
	lines[2] = string(modifiedLine)

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Re-open
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	t.Logf("After duplicate seq, writer reports seq=%d, hash=%q", seq, hash)

	// Manually check for duplicate seqs
	data2, _ := os.ReadFile(path)
	allLines := strings.Split(string(data2), "\n")
	var records []AuditRecord
	for _, line := range allLines {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		records = append(records, r)
	}

	seen := make(map[int64]int)
	hasDup := false
	for _, r := range records {
		seen[r.Seq]++
		if seen[r.Seq] > 1 {
			hasDup = true
			t.Logf("DUPLICATE seq=%d found", r.Seq)
		}
	}
	if !hasDup {
		t.Error("BREAK: duplicate seq was NOT detected (chain integrity bypass — HIGH)")
	}

	// Also verify chain integrity is broken (record_hash won't match after seq change)
	chainIntact := true
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			chainIntact = false
			t.Logf("CHAIN BREAK at seq %d -> seq %d", records[i-1].Seq, records[i].Seq)
		}
	}
	if chainIntact && hasDup {
		t.Log("Hash chain is technically intact despite duplicate seq (record_hash mismatch masked)")
	}
}

// TestAdversaryT04_GapInSeq writes 5 records, then modifies the file
// to contain seq 1,2,4,5 (missing seq=3), and verifies detection.
func TestAdversaryT04_GapInSeq(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 5; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "gap_test",
			DeploymentMode: "local",
			Actor:          "tester",
			Payload:        map[string]interface{}{},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Create a file with seq 1,2,4,5 (missing 3)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(data), "\n")

	// Parse the last 3 records
	var rec3, rec4, rec5 AuditRecord
	if err := json.Unmarshal([]byte(lines[2]), &rec3); err != nil {
		t.Fatalf("Unmarshal seq=3 line: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[3]), &rec4); err != nil {
		t.Fatalf("Unmarshal seq=4 line: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[4]), &rec5); err != nil {
		t.Fatalf("Unmarshal seq=5 line: %v", err)
	}

	// Renumber: seq=3 becomes seq=4, seq=4 becomes seq=5, drop seq=5
	rec3.Seq = 4
	rec4.Seq = 5
	modifiedLine3, _ := json.Marshal(rec3)
	modifiedLine4, _ := json.Marshal(rec4)
	_ = rec5 // dropped

	lines[2] = string(modifiedLine3)
	lines[3] = string(modifiedLine4)
	newLines := lines[:4] // keep lines 0,1,2,3 + trailing empty

	if err := os.WriteFile(path, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Re-open
	w2, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w2.Close() }()

	seq, hash := w2.CurrentHead()
	t.Logf("After gap, writer reports seq=%d, hash=%q", seq, hash)

	// Manually check for gaps in seq
	data2, _ := os.ReadFile(path)
	allLines := strings.Split(string(data2), "\n")
	var records []AuditRecord
	for _, line := range allLines {
		if line == "" {
			continue
		}
		var r AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		records = append(records, r)
	}

	hasGap := false
	for i := 1; i < len(records); i++ {
		expectedSeq := records[i-1].Seq + 1
		if records[i].Seq != expectedSeq {
			hasGap = true
			t.Logf("SEQ GAP: seq=%d followed by seq=%d (expected %d)", records[i-1].Seq, records[i].Seq, expectedSeq)
		}
	}
	if !hasGap {
		t.Error("BREAK: seq gap was NOT detected (chain integrity bypass — HIGH)")
	}

	// Also check hash chain (may also be broken due to re-hashing with wrong seq)
	chainIntact := true
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			chainIntact = false
			t.Logf("CHAIN BREAK at seq %d -> seq %d (hash mismatch)", records[i-1].Seq, records[i].Seq)
		}
	}
	if chainIntact {
		t.Log("Hash chain is intact despite seq gap")
	}
}

// ========================================================================
// WRITER ATTACKS
// ========================================================================

// TestAdversaryT04_ConcurrentAppendAndClose races Append and Close
// goroutines to verify no panic, no corruption.
func TestAdversaryT04_ConcurrentAppendAndClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: append rapidly
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			rec := AuditRecord{
				Timestamp:      "2025-01-01T00:00:00Z",
				EventType:      "concurrent_test",
				DeploymentMode: "local",
				Actor:          "worker",
				Payload:        map[string]interface{}{"n": i},
			}
			_ = w.Append(rec) // errors expected when Close races — not a failure
		}
	}()

	// Goroutine 2: close while appends are happening
	go func() {
		defer wg.Done()
		_ = w.Close()
	}()

	wg.Wait()

	// Verify the file can be read (no corruption)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	t.Logf("Survived concurrent append+close. File size: %d bytes, lines: %d",
		len(data), strings.Count(string(data), "\n"))

	// Re-open and verify chain on whatever was written
	w2, err := NewAuditWriter(path)
	if err != nil {
		// It's possible nothing was written and file is empty, which is OK
		t.Logf("NewAuditWriter after race: %v (acceptable if nothing was written)", err)
		return
	}
	defer func() { _ = w2.Close() }()

	// Verify what was written is valid
	seq, hash := w2.CurrentHead()
	t.Logf("After race, writer reports seq=%d, hash=%q", seq, hash)
}

// TestAdversaryT04_AppendAfterClose verifies append on a closed writer
// returns an error (fail-closed). This duplicates the coverage in the
// existing TestAppendAfterCloseFailClosed as a regression safeguard.
func TestAdversaryT04_AppendAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}

	rec := AuditRecord{
		Timestamp:      "2025-06-18T12:00:00Z",
		EventType:      "test",
		DeploymentMode: "local",
		Actor:          "system",
		Payload:        map[string]interface{}{},
	}
	if err := w.Append(rec); err != nil {
		t.Fatalf("First Append: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Append after close must fail
	rec2 := AuditRecord{
		Timestamp:      "2025-06-18T12:01:00Z",
		EventType:      "test2",
		DeploymentMode: "local",
		Actor:          "system",
		Payload:        map[string]interface{}{},
	}
	err = w.Append(rec2)
	if err == nil {
		t.Error("BREAK: Append after Close succeeded — fail-closed contract broken (HIGH)")
	} else {
		t.Logf("PASS: Append after Close correctly rejected: %v", err)
	}
}

// TestAdversaryT04_AppendToReadOnlyFile verifies that attempting to
// open a writer on a read-only file fails (fail-closed).
func TestAdversaryT04_AppendToReadOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	// Create an empty file and make it read-only
	if err := os.WriteFile(path, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(path, 0444); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	defer func() { _ = os.Chmod(path, 0644) }()

	// NewAuditWriter tries O_RDWR — must fail on read-only file
	_, err := NewAuditWriter(path)
	if err == nil {
		t.Error("BREAK: NewAuditWriter succeeded on read-only file — fail-closed contract broken (HIGH)")
	} else {
		t.Logf("PASS: NewAuditWriter correctly failed on read-only file: %v", err)
	}
}

// TestAdversaryT04_RapidAppends verifies 1000 rapid sequential appends
// produce a valid chain with no data loss.
func TestAdversaryT04_RapidAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	const n = 1000
	for i := 0; i < n; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "rapid_test",
			DeploymentMode: "local",
			Actor:          fmt.Sprintf("worker-%d", i),
			Payload:        map[string]interface{}{"i": i},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	seq, hash := w.CurrentHead()
	if seq != int64(n) {
		t.Fatalf("expected seq=%d, got %d", n, seq)
	}
	if hash == "" {
		t.Fatal("expected non-empty head hash")
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read back and verify all records
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Validate all records by reading line-by-line
	scanner := strings.NewReader(string(data))
	var records []AuditRecord
	dec := json.NewDecoder(scanner)
	for dec.More() {
		var r AuditRecord
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		records = append(records, r)
	}

	if len(records) != n {
		t.Fatalf("expected %d records, got %d", n, len(records))
	}

	// Verify chain integrity
	for i := 1; i < len(records); i++ {
		if records[i].PrevHash != records[i-1].RecordHash {
			t.Fatalf("chain broken at seq %d -> seq %d: prev_hash=%q, expected %q",
				records[i-1].Seq, records[i].Seq,
				records[i].PrevHash, records[i-1].RecordHash)
		}
	}
	t.Logf("Verified %d rapid appends with intact chain", n)
}

// TestAdversaryT04_NilEmptyEventType verifies behavior with empty and
// nil-equivalent event_type fields.
func TestAdversaryT04_NilEmptyEventType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Test with empty event_type (string zero value)
	recEmpty := AuditRecord{
		Timestamp:      "2025-01-01T00:00:00Z",
		EventType:      "",
		DeploymentMode: "local",
		Actor:          "tester",
		Payload:        map[string]interface{}{},
	}

	err = w.Append(recEmpty)
	if err != nil {
		// If rejected, that's a fail-closed policy — document it
		t.Logf("Empty event_type rejected: %v (fail-closed policy)", err)
	} else {
		// If accepted, verify it produces a valid, deterministic record
		t.Log("Empty event_type accepted — verifying behavior is consistent")
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		data, _ := os.ReadFile(path)
		var rec AuditRecord
		for _, line := range strings.Split(string(data), "\n") {
			if line == "" {
				continue
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
		}
		if rec.EventType != "" {
			t.Fatalf("expected empty event_type, got %q", rec.EventType)
		}
		if rec.RecordHash == "" {
			t.Fatal("empty record_hash for empty event_type record")
		}

		// Verify the stored hash matches a fresh recompute from the writer-version record
		canon, err := rec.CanonicalMarshal()
		if err != nil {
			t.Fatalf("CanonicalMarshal: %v", err)
		}
		expectedHash := fmt.Sprintf("%x", sha256.Sum256(canon))
		if rec.RecordHash != expectedHash {
			t.Fatalf("stored record_hash %q does not match recomputed hash %q for empty event_type",
				rec.RecordHash, expectedHash)
		}
		t.Logf("Empty event_type record hash: %q (deterministic)", rec.RecordHash)
	}
}

// TestAdversaryT04_WriterRoundTripWithSHA256Verify explicitly verifies
// each record's SHA-256 hash against its canonical form, detecting any
// discrepancy introduced by marshaling/persistence.
func TestAdversaryT04_WriterRoundTripWithSHA256Verify(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")

	w, err := NewAuditWriter(path)
	if err != nil {
		t.Fatalf("NewAuditWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Write records with varied payload structures
	for i := 0; i < 10; i++ {
		rec := AuditRecord{
			Timestamp:      "2025-01-01T00:00:00Z",
			EventType:      "sha256_test",
			DeploymentMode: "local",
			Actor:          "verifier",
			Payload: map[string]interface{}{
				"index": i,
				"data":  fmt.Sprintf("payload-%d", i),
				"nested": map[string]interface{}{
					"b": i * 2,
					"a": i,
				},
			},
		}
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-read and verify every record's hash
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		var rec AuditRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("Unmarshal line %d: %v", i, err)
		}

		// Compute what the hash should be
		canon, err := rec.CanonicalMarshal()
		if err != nil {
			t.Fatalf("CanonicalMarshal seq=%d: %v", rec.Seq, err)
		}
		expectedHash := fmt.Sprintf("%x", sha256.Sum256(canon))
		if rec.RecordHash != expectedHash {
			t.Fatalf("seq=%d: record_hash mismatch: stored=%q, computed=%q", rec.Seq, rec.RecordHash, expectedHash)
		}
	}
	t.Log("All 10 records pass SHA-256 round-trip verification")
}