// Package oauth — local delegated OAuth token store (M13.9-T2).
//
// Cloud counterpart encrypts tokens in TenantOAuthVault DO. Locally tokens
// live under a dedicated Keychain service suffix (or an in-memory store for
// tests). Token values never appear in policy, audit payloads, or list APIs
// that this package exposes as metadata-only.

package oauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNotFound is returned when no token exists for the key.
var ErrNotFound = errors.New("oauth token not found")

// TokenKey is the local identity for a delegated token set.
type TokenKey struct {
	CredentialID     string
	EndUserIdentity  string
	// DeploymentID is optional locally (single-install); included for parity.
	DeploymentID string
}

// TokenPayload is the secret material. Callers must not log it.
type TokenPayload struct {
	AccessToken     string    `json:"access_token"`
	RefreshToken    string    `json:"refresh_token"`
	AccessExpiresAt time.Time `json:"access_expires_at"`
	GrantedScopes   []string  `json:"granted_scopes"`
}

// TokenRecord is payload + version for cache invalidation (SC6).
type TokenRecord struct {
	TokenPayload
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store persists delegated OAuth tokens.
type Store interface {
	Put(key TokenKey, payload TokenPayload) (version int, err error)
	Get(key TokenKey) (*TokenRecord, error)
	Delete(key TokenKey) error
	// ListMeta returns credential labels only (no token values).
	ListMeta() ([]Meta, error)
}

// Meta is a non-secret listing row.
type Meta struct {
	CredentialID    string
	EndUserIdentity string
	DeploymentID    string
	GrantedScopes   []string
	Version         int
	UpdatedAt       time.Time
}

// StorageKey builds the stable account/service account name.
func StorageKey(k TokenKey) string {
	dep := k.DeploymentID
	if dep == "" {
		dep = "_"
	}
	return strings.Join([]string{
		sanitize(k.CredentialID),
		sanitize(dep),
		sanitize(k.EndUserIdentity),
	}, "|")
}

func sanitize(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "|", "_")
	return s
}

// MemoryStore is an in-process store for unit tests.
type MemoryStore struct {
	mu   sync.Mutex
	data map[string]TokenRecord
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]TokenRecord)}
}

func (m *MemoryStore) Put(key TokenKey, payload TokenPayload) (int, error) {
	if key.CredentialID == "" || key.EndUserIdentity == "" {
		return 0, fmt.Errorf("credential_id and end_user_identity required")
	}
	sk := StorageKey(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.data[sk]
	next := prev.Version + 1
	rec := TokenRecord{
		TokenPayload: payload,
		Version:      next,
		UpdatedAt:    time.Now().UTC(),
	}
	// copy scopes
	rec.GrantedScopes = append([]string(nil), payload.GrantedScopes...)
	m.data[sk] = rec
	return next, nil
}

func (m *MemoryStore) Get(key TokenKey) (*TokenRecord, error) {
	sk := StorageKey(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.data[sk]
	if !ok {
		return nil, ErrNotFound
	}
	out := rec
	out.GrantedScopes = append([]string(nil), rec.GrantedScopes...)
	return &out, nil
}

func (m *MemoryStore) Delete(key TokenKey) error {
	sk := StorageKey(key)
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, sk)
	return nil
}

func (m *MemoryStore) ListMeta() ([]Meta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Meta
	for sk, rec := range m.data {
		parts := strings.Split(sk, "|")
		if len(parts) != 3 {
			continue
		}
		out = append(out, Meta{
			CredentialID:    parts[0],
			DeploymentID:    parts[1],
			EndUserIdentity: parts[2],
			GrantedScopes:   append([]string(nil), rec.GrantedScopes...),
			Version:         rec.Version,
			UpdatedAt:       rec.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CredentialID != out[j].CredentialID {
			return out[i].CredentialID < out[j].CredentialID
		}
		return out[i].EndUserIdentity < out[j].EndUserIdentity
	})
	return out, nil
}

// KeychainServicePrefix is the macOS Keychain service name prefix for
// delegated OAuth tokens. Full service: ai.agentpaas.oauth
const KeychainServicePrefix = "ai.agentpaas.oauth"

// EncodePayloadJSON serializes a payload for Keychain storage.
func EncodePayloadJSON(p TokenPayload) ([]byte, error) {
	return json.Marshal(p)
}

// DecodePayloadJSON deserializes a payload from Keychain storage.
func DecodePayloadJSON(b []byte) (TokenPayload, error) {
	var p TokenPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return TokenPayload{}, err
	}
	return p, nil
}
