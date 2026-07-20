package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/secrets"
)

func TestSecretAdd_AliasesSet(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	addCmd, _, err := cmd.Find([]string{"secret", "add"})
	if err != nil {
		t.Fatalf("Find secret add: %v", err)
	}

	setCmd, _, err := cmd.Find([]string{"secret", "set"})
	if err != nil {
		t.Fatalf("Find secret set: %v", err)
	}

	if addCmd != setCmd {
		t.Fatalf("add and set resolved to different commands: add=%p set=%p", addCmd, setCmd)
	}
}

func TestSecretRemove_AliasesRm(t *testing.T) {
	resetAgentCmd()
	cmd := AgentCmd()

	removeCmd, _, err := cmd.Find([]string{"secret", "remove"})
	if err != nil {
		t.Fatalf("Find secret remove: %v", err)
	}

	rmCmd, _, err := cmd.Find([]string{"secret", "rm"})
	if err != nil {
		t.Fatalf("Find secret rm: %v", err)
	}

	if removeCmd != rmCmd {
		t.Fatalf("remove and rm resolved to different commands: remove=%p rm=%p", removeCmd, rmCmd)
	}
}

func TestSecretAdd_StoresInFakeKeychain(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	secretValue := "test-secret-value"

	stdout, stderr, err := executeSecretCmd(t, store, secretValue, "secret", "add", "test-key")
	if err != nil {
		t.Fatalf("secret add returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `secret "test-key" stored`) {
		t.Fatalf("unexpected stdout: %s", stdout)
	}

	got, err := store.Get(context.Background(), "test-key")
	if err != nil {
		t.Fatalf("Get test-key: %v", err)
	}
	if string(got) != secretValue {
		t.Fatalf("stored value = %q, want %q", got, secretValue)
	}
}

func TestSecretList_NeverPrintsValue(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	secretValue := "super-secret-value"
	if err := store.Set(context.Background(), "listed-key", []byte(secretValue)); err != nil {
		t.Fatalf("Set: %v", err)
	}

	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "list")
	if err != nil {
		t.Fatalf("secret list returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "listed-key") {
		t.Fatalf("secret list output missing key name:\n%s", stdout)
	}
	if strings.Contains(stdout, secretValue) {
		t.Fatalf("secret list output leaked secret value:\n%s", stdout)
	}
}

func TestSecretRemove_DeletesFromStore(t *testing.T) {
	store := secrets.NewFakeKeyStore()
	if err := store.Set(context.Background(), "delete-me", []byte("value-to-delete")); err != nil {
		t.Fatalf("Set: %v", err)
	}

	stdout, stderr, err := executeSecretCmd(t, store, "", "secret", "remove", "delete-me")
	if err != nil {
		t.Fatalf("secret remove returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, `secret "delete-me" removed`) {
		t.Fatalf("unexpected stdout: %s", stdout)
	}

	_, err = store.Get(context.Background(), "delete-me")
	if !errorsIsSecretNotFound(err) {
		t.Fatalf("Get after remove error = %v, want ErrSecretNotFound", err)
	}
}
