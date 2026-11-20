package pack

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadLineage_Valid(t *testing.T) {
	dir := t.TempDir()
	parentPolicy := []byte("version: \"1.0\"\negress: []\n")
	prov := []ProvenanceEntry{{
		Action: "created", AgentName: "a", AgentVersion: "1",
		Timestamp: time.Now().UTC(),
	}}
	lf := LineageFile{
		Version: 1,
		Parent: LineageParent{
			AgentName:            "parent-agent",
			AgentVersion:         "2.0.0",
			PublisherFingerprint: "abc123",
			PublisherName:        "alice",
			LockDigest:           "lockdigest",
			BundleDigest:         "bundledigest",
			PolicyDigest:         "policydigest",
			PolicyYAMLB64:        base64.StdEncoding.EncodeToString(parentPolicy),
			Provenance:           prov,
		},
		ForkedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(lf)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLineage(dir)
	if err != nil {
		t.Fatalf("ReadLineage: %v", err)
	}
	if got.Version != 1 {
		t.Fatalf("version = %d", got.Version)
	}
	if got.Parent.AgentName != "parent-agent" {
		t.Fatalf("agent_name = %q", got.Parent.AgentName)
	}
	if got.Parent.AgentVersion != "2.0.0" {
		t.Fatalf("agent_version = %q", got.Parent.AgentVersion)
	}
	if got.Parent.PublisherFingerprint != "abc123" {
		t.Fatalf("publisher_fingerprint = %q", got.Parent.PublisherFingerprint)
	}
	if got.Parent.LockDigest != "lockdigest" {
		t.Fatalf("lock_digest = %q", got.Parent.LockDigest)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Parent.PolicyYAMLB64)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != string(parentPolicy) {
		t.Fatalf("policy yaml mismatch")
	}
	if len(got.Parent.Provenance) != 1 {
		t.Fatalf("provenance len = %d", len(got.Parent.Provenance))
	}
}

func TestReadLineage_UnknownField(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":1,"parent":{"agent_name":"a","agent_version":"1","publisher_fingerprint":"","publisher_name":"","lock_digest":"","bundle_digest":"","policy_digest":"","policy_yaml_b64":"","provenance":[]},"forked_at":"2020-01-01T00:00:00Z","evil":true}`
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLineage(dir); err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestReadLineage_UnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":2,"parent":{"agent_name":"a","agent_version":"1","publisher_fingerprint":"","publisher_name":"","lock_digest":"","bundle_digest":"","policy_digest":"","policy_yaml_b64":"","provenance":[]},"forked_at":"2020-01-01T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadLineage(dir)
	if err == nil || !strings.Contains(err.Error(), "unsupported lineage version") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadLineage_Oversized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, lineageFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write([]byte(`{"version":1`)); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxLineageFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLineage(dir); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadLineage_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lineageFileName), []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadLineage(dir); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestReadLineage_NotFound(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadLineage(dir)
	if err != ErrLineageNotFound {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatal("expected nil lineage")
	}
}