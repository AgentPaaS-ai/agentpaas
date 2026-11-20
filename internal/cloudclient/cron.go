package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// CronSchedule is one row from GET /v1/cron.
type CronSchedule struct {
	DeploymentID    string  `json:"deployment_id"`
	AgentName       *string `json:"agent_name"`
	AgentVersion    *string `json:"agent_version"`
	ImageDigest     string  `json:"image_digest"`
	Expr            string  `json:"expr"`
	ExprHuman       string  `json:"expr_human"`
	Enabled         bool    `json:"enabled"`
	CronLastFiredAt *string `json:"cron_last_fired_at"`
	NextFireAt      *string `json:"next_fire_at"`
	CreatedAt       string  `json:"created_at"`
}

// CronListResponse is GET /v1/cron.
type CronListResponse struct {
	Schedules []CronSchedule `json:"schedules"`
}

// CronConfigResponse is PUT /v1/deployments/:id/cron.
type CronConfigResponse struct {
	DeploymentID string `json:"deployment_id"`
	Expr         string `json:"expr"`
	Enabled      bool   `json:"enabled"`
}

// ListCron calls GET /v1/cron.
func (c *CloudClient) ListCron(ctx context.Context, token string) (*CronListResponse, error) {
	body, err := c.authenticatedGet(ctx, token, "/v1/cron", "list cron")
	if err != nil {
		return nil, err
	}
	var result CronListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("list cron: decode response: %w", err)
	}
	if result.Schedules == nil {
		result.Schedules = []CronSchedule{}
	}
	return &result, nil
}

// SetCron calls PUT /v1/deployments/:id/cron with expr+enabled.
func (c *CloudClient) SetCron(ctx context.Context, token, deploymentID, expr string, enabled bool) (*CronConfigResponse, error) {
	if strings.ContainsAny(deploymentID, "/\\\n\r") {
		return nil, fmt.Errorf("set cron: invalid deployment id")
	}
	payload, err := json.Marshal(map[string]interface{}{
		"expr":    expr,
		"enabled": enabled,
	})
	if err != nil {
		return nil, fmt.Errorf("set cron: marshal: %w", err)
	}
	return c.putCron(ctx, token, deploymentID, payload, "set cron")
}

// SetCronEnabled calls PUT with {enabled} only.
func (c *CloudClient) SetCronEnabled(ctx context.Context, token, deploymentID string, enabled bool) (*CronConfigResponse, error) {
	if strings.ContainsAny(deploymentID, "/\\\n\r") {
		return nil, fmt.Errorf("set cron enabled: invalid deployment id")
	}
	payload, err := json.Marshal(map[string]interface{}{"enabled": enabled})
	if err != nil {
		return nil, fmt.Errorf("set cron enabled: marshal: %w", err)
	}
	return c.putCron(ctx, token, deploymentID, payload, "set cron enabled")
}

func (c *CloudClient) putCron(ctx context.Context, token, deploymentID string, payload []byte, op string) (*CronConfigResponse, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		c.BaseURL+"/v1/deployments/"+deploymentID+"/cron",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", op, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError(op, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%s: not authenticated (token may be expired or invalid)", op)
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError(op, resp)
	}
	var result CronConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", op, err)
	}
	return &result, nil
}
