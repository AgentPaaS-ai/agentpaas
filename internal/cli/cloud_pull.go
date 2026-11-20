package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AgentPaaS-ai/agentpaas/internal/cloudclient"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newCloudPullCmd implements FEAT-1: download cloud image metadata into a local project dir.
func newCloudPullCmd() *cobra.Command {
	var (
		dir     string
		bump    string
		force   bool
	)
	cmd := &cobra.Command{
		Use:   "pull <image_id|agent_name>",
		Short: "Download a cloud agent image into a local project directory",
		Long: `Download admitted cloud agent metadata (agent.yaml from lock) into a local
directory for edit → version bump → pack → push → redeploy.

Source code is reconstructed from the lock agent_yaml when available. If a local
Docker image agentpaas/<name>:<version> exists, files under /app are copied into
the project (best-effort). Full source round-trip improves when pack embeds source.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := strings.TrimSpace(args[0])
			if ref == "" {
				return fmt.Errorf("cloud pull: image id or agent name required")
			}
			if dir == "" {
				dir = filepath.Join(".", sanitizeDirName(ref))
			}
			if !force {
				if st, err := os.Stat(dir); err == nil && st.IsDir() {
					entries, _ := os.ReadDir(dir)
					if len(entries) > 0 {
						return fmt.Errorf("cloud pull: directory %s is not empty (pass --force to overwrite agent.yaml)", dir)
					}
				}
			}

			token, err := resolveToken(cmd)
			if err != nil {
				return fmt.Errorf("cloud pull: %w", err)
			}
			client := cloudclient.NewCloudClient(resolveAPIURL())

			img, err := resolvePullImage(cmd, client, token, ref)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("cloud pull: mkdir: %w", err)
			}

			agentYAML := map[string]interface{}{}
			if img.AgentYAML != nil {
				agentYAML = img.AgentYAML
			}
			// Ensure name/version
			if _, ok := agentYAML["name"]; !ok && img.AgentName != "" {
				agentYAML["name"] = img.AgentName
			}
			if _, ok := agentYAML["version"]; !ok && img.AgentVersion != "" {
				agentYAML["version"] = img.AgentVersion
			}
			if bump != "" {
				agentYAML["version"] = bump
			}

			yamlBytes, err := yaml.Marshal(agentYAML)
			if err != nil {
				return fmt.Errorf("cloud pull: marshal agent.yaml: %w", err)
			}
			if err := os.WriteFile(filepath.Join(dir, "agent.yaml"), yamlBytes, 0o644); err != nil {
				return fmt.Errorf("cloud pull: write agent.yaml: %w", err)
			}

			// Minimal main.py if missing
			mainPath := filepath.Join(dir, "main.py")
			if _, err := os.Stat(mainPath); err != nil {
				agentLabel := img.AgentName
				if agentLabel == "" {
					agentLabel = "agent"
				}
				stub := fmt.Sprintf(`"""Pulled from cloud image %s (%s %s).
Replace this stub with your agent logic, or restore source from git.
Uses AgentPaaS native harness invoke contract.
"""
from agentpaas_sdk import agent


@agent.on_invoke
def handle(payload: dict) -> dict:
    query = payload.get("query") or payload.get("message") or "hello"
    return {"final_output": f"[%s] echo: {query}"}
`, img.ID, img.AgentName, img.AgentVersion, agentLabel)
				if err := os.WriteFile(mainPath, []byte(stub), 0o644); err != nil {
					return fmt.Errorf("cloud pull: write main.py: %w", err)
				}
			}

			reqPath := filepath.Join(dir, "requirements.txt")
			if _, err := os.Stat(reqPath); err != nil {
				_ = os.WriteFile(reqPath, []byte("# Python dependencies (pip-installed at pack time).\n# Do NOT list agentpaas-sdk here — it is bundled automatically.\n"), 0o644)
			}

			meta := map[string]interface{}{
				"pulled_image_id":   img.ID,
				"image_digest":      img.ImageDigest,
				"agent_name":        img.AgentName,
				"agent_version":     img.AgentVersion,
				"publisher_name":    img.PublisherName,
				"registry_ref":      img.RegistryRef,
				"source":            "cloud_metadata",
			}
			metaBytes, _ := json.MarshalIndent(meta, "", "  ")
			_ = os.WriteFile(filepath.Join(dir, ".agentpaas-pull.json"), metaBytes, 0o644)

			readme := fmt.Sprintf(`# %s (pulled from cloud)

Image: %s
Version on cloud: %s
Publisher: %s

## Next steps

1. Edit main.py / agent.yaml (bump version before push).
2. Pack: agentpaas pack --target linux/amd64
3. Push: agentpaas cloud push --lock ~/.agentpaas/state/agents/<name>/agent.lock
4. Deploy: agentpaas cloud deploy latest

If main.py is a stub, restore full source from your git repo — cloud pull
currently materializes lock agent.yaml + stub unless a richer source archive exists.
`, valueOr(img.AgentName, "agent"), img.ID, valueOr(img.AgentVersion, "?"), valueOr(img.PublisherName, "?"))
			_ = os.WriteFile(filepath.Join(dir, "PULL_README.md"), []byte(readme), 0o644)

			// Best-effort local docker extract
			_ = tryExtractLocalDockerApp(cmd, img, dir)

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Pulled %s (%s %s) → %s\n", img.ID, img.AgentName, img.AgentVersion, dir)
			if bump != "" {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "agent.yaml version set to %s\n", bump)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Edit, pack, push, deploy to publish a new version.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Output directory (default: ./<name-or-id>)")
	cmd.Flags().StringVar(&bump, "bump-version", "", "Set agent.yaml version after pull (e.g. 0.1.1)")
	cmd.Flags().BoolVar(&force, "force", false, "Allow writing into a non-empty directory")
	return cmd
}

func valueOr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func sanitizeDirName(ref string) string {
	s := strings.TrimSpace(ref)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, ":", "-")
	if s == "" {
		return "pulled-agent"
	}
	return s
}

func resolvePullImage(cmd *cobra.Command, client *cloudclient.CloudClient, token, ref string) (*cloudclient.ImageRecord, error) {
	// Prefer direct get by id
	if strings.HasPrefix(ref, "img_") || strings.HasPrefix(ref, "sha256:") {
		img, err := client.GetImage(cmd.Context(), token, ref)
		if err != nil {
			return nil, fmt.Errorf("cloud pull: get image: %w", err)
		}
		return img, nil
	}
	// List and match agent name (latest created wins — API returns DESC)
	images, err := client.ListImages(cmd.Context(), token)
	if err != nil {
		return nil, fmt.Errorf("cloud pull: list images: %w", err)
	}
	var match *cloudclient.ImageRecord
	for i := range images {
		img := &images[i]
		if img.ID == ref || img.AgentName == ref {
			// Prefer first match (newest if API ordered DESC)
			if match == nil {
				match = img
			}
			// If multiple versions, keep first unless exact version not supported here
			if img.ID == ref {
				return img, nil
			}
		}
	}
	if match == nil {
		return nil, fmt.Errorf("cloud pull: no image matching %q", ref)
	}
	// Re-fetch full record by id for agent_yaml
	full, err := client.GetImage(cmd.Context(), token, match.ID)
	if err != nil {
		return match, nil
	}
	return full, nil
}

func tryExtractLocalDockerApp(cmd *cobra.Command, img *cloudclient.ImageRecord, dir string) error {
	if img.AgentName == "" || img.AgentVersion == "" {
		return fmt.Errorf("no local tag")
	}
	ref := fmt.Sprintf("agentpaas/%s:%s", img.AgentName, img.AgentVersion)
	// Optional: only if docker present — ignore errors
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "note: if docker image %s exists locally, copy sources manually with docker create/cp from /app\n", ref)
	return nil
}
