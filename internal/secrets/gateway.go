package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Gateway struct {
	broker *Broker
	client *http.Client
}

type GatewayRequest struct {
	RunID        string
	PolicyRuleID string
	Method       string
	URL          string
	Body         io.Reader
}

func NewGateway(broker *Broker, client *http.Client) *Gateway {
	if client == nil {
		client = http.DefaultClient
	}
	copyClient := *client
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Gateway{broker: broker, client: &copyClient}
}

func (g *Gateway) Do(ctx context.Context, request GatewayRequest) (*http.Response, error) {
	method := request.Method
	if method == "" {
		method = http.MethodGet
	}
	currentURL := request.URL
	for hop := 0; hop < 10; hop++ {
		req, err := http.NewRequestWithContext(ctx, method, currentURL, request.Body)
		if err != nil {
			return nil, fmt.Errorf("create upstream request: %w", err)
		}

		credentialed := request.PolicyRuleID != ""
		if credentialed {
			injection, err := g.broker.RequestCredential(ctx, request.RunID, request.PolicyRuleID, currentURL, method)
			if err != nil {
				return nil, err
			}
			req.Header.Set(injection.HeaderName, injection.HeaderValue)
		} else if err := g.broker.ValidateEgress(ctx, request.RunID, currentURL, method); err != nil {
			return nil, err
		}

		resp, err := g.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("upstream request: %w", err)
		}
		if !isRedirect(resp.StatusCode) {
			return resp, nil
		}

		location := resp.Header.Get("Location")
		if location == "" {
			return resp, nil
		}
		_ = resp.Body.Close()
		nextURL, err := resolveRedirect(currentURL, location)
		if err != nil {
			return nil, err
		}
		if credentialed {
			err := g.broker.DenyCredentialedRedirect(ctx, request.RunID, request.PolicyRuleID, nextURL, method)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("credentialed redirect denied before injection to %s", nextURL)
		}
		currentURL = nextURL
	}
	return nil, errors.New("too many redirects")
}

func isRedirect(statusCode int) bool {
	switch statusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func resolveRedirect(currentURL, location string) (string, error) {
	base, err := url.Parse(currentURL)
	if err != nil {
		return "", fmt.Errorf("parse redirect base: %w", err)
	}
	next, err := url.Parse(location)
	if err != nil {
		return "", fmt.Errorf("parse redirect location: %w", err)
	}
	return base.ResolveReference(next).String(), nil
}
