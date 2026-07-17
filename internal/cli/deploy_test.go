package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestDeployCmdStructure(t *testing.T) {
	// Reset root so command registration is visible.
	rootCmd = nil
	cmd := AgentCmd()

	deploy, _, err := cmd.Find([]string{"deploy"})
	if err != nil {
		t.Fatalf("deploy command not found: %v", err)
	}
	subs := map[string]bool{}
	for _, c := range deploy.Commands() {
		subs[c.Name()] = true
	}
	for _, want := range []string{"create", "list", "inspect", "deactivate", "alias"} {
		if !subs[want] {
			t.Errorf("missing deploy subcommand %q", want)
		}
	}

	alias, _, err := cmd.Find([]string{"deploy", "alias"})
	if err != nil {
		t.Fatalf("deploy alias: %v", err)
	}
	aliasSubs := map[string]bool{}
	for _, c := range alias.Commands() {
		aliasSubs[c.Name()] = true
	}
	for _, want := range []string{"set", "promote", "rollback", "list"} {
		if !aliasSubs[want] {
			t.Errorf("missing alias subcommand %q", want)
		}
	}
}

func TestRunCmdContinuationFlags(t *testing.T) {
	rootCmd = nil
	cmd := AgentCmd()
	run, _, err := cmd.Find([]string{"run"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, name := range []string{"continue", "action", "attempt-lease", "input", "idempotency-key", "deployment-ref"} {
		if run.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
	// Control subcommands present.
	subs := map[string]bool{}
	for _, c := range run.Commands() {
		subs[c.Name()] = true
	}
	for _, want := range []string{"list", "cancel", "pause", "resume", "restart", "extend"} {
		if !subs[want] {
			t.Errorf("missing run subcommand %q", want)
		}
	}
}

func TestRunExtendFlags(t *testing.T) {
	rootCmd = nil
	cmd := AgentCmd()
	extend, _, err := cmd.Find([]string{"run", "extend"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"max-active-time", "max-llm-spend-usd", "extend-current-attempt", "reason", "idempotency-key"} {
		if extend.Flags().Lookup(name) == nil {
			t.Errorf("missing extend flag --%s", name)
		}
	}
}

func TestSplitPackageRef(t *testing.T) {
	n, v, err := splitPackageRef("demo@1.2.3")
	if err != nil || n != "demo" || v != "1.2.3" {
		t.Fatalf("%s %s %v", n, v, err)
	}
	n, v, err = splitPackageRef("solo")
	if err != nil || n != "solo" || v != "0.0.0" {
		t.Fatalf("%s %s %v", n, v, err)
	}
}

func TestReadInputFlag(t *testing.T) {
	data, err := readInputFlag(`{"a":1}`)
	if err != nil || string(data) != `{"a":1}` {
		t.Fatalf("%s %v", data, err)
	}
	tmp := t.TempDir()
	p := filepath.Join(tmp, "in.json")
	if err := os.WriteFile(p, []byte(`{"f":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err = readInputFlag("@" + p)
	if err != nil || string(data) != `{"f":true}` {
		t.Fatalf("%s %v", data, err)
	}
	empty, err := readInputFlag("")
	if err != nil || string(empty) != "{}" {
		t.Fatalf("%s %v", empty, err)
	}
}

func TestCLIIdempotencyKeyGeneration(t *testing.T) {
	k1, err := newCLIIdempotencyKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := newCLIIdempotencyKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(k1, "inv-") || !strings.HasPrefix(k2, "inv-") {
		t.Fatalf("%s %s", k1, k2)
	}
	if k1 == k2 {
		t.Fatal("keys must be unique")
	}
}

func TestDeployPromoteRequiresExpectedGeneration(t *testing.T) {
	rootCmd = nil
	cmd := AgentCmd()
	// Capture help / run without daemon — should fail on flag validation first.
	promote, _, err := cmd.Find([]string{"deploy", "alias", "promote"})
	if err != nil {
		t.Fatal(err)
	}
	buf := new(bytes.Buffer)
	promote.SetOut(buf)
	promote.SetErr(buf)
	promote.SetArgs([]string{"prod", "dep-abc"})
	// Execute will try to dial daemon after flag check; ensure expected-generation gate works.
	// RunE checks flag before dial when expected-generation not set.
	err = promote.RunE(promote, []string{"prod", "dep-abc"})
	if err == nil || !strings.Contains(err.Error(), "expected-generation") {
		t.Fatalf("want expected-generation error, got %v", err)
	}
}

func TestNoPublicRouteOrRecoverCommand(t *testing.T) {
	rootCmd = nil
	cmd := AgentCmd()
	for _, name := range []string{"route", "recover"} {
		if _, _, err := cmd.Find([]string{name}); err == nil {
			t.Errorf("public %q command must not exist", name)
		}
	}
}

// Ensure AgentCmd remains a *cobra.Command (compile-time-ish smoke).
var _ *cobra.Command
