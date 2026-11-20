package pack

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// syntheticGitHubPAT is not a credential. gitleaks 8.30.1 reports it as
// github-pat. A sequential ghp_ sample does not clear that rule's entropy.
const syntheticGitHubPAT = "ghp_nfxgskz7KRVzXHEOAanhLSNhoC6u4CWbz025"

// genericAPIKeyLine matches gitleaks 8.30.1 generic-api-key and no other rule.
const genericAPIKeyLine = "api_key = \"7f3c8a1b9e2d4c6f8a0b1c2d3e4f5a6b\"\n"

func githubPATAllowlistTOML() string {
	return "[extend]\nuseDefault = true\n\n[allowlist]\nregexTarget = \"line\"\nregexes = ['''ghp_[0-9a-zA-Z]{36}''']\n"
}

func TestRunBundleGHPRefusedDespiteInheritedConfig(t *testing.T) {
	requireGitleaks8301(t)
	allowPath := filepath.Join(t.TempDir(), "allow.toml")
	if err := os.WriteFile(allowPath, []byte(githubPATAllowlistTOML()), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	t.Run("GITLEAKS_CONFIG", func(t *testing.T) {
		withEnv(t, "GITLEAKS_CONFIG", allowPath)
		withoutEnv(t, "GITLEAKS_CONFIG_TOML")
		assertImageSecretRefused(t, map[string][]byte{
			"main.py": []byte("token = \"" + syntheticGitHubPAT + "\"\n"),
		}, "app/main.py", "github-pat")
	})
	t.Run("GITLEAKS_CONFIG_TOML", func(t *testing.T) {
		withEnv(t, "GITLEAKS_CONFIG_TOML", githubPATAllowlistTOML())
		withoutEnv(t, "GITLEAKS_CONFIG")
		assertImageSecretRefused(t, map[string][]byte{
			"main.py": []byte("token = \"" + syntheticGitHubPAT + "\"\n"),
		}, "app/main.py", "github-pat")
	})
}

func TestRunBundleGHPAllowCommentRefused(t *testing.T) {
	requireGitleaks8301(t)
	line := "token = \"" + syntheticGitHubPAT + "\" # gitleaks:allow\n"
	if !strings.HasSuffix(strings.TrimRight(line, "\n"), "gitleaks:allow") {
		t.Fatal("fixture line does not end in gitleaks:allow")
	}
	assertImageSecretRefused(t, map[string][]byte{
		"main.py": []byte(line),
	}, "app/main.py", "github-pat")
}

func TestRunBundleGHPInDependencyRefused(t *testing.T) {
	requireGitleaks8301(t)
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('clean')\n"),
	}, map[string][]byte{
		"main.py":               []byte("print('clean')\n"),
		"deps/idna/__init__.py": []byte("token = \"" + syntheticGitHubPAT + "\"\n"),
	})
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	assertRunBundleRefused(t, err, lockPath, "app/deps/idna/__init__.py", "github-pat")
}

func TestRunBundleGenericAPIKeyInDependencyWritten(t *testing.T) {
	requireGitleaks8301(t)
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py": []byte("print('clean')\n"),
	}, map[string][]byte{
		"main.py":               []byte("print('clean')\n"),
		"deps/idna/__init__.py": []byte(genericAPIKeyLine),
	})
	if err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image)); err != nil {
		t.Fatalf("dependency generic-api-key was refused: %v", err)
	}
	bundle := filepath.Join(filepath.Dir(lockPath), RunBundleName)
	entries := readTarMap(t, mustRead(t, bundle))
	got, ok := entries["app/deps/idna/__init__.py"]
	if !ok {
		t.Fatal("bundle missing dependency file")
	}
	if string(got) != genericAPIKeyLine {
		t.Fatalf("dependency bytes = %q, want the generic-api-key line", got)
	}
}

func TestRunBundleGenericAPIKeyInProjectRefused(t *testing.T) {
	requireGitleaks8301(t)
	assertImageSecretRefused(t, map[string][]byte{
		"main.py": []byte(genericAPIKeyLine),
	}, "app/main.py", "generic-api-key")
}

func TestRunBundleGenericAPIKeyInProjectUnderDepsRefused(t *testing.T) {
	requireGitleaks8301(t)
	project, lockPath, image := runBundleFixture(t, map[string][]byte{
		"main.py":       []byte("print('clean')\n"),
		"deps/local.py": []byte("print('from-host')\n"),
	}, map[string][]byte{
		"main.py":       []byte("print('clean')\n"),
		"deps/local.py": []byte(genericAPIKeyLine),
	})
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	assertRunBundleRefused(t, err, lockPath, "app/deps/local.py", "generic-api-key")
}

func TestPinnedScannerInvocation(t *testing.T) {
	withEnv(t, "GITLEAKS_CONFIG", filepath.Join(t.TempDir(), "ignored.toml"))
	withEnv(t, "GITLEAKS_CONFIG_TOML", githubPATAllowlistTOML())

	t.Run("bundle", func(t *testing.T) {
		rec := filepath.Join(t.TempDir(), "record")
		installRecordingGitleaks(t, rec)
		project, lockPath, image := runBundleFixture(t, map[string][]byte{
			"main.py":       []byte("print('clean')\n"),
			"deps/local.py": []byte("print('project')\n"),
		}, map[string][]byte{
			"main.py":                          []byte("print('clean')\n"),
			"deps/local.py":                    []byte("print('project')\n"),
			"deps/idna/__init__.py":            []byte("# dep\n"),
			"python/agentpaas_sdk/__init__.py": []byte("# sdk\n"),
		})
		if err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image)); err != nil {
			t.Fatalf("WriteRunBundle: %v", err)
		}
		calls := parseGitleaksRecord(t, rec)
		if len(calls) == 0 {
			t.Fatal("bundle scan did not invoke gitleaks")
		}
		for _, call := range calls {
			assertPinnedGitleaksCall(t, call)
		}
		assertScanSplit(t, calls)
	})

	t.Run("ScanDirectorySecrets", func(t *testing.T) {
		rec := filepath.Join(t.TempDir(), "record")
		installRecordingGitleaks(t, rec)
		dir := t.TempDir()
		writeScanTestFile(t, dir, "notes.txt", "print('clean')\n")
		findings, err := ScanDirectorySecrets(context.Background(), dir)
		if err != nil {
			t.Fatalf("ScanDirectorySecrets: %v", err)
		}
		if len(findings) != 0 {
			t.Fatalf("findings = %#v, want none", findings)
		}
		calls := parseGitleaksRecord(t, rec)
		if len(calls) != 1 {
			t.Fatalf("ScanDirectorySecrets invocations = %d, want 1", len(calls))
		}
		assertPinnedGitleaksCall(t, calls[0])
		if calls[0].source != dir && !samePath(calls[0].source, dir) {
			t.Fatalf("source = %s, want %s", calls[0].source, dir)
		}
	})
}

func TestFirstConfigCleanOnSDKAndWeatherAgent(t *testing.T) {
	requireGitleaks8301(t)
	root := packRepoRoot(t)

	sdkCopy := filepath.Join(t.TempDir(), "app", "python")
	if err := os.CopyFS(sdkCopy, os.DirFS(filepath.Join(root, "python"))); err != nil {
		t.Fatalf("copy SDK tree to /app/python: %v", err)
	}
	findings, err := ScanDirectorySecrets(context.Background(), sdkCopy)
	if err != nil {
		t.Fatalf("scan SDK copy: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("SDK tree at /app/python findings = %#v, want none", findings)
	}

	weather := filepath.Join(root, "demo", "weather-agent")
	findings, err = ScanDirectorySecrets(context.Background(), weather)
	if err != nil {
		t.Fatalf("scan demo/weather-agent: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("demo/weather-agent findings = %#v, want none", findings)
	}
}

func assertImageSecretRefused(t *testing.T, imageApp map[string][]byte, file, rule string) {
	t.Helper()
	projectFiles := make(map[string][]byte, len(imageApp))
	for rel := range imageApp {
		projectFiles[rel] = []byte("print('clean')\n")
	}
	project, lockPath, image := runBundleFixture(t, projectFiles, imageApp)
	err := WriteRunBundle(context.Background(), runBundleConfig(project, lockPath, image))
	assertRunBundleRefused(t, err, lockPath, file, rule)
}

func assertRunBundleRefused(t *testing.T, err error, lockPath, file, rule string) {
	t.Helper()
	if err == nil {
		t.Fatal("secret was written into the run bundle")
	}
	msg := err.Error()
	if !strings.Contains(msg, file) {
		t.Fatalf("error = %v, want file %s", err, file)
	}
	if !strings.Contains(msg, rule) {
		t.Fatalf("error = %v, want rule %s", err, rule)
	}
	bundle := filepath.Join(filepath.Dir(lockPath), RunBundleName)
	if _, statErr := os.Stat(bundle); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("run.bundle.tar present after refusal, stat err = %v", statErr)
	}
}

func requireGitleaks8301(t *testing.T) {
	t.Helper()
	out, err := exec.Command("gitleaks", "version").CombinedOutput()
	if err != nil {
		t.Fatalf("gitleaks version: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, "8.30.1") {
		t.Fatalf("gitleaks version = %q, want 8.30.1", got)
	}
	t.Logf("gitleaks %s", got)
}

func packRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func withEnv(t *testing.T, key, val string) {
	t.Helper()
	prev, ok := os.LookupEnv(key)
	if err := os.Setenv(key, val); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func withoutEnv(t *testing.T, key string) {
	t.Helper()
	prev, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, prev)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}

func installRecordingGitleaks(t *testing.T, record string) {
	t.Helper()
	binDir := t.TempDir()
	script := "#!/bin/sh\n" +
		"rec=\"${PACK_SCAN_RECORD:?}\"\n" +
		"{\n" +
		"printf '###CALL###\\n'\n" +
		"printf 'PWD=%s\\n' \"$(pwd)\"\n" +
		"printf 'ARGS\\n'\n" +
		"printf '%s\\n' \"$@\"\n" +
		"prev=\"\"\n" +
		"cfg=\"\"\n" +
		"src=\"\"\n" +
		"ign=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"--config\" ]; then cfg=\"$a\"; fi\n" +
		"  if [ \"$prev\" = \"--source\" ]; then src=\"$a\"; fi\n" +
		"  if [ \"$prev\" = \"--gitleaks-ignore-path\" ]; then ign=\"$a\"; fi\n" +
		"  prev=\"$a\"\n" +
		"done\n" +
		"printf 'CONFIG_PATH=%s\\n' \"$cfg\"\n" +
		"printf 'CONFIG_BODY\\n'\n" +
		"if [ -f \"$cfg\" ]; then cat \"$cfg\"; fi\n" +
		"printf '\\nSOURCE_PATH=%s\\n' \"$src\"\n" +
		"printf 'FILES\\n'\n" +
		"if [ -n \"$src\" ]; then find \"$src\" -type f | sort; fi\n" +
		"printf 'IGNORE_PATH=%s\\n' \"$ign\"\n" +
		"empty=no\n" +
		"if [ -d \"$ign\" ] && [ -z \"$(find \"$ign\" -mindepth 1 -print -quit)\" ]; then empty=yes; fi\n" +
		"printf 'IGNORE_EMPTY=%s\\n' \"$empty\"\n" +
		"printf 'ENV\\n'\n" +
		"env\n" +
		"printf '\\n###END###\\n'\n" +
		"} >> \"$rec\"\n" +
		"printf '[]\\n'\n" +
		"exit 0\n"
	path := filepath.Join(binDir, "gitleaks")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write recording gitleaks: %v", err)
	}
	withEnv(t, "PACK_SCAN_RECORD", record)
	withEnv(t, "PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

type recordedGitleaksCall struct {
	pwd         string
	args        []string
	configPath  string
	configBody  string
	source      string
	files       []string
	ignorePath  string
	ignoreEmpty string
	env         string
}

func parseGitleaksRecord(t *testing.T, path string) []recordedGitleaksCall {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gitleaks record: %v", err)
	}
	raw := string(data)
	if strings.Contains(raw, "GITLEAKS_") {
		t.Fatal("recorded child environment contains a GITLEAKS_ variable")
	}
	var calls []recordedGitleaksCall
	for _, chunk := range strings.Split(raw, "###CALL###\n")[1:] {
		body, _, _ := strings.Cut(chunk, "\n###END###")
		calls = append(calls, parseGitleaksChunk(t, body))
	}
	return calls
}

func parseGitleaksChunk(t *testing.T, body string) recordedGitleaksCall {
	t.Helper()
	var call recordedGitleaksCall
	section := ""
	var args, files, config []string
	for _, line := range strings.Split(body, "\n") {
		switch {
		case line == "ARGS" || line == "CONFIG_BODY" || line == "FILES" || line == "ENV":
			section = line
			continue
		case strings.HasPrefix(line, "PWD="):
			call.pwd = strings.TrimPrefix(line, "PWD=")
			section = ""
			continue
		case strings.HasPrefix(line, "CONFIG_PATH="):
			call.configPath = strings.TrimPrefix(line, "CONFIG_PATH=")
			section = ""
			continue
		case strings.HasPrefix(line, "SOURCE_PATH="):
			call.source = strings.TrimPrefix(line, "SOURCE_PATH=")
			section = ""
			continue
		case strings.HasPrefix(line, "IGNORE_PATH="):
			call.ignorePath = strings.TrimPrefix(line, "IGNORE_PATH=")
			section = ""
			continue
		case strings.HasPrefix(line, "IGNORE_EMPTY="):
			call.ignoreEmpty = strings.TrimPrefix(line, "IGNORE_EMPTY=")
			section = ""
			continue
		}
		switch section {
		case "ARGS":
			if line != "" {
				args = append(args, line)
			}
		case "FILES":
			if line != "" {
				files = append(files, line)
			}
		case "CONFIG_BODY":
			config = append(config, line)
		case "ENV":
			call.env += line + "\n"
		}
	}
	call.args = args
	call.files = files
	call.configBody = strings.Join(config, "\n")
	return call
}

func assertPinnedGitleaksCall(t *testing.T, call recordedGitleaksCall) {
	t.Helper()
	if !hasArg(call.args, "--ignore-gitleaks-allow") {
		t.Fatalf("args = %q, want --ignore-gitleaks-allow", call.args)
	}
	if hasArg(call.args, "--baseline-path") {
		t.Fatalf("args = %q, want no --baseline-path", call.args)
	}
	if call.configPath == "" {
		t.Fatal("--config was not set")
	}
	if pathInside(call.configPath, call.source) {
		t.Fatalf("config %s is inside scanned dir %s", call.configPath, call.source)
	}
	if !strings.Contains(call.configBody, "useDefault = true") {
		t.Fatalf("config body = %q, want pack-owned extend useDefault", call.configBody)
	}
	if call.ignorePath == "" {
		t.Fatal("--gitleaks-ignore-path was not set")
	}
	if !samePath(call.ignorePath, call.pwd) {
		t.Fatalf("ignore path %s != working directory %s", call.ignorePath, call.pwd)
	}
	if call.ignoreEmpty != "yes" {
		t.Fatalf("ignore dir empty = %q, want yes", call.ignoreEmpty)
	}
	if strings.Contains(call.env, "GITLEAKS_") {
		t.Fatal("child env contains a GITLEAKS_ variable")
	}
}

func assertScanSplit(t *testing.T, calls []recordedGitleaksCall) {
	t.Helper()
	var strict, deps int
	for _, call := range calls {
		disabled := strings.Contains(call.configBody, "generic-api-key") && strings.Contains(call.configBody, "curl-auth-header")
		if disabled {
			deps++
			for _, file := range call.files {
				rel := strings.TrimPrefix(file, call.source+"/")
				rel = filepath.ToSlash(rel)
				if rel == "agentpaas/requirements.lock" || strings.HasPrefix(rel, "app/python/") || rel == "app/main.py" || rel == "app/deps/local.py" {
					t.Fatalf("dependency config scanned %s", rel)
				}
				if !strings.HasPrefix(rel, "app/deps/") {
					t.Fatalf("dependency config scanned %s, want only app/deps", rel)
				}
			}
			continue
		}
		strict++
		joined := strings.Join(call.files, "\n")
		for _, want := range []string{
			"agentpaas/requirements.lock",
			"app/main.py",
			"app/deps/local.py",
			"app/python/agentpaas_sdk/__init__.py",
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("first config scan files = %s, missing %s", joined, want)
			}
		}
		if strings.Contains(joined, "app/deps/idna/__init__.py") {
			t.Fatal("first config scanned a dependency file")
		}
	}
	if strict != 1 || deps != 1 {
		t.Fatalf("scans strict=%d deps=%d, want 1 and 1", strict, deps)
	}
}

func hasArg(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func pathInside(path, dir string) bool {
	if dir == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func samePath(a, b string) bool {
	return canonicalPath(a) == canonicalPath(b)
}

// canonicalPath resolves symlinks on the existing ancestor so /var and
// /private/var compare equal after the temp dir has been removed.
func canonicalPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	cur := abs
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
}
