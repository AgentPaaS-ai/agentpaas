package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/AgentPaaS-ai/agentpaas/internal/home"
)

const cloudAPIURLFileName = "cloud-api-url"

// resolveAPIURL returns the cloud API base URL. Order:
// 1. AGENTPAAS_CLOUD_API_URL env var
// 2. Last successful URL persisted under ~/.agentpaas/config/
// 3. Production default
func resolveAPIURL() string {
	if u := strings.TrimSpace(os.Getenv("AGENTPAAS_CLOUD_API_URL")); u != "" {
		return strings.TrimRight(u, "/")
	}
	if u := loadPersistedCloudAPIURL(); u != "" {
		return u
	}
	return cloudclient.DefaultCloudAPIURL
}

func validateCloudAPIURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("cloud api url is empty")
	}
	if strings.ContainsAny(raw, "\n\r\t ") {
		return fmt.Errorf("cloud api url contains whitespace")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("cloud api url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("cloud api url must be https")
	}
	if u.User != nil {
		return fmt.Errorf("cloud api url must not contain userinfo")
	}
	if u.Host == "" {
		return fmt.Errorf("cloud api url host is empty")
	}
	return nil
}

func persistedCloudAPIURLPath() (string, error) {
	homeDir, err := home.DiscoverHome()
	if err != nil {
		return "", err
	}
	paths := home.NewHomePaths(homeDir)
	return filepath.Join(paths.Config, cloudAPIURLFileName), nil
}

func loadPersistedCloudAPIURL() string {
	path, err := persistedCloudAPIURLPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	u := strings.TrimSpace(string(data))
	if err := validateCloudAPIURL(u); err != nil {
		return ""
	}
	return strings.TrimRight(u, "/")
}

func persistCloudAPIURL(raw string) error {
	if err := validateCloudAPIURL(raw); err != nil {
		return err
	}
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	path, err := persistedCloudAPIURLPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("persist cloud api url: %w", err)
	}
	if err := os.WriteFile(path, []byte(u+"\n"), 0o600); err != nil {
		return fmt.Errorf("persist cloud api url: %w", err)
	}
	return nil
}

func clearPersistedCloudAPIURL() {
	path, err := persistedCloudAPIURLPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}
