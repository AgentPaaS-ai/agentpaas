package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdversaryForkCmdNonEmptyHiddenFileRefused(t *testing.T) {
	fix := materializeWeatherAgentForProvenance(t)
	target := filepath.Join(t.TempDir(), "hidden-cli")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".hidden"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := executeFork(t, fix.homeDir, fix.ref, target)
	if err == nil {
		t.Fatal("want error")
	}
}
