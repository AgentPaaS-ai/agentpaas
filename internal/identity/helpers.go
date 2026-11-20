package identity

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
)

// b64Encode encodes data to a standard base64 string.
func b64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// b64Decode decodes a standard base64 string.
func b64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// ecdsaSign signs digest with the ECDSA private key using crypto/rand.Reader
// for non-deterministic signing.
func ecdsaSign(key *ecdsa.PrivateKey, digest []byte) ([]byte, error) {
	return ecdsa.SignASN1(rand.Reader, key, digest)
}

// ecdsaVerify verifies an ECDSA signature over the given digest.
func ecdsaVerify(key *ecdsa.PublicKey, digest []byte, signature []byte) bool {
	return ecdsa.VerifyASN1(key, digest, signature)
}
