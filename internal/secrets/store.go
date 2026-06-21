package secrets

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unicode"
)

const MaxSecretValueSize = 64 * 1024

var (
	ErrInvalidSecretName   = errors.New("invalid secret name")
	ErrSecretTooLarge      = errors.New("secret value too large")
	ErrSecretNotFound      = errors.New("secret not found")
	ErrKeychainUnavailable = errors.New("macOS keychain unavailable")
)

type SecretStore interface {
	Set(ctx context.Context, name string, value []byte) error
	Get(ctx context.Context, name string) ([]byte, error)
	List(ctx context.Context) ([]SecretMeta, error)
	Delete(ctx context.Context, name string) error
	TouchLastUsed(ctx context.Context, name string) error
}

type SecretMeta struct {
	Name       string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastUsedAt time.Time
}

func ValidateSecretName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalidSecretName)
	}
	for _, r := range name {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return fmt.Errorf("%w: name must not contain whitespace, control, or invisible format characters", ErrInvalidSecretName)
		}
	}
	return nil
}

func validateSecretValue(value []byte) error {
	if len(value) > MaxSecretValueSize {
		return fmt.Errorf("%w: exceeds %d byte limit", ErrSecretTooLarge, MaxSecretValueSize)
	}
	return nil
}

type FakeKeyStore struct {
	mu      sync.Mutex
	entries map[string]fakeEntry
	now     func() time.Time
}

type fakeEntry struct {
	value []byte
	meta  SecretMeta
}

func NewFakeKeyStore() *FakeKeyStore {
	return &FakeKeyStore{
		entries: make(map[string]fakeEntry),
		now:     time.Now,
	}
}

func (f *FakeKeyStore) Set(_ context.Context, name string, value []byte) error {
	if err := ValidateSecretName(name); err != nil {
		return err
	}
	if err := validateSecretValue(value); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	now := f.now().UTC()
	entry, ok := f.entries[name]
	if !ok {
		entry.meta = SecretMeta{Name: name, CreatedAt: now}
	}
	entry.meta.UpdatedAt = now
	entry.value = append([]byte(nil), value...)
	f.entries[name] = entry
	return nil
}

func (f *FakeKeyStore) Get(_ context.Context, name string) ([]byte, error) {
	if err := ValidateSecretName(name); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	entry, ok := f.entries[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSecretNotFound, name)
	}
	return append([]byte(nil), entry.value...), nil
}

func (f *FakeKeyStore) List(_ context.Context) ([]SecretMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	result := make([]SecretMeta, 0, len(f.entries))
	for _, entry := range f.entries {
		result = append(result, entry.meta)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func (f *FakeKeyStore) Delete(_ context.Context, name string) error {
	if err := ValidateSecretName(name); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.entries[name]; !ok {
		return fmt.Errorf("%w: %s", ErrSecretNotFound, name)
	}
	delete(f.entries, name)
	return nil
}

func (f *FakeKeyStore) TouchLastUsed(_ context.Context, name string) error {
	if err := ValidateSecretName(name); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	entry, ok := f.entries[name]
	if !ok {
		return fmt.Errorf("%w: %s", ErrSecretNotFound, name)
	}
	entry.meta.LastUsedAt = f.now().UTC()
	f.entries[name] = entry
	return nil
}

var _ SecretStore = (*FakeKeyStore)(nil)
