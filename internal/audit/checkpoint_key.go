package audit

import (
	"crypto/ecdsa"
	"crypto/x509"
	"fmt"
	"os"
)

// DefaultCheckpointCadence is the number of audit records between automatic checkpoints.
const DefaultCheckpointCadence int64 = 100

// LoadOrGenerateCheckpointKey loads a PKCS#8 ECDSA private key from path, or generates
// and persists a new key if the file does not exist.
func LoadOrGenerateCheckpointKey(path string) (privateKeyDER []byte, publicKey *ecdsa.PublicKey, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read checkpoint key: %w", err)
		}
		privateKeyDER, publicKey, err = GenerateCheckpointKey()
		if err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(path, privateKeyDER, 0600); err != nil {
			return nil, nil, fmt.Errorf("write checkpoint key: %w", err)
		}
		return privateKeyDER, publicKey, nil
	}
	publicKey, err = PublicKeyFromCheckpointKeyDER(data)
	if err != nil {
		return nil, nil, err
	}
	return data, publicKey, nil
}

// PublicKeyFromCheckpointKeyDER parses a PKCS#8 ECDSA private key DER blob and returns its public key.
func PublicKeyFromCheckpointKeyDER(keyDER []byte) (*ecdsa.PublicKey, error) {
	key, err := x509.ParsePKCS8PrivateKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("parse checkpoint key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("checkpoint key is not ECDSA")
	}
	return &ecKey.PublicKey, nil
}