package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

func newCloudEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events <run_id>",
		Short: "Show events for a cloud run",
		Long: `Fetch the lifecycle and audit events recorded for one cloud run.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, token, err := cloudAuditClient(cmd, "cloud events")
			if err != nil {
				return err
			}
			result, err := client.GetRunEvents(cmd.Context(), token, args[0])
			if err != nil {
				return cloudAuditError(cmd, "cloud events", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Run: %s\n", result.RunID)
			return printCloudAuditEvents(cmd.OutOrStdout(), "Events", result.Events)
		},
	}
}

func newCloudAuditCmd() *cobra.Command {
	var since, until string
	var limit int

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Query the cloud audit log",
		Long: `Query tenant-scoped cloud audit events. Filters are optional; the
cloud API applies its own retention and default limit when omitted.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, token, err := cloudAuditClient(cmd, "cloud audit")
			if err != nil {
				return err
			}
			result, err := client.GetAudit(cmd.Context(), token, since, until, limit)
			if err != nil {
				return cloudAuditError(cmd, "cloud audit", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}
			return printCloudAuditEvents(cmd.OutOrStdout(), "Audit events", result.Events)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "Include events at or after this timestamp")
	cmd.Flags().StringVar(&until, "until", "", "Include events before or at this timestamp")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of events to return")
	cmd.AddCommand(newCloudAuditExportCmd())
	return cmd
}

func newCloudAuditExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export <run_id>",
		Short: "Export the audit log for a cloud run",
		Long: `Fetch the cloud audit export for one run. The export is returned as
JSON/JSONL by the cloud API and is never written to a local file.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, token, err := cloudAuditClient(cmd, "cloud audit export")
			if err != nil {
				return err
			}
			result, err := client.GetRunAuditExport(cmd.Context(), token, args[0])
			if err != nil {
				return cloudAuditError(cmd, "cloud audit export", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}
			return printCloudAuditExport(cmd.OutOrStdout(), result)
		},
	}
}

func newCloudMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "metrics",
		Short: "Show cloud audit and run metrics",
		Long: `Fetch aggregate run and audit metrics from AgentPaaS Cloud.

Requires a valid login. Use 'agentpaas cloud login' first.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, token, err := cloudAuditClient(cmd, "cloud metrics")
			if err != nil {
				return err
			}
			result, err := client.GetMetrics(cmd.Context(), token)
			if err != nil {
				return cloudAuditError(cmd, "cloud metrics", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}
			return printCloudMetrics(cmd.OutOrStdout(), result)
		},
	}
}

func cloudAuditClient(cmd *cobra.Command, operation string) (*cloudclient.CloudClient, string, error) {
	token, err := resolveToken(cmd)
	if err != nil {
		if strings.Contains(err.Error(), "not logged in") {
			return nil, "", printNotLoggedIn(cmd)
		}
		return nil, "", fmt.Errorf("%s: %w", operation, err)
	}
	return cloudclient.NewCloudClient(resolveAPIURL()), token, nil
}

func cloudAuditError(cmd *cobra.Command, operation string, err error) error {
	if strings.Contains(err.Error(), "not authenticated") {
		return printNotLoggedIn(cmd)
	}
	return fmt.Errorf("%s: %w", operation, err)
}

func printCloudAuditEvents(out io.Writer, heading string, events []cloudclient.CloudAuditEvent) error {
	_, _ = fmt.Fprintf(out, "%s (%d):\n", heading, len(events))
	if len(events) == 0 {
		_, _ = fmt.Fprintln(out, "  (none)")
		return nil
	}
	for _, event := range events {
		timestamp := event.Timestamp
		if timestamp == "" {
			timestamp = event.CreatedAt
		}
		kind := event.EventType
		if kind == "" {
			kind = event.Type
		}
		if kind == "" {
			kind = event.Category
		}
		_, _ = fmt.Fprintf(out, "  %s %s", timestamp, kind)
		if event.Actor != "" {
			_, _ = fmt.Fprintf(out, " actor=%s", event.Actor)
		}
		if event.Message != "" {
			_, _ = fmt.Fprintf(out, " — %s", event.Message)
		} else if event.Detail != "" {
			_, _ = fmt.Fprintf(out, " — %s", event.Detail)
		}
		if event.Payload != "" {
			_, _ = fmt.Fprintf(out, " payload=%s", event.Payload)
		}
		_, _ = fmt.Fprintln(out)
	}
	return nil
}

func printCloudAuditExport(out io.Writer, export *cloudclient.AuditExportResponse) error {
	_, _ = fmt.Fprintf(out, "Audit export for run %s:\n", export.RunID)
	if len(export.Raw) == 0 {
		return nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, export.Raw, "", "  "); err == nil {
		_, err = fmt.Fprintln(out, pretty.String())
		return err
	}
	_, err := fmt.Fprintln(out, string(export.Raw))
	return err
}

func printCloudMetrics(out io.Writer, metrics *cloudclient.MetricsResponse) error {
	_, _ = fmt.Fprintf(out, "Runs total: %d\n", metrics.RunsTotal)
	_, _ = fmt.Fprintf(out, "Runs active: %d\n", metrics.RunsActive)
	_, _ = fmt.Fprintf(out, "Runs succeeded: %d\n", metrics.RunsSucceeded)
	_, _ = fmt.Fprintf(out, "Runs failed: %d\n", metrics.RunsFailed)
	_, _ = fmt.Fprintf(out, "Events total: %d\n", metrics.EventsTotal)
	_, _ = fmt.Fprintf(out, "Audit records total: %d\n", metrics.AuditRecordsTotal)
	_, _ = fmt.Fprintf(out, "Latency p50: %g ms\n", metrics.LatencyMSP50)
	_, _ = fmt.Fprintf(out, "Latency p95: %g ms\n", metrics.LatencyMSP95)
	_, _ = fmt.Fprintf(out, "Latency p99: %g ms\n", metrics.LatencyMSP99)
	return nil
}
