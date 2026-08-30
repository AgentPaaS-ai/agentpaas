package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

var webhookNow = func() time.Time { return time.Now() }

func newCloudWebhookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "webhook",
		Short: "Configure cloud deployment webhooks",
		Long: `Configure ingress HMAC webhooks and completion/delivery destinations.

Ingress (doorbell): PUT /v1/deployments/<id>/webhook then fire with HMAC POST.
Completion (receipt) and delivery (final_output) are separate HTTPS PUTs.

HMAC secrets are never printed. Use --secret-stdin for set and fire.`,
		Example: `  # Ingress HMAC webhook (secret is not printed)
  printf '%s' "$HOOK_SECRET" | agentpaas cloud webhook set dep_x --secret-stdin

  # Fire the ingress hook (no tenant token)
  printf '%s' "$HOOK_SECRET" | agentpaas cloud webhook fire dep_x --body '{"ok":true}' --secret-stdin

  # Completion and delivery destinations (public HTTPS only)
  agentpaas cloud webhook completion dep_x --url https://example.com/complete
  agentpaas cloud webhook delivery dep_x --url https://example.com/deliver`,
	}
	cmd.AddCommand(newCloudWebhookSetCmd())
	cmd.AddCommand(newCloudWebhookFireCmd())
	cmd.AddCommand(newCloudWebhookCompletionCmd())
	cmd.AddCommand(newCloudWebhookDeliveryCmd())
	return cmd
}

func readWebhookSecret(cmd *cobra.Command, secret string, secretStdin bool) (string, error) {
	if secretStdin && secret != "" {
		return "", fmt.Errorf("cloud webhook: --secret and --secret-stdin are mutually exclusive")
	}
	if secretStdin {
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("cloud webhook: read --secret-stdin: %w", err)
		}
		secret = strings.TrimSpace(string(b))
	}
	if secret == "" {
		return "", fmt.Errorf("cloud webhook: --secret or --secret-stdin is required")
	}
	return secret, nil
}

func requireHTTPSWebhookURL(raw string) error {
	if !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("cloud webhook: --url must be https")
	}
	if strings.ContainsAny(raw, "\n\r\x00") {
		return fmt.Errorf("cloud webhook: --url contains invalid characters")
	}
	return nil
}

func newCloudWebhookSetCmd() *cobra.Command {
	var provider string
	var secret string
	var secretStdin bool
	cmd := &cobra.Command{
		Use:   "set <deployment>",
		Short: "Configure the ingress HMAC webhook on a deployment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secret, err := readWebhookSecret(cmd, secret, secretStdin)
			if err != nil {
				return err
			}
			switch provider {
			case "generic_hmac", "stripe":
			default:
				return fmt.Errorf("cloud webhook set: --provider must be generic_hmac or stripe")
			}
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud webhook set: %w", err)
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			depID, err := resolveDeploymentRef(cmd, client, token, args[0])
			if err != nil {
				return err
			}
			res, err := client.PutDeploymentWebhook(cmd.Context(), token, depID, provider, secret)
			if err != nil {
				return fmt.Errorf("cloud webhook set: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Webhook configured on %s (provider=%s)\n", res.DeploymentID, res.Provider)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "generic_hmac", "Ingress provider: generic_hmac|stripe")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC secret (prefer --secret-stdin)")
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "Read HMAC secret from stdin")
	return cmd
}

func newCloudWebhookFireCmd() *cobra.Command {
	var provider string
	var body string
	var secret string
	var secretStdin bool
	cmd := &cobra.Command{
		Use:   "fire <deployment>",
		Short: "POST a signed ingress webhook (no tenant token)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secret, err := readWebhookSecret(cmd, secret, secretStdin)
			if err != nil {
				return err
			}
			depID := strings.TrimSpace(args[0])
			if depID == "" || strings.ContainsAny(depID, "/\\\n\r") {
				return fmt.Errorf("cloud webhook fire: invalid deployment id")
			}
			rawBody := []byte(body)
			now := webhookNow()
			client := cloudclient.NewCloudClient(resolveAPIURL())
			var raw []byte
			switch provider {
			case "generic_hmac":
				raw, err = client.FireGenericHMAC(cmd.Context(), depID, rawBody, secret, now)
			case "stripe":
				raw, err = client.FireStripeHook(cmd.Context(), depID, rawBody, secret, now)
			default:
				return fmt.Errorf("cloud webhook fire: --provider must be generic_hmac or stripe")
			}
			if err != nil {
				return fmt.Errorf("cloud webhook fire: %w", err)
			}
			if jsonOutput(cmd) {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return nil
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "generic_hmac", "Ingress provider: generic_hmac|stripe")
	cmd.Flags().StringVar(&body, "body", "{}", "Raw JSON body to HMAC and POST")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC secret (prefer --secret-stdin)")
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "Read HMAC secret from stdin")
	return cmd
}

func putDestinationWebhook(cmd *cobra.Command, deploymentRef, destURL, kind string, put func(client *cloudclient.CloudClient, token, depID, destURL string) (*cloudclient.DestinationWebhookResponse, error)) error {
	if err := requireHTTPSWebhookURL(destURL); err != nil {
		return err
	}
	token, err := resolveToken(cmd)
	if err != nil {
		if strings.Contains(err.Error(), "not logged in") {
			return printNotLoggedIn(cmd)
		}
		return fmt.Errorf("cloud webhook %s: %w", kind, err)
	}
	client := cloudclient.NewCloudClient(resolveAPIURL())
	depID, err := resolveDeploymentRef(cmd, client, token, deploymentRef)
	if err != nil {
		return err
	}
	res, err := put(client, token, depID, destURL)
	if err != nil {
		return fmt.Errorf("cloud webhook %s: %w", kind, err)
	}
	if jsonOutput(cmd) {
		return printTextOrJSON(true, res, nil)
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s webhook set on %s\n", kind, depID)
	return nil
}

func newCloudWebhookCompletionCmd() *cobra.Command {
	var destURL string
	cmd := &cobra.Command{
		Use:   "completion <deployment>",
		Short: "Set the completion (receipt) webhook URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return putDestinationWebhook(cmd, args[0], destURL, "completion", func(client *cloudclient.CloudClient, token, depID, u string) (*cloudclient.DestinationWebhookResponse, error) {
				return client.PutCompletionWebhook(cmd.Context(), token, depID, u)
			})
		},
	}
	cmd.Flags().StringVar(&destURL, "url", "", "Public HTTPS destination (required)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}

func newCloudWebhookDeliveryCmd() *cobra.Command {
	var destURL string
	cmd := &cobra.Command{
		Use:   "delivery <deployment>",
		Short: "Set the delivery (final_output) webhook URL",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return putDestinationWebhook(cmd, args[0], destURL, "delivery", func(client *cloudclient.CloudClient, token, depID, u string) (*cloudclient.DestinationWebhookResponse, error) {
				return client.PutDeliveryWebhook(cmd.Context(), token, depID, u)
			})
		},
	}
	cmd.Flags().StringVar(&destURL, "url", "", "Public HTTPS destination (required)")
	_ = cmd.MarkFlagRequired("url")
	return cmd
}
