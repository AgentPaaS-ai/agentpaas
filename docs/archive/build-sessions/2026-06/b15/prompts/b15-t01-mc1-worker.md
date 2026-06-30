# Worker: B15-T01 MC1 — Rename `secret set`→`add`, `secret rm`→`remove` + aliases

## Repo
`~/projects/agentpaas`, on `main`. Last commit: `09debd9`.

## Scope (ONE micro-chunk)
The existing `agentpaas secret` command has `set`, `list`, `rm`. The B15-T01
spec requires `add`, `list`, `remove`, `rotate`, `test`. This chunk renames the
verbs to match the spec, keeping the old verbs as aliases for backward compat.

## Files to edit
- `internal/cli/control.go` — the `newSecretCmd()` function (lines ~570-640).

## Exact changes

### 1. `secret set <name>` → `secret add <name>` (primary), keep `set` as alias

In `newSecretCmd()`, change the `set` subcommand:
- `Use: "set <name>"` → `Use: "add <name>"`
- Add `Aliases: []string{"set"}` to the cobra.Command struct
- Keep `Short: "Create or update a secret from stdin"` (add is create-or-update)
- The success message: `"secret %q stored\n"` → keep as-is (or change to
  `"secret %q added\n"` — your call, but be consistent in tests)

### 2. `secret rm <name>` → `secret remove <name>` (primary), keep `rm` as alias

In `newSecretCmd()`, change the `rm` subcommand:
- `Use: "rm <name>"` → `Use: "remove <name>"`
- Add `Aliases: []string{"rm"}`
- Keep `Short: "Remove a secret"`
- Success message: `"secret %q removed\n"` — keep as-is

### 3. `secret list` — no change needed (already correct)

### 4. Help text
The `secret` parent command `Short` is "Manage local profile secrets" — keep it.
Update any `Long` if present to mention `add/list/remove/rotate/test` (rotate and
test are added in later chunks — don't add them here, just leave the Long as-is
or mention the three that exist).

## Tests to add

Create `internal/cli/secret_cmd_test.go` with:

1. `TestSecretAdd_AliasesSet` — verify `secret add` and `secret set` both resolve
   to the same command (use `cobra.Command.Find()` with `[]string{"secret","add"}`
   and `[]string{"secret","set"}`, assert same command returned).
2. `TestSecretRemove_AliasesRm` — same for `remove` and `rm`.
3. `TestSecretAdd_StoresInFakeKeychain` — use `secretStoreFactory` override (the
   existing `secretStoreFactory` var is a function variable — override it with
   `secrets.NewFakeKeyStore()` for the test, call `secret add <name>` via
   `cobra.Command.SetArgs([]string{"secret","add","test-key"})`, pipe stdin with
   the secret value, assert the store has the key.
4. `TestSecretList_NeverPrintsValue` — store a secret in FakeKeyStore, run
   `secret list`, assert stdout contains the name but NOT the value.
5. `TestSecretRemove_DeletesFromStore` — store a secret, run `secret remove`,
   assert it's gone from the FakeKeyStore.

Look at existing test patterns in `internal/cli/cli_test.go` for how to set up
cobra command tests with stdin piping and factory overrides. The
`secretStoreFactory` is a package-level var — override it in the test and
restore it via `t.Cleanup()`.

## Constraints
- Do NOT add `rotate` or `test` commands in this chunk — those are MC2/MC3.
- Do NOT change the `secretServiceName` or `newDefaultSecretStore` functions.
- Do NOT change the Keychain integration — only CLI command wiring.
- Run `make test` and `make lint` before finishing — both must pass.
- The existing `readSecretValue`, `writeSecretList`, `formatSecretTime`,
  `secretListItem` helpers stay as-is.

## Verification
- `go test ./internal/cli/... -run TestSecret -v` — all new tests pass
- `make test` — all 21+ packages still pass
- `make lint` — 0 issues
- `go run ./cmd/agentpaas secret add --help` shows "add" as primary
- `go run ./cmd/agentpaas secret set --help` still works (alias)

## Commit
`feat(cli): rename secret set→add, rm→remove with aliases (B15-T01 MC1)`

Do NOT push. Leave the commit on the local branch for orchestrator review.