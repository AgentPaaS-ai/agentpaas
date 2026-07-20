package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export [project-dir]",
		Short: "Export a signed .agentpaas bundle from a deployed agent",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectDir := "."
			if len(args) > 0 {
				projectDir = args[0]
			}
			absPath, err := resolveCLIProjectPath(projectDir)
			if err != nil {
				return fmt.Errorf("new export cmd: %w", err)
			}

			outPath, _ := cmd.Flags().GetString("output") // cobra flag default on missing
			if outPath == "" {
				agentYAML, err := readAgentYAMLName(absPath)
				if err != nil {
					return fmt.Errorf("new export cmd: %w", err)
				}
				outPath = agentYAML + ".agentpaas"
			}
			outPath, err = filepath.Abs(outPath)
			if err != nil {
				return fmt.Errorf("new export cmd: %w", err)
			}

			withImage, _ := cmd.Flags().GetBool("with-image")      // cobra flag default on missing
			yes, _ := cmd.Flags().GetBool("yes")                   // cobra flag default on missing
			includeRaw, _ := cmd.Flags().GetStringSlice("include") // optional value; zero on miss
			var includes []string
			for _, g := range includeRaw {
				g = strings.TrimSpace(g)
				if g != "" {
					includes = append(includes, g)
				}
			}

			sock, err := socketPath(cmd)
			if err != nil {
				return fmt.Errorf("new export cmd: %w", err)
			}
			client, conn, err := ConnectToDaemon(sock)
			if err != nil {
				return fmt.Errorf("new export cmd: %w", err)
			}
			defer func() { _ = conn.Close() }() // best-effort close

			ctx, cancel := contextWithTimeout(10 * time.Minute)
			defer cancel()

			prev, err := client.ExportPreview(ctx, &controlv1.ExportPreviewRequest{
				AgentProjectPath: absPath,
				IncludeGlobs:     includes,
			})
			if err != nil {
				return fmt.Errorf("export preview failed: %w", err)
			}

			if err := printExportManifest(os.Stdout, prev); err != nil {
				return fmt.Errorf("new export cmd: %w", err)
			}

			if !yes {
				if !isTerminal(os.Stdin) {
					return fmt.Errorf("refusing export without confirmation (use --yes on non-TTY)")
				}
				fmt.Fprintf(os.Stderr, "Export %d files for agent %q? [y/N] ", len(prev.GetFiles()), prev.GetAgentName())
				reader := bufio.NewReader(os.Stdin)
				line, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read confirmation: %w", err)
				}
				line = strings.TrimSpace(strings.ToLower(line))
				if line != "y" && line != "yes" {
					return fmt.Errorf("export cancelled")
				}
			}

			resp, err := client.Export(ctx, &controlv1.ExportRequest{
				AgentProjectPath: absPath,
				OutputPath:       outPath,
				WithImage:        withImage,
				IncludeGlobs:     includes,
				Confirmed:        true,
			})
			if err != nil {
				return fmt.Errorf("export failed: %w", err)
			}

			result := map[string]interface{}{
				"bundle_digest":         resp.GetBundleDigest(),
				"publisher_fingerprint": resp.GetPublisherFingerprint(),
				"file_count":            resp.GetFileCount(),
				"total_bytes":           resp.GetTotalBytes(),
				"output_path":           resp.GetOutputPath(),
			}
			return printTextOrJSON(jsonOutput(cmd), result, func(v interface{}) string {
				m := v.(map[string]interface{})
				return fmt.Sprintf("Bundle written: %s\nDigest: %s\nPublisher: %s\nFiles: %v",
					m["output_path"], m["bundle_digest"], m["publisher_fingerprint"], m["file_count"])
			})
		},
	}
	cmd.Flags().StringP("output", "o", "", "Output .agentpaas path (default: <agent>.agentpaas)")
	cmd.Flags().Bool("with-image", false, "Include locked OCI image layout in bundle")
	cmd.Flags().StringSlice("include", nil, "Additional files to include (digest-pinned as extra_files)")
	cmd.Flags().Bool("yes", false, "Skip interactive confirmation")
	return cmd
}

func printExportManifest(w *os.File, prev *controlv1.ExportPreviewResponse) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "Agent:\t%s@%s\n", prev.GetAgentName(), prev.GetAgentVersion()); err != nil {
		return fmt.Errorf("print export manifest: %w", err)
	}
	if _, err := fmt.Fprintf(tw, "Files:\n"); err != nil {
		return fmt.Errorf("print export manifest: %w", err)
	}
	for _, f := range prev.GetFiles() {
		tag := ""
		if f.GetExtra() {
			tag = " (extra)"
		}
		if _, err := fmt.Fprintf(tw, "  %s\t%s\t%d%s\n", f.GetPath(), f.GetDigest(), f.GetBytes(), tag); err != nil {
			return fmt.Errorf("print export manifest: %w", err)
		}
	}
	return tw.Flush()
}

func readAgentYAMLName(projectDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "agent.yaml"))
	if err != nil {
		return "", fmt.Errorf("read agent.yaml: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:")), nil
		}
	}
	return "", fmt.Errorf("agent.yaml missing name field")
}
