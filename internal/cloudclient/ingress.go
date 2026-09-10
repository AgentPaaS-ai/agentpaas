package cloudclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// IngressSourceResponse is POST /v1/ingress/sources.
type IngressSourceResponse struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Label      string `json:"label"`
	RequestURL string `json:"request_url"`
}

// IngressConnectionResponse is POST /v1/ingress/connections.
type IngressConnectionResponse struct {
	ID           string          `json:"id"`
	SourceID     string          `json:"source_id"`
	DeploymentID string          `json:"deployment_id"`
	Label        string          `json:"label"`
	FilterJSON   json.RawMessage `json:"filter_json"`
	Status       string          `json:"status"`
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
