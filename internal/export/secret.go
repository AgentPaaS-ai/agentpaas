package export

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

func runSecretGate(ctx context.Context, st *exportState) error {
	tmp, err := os.MkdirTemp("", "agentpaas-export-scan-*")
	if err != nil {
		return fmt.Errorf("create scan temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }() // best-effort remove

	for _, f := range st.sourceFiles {
		if err := copyExportFile(tmp, "source", f); err != nil {
			return fmt.Errorf("run secret gate: %w", err)
		}
	}
	for _, f := range st.extraFiles {
		if err := copyExportFile(tmp, "extra", f); err != nil {
			return fmt.Errorf("run secret gate: %w", err)
		}
	}

	for _, f := range st.sourceFiles {
		if err := scanSentinel(f.AbsPath); err != nil {
			return fmt.Errorf("run secret gate: %w", err)
		}
	}
	for _, f := range st.extraFiles {
		if err := scanSentinel(f.AbsPath); err != nil {
			return fmt.Errorf("run secret gate: %w", err)
		}
	}

	findings, err := pack.ScanDirectorySecrets(ctx, tmp)
	if err != nil {
		return fmt.Errorf("secret scan failed: %w", err)
	}
	if len(findings) > 0 {
		f := findings[0]
		return fmt.Errorf("export blocked: secret detected in %s:%d (%s)", f.File, f.Line, f.Rule)
	}
	return nil
}

func copyExportFile(tmp, prefix string, f pack.BuildFile) error {
	dest := filepath.Join(tmp, prefix, filepath.FromSlash(f.RelPath))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("copy export file: %w", err)
	}
	data, err := os.ReadFile(f.AbsPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", f.RelPath, err)
	}
	if err := os.WriteFile(dest, data, f.Info.Mode().Perm()); err != nil {
		return fmt.Errorf("stage %s: %w", f.RelPath, err)
	}
	return nil
}

func scanSentinel(absPath string) error {
	info, err := os.Lstat(absPath)
	if err != nil {
		return fmt.Errorf("scan sentinel: %w", err)
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("export blocked: symlink %s", absPath)
	}
	if info.IsDir() {
		return nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("scan sentinel: %w", err)
	}
	if pack.ContainsExportSentinel(data) {
		return fmt.Errorf("export blocked: sentinel secret in %s", filepath.Base(absPath))
	}
	return nil
}
