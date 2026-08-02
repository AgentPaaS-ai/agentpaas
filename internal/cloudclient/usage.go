package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// UsageMeter describes how cloud CPU minutes are estimated.
type UsageMeter struct {
	Formula             string `json:"formula"`
	SleepTailSecDefault int    `json:"sleep_tail_sec_default"`
	Note                string `json:"note"`
}

// UsageResponse is the response from GET /v1/usage.
type UsageResponse struct {
	Tier                string     `json:"tier"`
	ConcurrencyLimit    int        `json:"concurrency_limit"`
	ConcurrencyActive   int        `json:"concurrency_active"`
	AgentLimit          int        `json:"agent_limit"`
	AgentsUsed          int        `json:"agents_used"`
	CPUMinuteLimit      int        `json:"cpu_minute_limit"`
	CPUMinutesUsed      float64    `json:"cpu_minutes_used"`
	CPUMinutesRemaining *float64   `json:"cpu_minutes_remaining"`
	UsagePeriodStart    string     `json:"usage_period_start"`
	TrialExpiresAt      string     `json:"trial_expires_at"`
	DaysRemaining       *int       `json:"days_remaining"`
	Meter               UsageMeter `json:"meter"`
}

// GetUsage calls GET /v1/usage with a Bearer token.
func (c *CloudClient) GetUsage(ctx context.Context, token string) (*UsageResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/usage", nil)
	if err != nil {
		return nil, fmt.Errorf("get usage: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, wrapTransportError("get usage", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("get usage: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("get usage", resp)
	}

	var result UsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("get usage: decode response: %w", err)
	}
	return &result, nil
}
