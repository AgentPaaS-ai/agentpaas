package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	defaultKeychainTimeout = 10 * time.Second
	manifestAccount        = "_agentpaas_secret_index"
)

type KeychainStore struct {
	service string
	timeout time.Duration
}

type keychainEntry struct {
	ValueB64   string    `json:"value_b64"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

func NewKeychainStore(service string) (*KeychainStore, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("%w: keychain is available only on macOS; no plaintext fallback is available", ErrKeychainUnavailable)
	}
	if strings.TrimSpace(service) == "" {
		return nil, errors.New("keychain service name must not be empty")
	}
	return &KeychainStore{service: service, timeout: defaultKeychainTimeout}, nil
}

func (k *KeychainStore) Set(ctx context.Context, name string, value []byte) error {
	if err := ValidateSecretName(name); err != nil {
		return err
	}
	if err := validateSecretValue(value); err != nil {
		return err
	}

	now := time.Now().UTC()
	entry := keychainEntry{
		ValueB64:   base64.StdEncoding.EncodeToString(value),
		CreatedAt:  now,
		UpdatedAt:  now,
		LastUsedAt: time.Time{},
	}
	existing, err := k.getEntry(ctx, name)
	if err == nil {
		entry.CreatedAt = existing.CreatedAt
		entry.LastUsedAt = existing.LastUsedAt
	} else if !errors.Is(err, ErrSecretNotFound) {
		return err
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal keychain secret: %w", err)
	}
	if _, err := k.securityCall(ctx, "add-generic-password", "-a", name, "-s", k.service, "-w", string(data), "-U"); err != nil {
		return err
	}
	return k.addToManifest(ctx, name)
}

func (k *KeychainStore) Get(ctx context.Context, name string) ([]byte, error) {
	if err := ValidateSecretName(name); err != nil {
		return nil, err
	}
	entry, err := k.getEntry(ctx, name)
	if err != nil {
		return nil, err
	}
	value, err := base64.StdEncoding.DecodeString(entry.ValueB64)
	if err != nil {
		return nil, fmt.Errorf("decode keychain secret: %w", err)
	}
	return value, nil
}

func (k *KeychainStore) List(ctx context.Context) ([]SecretMeta, error) {
	names, err := k.loadManifest(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]SecretMeta, 0, len(names))
	for _, name := range names {
		entry, err := k.getEntry(ctx, name)
		if errors.Is(err, ErrSecretNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, SecretMeta{
			Name:       name,
			CreatedAt:  entry.CreatedAt,
			UpdatedAt:  entry.UpdatedAt,
			LastUsedAt: entry.LastUsedAt,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (k *KeychainStore) Delete(ctx context.Context, name string) error {
	if err := ValidateSecretName(name); err != nil {
		return err
	}
	if _, err := k.securityCall(ctx, "delete-generic-password", "-a", name, "-s", k.service); err != nil {
		return err
	}
	return k.removeFromManifest(ctx, name)
}

func (k *KeychainStore) TouchLastUsed(ctx context.Context, name string) error {
	if err := ValidateSecretName(name); err != nil {
		return err
	}
	entry, err := k.getEntry(ctx, name)
	if err != nil {
		return err
	}
	entry.LastUsedAt = time.Now().UTC()
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal keychain secret: %w", err)
	}
	if _, err := k.securityCall(ctx, "add-generic-password", "-a", name, "-s", k.service, "-w", string(data), "-U"); err != nil {
		return err
	}
	return nil
}

func (k *KeychainStore) getEntry(ctx context.Context, name string) (keychainEntry, error) {
	out, err := k.securityCall(ctx, "find-generic-password", "-a", name, "-s", k.service, "-w")
	if err != nil {
		return keychainEntry{}, err
	}
	var entry keychainEntry
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		return keychainEntry{}, fmt.Errorf("parse keychain secret metadata: %w", err)
	}
	return entry, nil
}

func (k *KeychainStore) loadManifest(ctx context.Context) ([]string, error) {
	out, err := k.securityCall(ctx, "find-generic-password", "-a", manifestAccount, "-s", k.service, "-w")
	if errors.Is(err, ErrSecretNotFound) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		return nil, fmt.Errorf("parse keychain manifest: %w", err)
	}
	return names, nil
}

func (k *KeychainStore) saveManifest(ctx context.Context, names []string) error {
	sort.Strings(names)
	data, err := json.Marshal(names)
	if err != nil {
		return fmt.Errorf("marshal keychain manifest: %w", err)
	}
	_, err = k.securityCall(ctx, "add-generic-password", "-a", manifestAccount, "-s", k.service, "-w", string(data), "-U")
	return err
}

func (k *KeychainStore) addToManifest(ctx context.Context, name string) error {
	names, err := k.loadManifest(ctx)
	if err != nil {
		return err
	}
	for _, existing := range names {
		if existing == name {
			return nil
		}
	}
	names = append(names, name)
	return k.saveManifest(ctx, names)
}

func (k *KeychainStore) removeFromManifest(ctx context.Context, name string) error {
	names, err := k.loadManifest(ctx)
	if err != nil {
		return err
	}
	filtered := names[:0]
	for _, existing := range names {
		if existing != name {
			filtered = append(filtered, existing)
		}
	}
	if len(filtered) == len(names) {
		return nil
	}
	if len(filtered) == 0 {
		return k.deleteManifest(ctx)
	}
	return k.saveManifest(ctx, filtered)
}

func (k *KeychainStore) deleteManifest(ctx context.Context) error {
	_, err := k.securityCall(ctx, "delete-generic-password", "-a", manifestAccount, "-s", k.service)
	if errors.Is(err, ErrSecretNotFound) {
		return nil
	}
	return err
}

func (k *KeychainStore) securityCall(ctx context.Context, args ...string) (string, error) {
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
		return "", fmt.Errorf("%w: security command timed out; unlock macOS Keychain and retry", ErrKeychainUnavailable)
	}
	if err != nil {
		low := strings.ToLower(msg)
		if strings.Contains(low, "item could not be found") || strings.Contains(low, "no matching") {
			return "", fmt.Errorf("%w: %s", ErrSecretNotFound, msg)
		}
		if strings.Contains(low, "locked") || strings.Contains(low, "unlock") || strings.Contains(low, "authenticated") {
			return "", fmt.Errorf("%w: unlock macOS Keychain and retry", ErrKeychainUnavailable)
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%w: %s", ErrKeychainUnavailable, msg)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

var _ SecretStore = (*KeychainStore)(nil)
