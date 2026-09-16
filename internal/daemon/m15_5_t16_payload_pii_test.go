package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/policy"
)

func TestM15_5_T16_D205_SamePolicyYAMLPayloadAndCompile(t *testing.T) {
	server := testControlServerForPayload(t, nil)
	writeDeployedLock(t, server.homePaths, "pii-agent", &pack.AgentYAML{
		Name:    "pii-agent",
		Version: "1.0.0",
	})
	deployedDir := pack.DeployedAgentPath(server.homePaths.Home, "pii-agent")
	policyYAML := `version: "1.0"
agent:
  name: pii-agent
egress:
  - domain: api.openai.com
    ports: [443]
guardrails:
  pii:
    action: reject
    builtins: [Ssn, Email]
    reject_status: 422
    reject_body: "pii_rejected"
`
	if err := os.WriteFile(filepath.Join(deployedDir, "policy.yaml"), []byte(policyYAML), 0o600); err != nil {
		t.Fatal(err)
	}

	payload, err := server.buildInvokePayload(context.Background(), "pii-agent", nil)
	if err != nil {
		t.Fatalf("buildInvokePayload: %v", err)
	}
	piiAny, ok := payload["pii"]
	if !ok || piiAny == nil {
		t.Fatalf("D205: pii missing from local invoke payload: %#v", payload)
	}
	pii, ok := piiAny.(map[string]any)
	if !ok {
		t.Fatalf("pii type %T", piiAny)
	}
	if pii["action"] != "reject" {
		t.Fatalf("action=%v", pii["action"])
	}
	if payload["guardrails"] != nil {
		t.Fatalf("mapping-form must not populate sequence guardrails: %#v", payload["guardrails"])
	}

	p, err := policy.ParsePolicy(strings.NewReader(policyYAML))
	if err != nil {
		t.Fatalf("ParsePolicy: %v", err)
	}
	out, err := policy.CompileGatewayConfig(p)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	outStr := string(out)
	if !strings.Contains(outStr, "api.openai.com") {
		t.Fatalf("D205: do not omit egress host to fake leftover ADV:\n%s", outStr)
	}
	if strings.Contains(outStr, "guardrails") {
		t.Fatalf("compile still omits route-level guardrails, got:\n%s", outStr)
	}
	if strings.Contains(strings.ToLower(outStr), "backendoauth") {
		t.Fatalf("backendOAuth leaked:\n%s", outStr)
	}

	clobber, err := server.buildInvokePayload(context.Background(), "pii-agent", []byte(`{"pii":{"action":"mask"}}`))
	if err != nil {
		t.Fatalf("clobber: %v", err)
	}
	got, _ := clobber["pii"].(map[string]any)
	if got["action"] != "reject" {
		t.Fatalf("user trigger must not clobber reserved pii, got %#v", clobber["pii"])
	}
}
