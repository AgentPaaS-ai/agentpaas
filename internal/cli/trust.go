package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
	"github.com/spf13/cobra"
)

// newTrustCmd creates the `agent trust` command for managing the trust store.
func newTrustCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Manage trusted publisher keys",
		Long: `Manage the publisher trust store — a local registry of trusted publisher
public keys with TOFU (trust on first use) and manual pre-pinning support.

The trust store is stored at <home>/trust/publishers.json. Commands operate
directly on the store file (no daemon required).`,
	}

	cmd.AddCommand(newTrustAddCmd())
	cmd.AddCommand(newTrustListCmd())
	cmd.AddCommand(newTrustShowCmd())
	cmd.AddCommand(newTrustRemoveCmd())

	return cmd
}

// trustStorePath returns the path to the trust store file in the home directory.
func trustStorePath(cmd *cobra.Command) (string, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return "", fmt.Errorf("trust store path: %w", err)
	}
	return trust.DefaultStorePath(homeDir), nil
}

// resolveFingerprint resolves a fingerprint or alias to a full normalized
// fingerprint. If the input looks like a full fingerprint (64 hex chars after
// normalization), it's returned directly. Otherwise it's treated as an alias
// prefix and looked up in the store.
func resolveFingerprint(store *trust.Store, input string) (string, error) {
	fp := trust.NormalizeFingerprint(input)
	if len(fp) == 64 {
		return fp, nil
	}

	// Treat as alias — find matching publisher.
	for _, p := range store.Publishers() {
		if p.Alias != "" && strings.EqualFold(p.Alias, strings.ToLower(input)) {
			return p.Fingerprint, nil
		}
	}

	return "", fmt.Errorf("no publisher found matching %q", input)
}

// newTrustAddCmd creates the `agent trust add` command.
func newTrustAddCmd() *cobra.Command {
	var (
		keyFile string
		alias   string
	)

	cmd := &cobra.Command{
		Use:   "add <fingerprint>",
		Short: "Add a publisher to the trust store (manual pre-pinning)",
		Long: `Add a publisher public key to the trust store by providing its
fingerprint and the PEM-encoded public key file.

The fingerprint accepts both compact form (64 hex chars) and display form
(with spaces between 4-char blocks). The public key PEM must be an ECDSA
P-256 key. The fingerprint is validated against the PEM to ensure
self-consistency before the publisher is trusted.

Example:
  agent trust add a1b2c3d4... --key publisher.pem --alias parvez`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fingerprint := args[0]

			if keyFile == "" {
				return fmt.Errorf("--key flag is required (path to PEM-encoded public key file)")
			}

			// Read the PEM file.
			pemData, err := os.ReadFile(keyFile)
			if err != nil {
				return fmt.Errorf("read key file %s: %w", keyFile, err)
			}

			// Load the store.
			storePath, err := trustStorePath(cmd)
			if err != nil {
				return fmt.Errorf("new trust add cmd: %w", err)
			}
			store, err := trust.Load(storePath)
			if err != nil {
				return fmt.Errorf("load trust store: %w", err)
			}

			// Validate alias is a slug.
			if alias != "" && !trust.IsValidAlias(alias) {
				return fmt.Errorf("invalid alias %q: must be lowercase alphanumeric with hyphens, max 64 chars", alias)
			}

			pub := trust.Publisher{
				Fingerprint:  fingerprint,
				PublicKeyPEM: string(pemData),
				Alias:        alias,
			}

			if err := store.Pin(pub, trust.SourceManual); err != nil {
				return fmt.Errorf("pin publisher: %w", err)
			}

			if err := store.Save(); err != nil {
				return fmt.Errorf("save trust store: %w", err)
			}

			displayFP := trust.DisplayFingerprint(fingerprint)
			if alias != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Trusted publisher %q (%s)\n", alias, displayFP) // best-effort CLI write
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Trusted publisher %s\n", displayFP) // best-effort CLI write
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&keyFile, "key", "", "Path to PEM-encoded public key file (required)")
	cmd.Flags().StringVar(&alias, "alias", "", "Human-readable alias for the publisher (optional slug)")
	_ = cmd.MarkFlagRequired("key") // flag registration; failure surfaces at Execute

	return cmd
}

// trustListEntry is the JSON-serializable output for trust list.
type trustListEntry struct {
	Fingerprint string `json:"fingerprint"`
	Alias       string `json:"alias"`
	FirstSeen   string `json:"first_seen"`
	LastUsed    string `json:"last_used"`
	Source      string `json:"source"`
	Status      string `json:"status"`
}

// newTrustListCmd creates the `agent trust list` command.
func newTrustListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List trusted publishers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := trustStorePath(cmd)
			if err != nil {
				return fmt.Errorf("new trust list cmd: %w", err)
			}
			store, err := trust.Load(storePath)
			if err != nil {
				return fmt.Errorf("load trust store: %w", err)
			}

			publishers := store.Publishers()

			if jsonOutput(cmd) {
				entries := make([]trustListEntry, 0, len(publishers))
				for _, p := range publishers {
					entries = append(entries, trustListEntry{
						Fingerprint: trust.DisplayFingerprint(p.Fingerprint),
						Alias:       p.Alias,
						FirstSeen:   p.FirstSeen,
						LastUsed:    p.LastUsed,
						Source:      string(p.Source),
						Status:      string(p.Status),
					})
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(entries)
			}

			if len(publishers) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No trusted publishers") // best-effort CLI write
				return nil
			}

			// Human-readable table.
			for _, p := range publishers {
				alias := p.Alias
				if alias == "" {
					alias = "-"
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-72s  %-16s  %-20s  %-20s  %-6s\n", // best-effort CLI write
					trust.DisplayFingerprint(p.Fingerprint),
					alias,
					p.FirstSeen,
					p.LastUsed,
					string(p.Source),
				)
			}
			return nil
		},
	}

	return cmd
}

// trustShowOutput is the JSON output for the trust show command.
type trustShowOutput struct {
	Fingerprint  string `json:"fingerprint"`
	Alias        string `json:"alias"`
	PublicKeyPEM string `json:"public_key_pem"`
	FirstSeen    string `json:"first_seen"`
	LastUsed     string `json:"last_used"`
	Source       string `json:"source"`
	Status       string `json:"status"`
}

// newTrustShowCmd creates the `agent trust show` command.
func newTrustShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <fingerprint-or-alias>",
		Short: "Show details for a trusted publisher",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := trustStorePath(cmd)
			if err != nil {
				return fmt.Errorf("new trust show cmd: %w", err)
			}
			store, err := trust.Load(storePath)
			if err != nil {
				return fmt.Errorf("load trust store: %w", err)
			}

			fp, err := resolveFingerprint(store, args[0])
			if err != nil {
				return fmt.Errorf("new trust show cmd: %w", err)
			}

			pub, ok := store.Get(fp)
			if !ok {
				return fmt.Errorf("publisher not found: %s", trust.DisplayFingerprint(fp))
			}

			if jsonOutput(cmd) {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(trustShowOutput{
					Fingerprint:  trust.DisplayFingerprint(pub.Fingerprint),
					Alias:        pub.Alias,
					PublicKeyPEM: pub.PublicKeyPEM,
					FirstSeen:    pub.FirstSeen,
					LastUsed:     pub.LastUsed,
					Source:       string(pub.Source),
					Status:       string(pub.Status),
				})
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Fingerprint:  %s\n", trust.DisplayFingerprint(pub.Fingerprint)) // best-effort CLI write
			if pub.Alias != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Alias:        %s\n", pub.Alias) // best-effort CLI write
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "First Seen:   %s\n", pub.FirstSeen)       // best-effort CLI write
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Last Used:    %s\n", pub.LastUsed)        // best-effort CLI write
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Source:       %s\n", pub.Source)          // best-effort CLI write
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Status:       %s\n", pub.Status)          // best-effort CLI write
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nPublic Key PEM:\n%s", pub.PublicKeyPEM) // best-effort CLI write
			return nil
		},
	}

	return cmd
}

// newTrustRemoveCmd creates the `agent trust remove` command.
func newTrustRemoveCmd() *cobra.Command {
	var yesFlag bool

	cmd := &cobra.Command{
		Use:   "remove <fingerprint-or-alias>",
		Short: "Remove a publisher from the trust store",
		Long: `Remove a publisher from the trust store.

By default, when running in a terminal, you must type the first 8 characters
of the fingerprint to confirm removal. Use --yes to skip confirmation in
non-interactive mode.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, err := trustStorePath(cmd)
			if err != nil {
				return fmt.Errorf("new trust remove cmd: %w", err)
			}
			store, err := trust.Load(storePath)
			if err != nil {
				return fmt.Errorf("load trust store: %w", err)
			}

			fp, err := resolveFingerprint(store, args[0])
			if err != nil {
				return fmt.Errorf("new trust remove cmd: %w", err)
			}

			pub, ok := store.Get(fp)
			if !ok {
				return fmt.Errorf("publisher not found: %s", trust.DisplayFingerprint(fp))
			}

			// Confirmation: type fp8 prefix or use --yes.
			if !yesFlag {
				if isTerminal(os.Stdin) {
					fp8 := fp[:8]
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), // best-effort CLI write
						"Type %q to confirm removal of publisher %s: ", fp8,
						trust.DisplayFingerprint(fp))
					var input string
					_, err := fmt.Scanln(&input)
					if err != nil {
						return fmt.Errorf("read confirmation: %w", err)
					}
					if strings.TrimSpace(input) != fp8 {
						return fmt.Errorf("confirmation mismatch: removal cancelled")
					}
				} else {
					return fmt.Errorf("not a TTY: use --yes to confirm removal of publisher %s",
						trust.DisplayFingerprint(fp))
				}
			}

			alias := pub.Alias
			if err := store.Remove(fp); err != nil {
				return fmt.Errorf("remove publisher: %w", err)
			}

			if err := store.Save(); err != nil {
				return fmt.Errorf("save trust store: %w", err)
			}

			if alias != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed publisher %q (%s)\n", alias, trust.DisplayFingerprint(fp)) // best-effort CLI write
			} else {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed publisher %s\n", trust.DisplayFingerprint(fp)) // best-effort CLI write
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&yesFlag, "yes", false, "Skip confirmation prompt")

	return cmd
}
