// Package hashutil provides small shared hashing helpers.
package hashutil

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hex returns the lowercase hex-encoded SHA-256 digest of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256HexString returns the lowercase hex-encoded SHA-256 digest of value.
func SHA256HexString(value string) string {
	return SHA256Hex([]byte(value))
}
