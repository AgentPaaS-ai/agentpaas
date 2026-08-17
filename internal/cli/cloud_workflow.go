package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

func newCloudWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage cloud workflows",
		Long: `Create, list, get, and start AgentPaaS Cloud workflows.

Use 'agentpaas cloud workflow instance' to inspect a started instance.`,
	}
	cmd.AddCommand(newCloudWorkflowCreateCmd())
	cmd.AddCommand(newCloudWorkflowListCmd())
	cmd.AddCommand(newCloudWorkflowGetCmd())
	cmd.AddCommand(newCloudWorkflowStartCmd())
	cmd.AddCommand(newCloudWorkflowInstanceCmd())
	cmd.AddCommand(newCloudWorkflowLiveCallCmd())
	cmd.AddCommand(newCloudWorkflowHangupCmd())
	return cmd
}

func newCloudWorkflowCreateCmd() *cobra.Command {
	var name string
	var envelopePath string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a cloud workflow from an envelope file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("cloud workflow create: --name is required")
			}
			envelope, err := readWorkflowJSONObjectFile(envelopePath, "envelope")
			if err != nil {
				return fmt.Errorf("cloud workflow create: %w", err)
			}

			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			rec, err := client.CreateWorkflow(cmd.Context(), token, cloudclient.CreateWorkflowRequest{
				Name:     name,
				Envelope: envelope,
			})
			if err != nil {
				return fmt.Errorf("cloud workflow create: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, rec, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", rec.ID, rec.Name, rec.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Workflow name (required)")
	cmd.Flags().StringVar(&envelopePath, "envelope", "", "Path to envelope JSON object file (required)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("envelope")
	return cmd
}

func newCloudWorkflowListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List cloud workflows",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.ListWorkflows(cmd.Context(), token)
			if err != nil {
				return fmt.Errorf("cloud workflow list: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			if len(res.Workflows) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No workflows.")
				return nil
			}
			for _, wf := range res.Workflows {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s\n", wf.ID, wf.Name, wf.Status)
			}
			return nil
		},
	}
}

func newCloudWorkflowGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: "Get a cloud workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			rec, err := client.GetWorkflow(cmd.Context(), token, args[0])
			if err != nil {
				return fmt.Errorf("cloud workflow get: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, rec, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %d\n", rec.ID, rec.Name, rec.Status, rec.Version)
			return nil
		},
	}
}

func newCloudWorkflowStartCmd() *cobra.Command {
	var handoffPath string
	cmd := &cobra.Command{
		Use:   "start <id>",
		Short: "Start a workflow instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var req cloudclient.StartWorkflowRequest
			if strings.TrimSpace(handoffPath) != "" {
				handoff, err := readWorkflowJSONObjectFile(handoffPath, "handoff-file")
				if err != nil {
					return fmt.Errorf("cloud workflow start: %w", err)
				}
				req.InitialHandoff = handoff
			}

			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			inst, err := client.StartWorkflowInstance(cmd.Context(), token, args[0], req)
			if err != nil {
				return fmt.Errorf("cloud workflow start: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, inst, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", inst.ID, inst.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&handoffPath, "handoff-file", "", "Optional JSON object file sent as initial_handoff")
	return cmd
}

func newCloudWorkflowInstanceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "instance <id>",
		Short: "Get a workflow instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			inst, err := client.GetWorkflowInstance(cmd.Context(), token, args[0])
			if err != nil {
				return fmt.Errorf("cloud workflow instance: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, inst, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %d\n", inst.ID, inst.WorkflowID, inst.Status, inst.CurrentStageIndex)
			printWorkflowInstanceStageCommits(cmd.OutOrStdout(), inst.StageCommits)
			return nil
		},
	}
}

func newCloudWorkflowLiveCallCmd() *cobra.Command {
	var callee string
	var workOrderJSON string
	var idempotencyKey string
	cmd := &cobra.Command{
		Use:   "live-call <instance_id>",
		Short: "Start a live-call child run from a parent workflow instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			callee = strings.TrimSpace(callee)
			if callee == "" {
				return fmt.Errorf("cloud workflow live-call: --callee is required")
			}
			if strings.ContainsAny(callee, "\n\r\x00") {
				return fmt.Errorf("cloud workflow live-call: --callee contains invalid characters")
			}
			idempotencyKey = strings.TrimSpace(idempotencyKey)
			if idempotencyKey == "" {
				return fmt.Errorf("cloud workflow live-call: --idempotency-key is required")
			}
			if strings.ContainsAny(idempotencyKey, "\n\r\x00") {
				return fmt.Errorf("cloud workflow live-call: --idempotency-key contains invalid characters")
			}
			workOrder, err := parseWorkflowJSONObjectFlag(workOrderJSON, "work-order-json")
			if err != nil {
				return fmt.Errorf("cloud workflow live-call: %w", err)
			}

			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.LiveCall(cmd.Context(), token, args[0], cloudclient.LiveCallRequest{
				NamedCallee:    callee,
				WorkOrder:      workOrder,
				IdempotencyKey: idempotencyKey,
			})
			if err != nil {
				return fmt.Errorf("cloud workflow live-call: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %v\n", res.ChildID, res.ParentInstanceID, res.Reused)
			return nil
		},
	}
	cmd.Flags().StringVar(&callee, "callee", "", "Named callee deployment id (required)")
	cmd.Flags().StringVar(&workOrderJSON, "work-order-json", "", "Work-order JSON object (required)")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "Idempotency key (required)")
	_ = cmd.MarkFlagRequired("callee")
	_ = cmd.MarkFlagRequired("work-order-json")
	_ = cmd.MarkFlagRequired("idempotency-key")
	return cmd
}

func newCloudWorkflowHangupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hangup <instance_id>",
		Short: "Hang up live-call children of a workflow instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := resolveToken(cmd)
			if err != nil {
				return err
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())
			res, err := client.Hangup(cmd.Context(), token, args[0])
			if err != nil {
				return fmt.Errorf("cloud workflow hangup: %w", err)
			}
			if jsonOutput(cmd) {
				return printTextOrJSON(true, res, nil)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%d\n", res.Cancelled)
			return nil
		},
	}
}

func parseWorkflowJSONObjectFlag(raw, flagName string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("--%s is required", flagName)
	}
	if strings.ContainsRune(trimmed, '\x00') {
		return nil, fmt.Errorf("--%s contains a null byte", flagName)
	}
	if !json.Valid([]byte(trimmed)) || trimmed[0] != '{' {
		return nil, fmt.Errorf("--%s must be a JSON object", flagName)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return nil, fmt.Errorf("--%s must be a JSON object: %w", flagName, err)
	}
	return json.RawMessage(trimmed), nil
}

func printWorkflowInstanceStageCommits(out io.Writer, raw json.RawMessage) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return
	}
	var commits []json.RawMessage
	if err := json.Unmarshal(trimmed, &commits); err != nil || len(commits) == 0 {
		return
	}
	for _, commit := range commits {
		var rec struct {
			StageIndex     int             `json:"stage_index"`
			TerminalStatus string          `json:"terminal_status"`
			Summary        string          `json:"summary"`
			Query          string          `json:"query"`
			Text           string          `json:"text"`
			Handoff        json.RawMessage `json:"handoff"`
		}
		if err := json.Unmarshal(commit, &rec); err != nil {
			continue
		}
		preview := firstNonEmptyHandoffPreview(rec.Summary, rec.Query, rec.Text)
		if preview == "" {
			preview = handoffPreviewFromJSON(rec.Handoff)
		}
		preview = truncateHandoffPreview(preview, 80)
		if preview == "" {
			_, _ = fmt.Fprintf(out, "%d  %s\n", rec.StageIndex, rec.TerminalStatus)
			continue
		}
		_, _ = fmt.Fprintf(out, "%d  %s  %s\n", rec.StageIndex, rec.TerminalStatus, preview)
	}
}

func firstNonEmptyHandoffPreview(values ...string) string {
	for _, v := range values {
		if s := sanitizeHandoffPreview(v); s != "" {
			return s
		}
	}
	return ""
}

func handoffPreviewFromJSON(raw json.RawMessage) string {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return ""
	}
	return firstNonEmptyHandoffPreview(
		stringFromAny(obj["summary"]),
		stringFromAny(obj["query"]),
		stringFromAny(obj["text"]),
	)
}

func stringFromAny(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func sanitizeHandoffPreview(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func truncateHandoffPreview(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

func readWorkflowJSONObjectFile(path, flagName string) (json.RawMessage, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("--%s is required", flagName)
	}
	data, err := readWorkflowJSONFile(path, flagName)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("--%s file is empty", flagName)
	}
	if !json.Valid(trimmed) || trimmed[0] != '{' {
		return nil, fmt.Errorf("--%s must be a JSON object", flagName)
	}
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return nil, fmt.Errorf("--%s must be a JSON object: %w", flagName, err)
	}
	return json.RawMessage(trimmed), nil
}

func readWorkflowJSONFile(path, flagName string) ([]byte, error) {
	if strings.ContainsRune(path, '\x00') {
		return nil, fmt.Errorf("--%s path contains a null byte", flagName)
	}
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if component == ".." {
			return nil, fmt.Errorf("--%s path must not contain '..'", flagName)
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve --%s path: %w", flagName, err)
	}
	for current := absPath; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("--%s file not found: %s", flagName, path)
			}
			return nil, fmt.Errorf("inspect --%s path: %w", flagName, err)
		}
		if info.Mode()&os.ModeSymlink != 0 && !isCloudWorkflowSystemSymlink(current) {
			return nil, fmt.Errorf("--%s path contains a symlink: %s", flagName, current)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("--%s file not found: %s", flagName, path)
		}
		return nil, fmt.Errorf("read --%s: %w", flagName, err)
	}
	return data, nil
}

func isCloudWorkflowSystemSymlink(path string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return path == "/var" || path == "/tmp"
}
