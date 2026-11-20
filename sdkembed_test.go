package agentpaas

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasEmbeddedSDK(t *testing.T) {
	if !HasEmbeddedSDK() {
		t.Fatal("HasEmbeddedSDK() = false; expected the SDK to be embedded in the binary")
	}
}

func TestExtractEmbeddedSDK(t *testing.T) {
	tmp := t.TempDir()

	sdkDir, err := ExtractEmbeddedSDK(tmp)
	if err != nil {
		t.Fatalf("ExtractEmbeddedSDK: %v", err)
	}

	// sdkDir should be tmp/python
	wantSDKDir := filepath.Join(tmp, "python")
	if sdkDir != wantSDKDir {
		t.Errorf("sdkDir = %q, want %q", sdkDir, wantSDKDir)
	}

	// Check that agentpaas_sdk/ exists with the expected files
	expected := []string{
		"agentpaas_sdk/__init__.py",
		"agentpaas_sdk/agent.py",
		"agentpaas_sdk/_rpc.py",
		"agentpaas_sdk/runner.py",
	}
	for _, f := range expected {
		p := filepath.Join(sdkDir, f)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s: %v", f, err)
		}
	}

	// Ensure no __pycache__ or .pyc files were extracted
	_ = filepath.Walk(sdkDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		base := filepath.Base(path)
		if base == "__pycache__" {
			t.Errorf("__pycache__ was extracted to %s", path)
		}
		if filepath.Ext(base) == ".pyc" {
			t.Errorf(".pyc file was extracted: %s", path)
		}
		return nil
	})
}

func TestExtractEmbeddedSDKToTemp(t *testing.T) {
	sdkDir, cleanup, err := ExtractEmbeddedSDKToTemp()
	if err != nil {
		t.Fatalf("ExtractEmbeddedSDKToTemp: %v", err)
	}
	defer cleanup()

	// Verify SDK dir exists and has files
	entries, err := os.ReadDir(filepath.Join(sdkDir, "agentpaas_sdk"))
	if err != nil {
		t.Fatalf("read agentpaas_sdk dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("agentpaas_sdk dir is empty after extraction")
	}

	// Cleanup should remove the temp dir
	cleanup()
	if _, err := os.Stat(sdkDir); !os.IsNotExist(err) {
		t.Errorf("after cleanup, sdkDir still exists: %v", err)
	}
}
