package pack

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

// telemetrySuffixes are registrable domains whose hosts must not appear in
// egress. A parent, wildcard, subdomain, or homoglyph of these is a bypass.
var telemetrySuffixes = []string{
	"crewai.com",
	"posthog.com",
	"sentry.io",
}

// ValidateCrewAITelemetry rejects telemetry egress and project files that
// would dial or install those hosts. A missing policy fails closed. A
// symlink project directory is rejected before it is read.
func ValidateCrewAITelemetry(projectDir string, pol *policy.Policy) error {
	if err := validateProjectDir(projectDir); err != nil {
		return fmt.Errorf("pack rejects CrewAI telemetry host in egress: %w", err)
	}
	if pol == nil {
		loaded, err := LoadPolicy(projectDir)
		if err != nil {
			return fmt.Errorf("pack rejects CrewAI telemetry host in egress: %w", err)
		}
		if loaded == nil {
			return fmt.Errorf("pack rejects CrewAI telemetry host in egress: missing policy")
		}
		pol = loaded
	}
	if err := rejectTelemetryEgress(pol); err != nil {
		return err
	}
	return scanProjectTelemetry(projectDir)
}

func rejectTelemetryEgress(pol *policy.Policy) error {
	if pol == nil {
		return fmt.Errorf("pack rejects CrewAI telemetry host in egress: missing policy")
	}
	for _, rule := range pol.Egress {
		if strings.TrimSpace(rule.CIDR) != "" {
			return fmt.Errorf("pack rejects CrewAI telemetry host in egress: cidr bypass")
		}
		if rule.Domain == "" {
			continue
		}
		if err := rejectTelemetryDomain(rule.Domain); err != nil {
			return err
		}
	}
	return nil
}

func rejectTelemetryDomain(raw string) error {
	if strings.ContainsAny(raw, "\n\r\t\x00") {
		return fmt.Errorf("pack rejects CrewAI telemetry host in egress")
	}
	host, err := egressHost(raw)
	if err != nil {
		return fmt.Errorf("pack rejects CrewAI telemetry host in egress: %w", err)
	}
	if host == "" {
		return nil
	}
	for _, r := range host {
		if r > unicode.MaxASCII {
			return fmt.Errorf("pack rejects CrewAI telemetry host in egress")
		}
	}
	if isTelemetryHost(host) {
		return fmt.Errorf("pack rejects CrewAI telemetry host in egress")
	}
	return nil
}

func egressHost(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	s = strings.TrimSuffix(s, ".")
	if s == "" {
		return "", nil
	}
	if strings.Contains(s, "://") {
		u, err := url.Parse(s)
		if err != nil {
			return "", err
		}
		s = u.Hostname()
	} else if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	} else if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s, "]") {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "*.")
	s = strings.TrimSuffix(s, ".")
	return s, nil
}

func isTelemetryHost(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "*.")
	for _, suf := range telemetrySuffixes {
		if host == suf || strings.HasSuffix(host, "."+suf) {
			return true
		}
	}
	return false
}

func scanProjectTelemetry(projectDir string) error {
	entries := []string{"main.py", "app.py", "requirements.txt"}
	for _, name := range entries {
		path := filepath.Join(projectDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("pack rejects CrewAI telemetry host in egress: %w", err)
		}
		text := strings.ToLower(string(data))
		if name == "requirements.txt" {
			if strings.Contains(text, "posthog") || strings.Contains(text, "sentry-sdk") || strings.Contains(text, "crewai[") {
				return fmt.Errorf("pack rejects CrewAI telemetry host in egress")
			}
		}
		for _, suf := range telemetrySuffixes {
			if strings.Contains(text, suf) {
				return fmt.Errorf("pack rejects CrewAI telemetry host in egress")
			}
		}
	}
	return nil
}
