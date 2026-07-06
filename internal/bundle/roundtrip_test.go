package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func TestBundleRoundTripVerify(t *testing.T) {
	fix := writeTestBundle(t, true)
	b, err := Open(fix.Path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = b.Close() }()

	report, err := Verify(b)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !report.Verified {
		t.Fatalf("Verify failed: %+v", report.Checks)
	}
	if len(report.Checks) != 9 {
		t.Fatalf("got %d checks, want 9", len(report.Checks))
	}

	extractDir := t.TempDir()
	if err := b.ExtractSource(extractDir); err != nil {
		t.Fatalf("ExtractSource: %v", err)
	}
	gotDigest, err := pack.ComputeBuildInputDigest(extractDir, nil)
	if err != nil {
		t.Fatalf("ComputeBuildInputDigest: %v", err)
	}
	if gotDigest != fix.Config.Lock.BuildInputDigest {
		t.Fatalf("extracted digest %q != lock %q", gotDigest, fix.Config.Lock.BuildInputDigest)
	}

	origMain, err := os.ReadFile(filepath.Join(fix.ProjectDir, "main.py"))
	if err != nil {
		t.Fatalf("read original main.py: %v", err)
	}
	gotMain, err := os.ReadFile(filepath.Join(extractDir, "main.py"))
	if err != nil {
		t.Fatalf("read extracted main.py: %v", err)
	}
	if string(origMain) != string(gotMain) {
		t.Fatalf("main.py content mismatch")
	}
}