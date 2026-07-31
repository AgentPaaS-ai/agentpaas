package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

// newCloudResultCmd creates the `agentpaas cloud result` command.
func newCloudResultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "result <run_id>",
		Short: "Show the result package for a cloud run",
		Long: `Show the final output, failure details, and signed artifacts for a
	completed cloud run.

Requires a valid cloud login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud result: %w", err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			result, err := client.GetRunResult(cmd.Context(), token, args[0])
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud result: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, result, nil)
			}
			return printCloudRunResult(cmd, result)
		},
	}
}

func printCloudRunResult(cmd *cobra.Command, result *cloudclient.RunResult) error {
	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "Run: %s\n", result.RunID)
	_, _ = fmt.Fprintf(out, "Status: %s\n", result.Status)
	if result.Error != nil && *result.Error != "" {
		_, _ = fmt.Fprintf(out, "Error: %s\n", *result.Error)
	}
	if result.FinishedAt != nil && *result.FinishedAt != "" {
		_, _ = fmt.Fprintf(out, "Finished: %s\n", *result.FinishedAt)
	}
	if output := bytes.TrimSpace(result.FinalOutput); len(output) > 0 && !bytes.Equal(output, []byte("null")) {
		_, _ = fmt.Fprintln(out, "Final output:")
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, output, "", "  "); err == nil {
			_, _ = fmt.Fprintln(out, pretty.String())
		} else {
			_, _ = fmt.Fprintln(out, string(output))
		}
	}
	if len(result.Artifacts) > 0 {
		_, _ = fmt.Fprintln(out, "Artifacts:")
		for _, artifact := range result.Artifacts {
			_, _ = fmt.Fprintf(out, "- %s (%d bytes, expires in %d seconds): %s\n", artifact.Name, artifact.SizeBytes, artifact.ExpiresInSec, artifact.URL)
		}
	}
	return nil
}

// newCloudLogsCmd creates the `agentpaas cloud logs` command.
func newCloudLogsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logs <run_id>",
		Short: "Print logs from a cloud run",
		Long: `Fetch and print the logs.txt artifact from a cloud run result package.

Requires a valid cloud login. Use 'agentpaas cloud login' first.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				if strings.Contains(err.Error(), "not logged in") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud logs: %w", err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			logs, err := client.GetRunLogs(cmd.Context(), token, args[0])
			if err != nil {
				if strings.Contains(err.Error(), "not authenticated") {
					return printNotLoggedIn(cmd)
				}
				return fmt.Errorf("cloud logs: %w", err)
			}

			if jsonOutput(cmd) {
				return printTextOrJSON(true, map[string]string{
					"run_id": args[0],
					"logs":   string(logs),
				}, nil)
			}
			if _, err := cmd.OutOrStdout().Write(logs); err != nil {
				return fmt.Errorf("cloud logs: write output: %w", err)
			}
			return nil
		},
	}
}
