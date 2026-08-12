package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	rebindDefaultPollInterval = 3 * time.Second
	rebindDefaultTimeout      = 120 * time.Second
)

var rebindValidInstanceTypes = map[string]bool{
	"lite":       true,
	"basic":      true,
	"standard-1": true,
	"standard-2": true,
	"standard-3": true,
	"standard-4": true,
}

// rebindOptions holds the resolved flags for the cloud rebind command.
type rebindOptions struct {
	image        string
	env          string
	appID        string
	instanceType string
	accountID    string
	apiToken     string
	yes          bool
	pollInterval time.Duration
	timeout      time.Duration
}

// rebindSummary is the JSON output for a successful rebind.
type rebindSummary struct {
	AppID       string `json:"app_id"`
	Env         string `json:"env"`
	Image       string `json:"image"`
	RolloutID   string `json:"rollout_id"`
	Status      string `json:"status"`
	Verified    bool   `json:"verified"`
	Entrypoint  string `json:"entrypoint"`
	FailedInst  int    `json:"failed_instances"`
}

// newCloudRebindCmd creates the `agentpaas cloud rebind` command.
//
// This is a founder/operator command that wraps the Cloudflare Containers API
// to rebind a container application image. It does NOT use the agentpaas cloud
// API or require agentpaas login — it talks directly to the CF API using a
// founder CF API token.
func newCloudRebindCmd() *cobra.Command {
	var opts rebindOptions

	cmd := &cobra.Command{
		Use:   "rebind",
		Short: "Rebind a CF container app to a new image (founder only)",
		Long: `Rebind a Cloudflare container application to a new agent image via the
CF Containers rollout API. Wraps app-ID resolution (T01), rollout (POST
/rollouts), polling, and post-deploy bind verification (T02).

This is a founder/operator command — it uses CF API tokens directly and does
not require agentpaas cloud login.

Required: --image <registry-ref>
Credentials: CF_ACCOUNT_ID and CF_API_TOKEN env vars (or --account-id /
--api-token flags).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudRebind(cmd, &opts)
		},
	}

	cmd.Flags().StringVar(&opts.image, "image", "", "Container image registry reference (required)")
	cmd.Flags().StringVar(&opts.env, "env", "staging", "Target environment: staging|prod")
	cmd.Flags().StringVar(&opts.appID, "app-id", "", "Override CF container app ID (skip name resolution)")
	cmd.Flags().StringVar(&opts.instanceType, "instance-type", "basic", "Cloudflare Container instance type (lite, basic, standard-1..4)")
	cmd.Flags().StringVar(&opts.accountID, "account-id", "", "Cloudflare account ID (or CF_ACCOUNT_ID env)")
	cmd.Flags().StringVar(&opts.apiToken, "api-token", "", "Cloudflare API token (or CF_API_TOKEN env)")
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().DurationVar(&opts.pollInterval, "poll-interval", rebindDefaultPollInterval, "Poll interval for rollout status")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", rebindDefaultTimeout, "Maximum time to wait for rollout completion")

	_ = cmd.MarkFlagRequired("image")

	return cmd
}

func runCloudRebind(cmd *cobra.Command, opts *rebindOptions) error {
	// Validate image.
	image := strings.TrimSpace(opts.image)
	if image == "" {
		return fmt.Errorf("cloud rebind: --image is required")
	}

	// Validate env.
	env := strings.ToLower(strings.TrimSpace(opts.env))
	if env != "staging" && env != "prod" {
		return fmt.Errorf("cloud rebind: --env must be 'staging' or 'prod', got %q", opts.env)
	}

	// Validate instance type.
	if !rebindValidInstanceTypes[opts.instanceType] {
		return fmt.Errorf("cloud rebind: --instance-type must be one of: lite, basic, standard-1, standard-2, standard-3, standard-4")
	}

	// Resolve CF_ACCOUNT_ID.
	accountID := strings.TrimSpace(opts.accountID)
	if accountID == "" {
		accountID = os.Getenv("CF_ACCOUNT_ID")
	}
	if accountID == "" {
		return fmt.Errorf("cloud rebind: CF_ACCOUNT_ID is required (set --account-id or CF_ACCOUNT_ID env var)")
	}

	// Resolve CF_API_TOKEN.
	apiToken := strings.TrimSpace(opts.apiToken)
	if apiToken == "" {
		apiToken = os.Getenv("CF_API_TOKEN")
	}
	if apiToken == "" {
		return fmt.Errorf("cloud rebind: CF_API_TOKEN is required (set --api-token or CF_API_TOKEN env var)")
	}

	// Resolve CF API base URL.
	apiBaseURL := os.Getenv("CF_API_BASE_URL")
	if apiBaseURL == "" {
		apiBaseURL = "https://api.cloudflare.com/client/v4"
	}

	client := &rebindHTTPClient{
		baseURL:  apiBaseURL,
		token:    apiToken,
		http:     &http.Client{Timeout: 30 * time.Second},
	}

	out := cmd.OutOrStdout()

	// Confirm unless --yes.
	if !opts.yes {
		_, _ = fmt.Fprintf(out, "Rebind %s container app to %s? (y/N) ", env, image)
		reader := bufio.NewReader(cmd.InOrStdin())
		response, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("cloud rebind: read confirmation: %w", err)
		}
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			return fmt.Errorf("cloud rebind: aborted by user")
		}
	}

	// Resolve app ID.
	appID := strings.TrimSpace(opts.appID)
	if appID == "" {
		if !jsonOutput(cmd) {
			_, _ = fmt.Fprintf(out, "Resolving container app ID for env=%s...\n", env)
		}
		resolved, err := resolveContainerAppID(cmd.Context(), client, accountID, env)
		if err != nil {
			return fmt.Errorf("cloud rebind: %w", err)
		}
		appID = resolved
		if !jsonOutput(cmd) {
			_, _ = fmt.Fprintf(out, "Resolved app ID: %s\n", appID)
		}
	}

	// Build rollout request body.
	rolloutBody := buildRolloutBody(image, opts.instanceType)

	// POST rollout.
	if !jsonOutput(cmd) {
		_, _ = fmt.Fprintf(out, "Posting rollout to app %s (image=%s instance=%s)...\n", appID, image, opts.instanceType)
	}
	rolloutID, err := postRollout(cmd.Context(), client, accountID, appID, rolloutBody)
	if err != nil {
		return fmt.Errorf("cloud rebind: post rollout: %w", err)
	}
	if !jsonOutput(cmd) {
		_, _ = fmt.Fprintf(out, "Rollout created: %s\n", rolloutID)
	}

	// Poll rollout status.
	pollInterval := opts.pollInterval
	if pollInterval <= 0 {
		pollInterval = rebindDefaultPollInterval
	}
	timeout := opts.timeout
	if timeout <= 0 {
		timeout = rebindDefaultTimeout
	}

	finalStatus, err := pollRollout(cmd.Context(), client, accountID, appID, rolloutID, pollInterval, timeout, out, jsonOutput(cmd))
	if err != nil {
		return fmt.Errorf("cloud rebind: %w", err)
	}
	if finalStatus != "completed" {
		return fmt.Errorf("cloud rebind: rollout %s ended with status %q (expected completed)", rolloutID, finalStatus)
	}

	// Verify: GET app config and check image + entrypoint.
	appConfig, err := getAppConfig(cmd.Context(), client, accountID, appID)
	if err != nil {
		return fmt.Errorf("cloud rebind: verify: %w", err)
	}

	verified, verifyErr := verifyAppConfig(appConfig, image)
	if !verified {
		return fmt.Errorf("cloud rebind: verification failed: %w", verifyErr)
	}

	summary := buildRebindSummary(appID, env, image, rolloutID, appConfig)
	if jsonOutput(cmd) {
		return printTextOrJSON(true, summary, nil)
	}
	_, _ = fmt.Fprintf(out, "Rebind complete.\n")
	_, _ = fmt.Fprintf(out, "  App ID:     %s\n", summary.AppID)
	_, _ = fmt.Fprintf(out, "  Image:      %s\n", summary.Image)
	_, _ = fmt.Fprintf(out, "  Rollout:    %s (%s)\n", summary.RolloutID, summary.Status)
	_, _ = fmt.Fprintf(out, "  Entrypoint: %s\n", summary.Entrypoint)
	_, _ = fmt.Fprintf(out, "  Verified:   image matches, entrypoint is [/agentpaas/harness]\n")
	return nil
}

// rebindHTTPClient is a minimal HTTP client for the Cloudflare Containers API.
// It is deliberately separate from cloudclient.CloudClient because this command
// talks to the CF platform API directly, not the agentpaas cloud API.
type rebindHTTPClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func (c *rebindHTTPClient) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}

// cfAPIResponse is the standard Cloudflare API envelope.
type cfAPIResponse struct {
	Success  bool            `json:"success"`
	Errors   []cfAPIError    `json:"errors"`
	Messages []string        `json:"messages"`
	Result   json.RawMessage `json:"result"`
}

type cfAPIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// cfContainerApp is a container application in the CF list response.
type cfContainerApp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// cfRollout is a rollout in the CF API response.
type cfRollout struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// cfAppConfig is the full container application configuration.
type cfAppConfig struct {
	ID            string           `json:"id"`
	Configuration cfAppConfigInner `json:"configuration"`
	LatestRollout *cfRollout       `json:"latest_rollout,omitempty"`
	Health        *cfAppHealth     `json:"health,omitempty"`
}

type cfAppConfigInner struct {
	Image      string   `json:"image"`
	Entrypoint []string `json:"entrypoint"`
}

type cfAppHealth struct {
	Instances cfAppInstances `json:"instances"`
}

type cfAppInstances struct {
	Failed int `json:"failed"`
}

// resolveContainerAppID lists container applications and matches by env pattern.
// staging: name contains "staging-runcontainer"
// prod: name ends with "-runcontainer" without "staging"
func resolveContainerAppID(ctx context.Context, client *rebindHTTPClient, accountID, env string) (string, error) {
	path := fmt.Sprintf("/accounts/%s/containers/applications", accountID)
	resp, err := client.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", fmt.Errorf("list applications: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("list applications: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("list applications: CF API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp cfAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("list applications: parse response: %w", err)
	}

	var apps []cfContainerApp
	if err := json.Unmarshal(apiResp.Result, &apps); err != nil {
		return "", fmt.Errorf("list applications: parse result: %w", err)
	}

	var matches []cfContainerApp
	for _, app := range apps {
		name := strings.ToLower(app.Name)
		if env == "staging" {
			if strings.Contains(name, "staging-runcontainer") {
				matches = append(matches, app)
			}
		} else { // prod
			if strings.HasSuffix(name, "-runcontainer") && !strings.Contains(name, "staging") {
				matches = append(matches, app)
			}
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no container application found matching %s pattern for env=%s", env, env)
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = fmt.Sprintf("%s (%s)", m.ID, m.Name)
		}
		return "", fmt.Errorf("multiple container applications matched %s pattern for env=%s: %s", env, env, strings.Join(names, ", "))
	}

	return matches[0].ID, nil
}

// buildRolloutBody constructs the JSON body for the POST /rollouts request,
// matching the rebind-container-image.sh format.
func buildRolloutBody(image, instanceType string) map[string]interface{} {
	return map[string]interface{}{
		"description":     "agentpaas-deploy-bind",
		"strategy":        "rolling",
		"kind":            "full_auto",
		"step_percentage": 100,
		"target_configuration": map[string]interface{}{
			"image":         image,
			"instance_type": instanceType,
			"disk":          map[string]interface{}{"size_mb": 2000, "size": "2GB"},
			"network":       map[string]interface{}{"assign_ipv6": "none", "assign_ipv4": "none", "mode": "private"},
			"command":       []string{},
			"entrypoint":    []string{"/agentpaas/harness"},
			"runtime":       "firecracker",
			"observability": map[string]interface{}{"logs": map[string]interface{}{"enabled": true}},
		},
	}
}

// postRollout POSTs the rollout request and returns the rollout ID.
func postRollout(ctx context.Context, client *rebindHTTPClient, accountID, appID string, body map[string]interface{}) (string, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal rollout body: %w", err)
	}

	path := fmt.Sprintf("/accounts/%s/containers/applications/%s/rollouts", accountID, appID)
	resp, err := client.do(ctx, http.MethodPost, path, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("post rollout: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("post rollout: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("post rollout: CF API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp cfAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("post rollout: parse response: %w", err)
	}

	var rollout cfRollout
	if err := json.Unmarshal(apiResp.Result, &rollout); err != nil {
		return "", fmt.Errorf("post rollout: parse result: %w", err)
	}
	if rollout.ID == "" {
		return "", fmt.Errorf("post rollout: response missing rollout ID")
	}
	return rollout.ID, nil
}

// pollRollout polls the rollout status until it reaches a terminal state
// (completed/failed) or the timeout expires.
func pollRollout(ctx context.Context, client *rebindHTTPClient, accountID, appID, rolloutID string, interval, timeout time.Duration, out io.Writer, jsonOut bool) (string, error) {
	path := fmt.Sprintf("/accounts/%s/containers/applications/%s/rollouts/%s", accountID, appID, rolloutID)
	deadline := time.Now().Add(timeout)

	for {
		resp, err := client.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return "", fmt.Errorf("poll rollout: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("poll rollout: read body: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("poll rollout: CF API returned %d: %s", resp.StatusCode, string(body))
		}

		var apiResp cfAPIResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return "", fmt.Errorf("poll rollout: parse response: %w", err)
		}

		var rollout cfRollout
		if err := json.Unmarshal(apiResp.Result, &rollout); err != nil {
			return "", fmt.Errorf("poll rollout: parse result: %w", err)
		}

		if !jsonOut {
			_, _ = fmt.Fprintf(out, "  Rollout status: %s\n", rollout.Status)
		}

		if rollout.Status == "completed" || rollout.Status == "failed" {
			return rollout.Status, nil
		}

		if time.Now().After(deadline) {
			return rollout.Status, fmt.Errorf("rollout timed out after %s (last status: %s)", timeout, rollout.Status)
		}

		select {
		case <-ctx.Done():
			return rollout.Status, ctx.Err()
		case <-time.After(interval):
		}
	}
}

// getAppConfig GETs the container application configuration.
func getAppConfig(ctx context.Context, client *rebindHTTPClient, accountID, appID string) (*cfAppConfig, error) {
	path := fmt.Sprintf("/accounts/%s/containers/applications/%s", accountID, appID)
	resp, err := client.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get app config: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("get app config: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get app config: CF API returned %d: %s", resp.StatusCode, string(body))
	}

	var apiResp cfAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("get app config: parse response: %w", err)
	}

	var cfg cfAppConfig
	if err := json.Unmarshal(apiResp.Result, &cfg); err != nil {
		return nil, fmt.Errorf("get app config: parse result: %w", err)
	}
	return &cfg, nil
}

// verifyAppConfig checks that the live app config matches the expected image
// and has the correct entrypoint.
func verifyAppConfig(cfg *cfAppConfig, expectedImage string) (bool, error) {
	if cfg.Configuration.Image != expectedImage {
		return false, fmt.Errorf("image mismatch: got %q, want %q", cfg.Configuration.Image, expectedImage)
	}

	// Entrypoint must be ["/agentpaas/harness"].
	entrypointOK := len(cfg.Configuration.Entrypoint) == 1 && cfg.Configuration.Entrypoint[0] == "/agentpaas/harness"
	if !entrypointOK {
		return false, fmt.Errorf("entrypoint mismatch: got %v, want [/agentpaas/harness]", cfg.Configuration.Entrypoint)
	}

	return true, nil
}

// buildRebindSummary constructs the success summary from the app config.
func buildRebindSummary(appID, env, image, rolloutID string, cfg *cfAppConfig) rebindSummary {
	summary := rebindSummary{
		AppID:     appID,
		Env:       env,
		Image:     cfg.Configuration.Image,
		RolloutID: rolloutID,
		Status:    "completed",
		Verified:  true,
		Entrypoint: fmt.Sprintf("%v", cfg.Configuration.Entrypoint),
	}
	if cfg.LatestRollout != nil {
		summary.Status = cfg.LatestRollout.Status
	}
	if cfg.Health != nil {
		summary.FailedInst = cfg.Health.Instances.Failed
	}
	return summary
}
