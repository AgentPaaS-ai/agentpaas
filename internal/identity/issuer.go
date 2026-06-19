package identity

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"
)

// LocalIdentityIssuer implements the IdentityIssuer interface by wrapping a
// LocalCA and KeyStore to issue SPIFFE-style workload certificates for
// agent run identities. It returns PEM-encoded certificate and private key
// bytes along with the SPIFFE URI.
type LocalIdentityIssuer struct {
	ca    *LocalCA
	store KeyStore
}

// NewLocalIdentityIssuer creates a new LocalIdentityIssuer backed by the
// given KeyStore and trust domain. It initializes the underlying LocalCA.
func NewLocalIdentityIssuer(store KeyStore, td *TrustDomain) (*LocalIdentityIssuer, error) {
	ca, err := NewLocalCA(store, td)
	if err != nil {
		return nil, fmt.Errorf("new local identity issuer: %w", err)
	}
	return &LocalIdentityIssuer{
		ca:    ca,
		store: store,
	}, nil
}

// IssueWorkloadCert generates a workload certificate and key for the given
// agent run. It returns PEM-encoded certificate, PEM-encoded private key,
// and the SPIFFE URI. This implements the IdentityIssuer interface defined
// in keystore.go.
func (l *LocalIdentityIssuer) IssueWorkloadCert(agentName, agentVersion, runID string, ttl time.Duration) ([]byte, []byte, string, error) {
	cert, key, spiffeURI, err := l.ca.IssueWorkloadCert(agentName, agentVersion, runID, ttl)
	if err != nil {
		return nil, nil, "", fmt.Errorf("issue workload cert: %w", err)
	}
	// Encode the certificate to PEM.
	certPEM, err := certToPEM(cert)
	if err != nil {
		return nil, nil, "", fmt.Errorf("encode cert to PEM: %w", err)
	}
	// Encode the private key to PEM.
	keyPEM, err := privateKeyToPEM(key)
	if err != nil {
		return nil, nil, "", fmt.Errorf("encode key to PEM: %w", err)
	}
	return certPEM, keyPEM, spiffeURI, nil
}

// certToPEM encodes an x509 certificate as PEM-encoded bytes.
func certToPEM(cert *x509.Certificate) ([]byte, error) {
	if cert == nil {
		return nil, fmt.Errorf("certificate is nil")
	}
	block := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}
	return pem.EncodeToMemory(block), nil
}

// privateKeyToPEM encodes an ECDSA private key as PEM-encoded bytes using
// PKCS8 format (not the deprecated key.D.Bytes() approach).
func privateKeyToPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("private key is nil")
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal private key: %w", err)
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}
	return pem.EncodeToMemory(block), nil
}

// Compile-time check that LocalIdentityIssuer satisfies IdentityIssuer.
var _ IdentityIssuer = (*LocalIdentityIssuer)(nil)