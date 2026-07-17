package routedrun

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationRegistry_CurrentNoOp(t *testing.T) {
	reg := DefaultMigrationRegistry()
	if reg.Current() != CurrentSchemaVersion {
		t.Fatalf("current = %s", reg.Current())
	}
	outVer, out, err := reg.Migrate(CurrentSchemaVersion, []byte(`{"schema_version":"0.3.0","x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if outVer != CurrentSchemaVersion {
		t.Fatalf("to = %s", outVer)
	}
	if string(out) != `{"schema_version":"0.3.0","x":1}` {
		t.Fatalf("bytes mutated: %s", out)
	}
}

func TestMigrationRegistry_UnknownFailsClosed(t *testing.T) {
	reg := DefaultMigrationRegistry()
	_, _, err := reg.Migrate("9.9.9", []byte(`{"schema_version":"9.9.9"}`))
	if err == nil {
		t.Fatal("expected error for unknown version")
	}
	if !errorsIs(err, ErrUnknownSchemaVersion) {
		t.Fatalf("want ErrUnknownSchemaVersion, got %v", err)
	}
}

func TestMigrationRegistry_Chain(t *testing.T) {
	reg, err := NewMigrationRegistry("0.3.0", []Migration{
		{
			FromVersion: "0.1.0",
			ToVersion:   "0.2.0",
			Apply: func(state []byte) ([]byte, error) {
				var m map[string]any
				if err := json.Unmarshal(state, &m); err != nil {
					return nil, err
				}
				m["schema_version"] = "0.2.0"
				m["v2"] = true
				return json.Marshal(m)
			},
		},
		{
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Apply: func(state []byte) ([]byte, error) {
				var m map[string]any
				if err := json.Unmarshal(state, &m); err != nil {
					return nil, err
				}
				m["schema_version"] = "0.3.0"
				m["v3"] = true
				return json.Marshal(m)
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	in := []byte(`{"schema_version":"0.1.0","a":1}`)
	to, out, err := reg.Migrate("0.1.0", in)
	if err != nil {
		t.Fatal(err)
	}
	if to != "0.3.0" {
		t.Fatalf("to=%s", to)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if m["schema_version"] != "0.3.0" || m["v2"] != true || m["v3"] != true {
		t.Fatalf("migrated map = %#v", m)
	}
}

func TestMigrationRegistry_MigrateFileAtomic(t *testing.T) {
	dir := t.TempDir()
	// Use registry with 0.2.0 -> 0.3.0
	reg, err := NewMigrationRegistry(CurrentSchemaVersion, []Migration{
		{
			FromVersion: "0.2.0",
			ToVersion:   CurrentSchemaVersion,
			Apply: func(state []byte) ([]byte, error) {
				var m map[string]any
				if err := json.Unmarshal(state, &m); err != nil {
					return nil, err
				}
				m["schema_version"] = CurrentSchemaVersion
				m["migrated"] = true
				return json.Marshal(m)
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rec.json")
	// Write old version with 0600 via atomic helper.
	old := []byte(`{"schema_version":"0.2.0","x":1}`)
	if err := atomicWriteFile(path, old, filePerm); err != nil {
		t.Fatal(err)
	}
	if err := reg.MigrateFile(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m["schema_version"] != CurrentSchemaVersion || m["migrated"] != true {
		t.Fatalf("after migrate: %#v", m)
	}
	// Idempotent second migrate.
	if err := reg.MigrateFile(path); err != nil {
		t.Fatal(err)
	}
	// Unknown newer fails closed without clobber.
	if err := atomicWriteFile(path, []byte(`{"schema_version":"99.0.0"}`), filePerm); err != nil {
		t.Fatal(err)
	}
	if err := reg.MigrateFile(path); err == nil {
		t.Fatal("expected fail closed on newer")
	}
	data, _ = os.ReadFile(path)
	if !stringsContains(string(data), "99.0.0") {
		t.Fatalf("file should be untouched: %s", data)
	}
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})())
}
