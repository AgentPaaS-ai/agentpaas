package trigger

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

const (
	// webhookMaxRetries is the number of retry attempts after the initial delivery.
	webhookMaxRetries = 3
	// webhookReplayWindow is the maximum age of a valid webhook delivery timestamp.
	webhookReplayWindow = 5 * time.Minute
	// webhookTimeout is the HTTP client timeout for each delivery attempt.
	webhookTimeout = 10 * time.Second
	// webhookHMACHeader is the HTTP header carrying the HMAC signature.
	webhookHMACHeader = "X-AgentPaaS-Signature"
	// webhookTimestampHeader is the HTTP header carrying the timestamp.
	webhookTimestampHeader = "X-AgentPaaS-Timestamp"
	// webhookEventHeader is the HTTP header carrying the event type.
	webhookEventHeader = "X-AgentPaaS-Event"
)

// WebhookConfig configures a webhook delivery target.
type WebhookConfig struct {
	Name   string
	URL    string
	Secret string
}

// WebhookDelivery represents a single delivery attempt.
type WebhookDelivery struct {
	Event      *RunEvent
	Hook       *WebhookConfig
	Attempt    int
	Status     string
	StatusCode int
	Error      string
	Timestamp  time.Time
}

// WebhookDeliverer delivers events to webhook targets.
type WebhookDeliverer struct {
	mu            sync.Mutex
	hooks         []*WebhookConfig
	httpClient    *http.Client
	audit         audit.AuditAppender
	egressChecker EgressChecker
	backoffBase   time.Duration
	deliveries    []WebhookDelivery
}

// EgressChecker checks whether a webhook URL is allowed by policy.
type EgressChecker interface {
	IsURLAllowed(rawURL string) bool
}

type defaultEgressChecker struct {
	allowedDomains []string
}

// defaultEgressChecker.IsURLAllowed reports whether url allowed.
func (d *defaultEgressChecker) IsURLAllowed(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	for _, allowed := range d.allowedDomains {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// NewEgressChecker creates an EgressChecker from policy egress rules.
func NewEgressChecker(rules []policy.EgressRule) EgressChecker {
	var domains []string
	for _, rule := range rules {
		if rule.Domain != "" {
			domains = append(domains, rule.Domain)
		}
	}
	return &defaultEgressChecker{allowedDomains: domains}
}

// NewWebhookDeliverer creates a webhook deliverer.
func NewWebhookDeliverer(hooks []*WebhookConfig, auditAppender audit.AuditAppender, egressChecker EgressChecker) *WebhookDeliverer {
	return &WebhookDeliverer{
		hooks: hooks,
		httpClient: &http.Client{
			Timeout: webhookTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		audit:         auditAppender,
		egressChecker: egressChecker,
		backoffBase:   time.Second,
	}
}

// DeliverSync delivers an event to all configured hooks synchronously.
func (d *WebhookDeliverer) DeliverSync(ctx context.Context, event *RunEvent) []WebhookDelivery {
	results := make([]WebhookDelivery, 0, len(d.hooks))
	for _, hook := range d.hooks {
		results = append(results, d.deliverToHook(ctx, hook, event))
	}
	return results
}

func (d *WebhookDeliverer) deliverToHook(ctx context.Context, hook *WebhookConfig, event *RunEvent) WebhookDelivery {
	if d.egressChecker != nil && !d.egressChecker.IsURLAllowed(hook.URL) {
		delivery := WebhookDelivery{
			Event:     event,
			Hook:      hook,
			Attempt:   0,
			Status:    "blocked",
			Error:     "URL not allowed by egress policy",
			Timestamp: time.Now().UTC(),
		}
		d.recordDelivery(delivery)
		d.auditDeadLetter(event, hook, delivery.Error)
		return delivery
	}

	var lastErr string
	var lastStatusCode int
	for attempt := 0; attempt <= webhookMaxRetries; attempt++ {
		delivery := d.attemptDelivery(ctx, hook, event, attempt)
		if delivery.Status == "delivered" {
			d.recordDelivery(delivery)
			d.auditDelivered(event, hook, attempt)
			return delivery
		}
		lastErr = delivery.Error
		lastStatusCode = delivery.StatusCode
		d.recordDelivery(delivery)
		if attempt < webhookMaxRetries {
			select {
			case <-ctx.Done():
				return WebhookDelivery{
					Event:     event,
					Hook:      hook,
					Attempt:   attempt,
					Status:    "failed",
					Error:     ctx.Err().Error(),
					Timestamp: time.Now().UTC(),
				}
			case <-time.After(time.Duration(1<<attempt) * d.backoffBase):
			}
		}
	}

	delivery := WebhookDelivery{
		Event:      event,
		Hook:       hook,
		Attempt:    webhookMaxRetries,
		Status:     "dead_lettered",
		StatusCode: lastStatusCode,
		Error:      lastErr,
		Timestamp:  time.Now().UTC(),
	}
	d.auditDeadLetter(event, hook, lastErr)
	return delivery
}

func (d *WebhookDeliverer) attemptDelivery(ctx context.Context, hook *WebhookConfig, event *RunEvent, attempt int) WebhookDelivery {
	payload, err := json.Marshal(event)
	if err != nil {
		return WebhookDelivery{Event: event, Hook: hook, Attempt: attempt, Status: "failed", Error: fmt.Sprintf("marshal: %v", err), Timestamp: time.Now().UTC()}
	}

	timestamp := time.Now().UTC()
	signature := computeHMAC(hook.Secret, timestamp, payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(payload))
	if err != nil {
		return WebhookDelivery{Event: event, Hook: hook, Attempt: attempt, Status: "failed", Error: fmt.Sprintf("request: %v", err), Timestamp: timestamp}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(webhookHMACHeader, "sha256="+signature)
	req.Header.Set(webhookTimestampHeader, timestamp.Format(time.RFC3339Nano))
	req.Header.Set(webhookEventHeader, string(event.Type))

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return WebhookDelivery{Event: event, Hook: hook, Attempt: attempt, Status: "failed", Error: err.Error(), Timestamp: timestamp}
	}
	defer func() { _ = resp.Body.Close() }() // best-effort close
	_, _ = io.Copy(io.Discard, resp.Body) // best-effort drain

	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return WebhookDelivery{Event: event, Hook: hook, Attempt: attempt, Status: "delivered", StatusCode: resp.StatusCode, Timestamp: timestamp}
	}
	return WebhookDelivery{Event: event, Hook: hook, Attempt: attempt, Status: "failed", StatusCode: resp.StatusCode, Error: fmt.Sprintf("HTTP %d", resp.StatusCode), Timestamp: timestamp}
}

func computeHMAC(secret string, timestamp time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "%d.", timestamp.Unix()) // best-effort write
	_, _ = mac.Write(body) // hash.Hash.Write never errors
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC verifies a webhook HMAC signature.
func VerifyHMAC(secret string, timestamp time.Time, body []byte, signature string) bool {
	expected := computeHMAC(secret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

// VerifyTimestamp checks that a timestamp is within the webhook replay window.
func VerifyTimestamp(timestamp time.Time, now time.Time) bool {
	diff := now.Sub(timestamp)
	if diff < 0 {
		diff = -diff
	}
	return diff <= webhookReplayWindow
}

// VerifyDelivery verifies a received webhook signature and timestamp.
func VerifyDelivery(secret string, body []byte, signature string, timestampStr string) error {
	timestamp, err := time.Parse(time.RFC3339Nano, timestampStr)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if !VerifyTimestamp(timestamp, time.Now().UTC()) {
		return fmt.Errorf("timestamp outside replay window")
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	if !VerifyHMAC(secret, timestamp, body, sig) {
		return fmt.Errorf("HMAC signature mismatch")
	}
	return nil
}

func (d *WebhookDeliverer) recordDelivery(delivery WebhookDelivery) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.deliveries = append(d.deliveries, delivery)
}

// GetDeliveries returns recorded deliveries.
func (d *WebhookDeliverer) GetDeliveries() []WebhookDelivery {
	d.mu.Lock()
	defer d.mu.Unlock()
	deliveries := make([]WebhookDelivery, len(d.deliveries))
	copy(deliveries, d.deliveries)
	return deliveries
}

func (d *WebhookDeliverer) auditDelivered(event *RunEvent, hook *WebhookConfig, attempt int) {
	if d.audit == nil {
		return
	}
	if err := d.audit.Append(audit.AuditRecord{
		EventType:      "webhook_delivered",
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:webhook",
		Payload: map[string]interface{}{
			"hook_name":  hook.Name,
			"hook_url":   redactURL(hook.URL),
			"run_id":     event.RunID,
			"event_type": string(event.Type),
			"event_id":   event.EventID,
			"attempt":    attempt,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", "webhook_delivered", err)
	}
}

func (d *WebhookDeliverer) auditDeadLetter(event *RunEvent, hook *WebhookConfig, reason string) {
	if d.audit == nil {
		return
	}
	if err := d.audit.Append(audit.AuditRecord{
		EventType:      "webhook_dead_lettered",
		Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
		DeploymentMode: "local",
		Actor:          "system:webhook",
		Payload: map[string]interface{}{
			"hook_name":  hook.Name,
			"hook_url":   redactURL(hook.URL),
			"run_id":     event.RunID,
			"event_type": string(event.Type),
			"event_id":   event.EventID,
			"reason":     reason,
		},
	}); err != nil {
		log.Printf("trigger: audit append (%s): %v", "webhook_dead_lettered", err)
	}
}

func redactURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "[invalid-url]"
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword(parsed.User.Username(), "REDACTED")
	}
	return parsed.String()
}
