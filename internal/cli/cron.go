package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/install"
	"github.com/spf13/cobra"
)

// newCronCmd creates the `agent cron` command.
func newCronCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cron",
		Short: "Manage cron schedules for agent invocations",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add <agent-name>",
		Short: "Add a cron schedule for an agent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			expr, _ := cmd.Flags().GetString("expr") // cobra flag default on missing
			if expr == "" {
				return fmt.Errorf("required flag --expr is missing")
			}
			version, _ := cmd.Flags().GetString("version") // cobra flag default on missing
		payload, _ := cmd.Flags().GetString("payload") // cobra flag default on missing
		contentType, _ := cmd.Flags().GetString("content-type") // cobra flag default on missing
			timezone, _ := cmd.Flags().GetString("timezone") // cobra flag default on missing

			sock, err := socketPath(cmd)
			if err != nil {
				return err
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(30 * time.Second)
			defer cancel()

			resolved, err := resolveCLIAgentRef(cmd, args[0])
			if err != nil {
				return err
			}

			resp, err := client.CronAdd(ctx, &controlv1.CronAddRequest{
				AgentName:    resolved.DaemonKey,
				Expr:         expr,
				AgentVersion: version,
				Timezone:     timezone,
				Payload:       []byte(payload),
				ContentType:   contentType,
			})
			if err != nil {
				return fmt.Errorf("cron add failed: %w", err)
			}

			scheduleID := ""
			if resp.GetSchedule() != nil {
				scheduleID = resp.GetSchedule().GetScheduleId()
			}

			result := struct {
				ScheduleID string `json:"schedule_id"`
				AgentName  string `json:"agent_name"`
				Expr       string `json:"expr"`
				Added      bool   `json:"added"`
			}{
				ScheduleID: scheduleID,
				AgentName:  resolved.Display,
				Expr:       expr,
				Added:      true,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					ScheduleID string `json:"schedule_id"`
					AgentName  string `json:"agent_name"`
					Expr       string `json:"expr"`
					Added      bool   `json:"added"`
				})
				return fmt.Sprintf("Cron schedule added: schedule_id=%s agent=%s expr=%s", r.ScheduleID, r.AgentName, r.Expr)
			})
		},
	})

	addCmd := cmd.Commands()[0]
	addCmd.Flags().String("expr", "", "Cron expression (e.g. \"*/5 * * * *\")")
	addCmd.Flags().String("version", "", "Agent version (optional)")
	addCmd.Flags().String("timezone", "", "Timezone (optional)")
	addCmd.Flags().String("payload", "", "Invocation payload (inline JSON or file path)")
	addCmd.Flags().String("content-type", "", "Payload content type (default: application/json)")

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all cron schedules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := socketPath(cmd)
			if err != nil {
				return err
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(30 * time.Second)
			defer cancel()

			resp, err := client.CronList(ctx, &controlv1.CronListRequest{})
			if err != nil {
				return fmt.Errorf("cron list failed: %w", err)
			}

			schedules := resp.GetSchedules()
			stateRoot := ""
			if homeDir, herr := getAgentpaasHome(cmd); herr == nil {
				stateRoot = filepath.Join(homeDir, "state")
			}
			formatAgent := func(name string) string {
				if stateRoot == "" {
					return name
				}
				return install.DisplayForDaemonKey(stateRoot, name)
			}

			jsonOut := jsonOutput(cmd)
			if jsonOut {
				type scheduleJSON struct {
					ScheduleID   string `json:"schedule_id"`
					Expr         string `json:"expr"`
					AgentName    string `json:"agent_name"`
					AgentVersion string `json:"agent_version,omitempty"`
					Timezone     string `json:"timezone,omitempty"`
				}
				items := make([]scheduleJSON, 0, len(schedules))
				for _, s := range schedules {
					items = append(items, scheduleJSON{
						ScheduleID:   s.GetScheduleId(),
						Expr:         s.GetExpr(),
						AgentName:    formatAgent(s.GetAgentName()),
						AgentVersion: s.GetAgentVersion(),
						Timezone:     s.GetTimezone(),
					})
				}
				return printTextOrJSON(true, items, nil)
			}

			// Text output with tabwriter.
			var b strings.Builder
			tw := tabwriter.NewWriter(&b, 0, 0, 3, ' ', 0)
			if _, err := fmt.Fprintln(tw, "SCHEDULE_ID\tEXPR\tAGENT_NAME"); err != nil {
				return err
			}
			for _, s := range schedules {
				if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\n",
					s.GetScheduleId(),
					s.GetExpr(),
					formatAgent(s.GetAgentName()),
				); err != nil {
					return err
				}
			}
			if err := tw.Flush(); err != nil {
				return err
			}
			return printTextOrJSON(false, nil, func(v interface{}) string {
				return strings.TrimSuffix(b.String(), "\n")
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <schedule-id>",
		Short: "Remove a cron schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sock, err := socketPath(cmd)
			if err != nil {
				return err
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(30 * time.Second)
			defer cancel()

			resp, err := client.CronRemove(ctx, &controlv1.CronRemoveRequest{
				ScheduleId: args[0],
			})
			if err != nil {
				return fmt.Errorf("cron remove failed: %w", err)
			}

			result := struct {
				ScheduleID string `json:"schedule_id"`
				Removed    bool   `json:"removed"`
			}{
				ScheduleID: args[0],
				Removed:    resp.GetRemoved(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					ScheduleID string `json:"schedule_id"`
					Removed    bool   `json:"removed"`
				})
				return fmt.Sprintf("Cron schedule removed: schedule_id=%s", r.ScheduleID)
			})
		},
	})

	return cmd
}
