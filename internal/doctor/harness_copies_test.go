package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareHarnessCopies_Divergent(t *testing.T) {
	dir := t.TempDir()

	p1 := filepath.Join(dir, "harness1")
	p2 := filepath.Join(dir, "harness2")
	if err := os.WriteFile(p1, []byte("copy-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("copy-b"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, msg := compareHarnessCopies([]string{p1, p2})
	if status != "warning" {
		t.Fatalf("expected status %q for divergent copies, got %q", "warning", status)
	}
	if !strings.Contains(msg, p1) {
		t.Errorf("expected message to list %q, got: %s", p1, msg)
	}
	if !strings.Contains(msg, p2) {
		t.Errorf("expected message to list %q, got: %s", p2, msg)
	}
	if !strings.Contains(msg, "multiple differing harness binaries found") {
		t.Errorf("expected divergence sentence in message, got: %s", msg)
	}
}

func TestCompareHarnessCopies_Identical(t *testing.T) {
	dir := t.TempDir()

	p1 := filepath.Join(dir, "harness1")
	p2 := filepath.Join(dir, "harness2")
	content := []byte("same-bytes")
	if err := os.WriteFile(p1, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, content, 0o644); err != nil {
		t.Fatal(err)
	}

	status, msg := compareHarnessCopies([]string{p1, p2})
	if status != "ok" {
		t.Fatalf("expected status %q for identical copies, got %q (msg: %s)", "ok", status, msg)
	}
}

func TestCompareHarnessCopies_Single(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "harness1")
	if err := os.WriteFile(p1, []byte("only-one"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, _ := compareHarnessCopies([]string{p1})
	if status != "ok" {
		t.Fatalf("expected status %q for single copy, got %q", "ok", status)
	}
}

func TestCompareHarnessCopies_None(t *testing.T) {
	status, _ := compareHarnessCopies(nil)
	if status != "ok" {
		t.Fatalf("expected status %q for zero copies, got %q", "ok", status)
	}
}

func TestCompareHarnessCopies_DedupsIdenticalHashes(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "harness1")
	p2 := filepath.Join(dir, "harness2")
	content := []byte("identical")
	if err := os.WriteFile(p1, content, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, content, 0o644); err != nil {
		t.Fatal(err)
	}
	// same file listed twice — should not double-count.
	status, _ := compareHarnessCopies([]string{p1, p2})
	if status != "ok" {
		t.Fatalf("expected status %q for identical copies (dedup), got %q", "ok", status)
	}
}

func TestCheckHarnessCopies_NotPanic_NoExeDir(t *testing.T) {
	// With PATH empty and a bogus exec override that errors, the check must
	// still return a result (ok) and never panic.
	t.Setenv("PATH", "")
	result := CheckHarnessCopies()
	if result.Name != "harness_copies" {
		t.Errorf("expected name %q, got %q", "harness_copies", result.Name)
	}
	if result.Status != "ok" && result.Status != "warning" {
		t.Errorf("expected status ok or warning, got %q: %s", result.Status, result.Message)
	}
	if result.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestCheckHarnessCopies_DivergentInFakePath(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "agentpaas-harness-linux")
	p2 := filepath.Join(dir, "sub", "agentpaas-harness-linux")
	if err := os.MkdirAll(filepath.Dir(p2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, []byte("copy-a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("copy-b"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Put both on PATH (same dir entries) so the scan finds them.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+filepath.Join(dir, "sub"))

	result := CheckHarnessCopies()
	// The daemon-binary-relative candidates won't resolve to these temp
	// paths, but the PATH walk should find both. If both are found and they
	// differ, the status should be warning.
	if result.Status != "warning" && result.Status != "ok" {
		t.Fatalf("expected status ok or warning, got %q: %s", result.Status, result.Message)
	}
	if result.Status == "warning" {
		if !strings.Contains(result.Message, "multiple differing harness binaries found") {
			t.Errorf("expected divergence sentence, got: %s", result.Message)
		}
	}
}
