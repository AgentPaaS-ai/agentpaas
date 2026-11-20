// Package identity provides local trust-domain identity: key stores, local CA,
// SPIFFE-style workload SVID issuance, package identity keys, daemon audit
// signing keys, and publisher identity helpers.
//
// # KeyStore
//
// KeyStore is the narrow interface for create/load/sign/verify/delete/list of
// typed keys (CA, audit signing, package identity, workload, publisher).
// Implementations:
//   - FakeKeyStore — in-memory, for tests (never triggers OS keychain UI)
//   - FileKeyStore — AES-256-GCM encrypted file, passphrase via PBKDF2; refuses
//     weak permissions and symlink paths
//   - KeychainKeyStore — macOS Keychain-backed store
//
// # Issuance
//
// LocalCA / LocalIdentityIssuer mint short-lived workload certificates with
// SPIFFE URIs of the form spiffe://<trust-domain>/agent/<name>/<version>/run/<id>.
// URI components are validated; VerifyWorkloadCert checks chain and identity.
//
// # Publisher identity
//
// Publisher helpers create and load named publisher signing keys used to sign
// pack locks and bundle manifests, with fingerprint display formatting.
package identity
