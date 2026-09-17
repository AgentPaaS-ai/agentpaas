package pack

import (
	"fmt"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

var crewAITelemetryHosts = []string{
	"telemetry.crewai.com",
	"app.posthog.com",
	"telemetry.sentry.io",
	"api.crewai.com",
}

// ValidateCrewAITelemetry rejects CrewAI telemetry hosts in policy egress (M16.3 SC5).
func ValidateCrewAITelemetry(projectDir string, pol *policy.Policy) error {
	if pol == nil {
		return nil
	}
	for _, rule := range pol.Egress {
		domain := strings.ToLower(strings.TrimSpace(rule.Domain))
		domain = strings.TrimSuffix(domain, ".")
		for _, host := range crewAITelemetryHosts {
			if domain == host || strings.HasSuffix(domain, "."+host) {
				return fmt.Errorf("pack rejects CrewAI telemetry host in egress")
			}
		}
	}
	return nil
}
