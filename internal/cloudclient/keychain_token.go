package cloudclient

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const (
	// KeychainServiceName is the macOS Keychain service name for cloud API tokens.
	KeychainServiceName = "agentpaas-cloud-api-token"
	// KeychainAccountName is the account name for the cloud API token entry.
	KeychainAccountName = "api-token"

	defaultKeychainTimeout = 10 * time.Second
)

// KeychainTokenStore stores the cloud API token in macOS Keychain.
type KeychainTokenStore struct {
	service string
	account string
	timeout time.Duration
}

// NewKeychainTokenStore creates a new KeychainTokenStore. Returns an error on
// non-macOS platforms.
func NewKeychainTokenStore() (*KeychainTokenStore, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("%w: keychain is available only on macOS", ErrTokenStoreUnavailable)
	}
	return &KeychainTokenStore{
		service: KeychainServiceName,
		account: KeychainAccountName,
		timeout: defaultKeychainTimeout,
	}, nil
}

// Set stores the token in the macOS Keychain. If a token already exists, it is
// overwritten.
func (k *KeychainTokenStore) Set(ctx context.Context, token string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("keychain token store set: token must not be empty")
	}

	// Delete existing entry first (best-effort, ignore not-found).
	_ = k.deleteEntry(ctx)

	if _, err := k.securityCall(ctx, "add-generic-password",
		"-a", k.account,
		"-s", k.service,
		"-w", token,
		"-U",
	); err != nil {
		return fmt.Errorf("keychain token store set: %w", err)
	}
	return nil
}

// Get retrieves the token from the macOS Keychain.
func (k *KeychainTokenStore) Get(ctx context.Context) (string, error) {
	out, err := k.securityCall(ctx, "find-generic-password",
		"-a", k.account,
		"-s", k.service,
		"-w",
	)
	if err != nil {
		return "", fmt.Errorf("keychain token store get: %w", err)
	}
	return strings.TrimRight(out, "\n"), nil
}

// Delete removes the token from the macOS Keychain. Idempotent — returns nil
// if no entry exists.
func (k *KeychainTokenStore) Delete(ctx context.Context) error {
	if err := k.deleteEntry(ctx); err != nil {
		return fmt.Errorf("keychain token store delete: %w", err)
	}
	return nil
}

func (k *KeychainTokenStore) deleteEntry(ctx context.Context) error {
	_, err := k.securityCall(ctx, "delete-generic-password",
		"-a", k.account,
		"-s", k.service,
	)
	if IsTokenNotFoundErr(err) {
		return nil
	}
	return err
}

func (k *KeychainTokenStore) securityCall(ctx context.Context, args ...string) (string, error) {
	timeout := k.timeout
	if timeout <= 0 {
		timeout = defaultKeychainTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, "security", args...)
	out, err := cmd.CombinedOutput()
	msg := strings.TrimSpace(string(out))

	if callCtx.Err() != nil {
		return "", fmt.Errorf("%w: security command timed out; unlock macOS Keychain and retry", ErrTokenStoreUnavailable)
	}
	if err != nil {
		low := strings.ToLower(msg)
		if strings.Contains(low, "item could not be found") || strings.Contains(low, "no matching") {
			return "", fmt.Errorf("%w: %s", ErrTokenNotFound, msg)
		}
		if strings.Contains(low, "locked") || strings.Contains(low, "unlock") || strings.Contains(low, "authenticated") {
			return "", fmt.Errorf("%w: unlock macOS Keychain and retry", ErrTokenStoreUnavailable)
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%w: %s", ErrTokenStoreUnavailable, msg)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// IsTokenNotFoundErr returns true if err wraps ErrTokenNotFound.
func IsTokenNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), ErrTokenNotFound.Error())
}

// IsTokenStoreUnavailableErr returns true if err wraps ErrTokenStoreUnavailable.
func IsTokenStoreUnavailableErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), ErrTokenStoreUnavailable.Error())
}
