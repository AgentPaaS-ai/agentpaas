# Worker: B15-T01 MC3 — Add `secret test` command + provider adapters

## Repo
`~/projects/agentpaas`, on branch `feat/b15-t01-mc3` (create from main).
MC1+MC2 are merged: `secret add/list/remove/rotate` all work.

## Scope (ONE micro-chunk)
Add `agentpaas secret test <name> [--provider <provider>]` — pre-deployment
credential validation. Makes a trivial authenticated HTTP call to the target
service OUTSIDE the container, before pack/run. Fail fast with a clear error
if the key is wrong, provider is unreachable, or the destination is not
recognized.

## Design

The `secret test` command:
1. Reads the secret value from the Keychain store (via `store.Get`)
2. Detects the provider from the secret name (e.g. `openai-key` → openai) or
   from a `--provider` flag
3. Uses a provider adapter to build a trivial test HTTP request:
   - OpenAI: POST https://api.openai.com/v1/chat/completions with
     `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"say OK"}]}`,
     header `Authorization: Bearer <key>`, expect 200 with choices[0].message.content
   - Anthropic: POST https://api.anthropic.com/v1/messages with
     `{"model":"claude-3-5-sonnet-20241022","max_tokens":5,"messages":[{"role":"user","content":"say OK"}]}`,
     headers `x-api-key: <key>` + `anthropic-version: 2023-06-01`, expect 200
   - xAI: POST https://api.x.ai/v1/chat/completions with
     `{"model":"grok-beta","messages":[{"role":"user","content":"say OK"}]}`,
     header `Authorization: Bearer <key>`, expect 200
   - HTTP/generic: GET the URL with `Authorization: Bearer <key>` header,
     expect 2xx (for third-party APIs like weather, stripe, etc.)
4. Reports success/failure with a clear message. NEVER prints the secret value.
5. Uses a short timeout (10s) so unreachable providers fail fast.

## Files to create/edit

### 1. Create `internal/secrets/providertest.go` — provider test adapters

```go
package secrets

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"
)

// ProviderTestResult is the outcome of a credential validation call.
type ProviderTestResult struct {
    Provider  string // "openai", "anthropic", "xiai", "http"
    Endpoint  string // the URL that was called
    Status    string // "ok" or "error"
    Detail    string // human-readable detail (never contains the secret)
    HTTPStatus int   // HTTP status code (0 if request never completed)
}

// TestProvider makes a trivial authenticated call to validate the credential.
// It NEVER returns the secret value in the result.
func TestProvider(ctx context.Context, provider string, secretValue []byte) ProviderTestResult {
    // never log/return secretValue
    testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()

    switch strings.ToLower(provider) {
    case "openai":
        return testOpenAI(testCtx, secretValue)
    case "anthropic":
        return testAnthropic(testCtx, secretValue)
    case "xiai", "xai":
        return testXAI(testCtx, secretValue)
    case "http", "generic", "":
        return ProviderTestResult{
            Provider: "http",
            Status:   "error",
            Detail:   "generic HTTP provider test requires --url flag (not implemented in this adapter)",
        }
    default:
        return ProviderTestResult{
            Provider: provider,
            Status:   "error",
            Detail:   fmt.Sprintf("unknown provider %q: supported providers are openai, anthropic, xiai", provider),
        }
    }
}
```

Implement `testOpenAI`, `testAnthropic`, `testXAI` as private functions. Each:
1. Builds an http.Request with the right URL, method, headers, JSON body
2. Uses `http.DefaultClient.Do(ctx)` with the timeout context
3. Checks the HTTP status code
4. Returns a ProviderTestResult with Status="ok" or "error" and a Detail
   message that describes the outcome WITHOUT leaking the key
5. On any error (network, non-2xx, JSON parse), returns Status="error" with
   a clear message like "openai returned HTTP 401: invalid api key" or
   "failed to reach api.openai.com: dial tcp: connection refused"

### 2. Edit `internal/cli/control.go` — add the `secret test` subcommand

Add to `newSecretCmd()`:

```go
cmd.AddCommand(&cobra.Command{
    Use:   "test <name>",
    Short: "Validate a credential by making a trivial authenticated call to the provider",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        name := args[0]
        if err := secrets.ValidateSecretName(name); err != nil {
            return err
        }
        provider, _ := cmd.Flags().GetString("provider")
        if provider == "" {
            provider = detectProviderFromName(name)
        }
        store, err := secretStoreFactory(cmd)
        if err != nil {
            return err
        }
        value, err := store.Get(cmd.Context(), name)
        if err != nil {
            return fmt.Errorf("secret %q: %w", name, err)
        }
        result := secrets.TestProvider(cmd.Context(), provider, value)
        // NEVER print the secret value
        if result.Status == "ok" {
            fmt.Fprintf(cmd.OutOrStdout(), "secret %q: %s test OK (%s, HTTP %d)\n", name, result.Provider, result.Endpoint, result.HTTPStatus)
        } else {
            fmt.Fprintf(cmd.OutOrStderr(), "secret %q: %s test FAILED: %s\n", name, result.Provider, result.Detail)
            return fmt.Errorf("credential test failed for %q", name)
        }
        return nil
    },
})
```

Register the `--provider` flag on the test subcommand:
```go
// After creating the test cmd but before AddCommand:
testCmd.Flags().String("provider", "", "credential provider: openai|anthropic|xiai (auto-detected from name if omitted)")
```

Add `detectProviderFromName(name string) string`:
```go
func detectProviderFromName(name string) string {
    lower := strings.ToLower(name)
    switch {
    case strings.Contains(lower, "openai") || strings.Contains(lower, "gpt"):
        return "openai"
    case strings.Contains(lower, "anthropic") || strings.Contains(lower, "claude"):
        return "anthropic"
    case strings.Contains(lower, "xai") || strings.Contains(lower, "grok"):
        return "xiai"
    default:
        return "openai" // default assumption for LLM keys
    }
}
```

### 3. Tests

Create `internal/secrets/providertest_test.go`:

Use `httptest.NewServer` to mock provider endpoints. For each provider:
- Start a test server that checks the Authorization/x-api-key header
- Call `TestProvider` with the test server URL (you'll need to make the
  endpoint URL injectable — add an `endpointOverride` parameter or use a
  package-level var that tests can override)

DESIGN for testability: Add an unexported `var openAIEndpoint = "https://api.openai.com/v1/chat/completions"`
(and similar for anthropic, xiai). Tests override these vars to point at the
httptest server, run the test, restore via t.Cleanup. This avoids needing
to change the TestProvider function signature.

Tests:
1. `TestProviderTestResult_NeverContainsSecret` — call TestProvider with a
   fake provider name, assert the result.Detail does NOT contain the secret
   value string.
2. `TestTestProvider_OpenAI_Success` — mock server returns 200 with valid
   JSON, assert Status="ok".
3. `TestTestProvider_OpenAI_InvalidKey` — mock server returns 401, assert
   Status="error" and Detail mentions "401" but NOT the secret value.
4. `TestTestProvider_Anthropic_Success` — similar for Anthropic (checks
   x-api-key header + anthropic-version header).
5. `TestTestProvider_XAI_Success` — similar for xAI.
6. `TestTestProvider_UnknownProvider` — assert error for unknown provider name.
7. `TestTestProvider_UnreachableProvider` — point at a non-listening port,
   assert Status="error" with a connection-refused style message.

In `internal/cli/cli_test.go`, add:
8. `TestSecretTest_NeverPrintsValue` — store a secret, run `secret test`,
   assert stdout/stderr never contain the value. Use a fake provider by
   overriding the endpoint var to point at a mock server. Assert the command
   either succeeds (mock returns 200) or fails gracefully (mock returns 401)
   but never prints the key.

## Constraints
- The secret value must NEVER appear in: ProviderTestResult.Detail, CLI stdout,
  CLI stderr, error messages, or logs.
- Use `strings.Contains` checks in tests to verify no leakage.
- Do NOT change the SecretStore interface.
- Do NOT add real network calls in unit tests — use httptest.NewServer.
- Run `make test` and `make lint` — both must pass.
- ProviderTestResult must be JSON-serializable (for future plugin use) but the
  secret value is never a field.

## Verification
- `go test ./internal/secrets/... -run TestProvider -v` — all pass
- `go test ./internal/cli/... -run TestSecretTest -v` — passes
- `make test` — all packages pass
- `make lint` — 0 issues
- `go run ./cmd/agentpaas secret test --help` shows usage

## Commit
`feat(cli): add secret test command with provider adapters for pre-deployment validation (B15-T01 MC3)`

Do NOT push.