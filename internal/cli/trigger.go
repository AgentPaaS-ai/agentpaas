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
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "invoke <agent-name>",
		Short: "Invoke an agent via the trigger REST API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveCLIAgentRef(cmd, args[0])
			if err != nil {
				return err
			}
			agentName := resolved.DaemonKey

			addr := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR")
			if addr == "" {
				addr = "127.0.0.1:7717"
			}

			payloadPath, _ := cmd.Flags().GetString("payload")
			contentType, _ := cmd.Flags().GetString("content-type")

			// Build request body.
			var body map[string]interface{}
			if payloadPath != "" {
				payloadBytes, err := os.ReadFile(payloadPath)
				if err != nil {
					return fmt.Errorf("read payload file: %w", err)
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
			defer func() { _ = resp.Body.Close() }()

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
			waitForResponse, _ := cmd.Flags().GetBool("wait")
			invokeResponse := ""
			if waitForResponse {
				homeDir, _ := homeDirPath(cmd)
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
	invokeCmd.Flags().String("payload", "", "Path to a payload file (optional)")
	invokeCmd.Flags().String("content-type", "application/json", "Payload content type")
	invokeCmd.Flags().Bool("wait", false, "Wait for run to complete and show invoke response")

	return cmd
}
