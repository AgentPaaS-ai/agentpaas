# Worker: B15-T01 MC2 — Add `secret rotate` command (atomic add+remove)

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc2` (create it from main).
MC1 has already been merged to main: `secret add` and `secret remove` now exist
(with `set`/`rm` as aliases).

## Scope (ONE micro-chunk)
Add `agentpaas secret rotate <name>` — reads a new value from stdin, validates
it, then atomically replaces the stored secret. "Atomic" means: if the new
value fails validation, the old secret is preserved unchanged. If the store
Set fails, the old secret is preserved.

## File to edit
- `internal/cli/control.go` — the `newSecretCmd()` function.

## Exact change

Add a new subcommand to `newSecretCmd()`:

```go
cmd.AddCommand(&cobra.Command{
    Use:   "rotate <name>",
    Short: "Replace a secret with a new value from stdin (atomic)",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        name := args[0]
        if err := secrets.ValidateSecretName(name); err != nil {
            return err
        }
        value, err := readSecretValue(cmd)
        if err != nil {
            return err
        }
        store, err := secretStoreFactory(cmd)
        if err != nil {
            return err
        }
        // Validate the new value BEFORE touching the existing secret.
        // readSecretValue already enforces size; this is defense-in-depth.
        if err := store.Set(cmd.Context(), name, value); err != nil {
            return fmt.Errorf("rotate secret %q: %w", name, err)
        }
        _, err = fmt.Fprintf(cmd.OutOrStdout(), "secret %q rotated\n", name)
        return err
    },
})
```

The existing `store.Set` is idempotent (it updates `UpdatedAt` and the value
in place — see FakeKeyStore.Set). So "rotate" is really just "Set with a
different success message." But the key semantic is: validation happens before
any store interaction, and if Set fails, the old value is preserved (FakeKeyStore
copies the value bytes, so a failed Set doesn't corrupt the existing entry).

## Test to add

In `internal/cli/cli_test.go`, add:

```go
func TestSecretRotateReplacesValue(t *testing.T) {
    store := secrets.NewFakeKeyStore()
    old := "old-value"
    new := "new-rotated-value"
    if err := store.Set(context.Background(), "rotate_me", []byte(old)); err != nil {
        t.Fatalf("Set old: %v", err)
    }

    stdout, stderr, err := executeSecretCmd(t, store, new, "secret", "rotate", "rotate_me")
    if err != nil {
        t.Fatalf("secret rotate returned error: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
    }

    got, err := store.Get(context.Background(), "rotate_me")
    if err != nil {
        t.Fatalf("Get after rotate: %v", err)
    }
    if string(got) != new {
        t.Fatalf("rotated value = %q, want %q", got, new)
    }
    if strings.Contains(stdout, new) {
        t.Fatalf("rotate output leaked new value: %s", stdout)
    }
    if strings.Contains(stdout, old) {
        t.Fatalf("rotate output leaked old value: %s", stdout)
    }
    if !strings.Contains(stdout, "rotated") {
        t.Fatalf("rotate output missing confirmation: %s", stdout)
    }
}

func TestSecretRotateRejectsOversizePreservesOld(t *testing.T) {
    store := secrets.NewFakeKeyStore()
    old := "preserved-old-value"
    if err := store.Set(context.Background(), "rotate_guard", []byte(old)); err != nil {
        t.Fatalf("Set old: %v", err)
    }
    oversize := strings.Repeat("x", secrets.MaxSecretValueSize+1)

    _, _, err := executeSecretCmd(t, store, oversize, "secret", "rotate", "rotate_guard")
    if err == nil {
        t.Fatal("secret rotate oversize: want error, got nil")
    }

    got, err := store.Get(context.Background(), "rotate_guard")
    if err != nil {
        t.Fatalf("Get after failed rotate: %v", err)
    }
    if string(got) != old {
        t.Fatalf("old value not preserved after failed rotate: got %q, want %q", got, old)
    }
}
```

## Constraints
- Do NOT modify the `secret add`, `secret list`, or `secret remove` commands —
  they were done in MC1.
- Do NOT add `secret test` — that's MC3.
- Do NOT change the SecretStore interface or FakeKeyStore implementation.
- Run `make test` and `make lint` — both must pass.
- The existing `executeSecretCmd` test helper is available (see cli_test.go).

## Verification
- `go test ./internal/cli/... -run TestSecretRotate -v` — passes
- `make test` — all packages pass
- `make lint` — 0 issues
- `go run ./cmd/agentpaas secret rotate --help` shows the rotate usage

## Commit
`feat(cli): add secret rotate command for atomic credential rotation (B15-T01 MC2)`

Do NOT push.