package pack

import "testing"

func TestMatchExactFile(t *testing.T) {
	matcher := NewIgnoreMatcher(".git\n")

	if !matcher.Match(".git") {
		t.Fatal("Match(.git) = false, want true")
	}
}

func TestMatchGlob(t *testing.T) {
	matcher := NewIgnoreMatcher("*.pyc\n")

	if !matcher.Match("foo.pyc") {
		t.Fatal("Match(foo.pyc) = false, want true")
	}
}

func TestMatchDirectory(t *testing.T) {
	matcher := NewIgnoreMatcher("node_modules/\n")

	if !matcher.Match("node_modules/package/index.js") {
		t.Fatal("Match(node_modules/package/index.js) = false, want true")
	}
}

func TestMatchNegation(t *testing.T) {
	matcher := NewIgnoreMatcher("*.pyc\n!important.pyc\n")

	if matcher.Match("important.pyc") {
		t.Fatal("Match(important.pyc) = true, want false")
	}
	if !matcher.Match("other.pyc") {
		t.Fatal("Match(other.pyc) = false, want true")
	}
}

func TestMatchComment(t *testing.T) {
	matcher := NewIgnoreMatcher("# *.pyc\n")

	if matcher.Match("foo.pyc") {
		t.Fatal("Match(foo.pyc) = true, want false")
	}
}

func TestDefaultIgnorePatterns(t *testing.T) {
	patterns := DefaultIgnorePatterns()
	if len(patterns) == 0 {
		t.Fatal("DefaultIgnorePatterns() is empty")
	}

	for _, want := range []string{".git", "__pycache__", "*.pyc", ".venv", "node_modules"} {
		if !stringSliceContains(patterns, want) {
			t.Fatalf("DefaultIgnorePatterns() missing %q in %#v", want, patterns)
		}
	}
}

func TestLoadIgnoreNotExist(t *testing.T) {
	projectDir := t.TempDir()

	matcher, err := LoadIgnore(projectDir)
	if err != nil {
		t.Fatalf("LoadIgnore() error = %v", err)
	}
	if !matcher.Match(".git") {
		t.Fatal("default matcher Match(.git) = false, want true")
	}
}

func TestLoadIgnoreExists(t *testing.T) {
	projectDir := t.TempDir()
	writeTestFile(t, projectDir, ".agentpaasignore", "custom.tmp\n")

	matcher, err := LoadIgnore(projectDir)
	if err != nil {
		t.Fatalf("LoadIgnore() error = %v", err)
	}
	if !matcher.Match("custom.tmp") {
		t.Fatal("Match(custom.tmp) = false, want true")
	}
	if matcher.Match(".git") {
		t.Fatal("Match(.git) = true, want false for custom matcher")
	}
}

func TestMatchEmptyPath(t *testing.T) {
	matcher := NewIgnoreMatcher("*.pyc\n")

	if matcher.Match("") {
		t.Fatal("Match(empty) = true, want false")
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
