# package identity

## Purpose

`identity` manages AgentPaaS cryptographic identity: encrypted/OS key stores,
local CA, SPIFFE workload certs, package keys, audit signing keys, and
publisher signing identity for locks and bundles.

## Key Types

| Type | Role |
|------|------|
| `KeyStore` | Key CRUD + sign/verify interface |
| `KeyID` / `KeyType` / `KeyMaterial` / `KeyMetadata` | Key addressing and types |
| `FakeKeyStore` | In-memory test store |
| `FileKeyStore` | Passphrase-encrypted on-disk store |
| `KeychainKeyStore` | macOS Keychain store |
| `LocalCA` | Issue/renew/verify workload certs; ensure CA/package/audit keys |
| `LocalIdentityIssuer` | IdentityIssuer adapter returning PEM material |
| `TrustDomain` | SPIFFE trust domain + URI build/parse/verify |
| `PublisherIdentity` | Named publisher key metadata |

## Key Functions

| Symbol | Role |
|--------|------|
| `NewFakeKeyStore` / `NewFileKeyStore` / `NewKeychainKeyStore` | Construct stores |
| `ValidateKeyID` | Enforce key ID charset/length |
| `NewLocalCA` / `IssueWorkloadCert` | CA lifecycle and SVID minting |
| `BuildURI` / `ParseURI` / `VerifyURI` | SPIFFE path helpers |
| `CreatePublisherIdentity` / `LoadPublisherIdentity` / `SignAsPublisher` | Publisher ops |
| `PublisherFingerprint` / display formatters | Fingerprint UX |

## Architecture

```
KeyStore (fake | file | keychain)
    |
    +-- Local CA key
    +-- Daemon audit signing key
    +-- Package identity keys (per agent)
    +-- Publisher signing key
    +-- Workload keys (short-lived)
    v
LocalCA.IssueWorkloadCert
    --> x509 SVID with SPIFFE URI SAN
```

FileKeyStore encrypts a single `keystore.json` with AES-GCM; permissions must
be 0600/0400 or open fails. Symlink store paths are rejected.

## Usage

```go
ks := identity.NewFakeKeyStore() // tests only
td := &identity.TrustDomain{Name: "agentpaas.local"}
ca, err := identity.NewLocalCA(ks, td)
if err != nil {
    return err
}
cert, key, uri, err := ca.IssueWorkloadCert("demo", "1.0.0", runID, time.Hour)
```

Production daemons prefer Keychain on macOS and FileKeyStore as encrypted fallback.
