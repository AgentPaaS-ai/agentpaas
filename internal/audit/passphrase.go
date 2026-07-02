package audit

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	keychainService = "com.agentpaas.daemon"
	keychainAccount = "audit-checkpoint-key"
	passphraseFile  = ".audit-key-passphrase"
)

// loadOrGeneratePassphrase loads the checkpoint key passphrase from macOS
// Keychain (if available) or a passphrase file. If no passphrase exists,
// generates a new random one and persists it.
func loadOrGeneratePassphrase(stateDir string) (string, error) {
	if runtime.GOOS == "darwin" {
		pass, err := keychainGet(keychainService, keychainAccount)
		if err == nil && pass != "" {
			return pass, nil
		}
		// Generate and store
		pass, err = generateRandomPassphrase()
		if err != nil {
			return "", err
		}
		if err := keychainSet(keychainService, keychainAccount, pass); err != nil {
			// Keychain write failed — fall back to file
			log.Printf("audit: keychain write failed (%v), falling back to passphrase file", err)
			return passphraseFileLoadOrGenerate(stateDir)
		}
		return pass, nil
	}
	return passphraseFileLoadOrGenerate(stateDir)
}

func passphraseFileLoadOrGenerate(stateDir string) (string, error) {
	p := filepath.Join(stateDir, passphraseFile)
	data, err := os.ReadFile(p)
	if err == nil {
		pass := string(data)
		if pass != "" {
			return pass, nil
		}
	}
	pass, err := generateRandomPassphrase()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(pass), 0600); err != nil {
		return "", fmt.Errorf("write passphrase file: %w", err)
	}
	return pass, nil
}

func generateRandomPassphrase() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate passphrase: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// keychainGet retrieves a password from macOS Keychain via the security CLI.
func keychainGet(service, account string) (string, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// keychainSet stores a password in macOS Keychain via the security CLI.
func keychainSet(service, account, password string) error {
	// Try to delete first (ignore error if not found)
	_ = exec.Command("security", "delete-generic-password", "-s", service, "-a", account).Run()
	cmd := exec.Command("security", "add-generic-password", "-s", service, "-a", account, "-w", password, "-U")
	return cmd.Run()
}
