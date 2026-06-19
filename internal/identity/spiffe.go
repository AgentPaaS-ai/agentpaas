package identity

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TrustDomain represents a SPIFFE trust domain with configurable host and
// optional tenant identification. It supports two modes:
//   - Local (P1): spiffe://local.agentpaas/agent/<name>/<ver>/run/<run_id>
//   - Hosted (P2): spiffe://tenant.agentpaas.ai/<tenant>/agent/<name>/<ver>/run/<run_id>
//
// The same URI path schema is used for both modes; only the trust domain
// prefix (host and optional tenant segment) differs.
type TrustDomain struct {
	Host     string
	IsHosted bool
	TenantID string
}

// ErrInvalidComponent is returned when a URI component contains path
// traversal characters or path separators.
var ErrInvalidComponent = errors.New("invalid URI component")

// ValidateURIComponent validates that a URI component does not contain path
// traversal characters ("..") or path separators ("/"). It returns
// ErrInvalidComponent if the component is invalid.
func ValidateURIComponent(component, name string) error {
	if strings.Contains(component, "..") || strings.Contains(component, "/") {
		return fmt.Errorf("%w: %s must not contain \"..\" or \"/\"", ErrInvalidComponent, name)
	}
	return nil
}

// BuildURI constructs a SPIFFE URI for the given agent identity within this
// trust domain. For local mode the URI is:
//
//	spiffe://local.agentpaas/agent/<name>/<ver>/run/<run_id>
//
// For hosted mode the URI includes the tenant segment:
//
//	spiffe://tenant.agentpaas.ai/<tenant>/agent/<name>/<ver>/run/<run_id>
//
// It returns an error if any component contains path traversal characters
// ("..") or path separators ("/").
func (td *TrustDomain) BuildURI(agentName, agentVersion, runID string) (string, error) {
	if err := ValidateURIComponent(agentName, "agentName"); err != nil {
		return "", err
	}
	if err := ValidateURIComponent(agentVersion, "agentVersion"); err != nil {
		return "", err
	}
	if err := ValidateURIComponent(runID, "runID"); err != nil {
		return "", err
	}
	if td.IsHosted {
		return fmt.Sprintf("spiffe://%s/%s/agent/%s/%s/run/%s",
			td.Host, td.TenantID, agentName, agentVersion, runID), nil
	}
	return fmt.Sprintf("spiffe://%s/agent/%s/%s/run/%s",
		td.Host, agentName, agentVersion, runID), nil
}

// ParseURI parses a SPIFFE URI of the form
// spiffe://<host>[/<tenant>]/agent/<name>/<ver>/run/<run_id> and returns
// the agent name, version, and run ID. It accepts both local and hosted
// formats.
func ParseURI(uri string) (agentName, agentVersion, runID string, err error) {
	// Check the raw URI starts with lowercase "spiffe://" before url.Parse
	// lowercases the scheme. This rejects SPIFFE://, Spiffe://, etc.
	if len(uri) < 9 || uri[:9] != "spiffe://" {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI: scheme must be lowercase \"spiffe\"")
	}
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI: %w", err)
	}
	if u.Scheme != "spiffe" {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI scheme %q, want \"spiffe\"", u.Scheme)
	}
	if u.Host == "" {
		return "", "", "", errors.New("invalid SPIFFE URI: empty host")
	}
	// Path: /agent/<name>/<ver>/run/<run_id> or /<tenant>/agent/<name>/<ver>/run/<run_id>
	path := strings.TrimPrefix(u.Path, "/")
	segments := strings.Split(path, "/")
	// We need at least 5 segments: agent/<name>/<ver>/run/<run_id>
	// Or 6 if hosted: <tenant>/agent/<name>/<ver>/run/<run_id>
	if len(segments) < 5 {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI path %q: too few segments", u.Path)
	}
	offset := 0
	// Check if the first segment is "agent" (local) or a tenant name (hosted).
	if segments[0] == "agent" {
		// Local format: agent/<name>/<ver>/run/<run_id>
		offset = 0
	} else if len(segments) >= 6 && segments[1] == "agent" {
		// Hosted format: <tenant>/agent/<name>/<ver>/run/<run_id>
		offset = 1
	} else {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI path %q: unexpected format", u.Path)
	}
	// After offset, we need: agent/<name>/<ver>/run/<run_id>
	if len(segments) < offset+5 {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI path %q: too few segments after trust domain", u.Path)
	}
	if segments[offset] != "agent" {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI: expected \"agent\" segment, got %q", segments[offset])
	}
	agentName = segments[offset+1]
	agentVersion = segments[offset+2]
	if segments[offset+3] != "run" {
		return "", "", "", fmt.Errorf("invalid SPIFFE URI: expected \"run\" segment, got %q", segments[offset+3])
	}
	runID = segments[offset+4]
	if agentName == "" || agentVersion == "" || runID == "" {
		return "", "", "", errors.New("invalid SPIFFE URI: empty component")
	}
	return agentName, agentVersion, runID, nil
}

// VerifyURI checks that the SPIFFE URI matches the expected agent name and
// version. It returns nil if the URI is valid and the identity matches.
func VerifyURI(uri, expectedAgentName, expectedAgentVersion string) error {
	name, ver, _, err := ParseURI(uri)
	if err != nil {
		return fmt.Errorf("verify SPIFFE URI: %w", err)
	}
	if name != expectedAgentName {
		return fmt.Errorf("SPIFFE URI agent name %q does not match expected %q", name, expectedAgentName)
	}
	if ver != expectedAgentVersion {
		return fmt.Errorf("SPIFFE URI agent version %q does not match expected %q", ver, expectedAgentVersion)
	}
	return nil
}

// VerifyWorkloadCert validates an x509 workload certificate. It checks that
// the certificate is currently valid (not expired, not yet active). This
// function does NOT verify the CA signature chain — that is done separately
// by the LocalCA's cert pool.
func VerifyWorkloadCert(cert *x509.Certificate) error {
	if cert == nil {
		return errors.New("certificate is nil")
	}
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return fmt.Errorf("certificate not yet valid: NotBefore=%v, now=%v", cert.NotBefore, now)
	}
	if now.After(cert.NotAfter) {
		return fmt.Errorf("certificate expired at %v", cert.NotAfter)
	}
	return nil
}

// FingerprintPublicKey computes a SHA-256 fingerprint of the DER-encoded
// public key bytes and returns it as a hex-encoded string with colons
// every two characters (similar to SSH key fingerprint format).
func FingerprintPublicKey(pub *x509.Certificate) string {
	der, err := x509.MarshalPKIXPublicKey(pub.PublicKey)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(der)
	// Format as hex with colons: xx:xx:xx...
	hexStr := hex.EncodeToString(hash[:])
	parts := make([]string, 0, len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		parts = append(parts, hexStr[i:i+2])
	}
	return strings.Join(parts, ":")
}