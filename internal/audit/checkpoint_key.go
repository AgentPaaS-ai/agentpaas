package audit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// DefaultCheckpointCadence is the number of audit records between automatic checkpoints.
const DefaultCheckpointCadence int64 = 100

const (
	// Encryption format version.
	checkpointKeyEncVersion = 1

	// PBKDF2 parameters (same as identity/filestore.go).
	checkpointKeyPbkdf2Iterations = 100_000
	checkpointKeySaltLen          = 32
	checkpointKeyNonceLen         = 12
	checkpointKeyAESKeyLen        = 32
)

// encryptedKeyFormat is the on-disk JSON envelope for the encrypted key.
type encryptedKeyFormat struct {
	Version   int    `json:"version"`
	KDF       string `json:"kdf"`
	SaltB64   string `json:"salt"`
	NonceB64  string `json:"nonce"`
	CipherB64 string `json:"cipher"` // AES-256-GCM ciphertext of DER
}

// deriveCheckpointKey derives an AES-256 key from passphrase+salt via PBKDF2.
func deriveCheckpointKey(passphrase string, salt []byte) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, checkpointKeyPbkdf2Iterations, checkpointKeyAESKeyLen)
}

// encryptCheckpointKeyDER encrypts DER bytes using AES-256-GCM with a
// passphrase-derived key. Returns the JSON envelope bytes.
func encryptCheckpointKeyDER(der []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, checkpointKeySaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	key, err := deriveCheckpointKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, checkpointKeyNonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, der, nil)
	envelope := encryptedKeyFormat{
		Version:   checkpointKeyEncVersion,
		KDF:       "pbkdf2-hmac-sha256",
		SaltB64:   base64.StdEncoding.EncodeToString(salt),
		NonceB64:  base64.StdEncoding.EncodeToString(nonce),
		CipherB64: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return json.Marshal(envelope)
}

// decryptCheckpointKeyDER decrypts the JSON envelope and returns the DER bytes.
func decryptCheckpointKeyJSON(data []byte, passphrase string) ([]byte, error) {
	var env encryptedKeyFormat
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse encrypted key envelope: %w", err)
	}
	if env.Version != checkpointKeyEncVersion {
		return nil, fmt.Errorf("unsupported checkpoint key version %d", env.Version)
	}
	salt, err := base64.StdEncoding.DecodeString(env.SaltB64)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.NonceB64)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.CipherB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	key, err := deriveCheckpointKey(passphrase, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	der, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt checkpoint key (wrong passphrase or corrupted): %w", err)
	}
	return der, nil
}

// isLegacyUnencryptedKey checks if the data at the given path is raw DER
// (legacy format) vs encrypted JSON envelope.
func isLegacyUnencryptedKey(data []byte) bool {
	// Legacy format is raw PKCS#8 DER (binary ASN.1, starts with 0x30).
	// Encrypted format is JSON (starts with '{').
	return len(data) > 0 && data[0] == 0x30
}

// resolveCheckpointKeyPassphrase gets the passphrase from env var, keychain,
// or generates a new one. Returns the passphrase and an error.
func resolveCheckpointKeyPassphrase(stateDir string) (string, error) {
	// Priority 1: env var
	if pass := os.Getenv("AGENTPAAS_AUDIT_KEY_PASSPHRASE"); pass != "" {
		return pass, nil
	}
	// Priority 2: keychain (macOS) or passphrase file (other OS)
	pass, err := loadOrGeneratePassphrase(stateDir)
	if err != nil {
		return "", fmt.Errorf("resolve checkpoint key passphrase: %w", err)
	}
	return pass, nil
}

// LoadOrGenerateCheckpointKey loads an encrypted ECDSA key from path, or
// generates and persists a new encrypted key if the file does not exist.
// The key is encrypted at rest with AES-256-GCM using a passphrase-derived key.
// If the file exists in legacy unencrypted DER format, it is migrated to
// encrypted format on next write.
func LoadOrGenerateCheckpointKey(path string) (privateKeyDER []byte, publicKey *ecdsa.PublicKey, err error) {
	stateDir := filepath.Dir(path)
	passphrase, err := resolveCheckpointKeyPassphrase(stateDir)
	if err != nil {
		return nil, nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("read checkpoint key: %w", err)
		}
		// Generate new key
		privateKeyDER, publicKey, err = GenerateCheckpointKey()
		if err != nil {
			return nil, nil, err
		}
		encrypted, encErr := encryptCheckpointKeyDER(privateKeyDER, passphrase)
		if encErr != nil {
			return nil, nil, fmt.Errorf("encrypt checkpoint key: %w", encErr)
		}
		if writeErr := os.WriteFile(path, encrypted, 0600); writeErr != nil {
			return nil, nil, fmt.Errorf("write checkpoint key: %w", writeErr)
		}
		return privateKeyDER, publicKey, nil
	}

	// Existing key — check if legacy unencrypted
	if isLegacyUnencryptedKey(data) {
		// Legacy raw DER format — migrate to encrypted on next write.
		// For now, parse it and return the DER (it will be re-encrypted
		// on the next key generation cycle). Log a warning.
		log.Printf("audit: checkpoint key is in legacy unencrypted DER format; will migrate to encrypted on regeneration")
		publicKey, err = PublicKeyFromCheckpointKeyDER(data)
		if err != nil {
			return nil, nil, err
		}
		return data, publicKey, nil
	}

	// Encrypted JSON envelope
	privateKeyDER, err = decryptCheckpointKeyJSON(data, passphrase)
	if err != nil {
		return nil, nil, err
	}
	publicKey, err = PublicKeyFromCheckpointKeyDER(privateKeyDER)
	if err != nil {
		return nil, nil, err
	}
	return privateKeyDER, publicKey, nil
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
