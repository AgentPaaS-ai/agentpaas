package cloudclient

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	genericHMACSignatureHeader = "X-Agentpaas-Signature"
	stripeSignatureHeader      = "Stripe-Signature"
)

// WebhookConfigResponse is PUT /v1/deployments/:id/webhook.
type WebhookConfigResponse struct {
	Configured   bool   `json:"configured"`
	Provider     string `json:"provider"`
	DeploymentID string `json:"deployment_id"`
}

// DestinationWebhookResponse is PUT completion-webhook / delivery-webhook.
type DestinationWebhookResponse struct {
	Configured   bool   `json:"configured"`
	URL          string `json:"url,omitempty"`
	DeploymentID string `json:"deployment_id,omitempty"`
}

func invalidDeploymentID(id string) bool {
	return id == "" || strings.ContainsAny(id, "/\\\n\r")
}

func (c *CloudClient) authenticatedJSON(ctx context.Context, method, token, path, op string, payload []byte, dest any) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("%s: create request: %w", op, err)
	}
	if payload != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return wrapTransportError(op, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%s: not authenticated (token may be expired or invalid)", op)
	}
	if !jsonOK(resp.StatusCode) {
		return statusError(op, resp)
	}
	if dest == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("%s: decode response: %w", op, err)
	}
	return nil
}

// PutDeploymentWebhook calls PUT /v1/deployments/:id/webhook.
func (c *CloudClient) PutDeploymentWebhook(ctx context.Context, token, deploymentID, provider, secret string) (*WebhookConfigResponse, error) {
	if invalidDeploymentID(deploymentID) {
		return nil, fmt.Errorf("put webhook: invalid deployment id")
	}
	payload, err := json.Marshal(map[string]string{
		"provider": provider,
		"secret":   secret,
	})
	if err != nil {
		return nil, fmt.Errorf("put webhook: marshal: %w", err)
	}
	var result WebhookConfigResponse
	if err := c.authenticatedJSON(ctx, http.MethodPut, token, "/v1/deployments/"+deploymentID+"/webhook", "put webhook", payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// PutCompletionWebhook calls PUT /v1/deployments/:id/completion-webhook.
func (c *CloudClient) PutCompletionWebhook(ctx context.Context, token, deploymentID, destURL string) (*DestinationWebhookResponse, error) {
	return c.putDestinationWebhook(ctx, token, deploymentID, "/completion-webhook", destURL, "put completion webhook")
}

// PutDeliveryWebhook calls PUT /v1/deployments/:id/delivery-webhook.
func (c *CloudClient) PutDeliveryWebhook(ctx context.Context, token, deploymentID, destURL string) (*DestinationWebhookResponse, error) {
	return c.putDestinationWebhook(ctx, token, deploymentID, "/delivery-webhook", destURL, "put delivery webhook")
}

func (c *CloudClient) putDestinationWebhook(ctx context.Context, token, deploymentID, suffix, destURL, op string) (*DestinationWebhookResponse, error) {
	if invalidDeploymentID(deploymentID) {
		return nil, fmt.Errorf("%s: invalid deployment id", op)
	}
	payload, err := json.Marshal(map[string]string{"url": destURL})
	if err != nil {
		return nil, fmt.Errorf("%s: marshal: %w", op, err)
	}
	var result DestinationWebhookResponse
	if err := c.authenticatedJSON(ctx, http.MethodPut, token, "/v1/deployments/"+deploymentID+suffix, op, payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SignGenericHMAC returns the X-Agentpaas-Signature / Stripe-Signature value.
// v1 is HMAC-SHA256(secret, "{t}.{raw_body}") as lowercase hex.
func SignGenericHMAC(secret string, t int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", t)
	_, _ = mac.Write(body)
	return fmt.Sprintf("t=%d,v1=%s", t, hex.EncodeToString(mac.Sum(nil)))
}

// FireDeploymentHook POSTs to /v1/deployments/:id/hooks/:provider with HMAC.
// This call is unauthenticated (no tenant token).
func (c *CloudClient) FireDeploymentHook(ctx context.Context, deploymentID, provider string, body []byte, signatureHeader, signature string) (json.RawMessage, error) {
	if invalidDeploymentID(deploymentID) {
		return nil, fmt.Errorf("fire webhook: invalid deployment id")
	}
	if strings.ContainsAny(provider, "/\\\n\r") || provider == "" {
		return nil, fmt.Errorf("fire webhook: invalid provider")
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.BaseURL+"/v1/deployments/"+deploymentID+"/hooks/"+provider,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("fire webhook: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(signatureHeader, signature)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("fire webhook", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("fire webhook", resp)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fire webhook: read response: %w", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return json.RawMessage(raw), nil
}

// FireGenericHMAC is FireDeploymentHook for provider generic_hmac.
func (c *CloudClient) FireGenericHMAC(ctx context.Context, deploymentID string, body []byte, secret string, now time.Time) (json.RawMessage, error) {
	t := now.Unix()
	sig := SignGenericHMAC(secret, t, body)
	return c.FireDeploymentHook(ctx, deploymentID, "generic_hmac", body, genericHMACSignatureHeader, sig)
}

// FireStripeHook is FireDeploymentHook for provider stripe.
func (c *CloudClient) FireStripeHook(ctx context.Context, deploymentID string, body []byte, secret string, now time.Time) (json.RawMessage, error) {
	t := now.Unix()
	sig := SignGenericHMAC(secret, t, body)
	return c.FireDeploymentHook(ctx, deploymentID, "stripe", body, stripeSignatureHeader, sig)
}
