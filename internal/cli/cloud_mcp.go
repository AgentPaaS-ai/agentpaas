package cli

import (
	"encoding/json"
	"fmt"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

func newCloudMcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Call MCP tools on a cloud deployment",
		Long: `Call MCP tools on a cloud deployment using a deployment invoke token.

Use --token or AGENTPAAS_CLOUD_INVOKE_TOKEN for the invoke token. This
command never uses the tenant cloud login token for the MCP request.`,
	}
	cmd.AddCommand(newCloudMcpCallCmd())
	return cmd
}

func newCloudMcpCallCmd() *cobra.Command {
	var argsJSON string
	var token string

	cmd := &cobra.Command{
		Use:   "call <deployment_id> <tool>",
		Short: "Call an MCP tool on a cloud deployment",
		Long: `Call an MCP tool on a deployment with a deployment invoke token.

Use --token or AGENTPAAS_CLOUD_INVOKE_TOKEN for the invoke token. This
command never uses the tenant cloud login token for the MCP request.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			deploymentID := args[0]
			tool := args[1]

			invokeToken, err := resolveCloudInvokeToken(cmd, token, deploymentID)
			if err != nil {
				return err
			}

			if argsJSON == "" {
				argsJSON = "{}"
			}
			if !json.Valid([]byte(argsJSON)) {
				return fmt.Errorf("cloud mcp call: invalid JSON --args")
			}

			rpc := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"method":  "tools/call",
				"params": map[string]any{
					"name":      tool,
					"arguments": json.RawMessage(argsJSON),
				},
			}
			body, err := json.Marshal(rpc)
			if err != nil {
				return fmt.Errorf("cloud mcp call: marshal request: %w", err)
			}

			client := cloudclient.NewCloudClient(resolveAPIURL())
			resp, err := client.McpCall(cmd.Context(), invokeToken, deploymentID, body)
			if err != nil {
				return fmt.Errorf("cloud mcp call: %w", err)
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(resp))
			return nil
		},
	}

	cmd.Flags().StringVar(&argsJSON, "args", "{}", "JSON tool arguments")
	cmd.Flags().StringVar(&token, "token", "", "Deployment invoke token")
	return cmd
}
