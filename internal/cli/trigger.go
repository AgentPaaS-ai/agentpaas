package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// triggerInvokeResponse is the JSON response from POST /v1/trigger/invoke.
type triggerInvokeResponse struct {
	Run struct {
		RunID     string `json:"runId"`
		AgentName string `json:"agentName"`
		Status    string `json:"status"`
	} `json:"run"`
}

// newTriggerCmd creates the `agent trigger` command.
func newTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trigger",
		Short: "Manage agent triggers and invocations",
		Long: `Invoke agents through the trigger REST API (HTTP).

Unlike 'agentpaas run' (gRPC to the control daemon), trigger uses
POST /v1/trigger/invoke. Address defaults to 127.0.0.1:7717 or
$AGENTPAAS_TRIGGER_REST_ADDR. Optional bearer auth via $AGENTPAAS_TRIGGER_API_KEY.`,
		Example: `  agentpaas trigger invoke weather
  agentpaas trigger invoke weather --payload '{"city":"SEA"}' --wait
  agentpaas trigger invoke weather --payload ./input.json`,
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "invoke <agent-name>",
		Short: "Invoke an agent via the trigger REST API",
		Long: `Start a run by posting to the trigger REST endpoint.

agent-name may be a bare name, name@pub8, or alias. Without --wait,
returns the run_id immediately. With --wait, polls for invoke-response.json
for up to 60 seconds.`,
		Example: `  agentpaas trigger invoke weather
  agentpaas trigger invoke weather --payload '{"q":"hi"}' --wait
  agentpaas trigger invoke weather --payload ./payload.json --content-type application/json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveCLIAgentRef(cmd, args[0])
			if err != nil {
				return fmt.Errorf("new trigger cmd: %w", err)
			}
			agentName := resolved.DaemonKey

			addr := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR")
			if addr == "" {
				addr = "127.0.0.1:7717"
			}

			payloadPath, _ := cmd.Flags().GetString("payload")      // cobra flag default on missing
			contentType, _ := cmd.Flags().GetString("content-type") // cobra flag default on missing

			// Build request body.
			var body map[string]interface{}
			if payloadPath != "" {
				var payloadBytes []byte
				// If the value starts with '{', treat it as inline JSON;
				// otherwise treat it as a file path.
				if strings.HasPrefix(strings.TrimSpace(payloadPath), "{") {
					payloadBytes = []byte(payloadPath)
				} else {
					payloadBytes, err = os.ReadFile(payloadPath)
					if err != nil {
						return fmt.Errorf("read payload file: %w", err)
					}
				}
				body = map[string]interface{}{
					"agentName":   agentName,
					"payload":     base64.StdEncoding.EncodeToString(payloadBytes),
					"contentType": contentType,
				}
			} else {
				body = map[string]interface{}{
					"agentName": agentName,
				}
			}

			bodyJSON, err := json.Marshal(body)
			if err != nil {
				return fmt.Errorf("marshal request body: %w", err)
			}

			url := fmt.Sprintf("http://%s/v1/trigger/invoke", addr)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(string(bodyJSON)))
			if err != nil {
				return fmt.Errorf("create request: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			if key := os.Getenv("AGENTPAAS_TRIGGER_API_KEY"); key != "" {
				req.Header.Set("Authorization", "Bearer "+key)
			}

			client := &http.Client{Timeout: 90 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("trigger invoke failed: %w", err)
			}
			defer func() { _ = resp.Body.Close() }() // best-effort close

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("read response: %w", err)
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf("trigger invoke failed: HTTP %d: %s", resp.StatusCode, string(respBody))
			}

			var tir triggerInvokeResponse
			if err := json.Unmarshal(respBody, &tir); err != nil {
				return fmt.Errorf("parse trigger response: %w", err)
			}

			runID := tir.Run.RunID
			status := tir.Run.Status

			// Wait for the run to complete, then read the invoke response (BUG 11 fix).
			// Only wait if --wait flag is set (default: false, returns immediately).
			waitForResponse, _ := cmd.Flags().GetBool("wait") // cobra flag default on missing
			invokeResponse := ""
			if waitForResponse {
				homeDir, _ := homeDirPath(cmd) // optional value; zero on miss
				if homeDir != "" && runID != "" {
					respPath := filepath.Join(homeDir, "state", "runs", runID, "invoke-response.json")
					for i := 0; i < 60; i++ { // wait up to 60 seconds
						if data, err := os.ReadFile(respPath); err == nil {
							invokeResponse = string(data)
							break
						}
						time.Sleep(1 * time.Second)
					}
				}
			}

			result := struct {
				RunID          string `json:"run_id"`
				Status         string `json:"status"`
				InvokeResponse string `json:"invoke_response,omitempty"`
			}{
				RunID:          runID,
				Status:         status,
				InvokeResponse: invokeResponse,
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				r := v.(struct {
					RunID          string `json:"run_id"`
					Status         string `json:"status"`
					InvokeResponse string `json:"invoke_response,omitempty"`
				})
				msg := fmt.Sprintf("Triggered agent: run_id=%s status=%s", r.RunID, r.Status)
				if r.InvokeResponse != "" {
					msg += "\nInvoke Response:\n" + r.InvokeResponse
				}
				return msg
			})
		},
	})

	invokeCmd := cmd.Commands()[0]
	invokeCmd.Flags().String("payload", "", "Inline JSON object (starts with '{') or path to a payload file")
	invokeCmd.Flags().String("content-type", "application/json", "MIME type of the payload bytes (default: application/json)")
	invokeCmd.Flags().Bool("wait", false, "Wait up to 60s for the run to finish and print invoke-response.json")

	return cmd
}
