package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seatWaitToml(flag string) string {
	return "[vars]\nSEAT_WAIT_WORKFLOW_PRIMITIVE = \"" + flag + "\"\n"
}

func writeSeatWaitWorker(t *testing.T, flag string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wrangler.toml"), []byte(seatWaitToml(flag)), 0o600); err != nil {
		t.Fatalf("write wrangler.toml: %v", err)
	}
	return dir
}

func withSeatWaitFixture(t *testing.T, fn seatWaitFixtureFunc) {
	t.Helper()
	prev := runSeatWaitFixture
	runSeatWaitFixture = fn
	t.Cleanup(func() { runSeatWaitFixture = prev })
}

func seatWaitProofLines(flag, parent string) string {
	line := func(scenario string) string {
		return fmt.Sprintf(
			"W27_SEAT_WAIT {\"flag\":%q,\"worker_env_flag\":%q,\"parent_error\":%q,\"child_error\":\"seat_wait_timeout\",\"child_promoted\":false,\"hop_elapsed_ms\":12,\"seat_wait_min_ms\":60000,\"retry_on_hop\":0,\"start_on_hop\":0}\n",
			scenario, flag, parent,
		)
	}
	return "W27_WORKER_ENV " + flag + "\n" + line("0") + line("1")
}

func TestCloudSeatWait_CommandRegistered(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	if _, _, err := cmd.Find([]string{"cloud", "seat-wait", "prove"}); err != nil {
		t.Fatalf("Find cloud seat-wait prove: %v", err)
	}
}

func TestCloudHelp_HasSeatWait(t *testing.T) {
	stdout, _, err := executeCloudCmd(t, "", "cloud", "--help")
	if err != nil {
		t.Fatalf("cloud --help: %v", err)
	}
	if !strings.Contains(stdout, "seat-wait") {
		t.Fatalf("cloud --help should mention seat-wait, got: %s", stdout)
	}
}

func TestSeatWaitFixtureArgs_DoesNotCallWrangler(t *testing.T) {
	args := seatWaitFixtureArgs()
	joined := strings.ToLower(strings.Join(args, " "))
	if strings.Contains(joined, "wrangler") {
		t.Fatalf("fixture args must not call wrangler: %v", args)
	}
	if strings.Contains(joined, "admin") {
		t.Fatalf("fixture args must not admin-PATCH: %v", args)
	}
}

func TestCloudSeatWaitProve_FlipsOnRunsFixtureFlipsOff(t *testing.T) {
	dir := writeSeatWaitWorker(t, "0")
	var seen []string
	withSeatWaitFixture(t, func(_ context.Context, workerDir string) (string, error) {
		data, err := os.ReadFile(filepath.Join(workerDir, "wrangler.toml"))
		if err != nil {
			return "", err
		}
		flag, err := seatWaitFlagValue(data)
		if err != nil {
			return "", err
		}
		seen = append(seen, flag)
		return seatWaitProofLines(flag, "max_duration_exceeded"), nil
	})

	stdout, stderr, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", dir)
	if err != nil {
		t.Fatalf("prove: %v stderr=%s stdout=%s", err, stderr, stdout)
	}
	if len(seen) != 2 || seen[0] != "1" || seen[1] != "0" {
		t.Fatalf("fixture saw flags %v, want [1 0]", seen)
	}
	after, err := os.ReadFile(filepath.Join(dir, "wrangler.toml"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	flag, err := seatWaitFlagValue(after)
	if err != nil {
		t.Fatalf("flag after: %v", err)
	}
	if flag != "0" {
		t.Fatalf("flag left %s, want 0", flag)
	}
	for _, want := range []string{
		"flag_before 0",
		"flag_on 1",
		"flag_after 0",
		"terminals_match true",
		"wrangler false",
		"flag_on_parent_error max_duration_exceeded",
		"flag_on_child_error seat_wait_timeout",
		"flag_on_child_promoted false",
		"flag_off_parent_error max_duration_exceeded",
		"flag_off_child_error seat_wait_timeout",
		"flag_off_child_promoted false",
		"hop_spanned_seat_wait_min false",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q\n%s", want, stdout)
		}
	}
}

func TestCloudSeatWaitProve_RestoresFlagWhenFixtureFails(t *testing.T) {
	dir := writeSeatWaitWorker(t, "0")
	withSeatWaitFixture(t, func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("fixture failed")
	})
	_, _, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", dir)
	if err == nil {
		t.Fatal("expected fixture failure")
	}
	after, readErr := os.ReadFile(filepath.Join(dir, "wrangler.toml"))
	if readErr != nil {
		t.Fatalf("read after: %v", readErr)
	}
	flag, flagErr := seatWaitFlagValue(after)
	if flagErr != nil {
		t.Fatalf("flag after: %v", flagErr)
	}
	if flag != "0" {
		t.Fatalf("flag left %s after fixture failure, want 0", flag)
	}
}

func TestCloudSeatWaitProve_RejectsRelativePath(t *testing.T) {
	_, _, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", "relative/cloud")
	if err == nil {
		t.Fatal("expected relative path rejection")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %v, want absolute", err)
	}
}

func TestCloudSeatWaitProve_RejectsDotDot(t *testing.T) {
	_, _, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", "/Users/pms88/../etc")
	if err == nil {
		t.Fatal("expected .. rejection")
	}
	if !strings.Contains(err.Error(), "..") {
		t.Fatalf("error = %v, want ..", err)
	}
}

func TestCloudSeatWaitProve_RejectsSystemDir(t *testing.T) {
	_, _, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", "/etc")
	if err == nil {
		t.Fatal("expected system dir rejection")
	}
	if !strings.Contains(err.Error(), "system") {
		t.Fatalf("error = %v, want system", err)
	}
}

func TestCloudSeatWaitProve_RejectsNullByte(t *testing.T) {
	_, err := validateSeatWaitWorkerDir("/tmp/cloud\x00")
	if err == nil {
		t.Fatal("expected null byte rejection")
	}
}

func TestCloudSeatWaitProve_RejectsSymlinkWrangler(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.toml")
	if err := os.WriteFile(real, []byte(seatWaitToml("0")), 0o600); err != nil {
		t.Fatalf("write real: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "wrangler.toml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	called := false
	withSeatWaitFixture(t, func(_ context.Context, _ string) (string, error) {
		called = true
		return "", nil
	})
	_, _, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", dir)
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
	if called {
		t.Fatal("fixture ran against a symlinked wrangler.toml")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("error = %v, want symlink", err)
	}
}

func TestCloudSeatWaitProve_MismatchLeavesFlagOff(t *testing.T) {
	dir := writeSeatWaitWorker(t, "0")
	withSeatWaitFixture(t, func(_ context.Context, workerDir string) (string, error) {
		data, err := os.ReadFile(filepath.Join(workerDir, "wrangler.toml"))
		if err != nil {
			return "", err
		}
		flag, err := seatWaitFlagValue(data)
		if err != nil {
			return "", err
		}
		parent := "max_duration_exceeded"
		if flag == "1" {
			parent = "seat_wait_timeout"
		}
		return seatWaitProofLines(flag, parent), nil
	})
	_, _, err := executeCloudCmd(t, "", "cloud", "seat-wait", "prove", "--worker-dir", dir)
	if err == nil {
		t.Fatal("expected terminal mismatch")
	}
	after, readErr := os.ReadFile(filepath.Join(dir, "wrangler.toml"))
	if readErr != nil {
		t.Fatalf("read after: %v", readErr)
	}
	flag, flagErr := seatWaitFlagValue(after)
	if flagErr != nil {
		t.Fatalf("flag after: %v", flagErr)
	}
	if flag != "0" {
		t.Fatalf("flag left %s after mismatch, want 0", flag)
	}
}
