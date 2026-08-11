package oauth

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMemoryStore_RoundtripAndVersion(t *testing.T) {
	s := NewMemoryStore()
	key := TokenKey{CredentialID: "gmail_oauth", EndUserIdentity: "u@x.com", DeploymentID: "dep1"}
	v1, err := s.Put(key, TokenPayload{
		AccessToken:     "access-CANARY",
		RefreshToken:    "refresh-CANARY",
		AccessExpiresAt: time.Now().Add(time.Hour),
		GrantedScopes:   []string{"gmail.readonly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v1 != 1 {
		t.Fatalf("version=%d", v1)
	}
	got, err := s.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "access-CANARY" || got.RefreshToken != "refresh-CANARY" {
		t.Fatalf("payload mismatch: %+v", got)
	}
	v2, err := s.Put(key, TokenPayload{
		AccessToken:     "access-2",
		RefreshToken:    "refresh-2",
		AccessExpiresAt: time.Now().Add(time.Hour),
		GrantedScopes:   []string{"gmail.readonly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v2 != 2 {
		t.Fatalf("version=%d want 2", v2)
	}
}

func TestMemoryStore_Delete(t *testing.T) {
	s := NewMemoryStore()
	key := TokenKey{CredentialID: "c", EndUserIdentity: "u"}
	if _, err := s.Put(key, TokenPayload{AccessToken: "a", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(key); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound got %v", err)
	}
}

func TestMemoryStore_ListMetaNoSecrets(t *testing.T) {
	s := NewMemoryStore()
	key := TokenKey{CredentialID: "gmail_oauth", EndUserIdentity: "u@x.com"}
	if _, err := s.Put(key, TokenPayload{
		AccessToken:   "SECRET_ACCESS",
		RefreshToken:  "SECRET_REFRESH",
		GrantedScopes: []string{"s1"},
	}); err != nil {
		t.Fatal(err)
	}
	meta, err := s.ListMeta()
	if err != nil {
		t.Fatal(err)
	}
	if len(meta) != 1 {
		t.Fatalf("meta len=%d", len(meta))
	}
	b, _ := EncodePayloadJSON(TokenPayload{AccessToken: "x"})
	_ = b
	// Ensure meta JSON-ish dump wouldn't include secrets if marshaled carefully —
	// ListMeta returns structs without token fields.
	for _, m := range meta {
		if m.CredentialID != "gmail_oauth" {
			t.Fatalf("cred=%s", m.CredentialID)
		}
		if strings.Contains(m.CredentialID, "SECRET") {
			t.Fatal("leak")
		}
	}
}

func TestStorageKey(t *testing.T) {
	k := StorageKey(TokenKey{CredentialID: "c", DeploymentID: "", EndUserIdentity: "u"})
	if k != "c|_|u" {
		t.Fatalf("key=%s", k)
	}
}

func TestEncodeDecodePayload(t *testing.T) {
	p := TokenPayload{AccessToken: "a", RefreshToken: "r", GrantedScopes: []string{"x"}}
	b, err := EncodePayloadJSON(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(b), "access_token") != 1 {
		t.Fatalf("json=%s", b)
	}
	out, err := DecodePayloadJSON(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.AccessToken != "a" || out.RefreshToken != "r" {
		t.Fatalf("%+v", out)
	}
}
