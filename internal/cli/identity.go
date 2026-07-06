package cli

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
)

// ---------------------------------------------------------------------------
// Identity keystore factory — overridable for tests
// ---------------------------------------------------------------------------

// identityStoreFactory is the function used to open the identity keystore.
// Tests replace it with a FakeKeyStore-returning function.
var identityStoreFactory = defaultIdentityStoreFactory

func defaultIdentityStoreFactory(_ *cobra.Command) (identity.KeyStore, error) {
	return identity.NewKeychainKeyStore("agentpaas-daemon")
}

func openIdentityStore(cmd *cobra.Command) (identity.KeyStore, error) {
	return identityStoreFactory(cmd)
}

// ---------------------------------------------------------------------------
// Identity command
// ---------------------------------------------------------------------------

// newIdentityCmd creates the `agent identity` command.
func newIdentityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Manage publisher identity (init, show, export, import)",
	}

	cmd.AddCommand(newIdentityInitCmd())
	cmd.AddCommand(newIdentityShowCmd())
	cmd.AddCommand(newIdentityExportCmd())
	cmd.AddCommand(newIdentityImportCmd())

	return cmd
}

// ---------------------------------------------------------------------------
// Identity init
// ---------------------------------------------------------------------------

func newIdentityInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new publisher identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			forceRotate, _ := cmd.Flags().GetBool("force-rotate")

			// Resolve name: from flag or interactive prompt.
			if name == "" {
				if isTerminal(cmd.InOrStdin()) {
					if _, err := fmt.Fprint(cmd.ErrOrStderr(), "Publisher name (GitHub-style slug, 1-39 chars): "); err != nil {
						return err
					}
					input, err := readLine(cmd.InOrStdin())
					if err != nil {
						return fmt.Errorf("read publisher name: %w", err)
					}
					name = strings.TrimSpace(input)
				} else {
					return fmt.Errorf("--name flag is required in non-interactive mode")
				}
			}

			if err := identity.ValidatePublisherName(name); err != nil {
				return err
			}

			ks, err := openIdentityStore(cmd)
			if err != nil {
				return fmt.Errorf("open identity keystore: %w", err)
			}

			if forceRotate {
				return runIdentityForceRotate(cmd, ks, name)
			}

			return runIdentityInit(cmd, ks, name)
		},
	}

	cmd.Flags().String("name", "", "Publisher name (GitHub-style slug, 1-39 chars)")
	cmd.Flags().Bool("force-rotate", false, "Replace existing identity with a new keypair")

	return cmd
}

func runIdentityInit(cmd *cobra.Command, ks identity.KeyStore, name string) error {
	pi, err := identity.CreatePublisherIdentity(ks, name)
	if err != nil {
		if errors.Is(err, identity.ErrPublisherIdentityExists) {
			return fmt.Errorf("publisher identity already exists — use --force-rotate to replace it")
		}
		return err
	}

	emitAudit(cmd, audit.EventTypePublisherIdentityCreated, map[string]string{
		"fingerprint": pi.Fingerprint,
		"name":        name,
	})

	printIdentityInitResult(cmd, pi)
	return nil
}

func runIdentityForceRotate(cmd *cobra.Command, ks identity.KeyStore, name string) error {
	// Load existing identity to capture old fingerprint.
	existing, err := identity.LoadPublisherIdentity(ks)
	if err != nil {
		if errors.Is(err, identity.ErrNoPublisherIdentity) {
			return fmt.Errorf("no existing publisher identity to rotate — use init without --force-rotate first")
		}
		return err
	}
	oldFP := existing.Fingerprint

	// Delete existing entries from keystore.
	_ = ks.Delete(identity.KeyID("publisher_identity"))
	_ = ks.Delete(identity.KeyID("publisher_identity_name"))

	// Create new identity.
	pi, err := identity.CreatePublisherIdentity(ks, name)
	if err != nil {
		return err
	}

	// Loud warning about key change.
	fmt.Fprintf(cmd.ErrOrStderr(), "\n⚠ WARNING: Publisher identity has been rotated.\n") //nolint:errcheck
	fmt.Fprintf(cmd.ErrOrStderr(), "Receivers who pinned the old key will hard-fail and must re-verify.\n") //nolint:errcheck
	fmt.Fprintf(cmd.ErrOrStderr(), "Old fingerprint: %s\n", identity.FormatFingerprintDisplay(oldFP)) //nolint:errcheck
	fmt.Fprintf(cmd.ErrOrStderr(), "New fingerprint: %s\n\n", identity.FormatFingerprintDisplay(pi.Fingerprint)) //nolint:errcheck

	emitAudit(cmd, audit.EventTypePublisherIdentityRotated, map[string]string{
		"old_fingerprint": oldFP,
		"new_fingerprint": pi.Fingerprint,
		"name":            name,
	})

	printIdentityInitResult(cmd, pi)
	return nil
}

func printIdentityInitResult(cmd *cobra.Command, pi *identity.PublisherIdentity) {
	jsonOut := jsonOutput(cmd)
	if jsonOut {
		_ = printTextOrJSON(true, identityShowResult{
			Name:          pi.Name,
			Fingerprint:   pi.Fingerprint,
			FingerprintDisplay: identity.FormatFingerprintDisplay(pi.Fingerprint),
			PublicKeyPEM:  pi.PublicKeyPEM,
			CreatedAt:     pi.CreatedAt.Format(time.RFC3339),
		}, nil)
		return
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Publisher identity created.\n")          //nolint:errcheck
	fmt.Fprintf(out, "Name:        %s\n", pi.Name)               //nolint:errcheck
	fmt.Fprintf(out, "Fingerprint: %s\n", identity.FormatFingerprintDisplay(pi.Fingerprint)) //nolint:errcheck
	fmt.Fprintf(out, "Created:     %s\n", pi.CreatedAt.Format(time.RFC3339)) //nolint:errcheck
	fmt.Fprintf(out, "\nShare this fingerprint with people who will receive your agents.\n") //nolint:errcheck
	fmt.Fprintf(out, "They verify it out-of-band to ensure authenticity.\n")               //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Identity show
// ---------------------------------------------------------------------------

type identityShowResult struct {
	Name               string `json:"name"`
	Fingerprint        string `json:"fingerprint"`
	FingerprintDisplay string `json:"fingerprint_display,omitempty"`
	PublicKeyPEM       string `json:"public_key_pem,omitempty"`
	CreatedAt          string `json:"created_at"`
}

func newIdentityShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Display publisher identity information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ks, err := openIdentityStore(cmd)
			if err != nil {
				return fmt.Errorf("open identity keystore: %w", err)
			}

			pi, err := identity.LoadPublisherIdentity(ks)
			if err != nil {
				if errors.Is(err, identity.ErrNoPublisherIdentity) {
					return fmt.Errorf("no publisher identity — run 'agent identity init' first")
				}
				return err
			}

			jsonOut := jsonOutput(cmd)
			if jsonOut {
				data := identityShowResult{
					Name:               pi.Name,
					Fingerprint:        pi.Fingerprint,
					FingerprintDisplay: identity.FormatFingerprintDisplay(pi.Fingerprint),
					PublicKeyPEM:       pi.PublicKeyPEM,
					CreatedAt:          pi.CreatedAt.Format(time.RFC3339),
				}
				return printTextOrJSON(true, data, nil)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Name:        %s\n", pi.Name)                               //nolint:errcheck
			fmt.Fprintf(out, "Fingerprint: %s\n", identity.FormatFingerprintDisplay(pi.Fingerprint)) //nolint:errcheck
			fmt.Fprintf(out, "Created:     %s\n", pi.CreatedAt.Format(time.RFC3339))      //nolint:errcheck
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// Identity export — encrypted backup
// ---------------------------------------------------------------------------

const (
	identityExportVersion    = 1
	identityPBKDF2Iterations = 100_000
	identitySaltLen          = 32
	identityNonceLen         = 12
	identityAESKeyLen        = 32
)

// identityExportPayload is the plaintext JSON that gets encrypted inside the envelope.
type identityExportPayload struct {
	PrivateKeyPEM string `json:"private_key_pem"`
	Name          string `json:"name"`
}

// identityExportEnvelope is the on-disk JSON format for an exported identity.
type identityExportEnvelope struct {
	Version   int    `json:"version"`
	KDF       string `json:"kdf"`
	SaltB64   string `json:"salt"`
	NonceB64  string `json:"nonce"`
	CipherB64 string `json:"ciphertext"`
}

func newIdentityExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an encrypted backup of the publisher identity",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			outPath, _ := cmd.Flags().GetString("out")
			if outPath == "" {
				return fmt.Errorf("--out flag is required")
			}

			ks, err := openIdentityStore(cmd)
			if err != nil {
				return fmt.Errorf("open identity keystore: %w", err)
			}

			// Load private key material from keystore.
			keyMaterial, err := ks.Load(identity.KeyID("publisher_identity"))
			if err != nil {
				if errors.Is(err, identity.ErrKeyNotFound) {
					return fmt.Errorf("no publisher identity — run 'agent identity init' first")
				}
				return fmt.Errorf("load publisher key: %w", err)
			}

			// Load name.
			nameMaterial, err := ks.Load(identity.KeyID("publisher_identity_name"))
			name := ""
			if err == nil {
				name = string(nameMaterial.Bytes)
			}

			// Build the plaintext payload.
			payload := identityExportPayload{
				PrivateKeyPEM: string(keyMaterial.Bytes),
				Name:          name,
			}
			payloadJSON, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal export payload: %w", err)
			}

			// Get passphrase interactively.
			passphrase, err := readExportPassphrase(cmd)
			if err != nil {
				return err
			}

			// Encrypt.
			envelope, err := encryptIdentityExport(payloadJSON, passphrase)
			if err != nil {
				return err
			}

			// Atomically write to file with mode 0600.
			if err := writeEnvelopeAtomic(outPath, 0600, envelope); err != nil {
				return err
			}

			// Load identity for audit fingerprint.
			pi, _ := identity.LoadPublisherIdentity(ks)
			fp := ""
			if pi != nil {
				fp = pi.Fingerprint
			}
			emitAudit(cmd, audit.EventTypePublisherIdentityExported, map[string]string{
				"fingerprint": fp,
				"name":        name,
			})

			fmt.Fprintf(cmd.OutOrStdout(), "Identity exported to %s\n", outPath) //nolint:errcheck
			return nil
		},
	}

	cmd.Flags().String("out", "", "Output file path (required)")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}

// readExportPassphrase reads and confirms the passphrase interactively.
// Minimum 12 characters. Never via argv.
func readExportPassphrase(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), "Enter export passphrase (min 12 chars): ") //nolint:errcheck
	pass1, err := readPassword(cmd)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	if len(pass1) < 12 {
		return "", fmt.Errorf("passphrase must be at least 12 characters (got %d)", len(pass1))
	}

	fmt.Fprint(cmd.ErrOrStderr(), "\nConfirm passphrase: ") //nolint:errcheck
	pass2, err := readPassword(cmd)
	if err != nil {
		return "", fmt.Errorf("read confirmation passphrase: %w", err)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "") //nolint:errcheck

	if pass1 != pass2 {
		return "", fmt.Errorf("passphrases do not match")
	}

	return pass1, nil
}

// readPassword reads a passphrase from the terminal without echo.
// Falls back to reading from stdin if not a TTY.
func readPassword(cmd *cobra.Command) (string, error) {
	f, ok := cmd.InOrStdin().(*os.File)
	if ok && isTerminal(f) {
		pw, err := term.ReadPassword(int(f.Fd()))
		if err != nil {
			return "", err
		}
		return string(pw), nil
	}

	// Non-TTY fallback: read from stdin.
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// encryptIdentityExport encrypts plaintext using AES-256-GCM with a
// PBKDF2-HMAC-SHA256 derived key. Returns the JSON envelope bytes.
func encryptIdentityExport(plaintext []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, identitySaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, passphrase, salt, identityPBKDF2Iterations, identityAESKeyLen)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	nonce := make([]byte, identityNonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	envelope := identityExportEnvelope{
		Version:   identityExportVersion,
		KDF:       "pbkdf2-hmac-sha256",
		SaltB64:   base64.StdEncoding.EncodeToString(salt),
		NonceB64:  base64.StdEncoding.EncodeToString(nonce),
		CipherB64: base64.StdEncoding.EncodeToString(ciphertext),
	}

	return json.Marshal(envelope)
}

// decryptIdentityExport decrypts a JSON envelope and returns the plaintext.
func decryptIdentityExport(data []byte, passphrase string) ([]byte, error) {
	var env identityExportEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse exported identity envelope: %w", err)
	}
	if env.Version != identityExportVersion {
		return nil, fmt.Errorf("unsupported export version %d", env.Version)
	}

	salt, err := base64.StdEncoding.DecodeString(env.SaltB64)
	if err != nil {
		return nil, fmt.Errorf("decode salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(env.NonceB64)
	if err != nil {
		return nil, fmt.Errorf("decode nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(env.CipherB64)
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, passphrase, salt, identityPBKDF2Iterations, identityAESKeyLen)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt identity (wrong passphrase or corrupted data): %w", err)
	}

	return plaintext, nil
}

// writeEnvelopeAtomic writes data atomically to path with the given file mode.
// Uses temp file in same directory + rename for atomicity.
func writeEnvelopeAtomic(path string, mode os.FileMode, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir export directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".identity-export-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write export data: %w", err)
	}

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod export file: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close export file: %w", err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename export file: %w", err)
	}

	cleanup = false
	return nil
}

// ---------------------------------------------------------------------------
// Identity import — decrypt and restore from backup
// ---------------------------------------------------------------------------

func newIdentityImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import <file>",
		Short: "Import an encrypted publisher identity backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]

			// Read the encrypted envelope.
			envelopeData, err := os.ReadFile(filePath)
			if err != nil {
				return fmt.Errorf("read export file: %w", err)
			}

			// Get passphrase.
			passphrase, err := readImportPassphrase(cmd)
			if err != nil {
				return err
			}

			// Decrypt.
			plaintext, err := decryptIdentityExport(envelopeData, passphrase)
			if err != nil {
				return err
			}

			// Parse the payload.
			var payload identityExportPayload
			if err := json.Unmarshal(plaintext, &payload); err != nil {
				return fmt.Errorf("parse identity payload: %w", err)
			}
			if payload.PrivateKeyPEM == "" {
				return fmt.Errorf("exported identity missing private key")
			}

			ks, err := openIdentityStore(cmd)
			if err != nil {
				return fmt.Errorf("open identity keystore: %w", err)
			}

			// Compute fingerprint of the imported key.
			importedFP, err := fingerprintFromPEM(payload.PrivateKeyPEM)
			if err != nil {
				return fmt.Errorf("parse imported private key: %w", err)
			}

			// Check if an identity already exists in the keystore.
			existing, err := identity.LoadPublisherIdentity(ks)
			if err != nil && !errors.Is(err, identity.ErrNoPublisherIdentity) {
				return fmt.Errorf("check existing identity: %w", err)
			}

			if existing != nil {
				if existing.Fingerprint == importedFP {
					// Idempotent success — same identity already exists.
					fmt.Fprintf(cmd.OutOrStdout(), "Identity already present (fingerprint %s) — nothing to do.\n", //nolint:errcheck
						identity.FormatFingerprintDisplay(importedFP))
					return nil
				}
				return fmt.Errorf("a different publisher identity already exists (fingerprint %s); "+
					"remove it first or import to a different home directory",
					identity.FormatFingerprintDisplay(existing.Fingerprint))
			}

			// Store private key in keystore.
			name := payload.Name
			if name == "" {
				name = "imported"
			}
			if err := identity.ValidatePublisherName(name); err != nil {
				return fmt.Errorf("imported publisher name invalid: %w", err)
			}

			keyMaterial := identity.KeyMaterial{
				Type:  identity.KeyTypePublisher,
				Bytes: []byte(payload.PrivateKeyPEM),
			}
			if err := ks.Create(identity.KeyID("publisher_identity"), identity.KeyTypePublisher, keyMaterial); err != nil {
				return fmt.Errorf("store imported private key: %w", err)
			}

			nameMaterial := identity.KeyMaterial{
				Type:  identity.KeyTypePublisher,
				Bytes: []byte(name),
			}
			if err := ks.Create(identity.KeyID("publisher_identity_name"), identity.KeyTypePublisher, nameMaterial); err != nil {
				// Rollback key entry.
				_ = ks.Delete(identity.KeyID("publisher_identity"))
				return fmt.Errorf("store imported publisher name: %w", err)
			}

			emitAudit(cmd, audit.EventTypePublisherIdentityImported, map[string]string{
				"fingerprint": importedFP,
				"name":        name,
			})

			fmt.Fprintf(cmd.OutOrStdout(), "Identity imported.\n")                               //nolint:errcheck
			fmt.Fprintf(cmd.OutOrStdout(), "Name:        %s\n", name)                        //nolint:errcheck
			fmt.Fprintf(cmd.OutOrStdout(), "Fingerprint: %s\n", identity.FormatFingerprintDisplay(importedFP)) //nolint:errcheck
			return nil
		},
	}
}

// readImportPassphrase reads the import passphrase from terminal.
func readImportPassphrase(cmd *cobra.Command) (string, error) {
	fmt.Fprint(cmd.ErrOrStderr(), "Enter import passphrase: ") //nolint:errcheck
	pw, err := readPassword(cmd)
	if err != nil {
		return "", fmt.Errorf("read passphrase: %w", err)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "") //nolint:errcheck
	return pw, nil
}

// fingerprintFromPEM computes a SHA-256 fingerprint from a PEM-encoded ECDSA
// private key (same algorithm as identity.PublisherFingerprint).
func fingerprintFromPEM(pemStr string) (string, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	return identity.PublisherFingerprint(&key.PublicKey), nil
}

// ---------------------------------------------------------------------------
// Audit emission — best-effort, never blocks identity operations
// ---------------------------------------------------------------------------

// emitAudit writes audit events via the daemon if available, otherwise
// logs a warning to stderr. Never fails the caller.
func emitAudit(cmd *cobra.Command, eventType string, payload map[string]string) {
	// Try to connect to the daemon.
	sock, err := socketPath(cmd)
	if err != nil {
		// Daemon socket path unresolvable — log locally.
		logAuditLocal(cmd, eventType, payload)
		return
	}

	_, conn, err := ConnectToDaemon(sock)
	if err != nil {
		// Daemon not running — acceptable, log locally.
		logAuditLocal(cmd, eventType, payload)
		return
	}
	defer func() { _ = conn.Close() }()

	// TODO: wire audit events through daemon control API.
	// For now, always log locally.
	logAuditLocal(cmd, eventType, payload)
}

// logAuditLocal writes an audit event to stderr as best-effort.
func logAuditLocal(cmd *cobra.Command, eventType string, payload map[string]string) {
	type auditEvent struct {
		EventType string            `json:"event_type"`
		Timestamp string            `json:"timestamp"`
		Payload   map[string]string `json:"payload"`
	}
	event := auditEvent{
		EventType: eventType,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}
	eventJSON, _ := json.Marshal(event)
	fmt.Fprintf(cmd.ErrOrStderr(), "audit: %s\n", string(eventJSON)) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------

// readLine reads a single line from a reader.
func readLine(r io.Reader) (string, error) {
	var buf [256]byte
	n, err := r.Read(buf[:])
	if err != nil {
		if err == io.EOF && n > 0 {
			return string(buf[:n]), nil
		}
		return "", err
	}
	s := string(buf[:n])
	s = strings.TrimRight(s, "\r\n")
	return s, nil
}

// Compile-time check that the key constants are known.
var _ = identity.KeyID("publisher_identity")
var _ = identity.KeyTypePublisher