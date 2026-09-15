package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

const advCanaryRTCLI = "CANARY_RT_m151t2_DO_NOT_EMIT"

func TestAdvSecretAdd_OAuthLlmStdinNeverEchoesCanary(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	stdout, stderr, err := executeSecretCmd(t, store, advCanaryRTCLI, "secret", "add", name)
	if err != nil {
		t.Fatalf("secret add: %v", err)
	}
	if strings.Contains(stdout, advCanaryRTCLI) || strings.Contains(stderr, advCanaryRTCLI) {
		t.Errorf("ADVERSARY BREAK: SC1: canary echoed on secret add stdout/stderr")
	}
	got, err := store.Get(context.Background(), name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != advCanaryRTCLI {
		t.Fatalf("stored value mismatch (stdin not the source)")
	}
}

func TestAdvSecretAdd_RejectsArgvCanary(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "add", name, advCanaryRTCLI)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: SC2: secret add accepted value as extra argv")
	}
	if strings.Contains(stdout, advCanaryRTCLI) || strings.Contains(stderr, advCanaryRTCLI) {
		t.Errorf("ADVERSARY BREAK: SC1: argv canary leaked into stdout/stderr")
	}
	if err != nil && strings.Contains(err.Error(), advCanaryRTCLI) {
		t.Errorf("ADVERSARY BREAK: SC1: argv canary leaked into returned error")
	}
	_, getErr := store.Get(context.Background(), name)
	if getErr == nil {
		t.Fatalf("ADVERSARY BREAK: SC2: store kept argv canary")
	}
}

func TestAdvSecretAdd_NoValueFlag(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()
	addCmd, _, err := cmd.Find([]string{"secret", "add"})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	for _, f := range []string{"value", "secret", "token", "refresh-token"} {
		if addCmd.Flags().Lookup(f) != nil || addCmd.PersistentFlags().Lookup(f) != nil {
			t.Errorf("ADVERSARY BREAK: SC2: secret add grew a --%s flag", f)
		}
	}
}

func TestAdvSecretAdd_EmptyStdinDoesNotStore(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "add", name)
	if err == nil {
		t.Errorf("ADVERSARY BREAK: SC2: empty stdin stored a secret under %s stdout=%s stderr=%s", name, stdout, stderr)
	}
	if strings.Contains(stdout, advCanaryRTCLI) || strings.Contains(stderr, advCanaryRTCLI) {
		t.Errorf("ADVERSARY BREAK: SC1: empty-stdin path leaked canary")
	}
	_, getErr := store.Get(context.Background(), name)
	if getErr == nil {
		t.Errorf("ADVERSARY BREAK: SC2: empty stdin wrote a keychain entry")
	}
}

func TestAdvSecretAdd_ArgvCanaryDoesNotWinOverStdin(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	argvCanary := "CANARY_ARGV_m151t2_DO_NOT_STORE"
	stdout, stderr, err := executeSecretCmd(t, store, advCanaryRTCLI, "secret", "add", name, argvCanary)
	if err == nil {
		t.Fatalf("ADVERSARY BREAK: SC2: extra argv accepted")
	}
	if strings.Contains(stdout, advCanaryRTCLI) || strings.Contains(stderr, advCanaryRTCLI) ||
		strings.Contains(stdout, argvCanary) || strings.Contains(stderr, argvCanary) {
		t.Errorf("ADVERSARY BREAK: SC1: canary leaked on extra-argv path")
	}
	_, getErr := store.Get(context.Background(), name)
	if getErr == nil {
		got, _ := store.Get(context.Background(), name)
		if string(got) == argvCanary {
			t.Errorf("ADVERSARY BREAK: SC2: stored argv canary instead of rejecting")
		}
	}
}

func TestAdvSecretAdd_ListNeverPrintsValue(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	if err := store.Set(context.Background(), name, []byte(advCanaryRTCLI)); err != nil {
		t.Fatalf("Set: %v", err)
	}
	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "list")
	if err != nil {
		t.Fatalf("secret list: %v", err)
	}
	if strings.Contains(stdout, advCanaryRTCLI) || strings.Contains(stderr, advCanaryRTCLI) {
		t.Errorf("ADVERSARY BREAK: SC1: secret list leaked refresh canary")
	}
}

func TestAdvSecretAdd_EnvFallbackNotUsed(t *testing.T) {
	t.Setenv("OAUTH_LLM_RT", advCanaryRTCLI)
	t.Setenv("AGENTPAAS_SECRET", advCanaryRTCLI)
	store := secrets.NewFakeKeyStore()
	name := "oauth_llm_rt_xai"
	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "add", name)
	if err == nil {
		got, getErr := store.Get(context.Background(), name)
		if getErr == nil && string(got) == advCanaryRTCLI {
			t.Errorf("ADVERSARY BREAK: SC2: empty stdin fell back to env canary")
		}
	}
	if strings.Contains(stdout, advCanaryRTCLI) || strings.Contains(stderr, advCanaryRTCLI) {
		t.Errorf("ADVERSARY BREAK: SC1: env fallback path leaked canary")
	}
}
