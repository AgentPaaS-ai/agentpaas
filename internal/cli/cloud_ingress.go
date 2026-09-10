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

Signing secrets and reply credentials are never printed. Use --secret-stdin.`,
		Example: `  # Create a Slack source (secret is not printed)
  printf '%s' "$SLACK_SIGNING_SECRET" | agentpaas cloud ingress source create --provider slack --label "vb-editorial" --secret-stdin

  # Subscribe a deployment; empty --filter matches all admitted events
  agentpaas cloud ingress connect src_01J... dep_x --label "support triage" --filter '{"match":"all","rules":[{"field":"event.channel","op":"eq","value":"C0123"}]}'

  # Inspect / lifecycle
  agentpaas cloud ingress sources
  agentpaas cloud ingress connections src_01J...
  agentpaas cloud ingress source disable src_01J...
  agentpaas cloud ingress connection disable con_01J...
  printf '%s' "$SLACK_SIGNING_SECRET" | agentpaas cloud ingress source rotate src_01J... --secret-stdin
  printf '%s' "$SLACK_BOT_TOKEN" | agentpaas cloud ingress source bind-reply src_01J... --credential slack-bot-token --secret-stdin
  agentpaas cloud ingress events src_01J... --tail 20
  agentpaas cloud ingress test-filter src_01J... --filter '{"match":"all","rules":[]}' --event '{}'`,
	}
	cmd.AddCommand(newCloudIngressSourceCmd())
	cmd.AddCommand(newCloudIngressConnectionCmd())
	cmd.AddCommand(newCloudIngressConnectCmd())
	cmd.AddCommand(newCloudIngressSourcesCmd())
	cmd.AddCommand(newCloudIngressConnectionsCmd())
	cmd.AddCommand(newCloudIngressEventsCmd())
	cmd.AddCommand(newCloudIngressTestFilterCmd())
	return cmd
}

func requireIngressID(verb, kind, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/\\\n\r") {
		return "", fmt.Errorf("%s: invalid %s id", verb, kind)
	}
	return id, nil
}

func resolveIngressToken(cmd *cobra.Command, verb string) (string, error) {
	token, err := resolveToken(cmd)
	if err != nil {
		if strings.Contains(err.Error(), "not logged in") {
			return "", printNotLoggedIn(cmd)
		}
		return "", fmt.Errorf("%s: %w", verb, err)
	}
	return token, nil
}

func newCloudIngressSourceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "source",
		Short: "Manage ingress sources",
	}
	cmd.AddCommand(newCloudIngressSourceCreateCmd())
	cmd.AddCommand(newCloudIngressSourceDisableCmd())
	cmd.AddCommand(newCloudIngressSourceRotateCmd())
	cmd.AddCommand(newCloudIngressSourceBindReplyCmd())
	return cmd
}

func newCloudIngressConnectionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connection",
		Short: "Manage ingress connections",
	}
	cmd.AddCommand(newCloudIngressConnectionDisableCmd())
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
			token, err := resolveIngressToken(cmd, "cloud ingress source create")
			if err != nil {
				return err
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
			sourceID, err := requireIngressID("cloud ingress connect", "source", args[0])
			if err != nil {
				return err
			}
			var filterJSON json.RawMessage
			filter = strings.TrimSpace(filter)
			if filter != "" {
				if !json.Valid([]byte(filter)) {
					return fmt.Errorf("cloud ingress connect: --filter must be JSON")
				}
				filterJSON = json.RawMessage(filter)
			}
			token, err := resolveIngressToken(cmd, "cloud ingress connect")
			if err != nil {
				return err
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

func newCloudIngressSourcesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sources",
		Short: "List ingress sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveIngressToken(cmd, "cloud ingress sources")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.ListIngressSources(cmd.Context(), token)
			if err != nil {
				return fmt.Errorf("cloud ingress sources: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			if len(res) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No ingress sources.")
				return nil
			}
			for _, s := range res {
				last := "-"
				if s.LastEventAt != nil && *s.LastEventAt != "" {
					last = *s.LastEventAt
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  status=%s  last_event_at=%s  connections=%d\n",
					s.ID, s.Provider, s.Label, s.Status, last, s.ConnectionCount)
			}
			return nil
		},
	}
}

func newCloudIngressConnectionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "connections <src_id>",
		Short: "List connections on an ingress source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceID, err := requireIngressID("cloud ingress connections", "source", args[0])
			if err != nil {
				return err
			}
			token, err := resolveIngressToken(cmd, "cloud ingress connections")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.GetIngressSource(cmd.Context(), token, sourceID)
			if err != nil {
				return fmt.Errorf("cloud ingress connections: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res.Connections, nil)
			}
			if len(res.Connections) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No connections.")
				return nil
			}
			for _, c := range res.Connections {
				filter := "-"
				if len(c.FilterJSON) > 0 && string(c.FilterJSON) != "null" {
					filter = string(c.FilterJSON)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  status=%s  filter=%s\n",
					c.ID, c.DeploymentID, c.Label, c.Status, filter)
			}
			return nil
		},
	}
}

func newCloudIngressSourceDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <src_id>",
		Short: "Pause one ingress source without touching connections",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceID, err := requireIngressID("cloud ingress source disable", "source", args[0])
			if err != nil {
				return err
			}
			token, err := resolveIngressToken(cmd, "cloud ingress source disable")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.DisableIngressSource(cmd.Context(), token, sourceID)
			if err != nil {
				return fmt.Errorf("cloud ingress source disable: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Disabled source %s (status=%s)\n", res.ID, res.Status)
			return nil
		},
	}
}

func newCloudIngressConnectionDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <con_id>",
		Short: "Pause one ingress connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conID, err := requireIngressID("cloud ingress connection disable", "connection", args[0])
			if err != nil {
				return err
			}
			token, err := resolveIngressToken(cmd, "cloud ingress connection disable")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.DisableIngressConnection(cmd.Context(), token, conID)
			if err != nil {
				return fmt.Errorf("cloud ingress connection disable: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Disabled connection %s (status=%s)\n", res.ID, res.Status)
			return nil
		},
	}
}

func newCloudIngressSourceRotateCmd() *cobra.Command {
	var secret string
	var secretStdin bool
	cmd := &cobra.Command{
		Use:   "rotate <src_id>",
		Short: "Rotate the signing secret for an ingress source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceID, err := requireIngressID("cloud ingress source rotate", "source", args[0])
			if err != nil {
				return err
			}
			secret, err := readSecretFlag(cmd, secret, secretStdin, "cloud ingress source rotate")
			if err != nil {
				return err
			}
			token, err := resolveIngressToken(cmd, "cloud ingress source rotate")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.RotateIngressSource(cmd.Context(), token, sourceID, secret)
			if err != nil {
				return fmt.Errorf("cloud ingress source rotate: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Rotated secret for %s\n", res.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&secret, "secret", "", "New signing secret (prefer --secret-stdin)")
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "Read signing secret from stdin")
	return cmd
}

func newCloudIngressSourceBindReplyCmd() *cobra.Command {
	var credential string
	var secret string
	var secretStdin bool
	cmd := &cobra.Command{
		Use:   "bind-reply <src_id>",
		Short: "Bind a reply credential (e.g. Slack bot token) to a source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceID, err := requireIngressID("cloud ingress source bind-reply", "source", args[0])
			if err != nil {
				return err
			}
			credential = strings.TrimSpace(credential)
			if credential == "" {
				return fmt.Errorf("cloud ingress source bind-reply: --credential is required")
			}
			secret, err := readSecretFlag(cmd, secret, secretStdin, "cloud ingress source bind-reply")
			if err != nil {
				return err
			}
			token, err := resolveIngressToken(cmd, "cloud ingress source bind-reply")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.BindIngressSourceReply(cmd.Context(), token, sourceID, credential, secret)
			if err != nil {
				return fmt.Errorf("cloud ingress source bind-reply: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Bound %s on %s\n", res.Credential, sourceID)
			return nil
		},
	}
	cmd.Flags().StringVar(&credential, "credential", "", "Reply credential name (slack-bot-token)")
	cmd.Flags().StringVar(&secret, "secret", "", "Credential secret (prefer --secret-stdin)")
	cmd.Flags().BoolVar(&secretStdin, "secret-stdin", false, "Read credential secret from stdin")
	_ = cmd.MarkFlagRequired("credential")
	return cmd
}

func newCloudIngressEventsCmd() *cobra.Command {
	var tail int
	cmd := &cobra.Command{
		Use:   "events <src_id>",
		Short: "Show recent events for an ingress source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceID, err := requireIngressID("cloud ingress events", "source", args[0])
			if err != nil {
				return err
			}
			if tail < 1 {
				return fmt.Errorf("cloud ingress events: --tail must be a positive integer")
			}
			token, err := resolveIngressToken(cmd, "cloud ingress events")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.ListIngressSourceEvents(cmd.Context(), token, sourceID, tail)
			if err != nil {
				return fmt.Errorf("cloud ingress events: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			if len(res) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No events.")
				return nil
			}
			for _, e := range res {
				eventID := "-"
				if e.ProviderEventID != nil && *e.ProviderEventID != "" {
					eventID = *e.ProviderEventID
				}
				matched := "-"
				if len(e.MatchedConnectionIDs) > 0 {
					matched = strings.Join(e.MatchedConnectionIDs, ",")
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  status=%s  matched=%s  %s\n",
					e.ID, e.Provider, eventID, e.Status, matched, e.CreatedAt)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&tail, "tail", 20, "Number of recent events (max 100)")
	return cmd
}

func newCloudIngressTestFilterCmd() *cobra.Command {
	var filter string
	var event string
	cmd := &cobra.Command{
		Use:   "test-filter <src_id>",
		Short: "Dry-run the event filter matcher",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := requireIngressID("cloud ingress test-filter", "source", args[0]); err != nil {
				return err
			}
			filter = strings.TrimSpace(filter)
			event = strings.TrimSpace(event)
			if filter == "" || !json.Valid([]byte(filter)) {
				return fmt.Errorf("cloud ingress test-filter: --filter must be JSON")
			}
			if event == "" || !json.Valid([]byte(event)) {
				return fmt.Errorf("cloud ingress test-filter: --event must be JSON")
			}
			token, err := resolveIngressToken(cmd, "cloud ingress test-filter")
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.TestIngressFilter(cmd.Context(), token, json.RawMessage(filter), json.RawMessage(event))
			if err != nil {
				return fmt.Errorf("cloud ingress test-filter: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "matched=%v\n", res.Matched)
			return nil
		},
	}
	cmd.Flags().StringVar(&filter, "filter", "", "JSON filter to evaluate")
	cmd.Flags().StringVar(&event, "event", "", "JSON event to test")
	_ = cmd.MarkFlagRequired("filter")
	_ = cmd.MarkFlagRequired("event")
	return cmd
}
