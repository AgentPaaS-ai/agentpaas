package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveNamedBinaryUsesHomebrewPrefixWhenLookPathFails(t *testing.T) {
	dir := t.TempDir()
	name := "agentpaas-doctor-docker-probe"
	fake := filepath.Join(dir, name)
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := resolveNamedBinary(name, []string{dir})
	if got != fake {
		t.Fatalf("resolveNamedBinary(%q) = %q, want %q", name, got, fake)
	}
}

func TestResolveNamedBinaryReturnsEmptyWhenMissing(t *testing.T) {
	got := resolveNamedBinary("agentpaas-doctor-docker-probe-missing", []string{t.TempDir()})
	if got != "" {
		t.Fatalf("resolveNamedBinary() = %q, want empty", got)
	}
}

func TestHomebrewBinDirsOrder(t *testing.T) {
	dirs := homebrewBinDirs()
	if len(dirs) != 2 {
		t.Fatalf("homebrewBinDirs() len = %d, want 2", len(dirs))
	}
	if dirs[0] != "/opt/homebrew/bin" || dirs[1] != "/usr/local/bin" {
		t.Fatalf("homebrewBinDirs() = %#v", dirs)
	}
}
