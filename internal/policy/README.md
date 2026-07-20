# package policy

## Purpose

`policy` is the policy.yaml toolchain: strict parse, validation, canonical
normalization, delta helpers, and compilation to agentgateway config plus
related allow-list/credential artifacts.

## Key Types

| Type | Role |
|------|------|
| `Policy` | Top-level policy document |
| `EgressRule`, `Credential`, `MCPServer`, `Hook`, `IngressRule` | Core sections |
| `LLMBudget`, `LLMRateLimit`, `LLMProviderLock` | LLM controls |
| `IngressAuth` / JWT / API key configs | Trigger auth |
| `Guardrail`, `Transformation`, `Observability` | Gateway extras |
| `RoutedRunPolicy`, `ModelRoute` | v1.1 routed extensions |
| `ValidationError` | Structured validation failure |
| `CanonicalPolicy` | Normalized form for digests/diffs |

## Key Functions

| Symbol | Role |
|--------|------|
| `ParsePolicy` | Strict YAML parse |
| `ValidatePolicy` / `ValidatePolicyWithRoute` | Semantic validation |
| `CompileGatewayConfig` | agentgateway YAML bytes |
| `CompileDNSAllowList` | DNS allow-list artifact |
| `CompileCredentialRules` | Credential routing rules |
| `Canonicalize` | Stable normalized policy |
| Delta helpers | Diff policies for recommend-patch / lineage |

## Architecture

```
policy.yaml
    |
    v
 ParsePolicy (strict) --> ValidatePolicy
    |
    +-- Canonicalize --> policy digest (pack lock)
    +-- CompileGatewayConfig --> gateway config mount
    +-- CompileDNSAllowList / CompileCredentialRules
    v
 runtime gateway sidecar + harness env
```

## Usage

```go
p, err := policy.ParsePolicy(bytes.NewReader(yamlBytes))
if err != nil {
    return err
}
if errs := policy.ValidatePolicy(p); len(errs) > 0 {
    return errs[0]
}
gw, err := policy.CompileGatewayConfig(p)
```

CLI: `agent policy` subcommands and pack/run paths inside the daemon.
