package cloudclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RevokeOAuthGrantRequest is the body for POST /v1/oauth/revoke.
//
// It deliberately omits tenant_id — the Cloud API derives the tenant from the
// authenticated Bearer token, so the customer CLI must never send it in the
// request body.  Sending tenant_id would be a security risk (tenant spoofing).
type RevokeOAuthGrantRequest struct {
	DeploymentID    string `json:"deployment_id"`
	CredentialID    string `json:"credential_id"`
	EndUserIdentity string `json:"end_user_identity"`
}

// OAuthRevokeResult is the outcome metadata returned by POST /v1/oauth/revoke.
// It never carries tokens or refresh-token material — only a confirmation that
// the grant was revoked plus identifying metadata echoed back for the operator.
type OAuthRevokeResult struct {
	DeploymentID    string `json:"deployment_id"`
	CredentialID    string `json:"credential_id"`
	EndUserIdentity string `json:"end_user_identity"`
	Revoked         bool   `json:"revoked"`
	RevokedAt       string `json:"revoked_at"`
}

// RevokeOAuthGrant calls POST /v1/oauth/revoke with a Bearer token to revoke a
// delegated OAuth grant from the Cloud.  The request body carries the
// deployment ID, credential ID, and end-user identity; tenant_id is intentionally
// excluded so the server resolves the tenant from the authenticated session.
func (c *CloudClient) RevokeOAuthGrant(ctx context.Context, token string, req RevokeOAuthGrantRequest) (*OAuthRevokeResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("oauth revoke: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/v1/oauth/revoke", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oauth revoke: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, wrapTransportError("oauth revoke", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("oauth revoke: not authenticated (token may be expired or invalid)")
	}
	if !jsonOK(resp.StatusCode) {
		return nil, statusError("oauth revoke", resp)
	}

	var result OAuthRevokeResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("oauth revoke: decode response: %w", err)
	}
	return &result, nil
}
