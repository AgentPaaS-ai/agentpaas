# Block 14B-T01 Fix: Default-Deny Gateway When No Policy Applied

## Problem (Adversary Finding 1 — HIGH)

In `internal/daemon/control_handlers.go`, the Run handler creates the gateway
container with `Command: []string{"-f", "/config.yaml"}` unconditionally. But
the bind mount of `/config.yaml` only happens when `gateway.yaml` exists (i.e.,
when a policy has been applied via PolicyApply).

When no policy has been applied:
- No bind mount → `/config.yaml` doesn't exist in the container
- Gateway starts with `-f /config.yaml` → file not found → crash loop
- The run aborts with an internal error

## Fix

When no policy has been applied (no gateway.yaml), create a minimal default-deny
gateway config and mount it. This ensures the gateway always has a valid config
file to read.

### Option A (PREFERRED): Write a default-deny config to a temp file

In the Run handler, after checking if gateway.yaml exists:

```go
// Determine gateway config mount.
gatewayConfigPath := filepath.Join(s.homePaths.Config, "gateway.yaml")
var gatewayBinds []string

if _, err := os.Stat(gatewayConfigPath); err == nil {
    // Policy exists — mount it read-only.
    gatewayBinds = []string{fmt.Sprintf("%s:/config.yaml:ro", gatewayConfigPath)}
} else {
    // No policy applied — write a minimal default-deny config to a per-run
    // temp file and mount it. This ensures the gateway starts with a valid
    // config and enforces deny-all (no egress allowed).
    denyAllConfig := []byte("config:\n  dns:\n    lookupFamily: V4Only\nbinds: []\n")
    perRunConfigDir := filepath.Join(s.homePaths.State, "runs", runID, "gateway-config")
    if err := os.MkdirAll(perRunConfigDir, 0o700); err != nil {
        _ = rt.RemoveNetwork(ctx, egressNetID)
        _ = rt.RemoveNetwork(ctx, netID)
        return nil, status.Errorf(codes.Internal, "create gateway config dir: %v", err)
    }
    denyAllPath := filepath.Join(perRunConfigDir, "config.yaml")
    if err := os.WriteFile(denyAllPath, denyAllConfig, 0o600); err != nil {
        _ = rt.RemoveNetwork(ctx, egressNetID)
        _ = rt.RemoveNetwork(ctx, netID)
        return nil, status.Errorf(codes.Internal, "write default-deny config: %v", err)
    }
    gatewayBinds = []string{fmt.Sprintf("%s:/config.yaml:ro", denyAllPath)}
}
```

### Also Fix: Finding 3 (MEDIUM) — Explicit gateway User

While editing the gateway ContainerSpec, add explicit `User: "64000"`:

```go
gatewayID, err := rt.Create(ctx, runtime.ContainerSpec{
    Image:      runtime.GatewayImage,
    Command:    []string{"-f", "/config.yaml"},
    Labels:     runtime.Labels(runtime.ResourceTypeGateway, runID),
    NetworkIDs: []string{string(netID), string(egressNetID)},
    Binds:      gatewayBinds,
    User:       "64000",  // explicit non-root
})
```

## Test

Update `TestRun_CreatesGatewayAndEgressNetwork` or add a new test:

`TestRun_DefaultDenyGatewayWhenNoPolicy` — when no gateway.yaml exists:
- Verify a default-deny config is written to the per-run config dir
- Verify it's mounted as /config.yaml:ro in the gateway container
- Verify the config contains no allow rules (empty binds)

## Constraints

- Do NOT change the existing policy-applied path (gateway.yaml exists → mount it).
- The default-deny config must be valid YAML that agentgateway accepts.
- The per-run config dir should be cleaned up when the run stops (add to Stop handler
  cleanup, or use the existing audit dir cleanup pattern).
- Run `make lint` and `go test ./internal/daemon/... -race -count=1` — both must pass.
