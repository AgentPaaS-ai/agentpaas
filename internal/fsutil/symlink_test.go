package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRejectSymlinkLeaf_MissingOK(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := RejectSymlinkLeaf(missing); err != nil {
		t.Fatalf("missing path should be OK: %v", err)
	}
}

func TestRejectSymlinkLeaf_RegularFileOK(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RejectSymlinkLeaf(path); err != nil {
		t.Fatalf("regular file rejected: %v", err)
	}
}

func TestRejectSymlinkLeaf_SymlinkRejected(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := RejectSymlinkLeaf(link)
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("got %v, want ErrSymlink", err)
	}
	var se *SymlinkError
	if !errors.As(err, &se) {
		t.Fatalf("want *SymlinkError, got %T", err)
	}
	if se.Path != link {
		t.Fatalf("Path = %q, want %q", se.Path, link)
	}
}

func TestRejectSymlinkPathAndParent_Empty(t *testing.T) {
	t.Parallel()
	if err := RejectSymlinkPathAndParent(""); err == nil {
		t.Fatal("empty path must error")
	}
}

func TestRejectSymlinkPathAndParent_ParentSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(realDir, "f")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "linkdir")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatal(err)
	}
	// path itself is a normal file under a symlink parent
	viaLink := filepath.Join(linkDir, "f")
	err := RejectSymlinkPathAndParent(viaLink)
	if err == nil {
		t.Fatal("expected parent symlink rejection")
	}
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("got %v, want ErrSymlink", err)
	}
}

func TestRejectSymlinkInRoot_Escape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	err := RejectSymlinkInRoot(root, outside)
	if err == nil {
		t.Fatal("expected path escape error")
	}
	if !errors.Is(err, ErrPathEscapes) {
		t.Fatalf("got %v, want ErrPathEscapes", err)
	}
}

func TestRejectSymlinkInRoot_RootOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := RejectSymlinkInRoot(root, root); err != nil {
		t.Fatalf("root==path should pass: %v", err)
	}
}

func TestRejectSymlinkInRoot_NestedSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "evil")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	err := RejectSymlinkInRoot(root, link)
	if err == nil {
		t.Fatal("expected nested symlink rejection")
	}
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("got %v, want ErrSymlink", err)
	}
}

func TestRejectSymlinkWalk_RequireAbsolute(t *testing.T) {
	t.Parallel()
	err := RejectSymlinkWalk("relative/path", WalkOptions{RequireAbsolute: true})
	if err == nil {
		t.Fatal("relative path with RequireAbsolute must fail")
	}
}

func TestRejectSymlinkWalk_RejectDotDot(t *testing.T) {
	t.Parallel()
	// Use a path that still contains ".." after Clean is not used before check
	// when ResolveAbs is false — Clean collapses .., so construct via opts.
	err := RejectSymlinkWalk("/tmp/foo/../bar", WalkOptions{
		RequireAbsolute: true,
		RejectDotDot:    true,
		// Clean removes .. before RejectDotDot sees it in RejectSymlinkWalk —
		// HasDotDotPathSegment is applied to cleaned path. Document actual behavior.
	})
	// After Clean("/tmp/foo/../bar") => "/tmp/bar", no ".." remains.
	if err != nil && strings.Contains(err.Error(), "dot-dot") {
		t.Fatalf("cleaned path should not retain ..: %v", err)
	}
	// Explicit unclean-looking segment without Clean would need raw input; Clean always runs.
	if HasDotDotPathSegment("/tmp/foo/../bar") {
		// path has .. before clean
	} else {
		t.Fatal("HasDotDotPathSegment should detect .. in raw path")
	}
	if HasDotDotPathSegment(filepath.Clean("/tmp/foo/../bar")) {
		t.Fatal("cleaned path must not report ..")
	}
}

func TestRejectSymlinkWalk_MissingAllowLeaf(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missingLeaf := filepath.Join(root, "nope")
	// On macOS TempDir often lives under /var which is a symlink to /private/var.
	// SkipVolumeRootSymlinks matches production pack/detect behavior.
	opts := WalkOptions{Missing: MissingAllowLeaf, SkipVolumeRootSymlinks: true, ResolveAbs: true}
	if err := RejectSymlinkWalk(missingLeaf, opts); err != nil {
		t.Fatalf("MissingAllowLeaf should allow missing leaf: %v", err)
	}
	// Missing intermediate component should fail under MissingAllowLeaf
	deep := filepath.Join(root, "missing-mid", "leaf")
	if err := RejectSymlinkWalk(deep, WalkOptions{Missing: MissingAllowLeaf}); err == nil {
		// Without SkipVolumeRootSymlinks may fail earlier on /var; accept either
		// intermediate-missing error or volume-root symlink rejection as fail-closed.
		t.Fatal("MissingAllowLeaf must fail when intermediate is missing")
	}
}

func TestRejectSymlinkWalk_MissingFail(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missing := filepath.Join(root, "gone")
	err := RejectSymlinkWalk(missing, WalkOptions{Missing: MissingFail, SkipVolumeRootSymlinks: true, ResolveAbs: true})
	if err == nil {
		t.Fatal("MissingFail must reject missing path")
	}
}

func TestRejectSymlinkWalk_SymlinkComponent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	err := RejectSymlinkWalk(link, WalkOptions{Missing: MissingFail, SkipVolumeRootSymlinks: true, ResolveAbs: true})
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("got %v, want ErrSymlink", err)
	}
}

func TestHasDotDotPathSegment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		want bool
	}{
		{"foo/bar", false},
		{"foo/../bar", true},
		{"../bar", true},
		{"foo/..", true},
		{"foo/..bar", false},
		{"", false},
		{".", false},
	}
	for _, tc := range cases {
		got := HasDotDotPathSegment(tc.path)
		if got != tc.want {
			t.Errorf("HasDotDotPathSegment(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func TestSafeID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		// wantExact is used when output is deterministic non-hash
		wantExact string
		// wantPrefix is used for hashed outputs
		wantPrefix string
	}{
		{"empty", "", "_invalid", ""},
		{"dot", ".", "_invalid", ""},
		{"dotdot", "..", "_invalid", ""},
		{"simple", "agent-1", "agent-1", ""},
		{"slash", "a/b", "", "h-"},
		{"backslash", `a\b`, "", "h-"},
		{"control", "a\x00b", "", "h-"},
		{"long", strings.Repeat("a", 201), "", "h-"},
		{"boundary_200", strings.Repeat("b", 200), strings.Repeat("b", 200), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeID(tc.in)
			if tc.wantExact != "" {
				if got != tc.wantExact {
					t.Fatalf("SafeID(%q) = %q, want %q", tc.in, got, tc.wantExact)
				}
				return
			}
			if !strings.HasPrefix(got, tc.wantPrefix) {
				t.Fatalf("SafeID(%q) = %q, want prefix %q", tc.in, got, tc.wantPrefix)
			}
			// Stable hash
			if again := SafeID(tc.in); again != got {
				t.Fatalf("SafeID not stable: %q vs %q", got, again)
			}
			// Safe as single path component
			if strings.ContainsAny(got, `/\:`) {
				t.Fatalf("SafeID produced unsafe component: %q", got)
			}
		})
	}
}

func TestSymlinkError_NilReceiver(t *testing.T) {
	t.Parallel()
	var se *SymlinkError
	if se.Error() != ErrSymlink.Error() {
		t.Fatalf("nil SymlinkError.Error = %q", se.Error())
	}
	if !se.Is(ErrSymlink) {
		t.Fatal("nil SymlinkError should Is ErrSymlink")
	}
}

func TestPathEscapesError_NilReceiver(t *testing.T) {
	t.Parallel()
	var pe *PathEscapesError
	if pe.Error() != ErrPathEscapes.Error() {
		t.Fatalf("nil PathEscapesError.Error = %q", pe.Error())
	}
	if !pe.Is(ErrPathEscapes) {
		t.Fatal("nil PathEscapesError should Is ErrPathEscapes")
	}
}
