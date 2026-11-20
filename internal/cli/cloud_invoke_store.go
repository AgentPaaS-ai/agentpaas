package cli

import (
	"fmt"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
)

const cloudInvokeTokenStoreName = "invoke-tokens.json"

var cloudInvokeTokenStoreFactory = func(path string) (cloudclient.InvokeTokenStore, error) {
	return cloudclient.NewFileInvokeTokenStore(path)
}

func newCloudInvokeTokenStore(cmd *cobra.Command) (cloudclient.InvokeTokenStore, error) {
	homeDir, err := homeDirPath(cmd)
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	path := filepath.Join(homeDir, cloudInvokeTokenStoreName)
	store, err := cloudInvokeTokenStoreFactory(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return store, nil
}
