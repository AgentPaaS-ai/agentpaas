package audit

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"time"
)

// CheckpointRecord is a signed snapshot of the audit hash chain at a specific
// sequence number. Each checkpoint captures the head anchor (seq + record_hash)
// of the audit chain at a point in time, is cryptographically linked to the
// previous checkpoint, and is signed with the daemon's audit signing key.
//
// The checkpoint chain is independent of the audit record chain but provides
// a separate signed attestation that can be used to verify the integrity of
// the audit log at the checkpointed point.
type CheckpointRecord struct {
	Seq                int64  `json:"seq"`
	HeadAnchorSeq      int64  `json:"head_anchor_seq"`
	HeadAnchorHash     string `json:"head_anchor_hash"`
	PrevCheckpointHash string `json:"prev_checkpoint_hash"`
	CheckpointHash     string `json:"checkpoint_hash"`
	Signature          []byte `json:"signature"`
	Timestamp          string `json:"timestamp"`
}

// canonicalCheckpoint mirrors CheckpointRecord without CheckpointHash for
// deterministic serialization used in hash computation.
type canonicalCheckpoint struct {
	Seq                int64  `json:"seq"`
	HeadAnchorSeq      int64  `json:"head_anchor_seq"`
	HeadAnchorHash     string `json:"head_anchor_hash"`
	PrevCheckpointHash string `json:"prev_checkpoint_hash"`
	Timestamp          string `json:"timestamp"`
}

// CanonicalMarshal produces deterministic JSON for hashing and signing.
// It serializes the checkpoint without the CheckpointHash and Signature
// fields, with no extra whitespace.
func (c *CheckpointRecord) CanonicalMarshal() ([]byte, error) {
	cc := canonicalCheckpoint{
		Seq:                c.Seq,
		HeadAnchorSeq:      c.HeadAnchorSeq,
		HeadAnchorHash:     c.HeadAnchorHash,
		PrevCheckpointHash: c.PrevCheckpointHash,
		Timestamp:          c.Timestamp,
	}
	return json.Marshal(cc)
}

// computeCheckpointHash computes the SHA-256 hex digest of the canonical JSON
// representation of the checkpoint (without CheckpointHash and Signature).
func (c *CheckpointRecord) computeCheckpointHash() (string, error) {
	canonical, err := c.CanonicalMarshal()
	if err != nil {
		return "", fmt.Errorf("canonical marshal: %w", err)
	}
	hash := sha256.Sum256(canonical)
	return fmt.Sprintf("%x", hash), nil
}

// Sign signs the checkpoint using the given ECDSA P-256 private key. It sets
// CheckpointHash to the self-hash and Signature to the ASN.1 DER-encoded
// signature. The signature is computed over SHA-256(checkpointHash).
func (c *CheckpointRecord) Sign(key *ecdsa.PrivateKey) error {
	if key == nil {
		return fmt.Errorf("checkpoint sign: nil private key")
	}
	hash, err := c.computeCheckpointHash()
	if err != nil {
		return fmt.Errorf("checkpoint sign: %w", err)
	}
	c.CheckpointHash = hash

	// Sign the double-hash: SHA-256 of the hex checkpoint hash string
	hashBytes := sha256.Sum256([]byte(hash))
	sig, err := ecdsa.SignASN1(rand.Reader, key, hashBytes[:])
	if err != nil {
		return fmt.Errorf("checkpoint sign: %w", err)
	}
	c.Signature = sig
	return nil
}

// VerifySignature checks that the checkpoint's signature is valid for the
// checkpoint hash using the given ECDSA P-256 public key.
func (c *CheckpointRecord) VerifySignature(pub *ecdsa.PublicKey) bool {
	if c.CheckpointHash == "" || len(c.Signature) == 0 || pub == nil {
		return false
	}
	hashBytes := sha256.Sum256([]byte(c.CheckpointHash))
	return ecdsa.VerifyASN1(pub, hashBytes[:], c.Signature)
}

// GenerateCheckpointKey creates a new ECDSA P-256 key pair for checkpoint
// signing. The private key is returned as PKCS#8 DER bytes for storage.
// Use x509.ParsePKCS8PrivateKey to recover the ecdsa.PrivateKey.
func GenerateCheckpointKey() (privateKeyDER []byte, publicKey *ecdsa.PublicKey, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate checkpoint key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal checkpoint private key: %w", err)
	}
	return der, &key.PublicKey, nil
}

// NewCheckpoint creates a new CheckpointRecord for the given head anchor.
// The checkpoint is assigned the given checkpoint seq number. If
// prevCheckpointHash is empty, this is treated as the genesis checkpoint.
// Sign must be called separately to sign the checkpoint.
func NewCheckpoint(seq int64, headAnchorSeq int64, headAnchorHash string, prevCheckpointHash string) *CheckpointRecord {
	return &CheckpointRecord{
		Seq:                seq,
		HeadAnchorSeq:      headAnchorSeq,
		HeadAnchorHash:     headAnchorHash,
		PrevCheckpointHash: prevCheckpointHash,
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
	}
}

// CheckpointErrorType classifies verification failures.
type CheckpointErrorType string

const (
	// ErrTypeTamperMiddle means a record in the middle of the chain was modified.
	ErrTypeTamperMiddle CheckpointErrorType = "tamper_middle"
	// ErrTypeTailTruncation means records at the tail were removed.
	ErrTypeTailTruncation CheckpointErrorType = "tail_truncation"
	// ErrTypeReorder means records were reordered.
	ErrTypeReorder CheckpointErrorType = "reorder"
	// ErrTypeMissingCheckpoint means a checkpoint is missing from the chain.
	ErrTypeMissingCheckpoint CheckpointErrorType = "missing_checkpoint"
	// ErrTypeSignature means a checkpoint signature is invalid.
	ErrTypeSignature CheckpointErrorType = "invalid_signature"
	// ErrTypeCheckpointChain means the checkpoint-to-checkpoint hash chain is broken.
	ErrTypeCheckpointChain CheckpointErrorType = "broken_checkpoint_chain"
)

// CheckpointError carries structured information about a verification failure.
type CheckpointError struct {
	Type    CheckpointErrorType `json:"type"`
	Message string             `json:"message"`
	Line    int                `json:"line,omitempty"`
	Seq     int64              `json:"seq,omitempty"`
}

func (e *CheckpointError) Error() string {
	return e.Message
}