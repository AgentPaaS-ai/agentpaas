package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
)

func TestValidateCloudAPIURL(t *testing.T) {
	t.Parallel()
	ok := []string{
		"https://cloud.agentpaas.ai",
		"https://agentpaas-cloud-api-staging.parvezsyed.workers.dev",
	}
	for _, u := range ok {
		if err := validateCloudAPIURL(u); err != nil {
			t.Errorf("validateCloudAPIURL(%q) = %v, want nil", u, err)
		}
	}
	bad := []string{
		"",
		"http://cloud.agentpaas.ai",
		"https://user:pass@cloud.agentpaas.ai",
		"https://cloud.agentpaas.ai\nhttps://evil.example",
		"not a url",
		"file:///etc/passwd",
	}
	for _, u := range bad {
		if err := validateCloudAPIURL(u); err == nil {
			t.Errorf("validateCloudAPIURL(%q) = nil, want error", u)
		}
	}
}

func TestResolveAPIURL_Order(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("AGENTPAAS_HOME", homeDir)
	os.Unsetenv("AGENTPAAS_CLOUD_API_URL")

	if got := resolveAPIURL(); got != cloudclient.DefaultCloudAPIURL {
		t.Fatalf("default: got %q want %q", got, cloudclient.DefaultCloudAPIURL)
	}

	staging := "https://agentpaas-cloud-api-staging.parvezsyed.workers.dev"
	if err := persistCloudAPIURL(staging); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if got := resolveAPIURL(); got != staging {
		t.Fatalf("persisted: got %q want %q", got, staging)
	}

	t.Setenv("AGENTPAAS_CLOUD_API_URL", "https://cloud.agentpaas.ai")
	if got := resolveAPIURL(); got != "https://cloud.agentpaas.ai" {
		t.Fatalf("env wins: got %q", got)
	}

	os.Unsetenv("AGENTPAAS_CLOUD_API_URL")
	clearPersistedCloudAPIURL()
	if got := resolveAPIURL(); got != cloudclient.DefaultCloudAPIURL {
		t.Fatalf("cleared: got %q want default", got)
	}

	path := filepath.Join(homeDir, "config", cloudAPIURLFileName)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleared file still exists: %v", err)
	}
}
