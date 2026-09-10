package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

func newCloudIngressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ingress",
		Short: "Connect external apps to cloud deployments",
		Long: `Create an ingress source (one external app) and connect deployments to it.

Signing secrets are never printed. Use --secret-stdin for source create.

G1 covers source create and connect only.`,
		Example: `  # Create a Slack source (secret is not printed)
  printf '%s' "$SLACK_SIGNING_SECRET" | agentpaas cloud ingress source create --provider slack --label "vb-editorial" --secret-stdin

  # Subscribe a deployment; empty --filter matches all admitted events
  agentpaas cloud ingress connect src_01J... dep_x --label "support triage" --filter '{"match":"all","rules":[{"field":"event.channel","op":"eq","value":"C0123"}]}'`,
	}
	cmd.AddCommand(newCloudIngressSourceCmd())
	cmd.AddCommand(newCloudIngressConnectCmd())
	return cmd
}

func newCloudIngressSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Manage ingress sources",
	}
	cmd.AddCommand(newCloudIngressSourceCreateCmd())
	return cmd
}

func newCloudIngressSourceCreateCmd() *cobra.Command {
	var provider string
	var label string
	var secret string
	var secretStdin bool
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an ingress source and print its Request URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			secret, err := readSecretFlag(cmd, secret, secretStdin, "cloud ingress source create")
			if err != nil {
				return err
			}
			provider = strings.TrimSpace(provider)
			label = strings.TrimSpace(label)
			if provider == "" {
				return fmt.Errorf("cloud ingress source create: --provider is required")
			}
			if label == "" {
				return fmt.Errorf("cloud ingress source create: --label is required")
			}
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud ingress source create: %w", err)
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.CreateIngressSource(cmd.Context(), token, provider, label, secret)
			if err != nil {
				return fmt.Errorf("cloud ingress source create: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s\nRequest URL: %s\n", res.ID, res.RequestURL)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Ingress provider (e.g. slack)")
	cmd.Flags().StringVar(&label, "label", "", "Human label for this source")
	cmd.Flags().StringVar(&secret, "secret", "", "Signing secret (prefer --secret-stdin)")
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "Read signing secret from stdin")
	_ = cmd.MarkFlagRequired("provider")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}

func newCloudIngressConnectCmd() *cobra.Command {
	var label string
	var filter string
	cmd := &cobra.Command{
		Use:   "connect <src_id> <deployment>",
		Short: "Subscribe a deployment to an ingress source",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			label = strings.TrimSpace(label)
			if label == "" {
				return fmt.Errorf("cloud ingress connect: --label is required")
			}
			sourceID := strings.TrimSpace(args[0])
			if sourceID == "" || strings.ContainsAny(sourceID, "/\\\n\r") {
				return fmt.Errorf("cloud ingress connect: invalid source id")
			}
			var filterJSON json.RawMessage
			filter = strings.TrimSpace(filter)
			if filter != "" {
				if !json.Valid([]byte(filter)) {
					return fmt.Errorf("cloud ingress connect: --filter must be JSON")
				}
				filterJSON = json.RawMessage(filter)
			}
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud ingress connect: %w", err)
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			depID, err := resolveDeploymentRef(cmd, client, token, args[1])
			if err != nil {
				return err
			}
			res, err := client.CreateIngressConnection(cmd.Context(), token, sourceID, depID, label, filterJSON)
			if err != nil {
				return fmt.Errorf("cloud ingress connect: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Connected %s -> %s (%s, status=%s)\n", res.SourceID, res.DeploymentID, res.ID, res.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "Human label for this connection")
	cmd.Flags().StringVar(&filter, "filter", "", "JSON event filter (omit to match all admitted events)")
	_ = cmd.MarkFlagRequired("label")
	return cmd
}
