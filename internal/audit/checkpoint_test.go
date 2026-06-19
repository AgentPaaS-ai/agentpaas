package audit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestNewCheckpoint(t *testing.T) {
	cp := NewCheckpoint(1, 5, "abcdef123456", "")
	if cp.Seq != 1 {
		t.Fatalf("expected seq=1, got %d", cp.Seq)
	}
	if cp.HeadAnchorSeq != 5 {
		t.Fatalf("expected head_anchor_seq=5, got %d", cp.HeadAnchorSeq)
	}
	if cp.HeadAnchorHash != "abcdef123456" {
		t.Fatalf("expected head_anchor_hash=abcdef123456, got %q", cp.HeadAnchorHash)
	}
	if cp.PrevCheckpointHash != "" {
		t.Fatalf("expected empty prev_checkpoint_hash, got %q", cp.PrevCheckpointHash)
	}
	if cp.Timestamp == "" {
		t.Fatal("expected non-empty timestamp")
	}
	if cp.CheckpointHash != "" {
		t.Fatalf("expected empty checkpoint_hash before signing, got %q", cp.CheckpointHash)
	}
	if len(cp.Signature) != 0 {
		t.Fatal("expected empty signature before signing")
	}
}

func TestCheckpointSignVerify(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	cp := NewCheckpoint(1, 5, "abcdef123456", "")
	if err := cp.Sign(key); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if cp.CheckpointHash == "" {
		t.Fatal("expected non-empty checkpoint_hash after signing")
	}
	if len(cp.Signature) == 0 {
		t.Fatal("expected non-empty signature after signing")
	}

	// Verify with the correct public key
	if !cp.VerifySignature(&key.PublicKey) {
		t.Fatal("VerifySignature failed with correct key")
	}

	// Verify with wrong key
	wrongKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey (wrong): %v", err)
	}
	if cp.VerifySignature(&wrongKey.PublicKey) {
		t.Fatal("VerifySignature should fail with wrong key")
	}

	// Tamper the hash and verify
	cp.CheckpointHash = "tampered"
	if cp.VerifySignature(&key.PublicKey) {
		t.Fatal("VerifySignature should fail with tampered hash")
	}
}

func TestCheckpointNilKeySign(t *testing.T) {
	cp := NewCheckpoint(1, 5, "hash", "")
	err := cp.Sign(nil)
	if err == nil {
		t.Fatal("expected error signing with nil key")
	}
}

func TestCheckpointVerifyNilPublicKey(t *testing.T) {
	cp := NewCheckpoint(1, 5, "hash", "")
	if cp.VerifySignature(nil) {
		t.Fatal("VerifySignature should return false with nil public key")
	}
}

func TestCheckpointVerifyUnsigned(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	cp := NewCheckpoint(1, 5, "hash", "")
	// Compute hash without signing
	h, err := cp.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash: %v", err)
	}
	cp.CheckpointHash = h

	if cp.VerifySignature(&key.PublicKey) {
		t.Fatal("VerifySignature should return false for unsigned checkpoint")
	}
}

func TestGenerateCheckpointKey(t *testing.T) {
	der, pub, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}

	if len(der) == 0 {
		t.Fatal("expected non-empty DER bytes")
	}

	// Parse back
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		t.Fatalf("ParsePKCS8PrivateKey: %v", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("parsed key is not ECDSA")
	}

	// Verify the public key matches
	if ecKey.PublicKey.X.Cmp(pub.X) != 0 || ecKey.PublicKey.Y.Cmp(pub.Y) != 0 {
		t.Fatal("public key mismatch after parse")
	}

	// Use the key for signing
	cp := NewCheckpoint(1, 5, "hash", "")
	if err := cp.Sign(ecKey); err != nil {
		t.Fatalf("Sign with parsed key: %v", err)
	}
	if !cp.VerifySignature(pub) {
		t.Fatal("VerifySignature with original public key")
	}
}

func TestCheckpointManagerCreate(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	m, err := NewCheckpointManager(cpPath, 10, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	cp, err := m.CreateCheckpoint(5, "anchor_hash_5")
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	if cp.Seq != 1 {
		t.Fatalf("expected seq=1, got %d", cp.Seq)
	}
	if cp.HeadAnchorSeq != 5 {
		t.Fatalf("expected head_anchor_seq=5, got %d", cp.HeadAnchorSeq)
	}
	if cp.PrevCheckpointHash != "" {
		t.Fatalf("expected empty prev_checkpoint_hash, got %q", cp.PrevCheckpointHash)
	}

	// Second checkpoint
	cp2, err := m.CreateCheckpoint(10, "anchor_hash_10")
	if err != nil {
		t.Fatalf("CreateCheckpoint 2: %v", err)
	}
	if cp2.Seq != 2 {
		t.Fatalf("expected seq=2, got %d", cp2.Seq)
	}
	if cp2.PrevCheckpointHash != cp.CheckpointHash {
		t.Fatalf("prev_checkpoint_hash: got %q, expected %q", cp2.PrevCheckpointHash, cp.CheckpointHash)
	}

	// Check latest anchor
	seq, hash := m.LatestAnchor()
	if seq != 10 {
		t.Fatalf("expected latest anchor seq=10, got %d", seq)
	}
	if hash != "anchor_hash_10" {
		t.Fatalf("expected latest anchor hash=anchor_hash_10, got %q", hash)
	}
}

func TestCheckpointManagerReplay(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	m, err := NewCheckpointManager(cpPath, 10, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}

	cp1, err := m.CreateCheckpoint(5, "hash_5")
	if err != nil {
		t.Fatalf("CreateCheckpoint 1: %v", err)
	}
	_ = cp1
	cp2, err := m.CreateCheckpoint(10, "hash_10")
	if err != nil {
		t.Fatalf("CreateCheckpoint 2: %v", err)
	}
	_ = cp2
	cp3, err := m.CreateCheckpoint(15, "hash_15")
	if err != nil {
		t.Fatalf("CreateCheckpoint 3: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open and verify state is reconstructed
	m2, err := NewCheckpointManager(cpPath, 10, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager (2nd): %v", err)
	}
	defer func() { _ = m2.Close() }()

	if seq := m2.CheckpointSeq(); seq != 3 {
		t.Fatalf("expected seq=3 after replay, got %d", seq)
	}

	seq, hash := m2.LatestAnchor()
	if seq != 15 {
		t.Fatalf("expected latest anchor seq=15, got %d", seq)
	}
	if hash != "hash_15" {
		t.Fatalf("expected latest anchor hash=hash_15, got %q", hash)
	}

	// Continuing should use seq=4
	cp4, err := m2.CreateCheckpoint(20, "hash_20")
	if err != nil {
		t.Fatalf("CreateCheckpoint after replay: %v", err)
	}
	if cp4.Seq != 4 {
		t.Fatalf("expected seq=4 after replay, got %d", cp4.Seq)
	}
	if cp4.PrevCheckpointHash != cp3.CheckpointHash {
		t.Fatalf("prev_checkpoint_hash: got %q, expected %q", cp4.PrevCheckpointHash, cp3.CheckpointHash)
	}
}

func TestCheckpointManagerShouldCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	m, err := NewCheckpointManager(cpPath, 10, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	// With cadence=10, should checkpoint after audit seq 10
	if m.ShouldCheckpoint(5) {
		t.Fatal("ShouldCheckpoint(5) should be false (cadence 10, lastAnchor 0)")
	}
	if m.ShouldCheckpoint(9) {
		t.Fatal("ShouldCheckpoint(9) should be false")
	}
	if !m.ShouldCheckpoint(10) {
		t.Fatal("ShouldCheckpoint(10) should be true")
	}
	if !m.ShouldCheckpoint(15) {
		t.Fatal("ShouldCheckpoint(15) should be true (we haven't created one yet)")
	}

	// Create a checkpoint at seq 10
	_, err = m.CreateCheckpoint(10, "hash_10")
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	// Now cadence should start from 10
	if m.ShouldCheckpoint(15) {
		t.Fatal("ShouldCheckpoint(15) should be false (lastAnchor=10, cadence=10)")
	}
	if m.ShouldCheckpoint(19) {
		t.Fatal("ShouldCheckpoint(19) should be false")
	}
	if !m.ShouldCheckpoint(20) {
		t.Fatal("ShouldCheckpoint(20) should be true (lastAnchor=10 + cadence=10 = 20)")
	}
}

func TestCheckpointManagerZeroCadence(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	m, err := NewCheckpointManager(cpPath, 0, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	// With cadence=0, ShouldCheckpoint should always be false
	if m.ShouldCheckpoint(100) {
		t.Fatal("ShouldCheckpoint should be false with cadence=0")
	}

	// But we can still create explicit checkpoints
	cp, err := m.CreateCheckpoint(50, "explicit_hash")
	if err != nil {
		t.Fatalf("CreateCheckpoint on zero-cadence manager: %v", err)
	}
	if cp.Seq != 1 {
		t.Fatalf("expected seq=1, got %d", cp.Seq)
	}
}

func TestCheckpointManagerSignWithKey(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	// Generate key for signing
	der, pub, err := GenerateCheckpointKey()
	if err != nil {
		t.Fatalf("GenerateCheckpointKey: %v", err)
	}

	m, err := NewCheckpointManager(cpPath, 10, der)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	cp, err := m.CreateCheckpoint(5, "signed_hash")
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	// Verify signature
	if !cp.VerifySignature(pub) {
		t.Fatal("VerifySignature should pass with correct public key")
	}
	if cp.CheckpointHash == "" {
		t.Fatal("expected non-empty checkpoint_hash")
	}
	if len(cp.Signature) == 0 {
		t.Fatal("expected non-empty signature")
	}
}

func TestCheckpointManagerRejectBehindSeq(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	m, err := NewCheckpointManager(cpPath, 10, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	_, err = m.CreateCheckpoint(10, "hash_10")
	if err != nil {
		t.Fatalf("CreateCheckpoint: %v", err)
	}

	// Trying to checkpoint at an earlier seq should fail
	_, err = m.CreateCheckpoint(5, "hash_5")
	if err == nil {
		t.Fatal("expected error for checkpoint behind last anchor seq")
	}
}

func TestCheckpointCanonicalDeterminism(t *testing.T) {
	// Two checkpoints with same data should produce same canonical JSON
	cp1 := NewCheckpoint(1, 5, "anchor_hash", "prev_hash")
	cp1.Timestamp = "2025-01-01T00:00:00Z"
	cp2 := NewCheckpoint(1, 5, "anchor_hash", "prev_hash")
	cp2.Timestamp = "2025-01-01T00:00:00Z"

	canon1, err := cp1.CanonicalMarshal()
	if err != nil {
		t.Fatalf("CanonicalMarshal 1: %v", err)
	}
	canon2, err := cp2.CanonicalMarshal()
	if err != nil {
		t.Fatalf("CanonicalMarshal 2: %v", err)
	}

	if string(canon1) != string(canon2) {
		t.Fatalf("canonical JSON differs: %q vs %q", string(canon1), string(canon2))
	}

	h1, err := cp1.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash 1: %v", err)
	}
	h2, err := cp2.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash 2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("checkpoint hash differs: %q vs %q", h1, h2)
	}
}

func TestCheckpointMarshalUnmarshal(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	cp := NewCheckpoint(3, 25, "anchor_hash_value", "prev_cp_hash_value")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	if err := cp.Sign(key); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Marshal to JSON and back
	jsonBytes, err := json.Marshal(cp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var cp2 CheckpointRecord
	if err := json.Unmarshal(jsonBytes, &cp2); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if cp2.Seq != 3 {
		t.Fatalf("seq: got %d, expected %d", cp2.Seq, 3)
	}
	if cp2.HeadAnchorSeq != 25 {
		t.Fatalf("head_anchor_seq: got %d, expected %d", cp2.HeadAnchorSeq, 25)
	}
	if cp2.HeadAnchorHash != "anchor_hash_value" {
		t.Fatalf("head_anchor_hash: got %q, expected anchor_hash_value", cp2.HeadAnchorHash)
	}
	if cp2.PrevCheckpointHash != "prev_cp_hash_value" {
		t.Fatalf("prev_checkpoint_hash: got %q, expected prev_cp_hash_value", cp2.PrevCheckpointHash)
	}
	if cp2.CheckpointHash != cp.CheckpointHash {
		t.Fatalf("checkpoint_hash mismatch: %q vs %q", cp2.CheckpointHash, cp.CheckpointHash)
	}
	if len(cp2.Signature) != len(cp.Signature) {
		t.Fatalf("signature length mismatch")
	}
}

func TestWriteCheckpointJSONL(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	cp := NewCheckpoint(1, 5, "anchor_hash", "")
	cp.Timestamp = "2025-01-01T00:00:00Z"
	h, err := cp.computeCheckpointHash()
	if err != nil {
		t.Fatalf("computeCheckpointHash: %v", err)
	}
	cp.CheckpointHash = h

	if err := WriteCheckpointJSONL(cpPath, cp); err != nil {
		t.Fatalf("WriteCheckpointJSONL: %v", err)
	}

	// Read back
	checkpoints, err := readCheckpoints(cpPath)
	if err != nil {
		t.Fatalf("readCheckpoints: %v", err)
	}
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint, got %d", len(checkpoints))
	}
	if checkpoints[0].Seq != 1 {
		t.Fatalf("expected seq=1, got %d", checkpoints[0].Seq)
	}
}

func TestCheckpointManagerClose(t *testing.T) {
	dir := t.TempDir()
	cpPath := filepath.Join(dir, "audit.checkpoints")

	m, err := NewCheckpointManager(cpPath, 10, nil)
	if err != nil {
		t.Fatalf("NewCheckpointManager: %v", err)
	}

	// Double close should be safe
	if err := m.Close(); err != nil {
		t.Fatalf("First Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Second Close should be no-op: %v", err)
	}
}