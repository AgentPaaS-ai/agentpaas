package cli

import (
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

// newCloudOauthCmd creates the `agentpaas cloud oauth` command group for
// managing delegated OAuth grants on the AgentPaaS Cloud control plane.
func newCloudOauthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oauth",
		Short: "Manage delegated OAuth grants on AgentPaaS Cloud",
		Long: `Manage delegated OAuth grants that allow a cloud deployment to call
third-party APIs on behalf of an end user.

Subcommands operate on grants stored server-side; the CLI never handles or
prints OAuth access tokens or refresh tokens. The request body never includes
tenant_id — the Cloud API derives the tenant from the authenticated session.`,
	}
	cmd.AddCommand(newCloudOauthRevokeCmd())
	return cmd
}

// newCloudOauthRevokeCmd creates the `agentpaas cloud oauth revoke` command.
//
// It calls authenticated POST /v1/oauth/revoke with a JSON body carrying the
// deployment ID, credential ID, and end-user identity.  tenant_id is
// intentionally excluded from the request body — the server resolves the
// tenant from the Bearer token, preventing tenant spoofing.  The command
// prints only outcome metadata (deployment/credential/identity/revoked/
// revoked_at) and never prints tokens.
func newCloudOauthRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <deployment-id> <credential-id> <end-user-identity>",
		Short: "Revoke a delegated OAuth grant from the cloud",
		Long: `Revoke a delegated OAuth grant so a cloud deployment can no longer use
the end user's credential to call a third-party API.

This calls POST /v1/oauth/revoke with the deployment ID, credential ID, and
end-user identity. The request body never includes tenant_id; the Cloud API
derives the tenant from the authenticated Bearer token, preventing tenant
spoofing. The command prints only outcome metadata (IDs, revoked status,
timestamp) and never prints OAuth access or refresh tokens.

Requires a valid cloud login. Use 'agentpaas cloud login' first.`,
		Example: `  # Revoke a grant for a specific end user on a deployment
  agentpaas cloud oauth revoke dep-abc123 cred-openai alice@example.com

  # JSON output for scripting
  agentpaas cloud oauth revoke dep-abc123 cred-openai alice@example.com --json`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := args[0]
			credentialID := args[1]
			endUserIdentity := args[2]

			// Sanitize: none of the path-influencing args may contain slashes,
			// backslashes, or newlines.  They go into the JSON body, not the
			// URL path, but sanitizing here is defense-in-depth.
			for _, v := range []string{deploymentID, credentialID, endUserIdentity} {
				if strings.ContainsAny(v, "/\\\n\r") {
					return fmt.Errorf("cloud oauth revoke: invalid argument %q: must not contain '/', '\\', newline, or carriage return", v)
				}
				if v == "" {
					return fmt.Errorf("cloud oauth revoke: arguments must be non-empty")
				}
			}

			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud oauth revoke: %w", err)
			}

			apiURL := resolveAPIURL()
			client := cloudclient.NewCloudClient(apiURL)

			req := cloudclient.RevokeOAuthGrantRequest{
				DeploymentID:    deploymentID,
				CredentialID:    credentialID,
				EndUserIdentity: endUserIdentity,
			}

			result, err := client.RevokeOAuthGrant(cmd.Context(), token, req)
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud oauth revoke: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Revoked OAuth grant:\n")
			_, _ = fmt.Fprintf(out, "  Deployment:     %s\n", result.DeploymentID)
			_, _ = fmt.Fprintf(out, "  Credential:     %s\n", result.CredentialID)
			_, _ = fmt.Fprintf(out, "  End-user:       %s\n", result.EndUserIdentity)
			_, _ = fmt.Fprintf(out, "  Revoked:        %v\n", result.Revoked)
			_, _ = fmt.Fprintf(out, "  Revoked at:     %s\n", result.RevokedAt)
			return nil
		},
	}
}
