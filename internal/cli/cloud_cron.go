package cli

import (
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

var cronNamedExprs = map[string]struct{}{
	"every_1m":  {},
	"every_5m":  {},
	"every_15m": {},
	"every_1h":  {},
}

// validCloudCronExpr accepts named intervals or standard 5-field cron (UTC).
func validCloudCronExpr(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if _, ok := cronNamedExprs[expr]; ok {
		return true
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	// Lightweight shape check; cloud API is the source of truth for field ranges.
	for _, f := range fields {
		if f == "" {
			return false
		}
		for _, r := range f {
			switch {
			case r >= '0' && r <= '9':
			case r == '*' || r == '/' || r == '-' || r == ',':
			default:
				return false
			}
		}
	}
	return true
}

func newCloudCronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Manage cloud deployment cron schedules",
		Long: `Set, enable, disable, or list cloud deployment cron schedules.

Named intervals: every_1m, every_5m, every_15m, every_1h.
Or standard 5-field cron in UTC, e.g. "30 9 * * 1-5" (09:30 UTC Mon-Fri).
Dashboard Cron tab is read-only; use these CLI verbs (or Hermes) to change schedules.`,
	}
	cmd.AddCommand(newCloudCronSetCmd())
	cmd.AddCommand(newCloudCronDisableCmd())
	cmd.AddCommand(newCloudCronEnableCmd())
	cmd.AddCommand(newCloudCronListCmd())
	return cmd
}

func newCloudCronSetCmd() *cobra.Command {
	var expr string
	cmd := &cobra.Command{
		Use:   "set <deployment>",
		Short: "Create or change a deployment cron schedule (enables it)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			expr = strings.Join(strings.Fields(strings.TrimSpace(expr)), " ")
			if !validCloudCronExpr(expr) {
				return fmt.Errorf("cloud cron set: --expr must be every_1m|every_5m|every_15m|every_1h or a 5-field cron in UTC (e.g. \"30 9 * * 1-5\")")
			}
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			depID, err := resolveDeploymentRef(cmd, client, token, args[0])
			if err != nil {
				return err
			}
			res, err := client.SetCron(cmd.Context(), token, depID, expr, true)
			if err != nil {
				return fmt.Errorf("cloud cron set: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cron set on %s: %s (enabled=%v)\n", res.DeploymentID, res.Expr, res.Enabled)
			return nil
		},
	}
	cmd.Flags().StringVar(&expr, "expr", "", "Schedule: every_1m|every_5m|every_15m|every_1h or 5-field cron UTC (required)")
	_ = cmd.MarkFlagRequired("expr")
	return cmd
}

func newCloudCronDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <deployment>",
		Short: "Disable cron on a deployment (keeps the stored schedule)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			depID, err := resolveDeploymentRef(cmd, client, token, args[0])
			if err != nil {
				return err
			}
			res, err := client.SetCronEnabled(cmd.Context(), token, depID, false)
			if err != nil {
				return fmt.Errorf("cloud cron disable: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cron disabled on %s (expr=%s kept)\n", res.DeploymentID, res.Expr)
			return nil
		},
	}
}

func newCloudCronEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <deployment>",
		Short: "Re-enable a previously set cron schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			depID, err := resolveDeploymentRef(cmd, client, token, args[0])
			if err != nil {
				return err
			}
			res, err := client.SetCronEnabled(cmd.Context(), token, depID, true)
			if err != nil {
				return fmt.Errorf("cloud cron enable: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Cron enabled on %s: %s\n", res.DeploymentID, res.Expr)
			return nil
		},
	}
}

func newCloudCronListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cloud cron schedules for this tenant",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.ListCron(cmd.Context(), token)
			if err != nil {
				return fmt.Errorf("cloud cron list: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			if len(res.Schedules) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No cron schedules. Set one with: agentpaas cloud cron set <deployment> --expr every_5m")
				return nil
			}
			for _, s := range res.Schedules {
				name := s.DeploymentID
				if s.AgentName != nil && *s.AgentName != "" {
					name = *s.AgentName
					if s.AgentVersion != nil && *s.AgentVersion != "" {
						name = name + "@" + *s.AgentVersion
					}
				}
				state := "disabled"
				if s.Enabled {
					state = "enabled"
				}
				last := "-"
				if s.CronLastFiredAt != nil {
					last = *s.CronLastFiredAt
				}
				next := "-"
				if s.NextFireAt != nil {
					next = *s.NextFireAt
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s (%s)  last=%s  next=%s  %s\n",
					s.DeploymentID, name, s.Expr, state, last, next, s.ExprHuman)
			}
			return nil
		},
	}
}

// resolveDeploymentRef accepts deployment id or agent name.
func resolveDeploymentRef(cmd *cobra.Command, client *cloudclient.CloudClient, token, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("deployment id or agent name required")
	}
	if strings.HasPrefix(ref, "dep_") {
		return ref, nil
	}
	deps, err := client.ListDeployments(cmd.Context(), token)
	if err != nil {
		return "", fmt.Errorf("list deployments: %w", err)
	}
	images, err := client.ListImages(cmd.Context(), token)
	if err != nil {
		return "", fmt.Errorf("list images: %w", err)
	}
	digToName := map[string]string{}
	for _, img := range images {
		if img.AgentName != "" {
			digToName[img.ImageDigest] = img.AgentName
		}
	}
	var matches []cloudclient.DeploymentRecord
	for _, d := range deps {
		if d.ID == ref {
			return d.ID, nil
		}
		name := digToName[d.ImageDigest]
		if name == ref {
			matches = append(matches, d)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no deployment matching %q", ref)
	}
	if len(matches) > 1 {
		var ids []string
		for _, m := range matches {
			ids = append(ids, m.ID)
		}
		return "", fmt.Errorf("ambiguous agent name %q matches multiple deployments: %s", ref, strings.Join(ids, ", "))
	}
	return matches[0].ID, nil
}
