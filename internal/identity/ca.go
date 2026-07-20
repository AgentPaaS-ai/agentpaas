package identity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"sync"
	"time"
)

// Default key IDs stored in the KeyStore.
const (
	caKeyID                    KeyID = "local_ca"
	auditSigningKeyID          KeyID = "daemon_audit_signing"
	packageIdentityBase        KeyID = "package_identity"
	publisherIdentityKeyID     KeyID = "publisher_identity"
	publisherIdentityNameKeyID KeyID = "publisher_identity_name"
)

// LocalCA manages a local certificate authority and related cryptographic
// identities. It creates and stores a CA signing key (ECDSA P-256) in a
// KeyStore, issues per-run workload certificates signed by that CA, manages
// per-agent package identity keys (AIDs), and manages a daemon audit signing
// key. Package identity keys are never returned to callers — only their
// public key fingerprints are exposed.
type LocalCA struct {
	store       KeyStore
	trustDomain *TrustDomain
	caKeyID     KeyID
	// Cached CA certificate (self-signed, rebuilt on first use).
	caCert   *x509.Certificate
	caCertMu sync.RWMutex
}

// NewLocalCA creates a new LocalCA that uses the given KeyStore and trust
// domain. It ensures the CA signing key exists in the store (creating it
// if necessary) and initializes the in-memory CA certificate.
func NewLocalCA(store KeyStore, td *TrustDomain) (*LocalCA, error) {
	ca := &LocalCA{
		store:       store,
		trustDomain: td,
		caKeyID:     caKeyID,
	}
	// Ensure the CA key exists.
	if err := ca.ensureCAKey(); err != nil {
		return nil, fmt.Errorf("initialize CA key: %w", err)
	}
	return ca, nil
}

// ensureCAKey creates the CA signing key in the keystore if it does not
// already exist. The key is ECDSA P-256.
func (ca *LocalCA) ensureCAKey() error {
	_, err := ca.store.Load(ca.caKeyID)
	if err == nil {
		return nil // already exists
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return err
	}
	// Generate a new ECDSA P-256 key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal CA key: %w", err)
	}
	pemBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	material := KeyMaterial{
		Type:  KeyTypeCA,
		Bytes: pem.EncodeToMemory(pemBlock),
	}
	return ca.store.Create(ca.caKeyID, KeyTypeCA, material)
}

// getCACertificate returns a self-signed CA certificate for the local CA
// key. The certificate is cached in memory after first creation.
func (ca *LocalCA) getCACertificate() (*x509.Certificate, error) {
	ca.caCertMu.RLock()
	if ca.caCert != nil {
		defer ca.caCertMu.RUnlock()
		return ca.caCert, nil
	}
	ca.caCertMu.RUnlock()

	ca.caCertMu.Lock()
	defer ca.caCertMu.Unlock()
	// Double-check after acquiring write lock.
	if ca.caCert != nil {
		return ca.caCert, nil
	}

	km, err := ca.store.Load(ca.caKeyID)
	if err != nil {
		return nil, fmt.Errorf("load CA key: %w", err)
	}
	key, err := parseECDSAPrivateKey(km.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}

	// Build a self-signed CA certificate valid for 10 years.
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate CA serial: %w", err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "AgentPaaS Local CA",
			Organization: []string{"AgentPaaS"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse CA certificate: %w", err)
	}
	ca.caCert = cert
	return cert, nil
}

// IssueWorkloadCert generates a workload certificate and ephemeral key pair
// for the given agent run. The certificate is signed by the local CA and
// contains the SPIFFE URI as a URI SAN. Returns the certificate, private
// key, SPIFFE URI, and any error.
func (ca *LocalCA) IssueWorkloadCert(agentName, agentVersion, runID string, ttl time.Duration) (*x509.Certificate, *ecdsa.PrivateKey, string, error) {
	if agentName == "" {
		return nil, nil, "", fmt.Errorf("%w: agentName must not be empty", ErrInvalidComponent)
	}
	if agentVersion == "" {
		return nil, nil, "", fmt.Errorf("%w: agentVersion must not be empty", ErrInvalidComponent)
	}
	if runID == "" {
		return nil, nil, "", fmt.Errorf("%w: runID must not be empty", ErrInvalidComponent)
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	spiffeURI, err := ca.trustDomain.BuildURI(agentName, agentVersion, runID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("build SPIFFE URI: %w", err)
	}

	// Generate ephemeral workload key pair.
	workloadKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, "", fmt.Errorf("generate workload key: %w", err)
	}

	// Get the CA certificate and key.
	caCert, err := ca.getCACertificate()
	if err != nil {
		return nil, nil, "", fmt.Errorf("get CA certificate: %w", err)
	}
	km, err := ca.store.Load(ca.caKeyID)
	if err != nil {
		return nil, nil, "", fmt.Errorf("load CA key: %w", err)
	}
	caKey, err := parseECDSAPrivateKey(km.Bytes)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parse CA key: %w", err)
	}

	// Build the workload certificate template.
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, "", fmt.Errorf("generate serial: %w", err)
	}
	now := time.Now()
	parsedURI, err := url.Parse(spiffeURI)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parse SPIFFE URI: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   fmt.Sprintf("agent:%s:%s", agentName, runID),
			Organization: []string{"AgentPaaS"},
		},
		NotBefore: now.Add(-5 * time.Minute), // 5 min clock skew tolerance
		NotAfter:  now.Add(ttl),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageClientAuth,
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  false,
		URIs:                  []*url.URL{parsedURI},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &workloadKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, "", fmt.Errorf("create workload certificate: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, "", fmt.Errorf("parse workload certificate: %w", err)
	}
	return cert, workloadKey, spiffeURI, nil
}

// RenewWorkloadCert issues a new workload certificate for the same agent
// identity, replacing any existing workload key. It is intended to be called
// before the current certificate expires (e.g., at 80% of TTL).
func (ca *LocalCA) RenewWorkloadCert(agentName, agentVersion, runID string, ttl time.Duration) (*x509.Certificate, *ecdsa.PrivateKey, string, error) {
	return ca.IssueWorkloadCert(agentName, agentVersion, runID, ttl)
}

// VerifyWorkloadCert validates a workload certificate against the local CA.
// It checks the CA signature chain and certificate validity period.
func (ca *LocalCA) VerifyWorkloadCert(cert *x509.Certificate) error {
	if cert == nil {
		return errors.New("certificate is nil")
	}
	// Verify the certificate is currently valid.
	if err := VerifyWorkloadCert(cert); err != nil {
		return fmt.Errorf("local ca verify workload cert: %w", err)
	}
	// Get the CA certificate and verify the signature chain.
	caCert, err := ca.getCACertificate()
	if err != nil {
		return fmt.Errorf("get CA certificate: %w", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	intermediates := x509.NewCertPool()
	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   time.Now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("workload cert verification failed: %w", err)
	}
	return nil
}

// EnsurePackageIdentityKey creates a package identity key (AID) for the
// given agent if it does not already exist. It returns the public key
// fingerprint (SHA-256 of the DER-encoded public key, hex with colons).
// The private key is stored in the KeyStore and is never returned to
// callers — only the fingerprint is exposed.
func (ca *LocalCA) EnsurePackageIdentityKey(agentName string) (fingerprint string, err error) {
	keyID := packageIdentityKeyID(agentName)
	// Try to load existing key.
	km, loadErr := ca.store.Load(keyID)
	if loadErr == nil {
		// Key exists; compute and return fingerprint.
		key, parseErr := parseECDSAPrivateKey(km.Bytes)
		if parseErr != nil {
			return "", fmt.Errorf("parse existing AID key: %w", parseErr)
		}
		return publicKeyFingerprint(&key.PublicKey), nil
	}
	if !errors.Is(loadErr, ErrKeyNotFound) {
		return "", fmt.Errorf("load AID key: %w", loadErr)
	}
	// Generate a new package identity key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate AID key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", fmt.Errorf("marshal AID key: %w", err)
	}
	pemBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	material := KeyMaterial{
		Type:  KeyTypePackageIdentity,
		Bytes: pem.EncodeToMemory(pemBlock),
	}
	if err := ca.store.Create(keyID, KeyTypePackageIdentity, material); err != nil {
		return "", fmt.Errorf("store AID key: %w", err)
	}
	return publicKeyFingerprint(&key.PublicKey), nil
}

// EnsureDaemonAuditSigningKey creates the daemon audit signing key in the
// keystore if it does not already exist. This is idempotent — subsequent
// calls are no-ops. The key is ECDSA P-256.
func (ca *LocalCA) EnsureDaemonAuditSigningKey() error {
	_, err := ca.store.Load(auditSigningKeyID)
	if err == nil {
		return nil // already exists
	}
	if !errors.Is(err, ErrKeyNotFound) {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate audit signing key: %w", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal audit signing key: %w", err)
	}
	pemBlock := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	material := KeyMaterial{
		Type:  KeyTypeAuditSigning,
		Bytes: pem.EncodeToMemory(pemBlock),
	}
	return ca.store.Create(auditSigningKeyID, KeyTypeAuditSigning, material)
}

// packageIdentityKeyID returns the KeyID for a given agent's package
// identity key.
func packageIdentityKeyID(agentName string) KeyID {
	return KeyID(string(packageIdentityBase) + "_" + agentName)
}

// publicKeyFingerprint computes a SHA-256 fingerprint of a DER-encoded ECDSA
// public key and returns it as a colon-separated hex string.
func publicKeyFingerprint(pub *ecdsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(der)
	hexStr := hex.EncodeToString(hash[:])
	// Format as xx:xx:xx:...
	result := ""
	for i := 0; i < len(hexStr); i += 2 {
		if i > 0 {
			result += ":"
		}
		result += hexStr[i : i+2]
	}
	return result
}

// Compile-time check that *ecdsa.PrivateKey satisfies crypto.Signer (used
// by x509.CreateCertificate).
var _ crypto.Signer = (*ecdsa.PrivateKey)(nil)
