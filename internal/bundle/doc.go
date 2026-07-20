// Package bundle implements the deterministic .agentpaas archive format:
// hardened tar.gz reader, deterministic writer, offline verification, inspect
// rendering, and consent-card helpers.
//
// A bundle is a gzip-compressed tar with lexicographically sorted entries,
// fixed tar header metadata (SOURCE_DATE_EPOCH mtime, uid/gid 0), and a
// publisher-signed manifest.json that digest-pins lock, policy, SBOM, source,
// and optional OCI image layout contents.
//
// # Hardened read path
//
// Open stream-scans headers before extracting payloads. It enforces entry
// count, per-file, and total uncompressed caps; rejects unsafe paths
// (absolute, .., symlinks, duplicates); and loads only metadata files into
// memory. Source and image trees are indexed for explicit Extract* calls.
//
// # Verification
//
// Verify runs offline checks including manifest parse/signature, publisher
// match, lock provenance, and content digests for policy/SBOM/source/image.
// Verified is true only when every check passes.
package bundle
