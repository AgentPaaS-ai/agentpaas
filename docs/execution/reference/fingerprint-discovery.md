# Fingerprint Discovery: Trust Verification Without Out-of-Band Channels

## Problem

The current trust model requires the publisher to manually share their
fingerprint out-of-band (text message, email, voice) for every new
receiver. This doesn't scale — a publisher with 100 recipients shouldn't
have to send their fingerprint 100 times.

## Current State (B21-B25)

- TOFU (trust-on-first-use): receiver sees fingerprint in the install
  consent card, verifies it out-of-band, and approves.
- After first approval, the publisher's key is pinned locally. Future
  agents from the same publisher verify automatically.
- No automated fingerprint discovery exists.

## Options to Evaluate (Post-B25)

### Option 1: Well-Known Repo (RECOMMENDED for v0.3)

Publisher publishes their AgentPaaS public key and fingerprint to a
well-known GitHub repository following a convention:

```
github.com/<username>/.agentpaas-identity
```

The repo contains a `identity.json` file:
```json
{
  "publisher_name": "parvezsyed",
  "fingerprint": "061ccb96e83a0ca04c258012935b7c42fdeba67065084f94ad2d3d559fe46f98",
  "public_key_pem": "-----BEGIN PUBLIC KEY-----\n...",
  "created_at": "2026-07-12T15:57:38-07:00",
  "github_username": "parvezsyed"
}
```

During `agentpaas install`, if the receiver hasn't pinned this publisher
yet, the CLI fetches `https://github.com/<publisher_name>/.agentpaas-identity/raw/main/identity.json`
and compares the fingerprint. If it matches the bundle's embedded
fingerprint, the install proceeds with a "verified via GitHub" mark
instead of requiring manual out-of-band verification.

**Pros:** Simple, decentralized, uses GitHub as hosting only, no
central authority. Users already have GitHub accounts.
**Cons:** Requires a repo per publisher. GitHub API rate limits for
unauthenticated requests.

### Option 2: GitHub GPG Key Reuse (PREFERRED by user)

GitHub already verifies GPG keys for commit signing. AgentPaaS could
publish the publisher's public key as a GPG key on GitHub's key
management, then verify it during install:

1. `agentpaas identity init` generates an ECDSA key (current behavior)
2. Add a command: `agentpaas identity publish-github` — exports the
   public key, uploads it as a GPG subkey to the user's GitHub account
   via the GitHub API (`POST /user/gpg_keys`), and records the GitHub
   username.
3. During `agentpaas install`, the CLI fetches the publisher's GitHub
   GPG keys (`GET /users/<username>/gpg_keys`) and checks if the
   AgentPaaS public key matches any uploaded key.
4. If matched, install shows "verified via GitHub (<username>)" in the
   consent card.

**Pros:** No extra repo needed. Reuses GitHub's existing key management.
GitHub already verifies these keys (verified badge on commits). One
publish step, then all future installs auto-verify.
**Cons:** GPG key format ≠ ECDSA — need format conversion or switch to
RSA/Ed25519 for GPG compatibility. GitHub GPG API may have constraints
on key type. More complex implementation.

**Implementation note:** GitHub's GPG API expects GPG-armored public
keys. AgentPaaS uses ECDSA P-256. Options: (a) wrap the ECDSA key in
a GPG packet format, (b) generate a separate Ed25519 key for GitHub
publishing and cross-sign it with the AgentPaaS identity, or (c) use
a different GitHub API endpoint (e.g., SSH keys, which support Ed25519
natively).

### Option 3: AgentPaaS Registry (Future, v0.4+)

A central registry (keyserver-like) mapping publisher names to
fingerprints. Publishers register once; receivers look up automatically.

**Pros:** Platform-agnostic. Works without GitHub.
**Cons:** Requires infrastructure. Central authority. Another service
to maintain and secure.

## Recommended Implementation Order

1. **v0.3**: Option 2 (GitHub GPG key reuse) — user's preferred approach.
   Reuses existing GitHub identity. One `identity publish-github` command,
   then auto-verify for all future installs.
2. **v0.3.1**: Option 1 (well-known repo) as fallback for users without
   GPG key access or who prefer a simpler model.
3. **v0.4+**: Option 3 (registry) only if demand warrants.

## Security Considerations

- GitHub verification proves the publisher controls a GitHub account,
  NOT that they are trustworthy. It replaces the manual fingerprint
  comparison, not the trust decision itself.
- A compromised GitHub account could publish a fake fingerprint. This
  is the same risk as GitHub commit signing — acceptable for most use
  cases, but high-security environments should still verify out-of-band.
- The install consent card should always show the verification method:
  "verified via GitHub" vs "verified out-of-band" vs "unverified (TOFU)".
