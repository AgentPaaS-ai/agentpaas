# Block 14B-T02 Fix: Sanitize Gateway IP for Env Var Injection

## Problem (Adversary Finding — HIGH)

The gateway IP from InspectContainerIP is directly interpolated into HTTP_PROXY
env vars without sanitization:

```go
fmt.Sprintf("HTTP_PROXY=http://%s:7799", gatewayIP)
```

If the IP contains newline characters (e.g., "172.18.0.42\nHTTP_PROXY=http://attacker:1234\n"),
it would inject additional env vars into the container. While the IP comes from
Docker's API (trusted source), defense-in-depth requires validation.

## Fix

Add IP validation before using it in env vars. In `internal/daemon/control_handlers.go`:

```go
import "net"

// After discovering gatewayIP:
if gatewayIP != "" {
    // Validate the IP address to prevent env var injection.
    // Docker returns a valid IP, but we validate defensively.
    if ip := net.ParseIP(gatewayIP); ip == nil {
        fmt.Fprintf(os.Stderr, "daemon: gateway IP %q is not a valid IP address, skipping proxy env\n", gatewayIP)
        gatewayIP = ""
    }
}
```

Place this validation right after the InspectContainerIP call, before building
the proxyEnv slice.

The `net.ParseIP` function rejects any string that's not a valid IPv4 or IPv6
address, including strings with newlines, spaces, or injection characters.

## Test

Update the adversary test in `internal/daemon/adversary_t02_test.go`:

`TestAdversaryT02_ProxyEnvInjection_MaliciousGatewayIP` — currently this test
documents the break (malicious IP produces injection). After the fix, update it
to verify the malicious IP is REJECTED (no proxy env vars set at all):

```go
func TestAdversaryT02_ProxyEnvInjection_MaliciousGatewayIP(t *testing.T) {
    badIP := "172.18.0.42\nHTTP_PROXY=http://attacker:1234\n"
    var capturedSpec runtime.ContainerSpec
    mock := defaultMockRuntimeDriver()
    mock.inspectContainerIPFunc = func(_ context.Context, _ runtime.ContainerID, _ string) (string, error) {
        return badIP, nil
    }
    mock.createFunc = func(_ context.Context, spec runtime.ContainerSpec) (runtime.ContainerID, error) {
        if spec.Image == runtime.GatewayImage {
            return runtime.ContainerID("gateway-test"), nil
        }
        capturedSpec = spec
        return runtime.ContainerID("container-test"), nil
    }

    server, _ := testServerWithMockRuntime(t, mock)
    _, err := server.Run(context.Background(), &controlv1.RunRequest{AgentName: "test-agent"})
    if err != nil {
        t.Fatalf("Run() error = %v", err)
    }

    // After fix: malicious IP should be rejected, no proxy env vars set.
    for _, env := range capturedSpec.Env {
        if strings.Contains(env, "HTTP_PROXY") || strings.Contains(env, "HTTPS_PROXY") {
            t.Fatalf("ADVERSARY: proxy env var set despite malicious gateway IP: %q", env)
        }
        if strings.Contains(env, "\n") || strings.Contains(env, "attacker") {
            t.Fatalf("ADVERSARY: env var injection succeeded: %q", env)
        }
    }
}
```

## Constraints

- Use `net.ParseIP` for validation (stdlib, no new deps).
- Run `make lint` and `go test ./internal/daemon/... -race -count=1` — both must pass.
- ALL existing tests must still pass.
