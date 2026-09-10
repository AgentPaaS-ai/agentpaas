package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// IngressSourceResponse is POST /v1/ingress/sources and other source payloads.
type IngressSourceResponse struct {
	ID              string  `json:"id"`
	Provider        string  `json:"provider"`
	Label           string  `json:"label"`
	RequestURL      string  `json:"request_url"`
	Status          string  `json:"status,omitempty"`
	LastEventAt     *string `json:"last_event_at,omitempty"`
	ConnectionCount int     `json:"connection_count,omitempty"`
}

// IngressSourceDetail is GET /v1/ingress/sources/:id (includes connections).
type IngressSourceDetail struct {
	IngressSourceResponse
	Connections []IngressConnectionResponse `json:"connections"`
}

// IngressConnectionResponse is POST /v1/ingress/connections and connection payloads.
type IngressConnectionResponse struct {
	ID           string          `json:"id"`
	SourceID     string          `json:"source_id"`
	DeploymentID string          `json:"deployment_id"`
	Label        string          `json:"label"`
	FilterJSON   json.RawMessage `json:"filter_json"`
	Status       string          `json:"status"`
}

// IngressEvent is one row from GET /v1/ingress/sources/:id/events.
type IngressEvent struct {
	ID                   string          `json:"id"`
	Provider             string          `json:"provider"`
	ProviderEventID      *string         `json:"provider_event_id"`
	Status               string          `json:"status"`
	MatchedConnectionIDs []string        `json:"matched_connection_ids"`
	CreatedAt            string          `json:"created_at"`
	PayloadJSON          json.RawMessage `json:"payload_json"`
}

// IngressBindReplyResponse is POST /v1/ingress/sources/:id/bind-reply.
type IngressBindReplyResponse struct {
	OK         bool   `json:"ok"`
	Credential string `json:"credential"`
}

// IngressTestFilterResponse is POST /v1/ingress/test-filter.
type IngressTestFilterResponse struct {
	Matched bool `json:"matched"`
}

type ingressConnectionRequest struct {
	SourceID     string          `json:"source_id"`
	DeploymentID string          `json:"deployment_id"`
	Label        string          `json:"label"`
	FilterJSON   json.RawMessage `json:"filter_json,omitempty"`
}

func invalidIngressID(id string) bool {
	return invalidDeploymentID(id)
}

func ingressSourcePath(id, suffix string) (string, error) {
	if invalidIngressID(id) {
		return "", fmt.Errorf("invalid source id")
	}
	path := "/v1/ingress/sources/" + id
	if suffix != "" {
		path += suffix
	}
	return path, nil
}

func ingressConnectionPath(id, suffix string) (string, error) {
	if invalidIngressID(id) {
		return "", fmt.Errorf("invalid connection id")
	}
	path := "/v1/ingress/connections/" + id
	if suffix != "" {
		path += suffix
	}
	return path, nil
}

// CreateIngressSource calls POST /v1/ingress/sources.
// The signing secret is sent in the JSON body and is never logged.
func (c *CloudClient) CreateIngressSource(ctx context.Context, token, provider, label, secret string) (*IngressSourceResponse, error) {
	if provider == "" {
		return nil, fmt.Errorf("create ingress source: provider is required")
	}
	if label == "" {
		return nil, fmt.Errorf("create ingress source: label is required")
	}
	if secret == "" {
		return nil, fmt.Errorf("create ingress source: secret is required")
	}
	payload, err := json.Marshal(map[string]string{
		"provider": provider,
		"label":    label,
		"secret":   secret,
	})
	if err != nil {
		return nil, fmt.Errorf("create ingress source: marshal: %w", err)
	}
	var result IngressSourceResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, "/v1/ingress/sources", "create ingress source", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateIngressConnection calls POST /v1/ingress/connections.
// filterJSON is optional raw JSON; empty omits filter_json (match all).
func (c *CloudClient) CreateIngressConnection(ctx context.Context, token, sourceID, deploymentID, label string, filterJSON json.RawMessage) (*IngressConnectionResponse, error) {
	if invalidIngressID(sourceID) {
		return nil, fmt.Errorf("create ingress connection: invalid source id")
	}
	if invalidIngressID(deploymentID) {
		return nil, fmt.Errorf("create ingress connection: invalid deployment id")
	}
	if label == "" {
		return nil, fmt.Errorf("create ingress connection: label is required")
	}
	payload, err := json.Marshal(ingressConnectionRequest{
		SourceID:     sourceID,
		DeploymentID: deploymentID,
		Label:        label,
		FilterJSON:   filterJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("create ingress connection: marshal: %w", err)
	}
	var result IngressConnectionResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, "/v1/ingress/connections", "create ingress connection", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListIngressSources calls GET /v1/ingress/sources.
func (c *CloudClient) ListIngressSources(ctx context.Context, token string) ([]IngressSourceResponse, error) {
	var result []IngressSourceResponse
	if err := c.authenticatedJSON(ctx, http.MethodGet, token, "/v1/ingress/sources", "list ingress sources", nil, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = []IngressSourceResponse{}
	}
	return result, nil
}

// GetIngressSource calls GET /v1/ingress/sources/:id (includes connections).
func (c *CloudClient) GetIngressSource(ctx context.Context, token, sourceID string) (*IngressSourceDetail, error) {
	path, err := ingressSourcePath(sourceID, "")
	if err != nil {
		return nil, fmt.Errorf("get ingress source: %w", err)
	}
	var result IngressSourceDetail
	if err := c.authenticatedJSON(ctx, http.MethodGet, token, path, "get ingress source", nil, &result); err != nil {
		return nil, err
	}
	if result.Connections == nil {
		result.Connections = []IngressConnectionResponse{}
	}
	return &result, nil
}

// ListIngressSourceEvents calls GET /v1/ingress/sources/:id/events?limit=.
func (c *CloudClient) ListIngressSourceEvents(ctx context.Context, token, sourceID string, limit int) ([]IngressEvent, error) {
	path, err := ingressSourcePath(sourceID, "/events")
	if err != nil {
		return nil, fmt.Errorf("list ingress events: %w", err)
	}
	if limit < 1 {
		return nil, fmt.Errorf("list ingress events: limit must be a positive integer")
	}
	if limit > 100 {
		limit = 100
	}
	path += "?limit=" + strconv.Itoa(limit)
	var result []IngressEvent
	if err := c.authenticatedJSON(ctx, http.MethodGet, token, path, "list ingress events", nil, &result); err != nil {
		return nil, err
	}
	if result == nil {
		result = []IngressEvent{}
	}
	return result, nil
}

// DisableIngressSource calls POST /v1/ingress/sources/:id/disable.
func (c *CloudClient) DisableIngressSource(ctx context.Context, token, sourceID string) (*IngressSourceResponse, error) {
	path, err := ingressSourcePath(sourceID, "/disable")
	if err != nil {
		return nil, fmt.Errorf("disable ingress source: %w", err)
	}
	var result IngressSourceResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, path, "disable ingress source", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DisableIngressConnection calls POST /v1/ingress/connections/:id/disable.
func (c *CloudClient) DisableIngressConnection(ctx context.Context, token, connectionID string) (*IngressConnectionResponse, error) {
	path, err := ingressConnectionPath(connectionID, "/disable")
	if err != nil {
		return nil, fmt.Errorf("disable ingress connection: %w", err)
	}
	var result IngressConnectionResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, path, "disable ingress connection", nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// RotateIngressSource calls POST /v1/ingress/sources/:id/rotate with {secret}.
// The signing secret is sent in the JSON body and is never logged.
func (c *CloudClient) RotateIngressSource(ctx context.Context, token, sourceID, secret string) (*IngressSourceResponse, error) {
	path, err := ingressSourcePath(sourceID, "/rotate")
	if err != nil {
		return nil, fmt.Errorf("rotate ingress source: %w", err)
	}
	if secret == "" {
		return nil, fmt.Errorf("rotate ingress source: secret is required")
	}
	payload, err := json.Marshal(map[string]string{"secret": secret})
	if err != nil {
		return nil, fmt.Errorf("rotate ingress source: marshal: %w", err)
	}
	var result IngressSourceResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, path, "rotate ingress source", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// BindIngressSourceReply calls POST /v1/ingress/sources/:id/bind-reply.
// The reply credential secret is sent in the JSON body and is never logged.
func (c *CloudClient) BindIngressSourceReply(ctx context.Context, token, sourceID, credential, secret string) (*IngressBindReplyResponse, error) {
	path, err := ingressSourcePath(sourceID, "/bind-reply")
	if err != nil {
		return nil, fmt.Errorf("bind ingress reply: %w", err)
	}
	if credential == "" {
		return nil, fmt.Errorf("bind ingress reply: credential is required")
	}
	if secret == "" {
		return nil, fmt.Errorf("bind ingress reply: secret is required")
	}
	payload, err := json.Marshal(map[string]string{
		"credential": credential,
		"secret":     secret,
	})
	if err != nil {
		return nil, fmt.Errorf("bind ingress reply: marshal: %w", err)
	}
	var result IngressBindReplyResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, path, "bind ingress reply", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TestIngressFilter calls POST /v1/ingress/test-filter with {filter, event}.
func (c *CloudClient) TestIngressFilter(ctx context.Context, token string, filter, event json.RawMessage) (*IngressTestFilterResponse, error) {
	if len(filter) == 0 || !json.Valid(filter) {
		return nil, fmt.Errorf("test ingress filter: filter must be JSON")
	}
	if len(event) == 0 || !json.Valid(event) {
		return nil, fmt.Errorf("test ingress filter: event must be JSON")
	}
	payload, err := json.Marshal(map[string]json.RawMessage{
		"filter": filter,
		"event":  event,
	})
	if err != nil {
		return nil, fmt.Errorf("test ingress filter: marshal: %w", err)
	}
	var result IngressTestFilterResponse
	if err := c.authenticatedJSON(ctx, http.MethodPost, token, "/v1/ingress/test-filter", "test ingress filter", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
