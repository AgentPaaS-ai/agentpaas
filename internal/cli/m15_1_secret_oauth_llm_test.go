package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

const canaryRefreshCLI = "canary-refresh-token-m151-oss"

func TestSecretAdd_OAuthLlmRefreshStdinNeverArgv(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	stdout, stderr, err := executeSecretCmd(t, store, canaryRefreshCLI, "secret", "add", name)
	if err != nil {
		t.Fatalf("secret add: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if strings.Contains(stdout, canaryRefreshCLI) || strings.Contains(stderr, canaryRefreshCLI) {
		t.Fatalf("canary echoed\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	got, err := store.Get(context.Background(), name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != canaryRefreshCLI {
		t.Fatalf("stored=%q", got)
	}
}

func TestSecretAdd_RejectsValueAsExtraArg(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "add", name, canaryRefreshCLI)
	if err == nil {
		t.Fatalf("expected error for value-as-argv, stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stdout, canaryRefreshCLI) {
		t.Fatalf("canary in stdout: %s", stdout)
	}
	_, getErr := store.Get(context.Background(), name)
	if getErr == nil {
		t.Fatalf("store must not keep argv canary")
	}
}

func TestSecretAdd_OAuthLlmNoValueFlag(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	addCmd, _, err := cmd.Find([]string{"secret", "add"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if addCmd.Flags().Lookup("value") != nil || addCmd.PersistentFlags().Lookup("value") != nil {
		t.Fatalf("secret add must not grow a --value flag")
	}
}
