package export

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/hashutil"
	"github.com/AgentPaaS-ai/agentpaas/internal/identity"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
)

// ErrDirtySource is returned when project source no longer matches the locked digest.
var ErrDirtySource = errors.New("source changed since last pack; run agentpaas pack to rebuild")

// ErrNoPublisherIdentity is returned when publisher identity is missing.
var ErrNoPublisherIdentity = errors.New("publisher identity not initialized; run agentpaas identity init")

// ErrV1Lock is returned when the deployed lock is not schema v2 with publisher.
var ErrV1Lock = errors.New("agent was packed without publisher identity; run agentpaas pack after agentpaas identity init")

// FileManifestEntry describes one file that would be written into the bundle.
type FileManifestEntry struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
	Bytes  int64  `json:"bytes"`
	Extra  bool   `json:"extra,omitempty"`
}

// Config controls the export pipeline.
type Config struct {
	Home           string
	ProjectDir     string
	OutputPath     string
	WithImage      bool
	IncludeGlobs   []string
	SkipConfirm    bool
	PublisherStore identity.KeyStore
	Audit          pack.AuditAppender
}

// PreviewResult is returned by Preview before writing a bundle.
type PreviewResult struct {
	AgentName    string
	AgentVersion string
	Files        []FileManifestEntry
}

// Result is returned after a successful export.
type Result struct {
	BundleDigest         string
	PublisherFingerprint string
	FileCount            int
	TotalBytes           int64
	OutputPath           string
}

// Preview validates preconditions and returns the file manifest without writing.
func Preview(ctx context.Context, cfg Config) (*PreviewResult, error) {
	st, err := prepareExportState(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &PreviewResult{
		AgentName:    st.agentName,
		AgentVersion: st.agentVersion,
		Files:        st.manifest,
	}, nil
}

// Run executes the full export pipeline (preconditions, secret gate, bundle write).
func Run(ctx context.Context, cfg Config) (*Result, error) {
	if cfg.OutputPath == "" {
		return nil, errors.New("output path is required")
	}
	st, err := prepareExportState(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := runSecretGate(ctx, st); err != nil {
		return nil, err
	}

	imageDir := ""
	if cfg.WithImage {
		imageDir, err = materializeLockedImage(ctx, cfg.Home, st.agentName, st.lock)
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.RemoveAll(imageDir) }() // best-effort remove
	}

	pubKey, err := loadPublisherPrivateKey(cfg.PublisherStore)
	if err != nil {
		return nil, err
	}

	manifest := buildBundleManifest(st)
	bundleCfg := bundle.BundleConfig{
		ProjectDir:      cfg.ProjectDir,
		Manifest:        manifest,
		Lock:            st.lock,
		PolicyYAML:      st.policyYAML,
		SBOM:            st.sbom,
		ImageDir:        imageDir,
		PublisherKey:    pubKey,
		SourceDateEpoch: st.lock.Reproducibility.SourceDateEpoch,
		Ignore:          st.ignore,
		ExtraFiles:      st.extraFiles,
	}

	outPath, err := filepath.Abs(cfg.OutputPath)
	if err != nil {
		return nil, fmt.Errorf("resolve output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	br, err := bundle.WriteToFile(bundleCfg, outPath)
	if err != nil {
		_ = os.Remove(outPath) // best-effort remove
		_ = os.Remove(outPath + ".tmp") // best-effort remove
		return nil, err
	}

	if cfg.Audit != nil {
		if err := cfg.Audit.Append(audit.AuditRecord{
			Timestamp:      time.Now().UTC().Format(time.RFC3339Nano),
			EventType:      "bundle_exported",
			DeploymentMode: "local",
			Actor:          "cli",
			Payload: map[string]interface{}{
				"agent":                 st.agentName,
				"version":               st.agentVersion,
				"bundle_digest":         br.BundleDigest,
				"publisher_fingerprint": st.lock.Publisher.Fingerprint,
				"file_count":            br.FileCount,
				"with_image":            cfg.WithImage,
			},
		}); err != nil {
			log.Printf("export: audit append (%s): %v", "bundle_exported", err)
		}
	}

	return &Result{
		BundleDigest:         br.BundleDigest,
		PublisherFingerprint: st.lock.Publisher.Fingerprint,
		FileCount:            br.FileCount,
		TotalBytes:           br.TotalBytes,
		OutputPath:           outPath,
	}, nil
}

type exportState struct {
	agentName    string
	agentVersion string
	lock         *pack.AgentLock
	policyYAML   []byte
	sbom         []byte
	ignore       *pack.IgnoreMatcher
	sourceFiles  []pack.BuildFile
	extraFiles   []pack.BuildFile
	manifest     []FileManifestEntry
}

func prepareExportState(ctx context.Context, cfg Config) (*exportState, error) {
	if cfg.Home == "" {
		return nil, errors.New("home path is required")
	}
	absProject, err := filepath.Abs(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	info, err := os.Stat(absProject)
	if err != nil {
		return nil, fmt.Errorf("project directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project path is not a directory: %s", absProject)
	}

	agentYAML, err := pack.LoadAgentYAML(absProject)
	if err != nil {
		return nil, fmt.Errorf("load agent.yaml: %w", err)
	}
	agentName := "default"
	agentVersion := "latest"
	if agentYAML != nil {
		if agentYAML.Name != "" {
			agentName = agentYAML.Name
		}
		if agentYAML.Version != "" {
			agentVersion = agentYAML.Version
		}
	}
	if err := CheckDeniedProjectFiles(absProject); err != nil {
		return nil, err
	}

	lock, err := pack.LoadDeployedLock(cfg.Home, agentName)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent %q is not deployed; run agentpaas pack first", agentName)
		}
		return nil, fmt.Errorf("load deployed lock: %w", err)
	}

	if lock.SchemaVersion < 2 || lock.Publisher == nil {
		return nil, ErrV1Lock
	}
	if cfg.PublisherStore != nil {
		if _, err := identity.LoadPublisherIdentity(cfg.PublisherStore); err != nil {
			if errors.Is(err, identity.ErrNoPublisherIdentity) {
				return nil, ErrNoPublisherIdentity
			}
			return nil, fmt.Errorf("publisher identity: %w", err)
		}
	}

	if err := verifyDeployedForExport(ctx, cfg.Home, agentName); err != nil {
		return nil, err
	}

	ignore, err := ExportIgnoreMatcher(absProject)
	if err != nil {
		return nil, err
	}

	currentDigest, err := pack.ComputeBuildInputDigest(absProject, ignore)
	if err != nil {
		return nil, fmt.Errorf("compute source digest: %w", err)
	}
	if currentDigest != lock.BuildInputDigest {
		return nil, fmt.Errorf("%w (locked %s, current %s); run agentpaas pack to rebuild",
			ErrDirtySource, lock.BuildInputDigest, currentDigest)
	}

	sourceFiles, err := pack.CollectBuildFiles(absProject, ignore)
	if err != nil {
		return nil, fmt.Errorf("collect source files: %w", err)
	}

	extraFiles, err := collectIncludeFiles(absProject, cfg.IncludeGlobs, ignore)
	if err != nil {
		return nil, fmt.Errorf("collect --include files: %w", err)
	}

	manifest := buildFileManifest(sourceFiles, extraFiles)
	for _, e := range manifest {
		if denied, reason := IsDeniedExportPath(strings.TrimPrefix(e.Path, "source/")); denied && !e.Extra {
			return nil, fmt.Errorf("export blocked: %s (%s)", e.Path, reason)
		}
		if e.Extra {
			if denied, reason := IsDeniedExportPath(strings.TrimPrefix(e.Path, "extra/")); denied {
				return nil, fmt.Errorf("export blocked: %s (%s)", e.Path, reason)
			}
		}
	}

	policyYAML, err := os.ReadFile(filepath.Join(absProject, "policy.yaml"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read policy.yaml: %w", err)
	}
	if policyYAML == nil {
		policyYAML = []byte{}
	}

	sbom, err := loadSBOMForExport(cfg.Home, agentName, lock)
	if err != nil {
		return nil, err
	}

	return &exportState{
		agentName:    agentName,
		agentVersion: agentVersion,
		lock:         lock,
		policyYAML:   policyYAML,
		sbom:         sbom,
		ignore:       ignore,
		sourceFiles:  sourceFiles,
		extraFiles:   extraFiles,
		manifest:     manifest,
	}, nil
}

func verifyDeployedForExport(ctx context.Context, home, agentName string) error {
	_ = ctx // unused context; interface compliance
	lock, err := pack.LoadDeployedLock(home, agentName)
	if err != nil {
		return err
	}
	if err := pack.VerifyAgentLock(lock, ""); err != nil {
		return fmt.Errorf("deployed lock invalid: %w", err)
	}
	if err := pack.VerifyLockfileSignature(lock); err != nil {
		return fmt.Errorf("deployed lock signature: %w", err)
	}
	if lock.Publisher != nil {
		if err := pack.VerifyPublisherSignature(lock); err != nil {
			return fmt.Errorf("deployed publisher signature: %w", err)
		}
	}
	deployed, err := pack.LoadDeployedAgent(home, agentName)
	if err != nil {
		return err
	}
	if deployed.ImageDigest != lock.ImageDigest {
		return fmt.Errorf("deployed image digest mismatch: lock has %s, deployed has %s", lock.ImageDigest, deployed.ImageDigest)
	}
	if deployed.SourceDigest != lock.BuildInputDigest {
		return fmt.Errorf("deployed source digest mismatch: lock has %s, deployed has %s", lock.BuildInputDigest, deployed.SourceDigest)
	}
	return nil
}

func loadSBOMForExport(home, agentName string, lock *pack.AgentLock) ([]byte, error) {
	deployedSBOM := filepath.Join(home, "state", "agents", agentName, "sbom.spdx.json")
	if data, err := os.ReadFile(deployedSBOM); err == nil {
		got := sha256Hex(data)
		if lock.SBOMDigest != "" && got != lock.SBOMDigest {
			return nil, fmt.Errorf("deployed sbom digest %s does not match lock %s", got, lock.SBOMDigest)
		}
		return data, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ref := pack.LocalImageRef(agentName, lock.ImageDigest)
	data, _, err := pack.GenerateSBOM(ctx, ref) // intentionally ignored (reviewed)
	if err != nil {
		return nil, fmt.Errorf("generate sbom for export: %w", err)
	}
	got := sha256Hex(data)
	if lock.SBOMDigest != "" && got != lock.SBOMDigest {
		return nil, fmt.Errorf("generated sbom digest %s does not match lock %s", got, lock.SBOMDigest)
	}
	return data, nil
}

func buildFileManifest(source []pack.BuildFile, extra []pack.BuildFile) []FileManifestEntry {
	var out []FileManifestEntry
	for _, f := range source {
		out = append(out, fileEntry("source/"+f.RelPath, f.AbsPath, false))
	}
	for _, f := range extra {
		out = append(out, fileEntry("extra/"+f.RelPath, f.AbsPath, true))
	}
	return out
}

func fileEntry(tarPath, absPath string, extra bool) FileManifestEntry {
	data, _ := os.ReadFile(absPath) // best-effort read; empty on fail
	digest := sha256Hex(data)
	var size int64
	if st, err := os.Stat(absPath); err == nil {
		size = st.Size()
	}
	return FileManifestEntry{
		Path:   filepath.ToSlash(tarPath),
		Digest: digest,
		Bytes:  size,
		Extra:  extra,
	}
}

func buildBundleManifest(st *exportState) *bundle.Manifest {
	extra := make([]bundle.ManifestExtraFile, 0, len(st.extraFiles))
	for _, f := range st.extraFiles {
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		extra = append(extra, bundle.ManifestExtraFile{
			Path:   f.RelPath,
			Digest: sha256Hex(data),
			Bytes:  int64(len(data)),
		})
	}
	pub := st.lock.Publisher
	return &bundle.Manifest{
		BundleSchemaVersion: bundle.BundleSchemaVersion,
		Publisher: bundle.ManifestPublisherInfo{
			Name:         pub.Name,
			Fingerprint:  pub.Fingerprint,
			PublicKeyPEM: pub.PublicKeyPEM,
		},
		Contents: bundle.ManifestContents{
			Lock:   bundle.ManifestDigestEntry{},
			Policy: bundle.ManifestDigestEntry{},
			SBOM:   bundle.ManifestDigestEntry{},
			Source: bundle.ManifestDigestEntry{Digest: st.lock.BuildInputDigest},
		},
		CreatedAt:  st.lock.CreatedAt,
		ExtraFiles: extra,
	}
}

func collectIncludeFiles(projectDir string, globs []string, ignore *pack.IgnoreMatcher) ([]pack.BuildFile, error) {
	if len(globs) == 0 {
		return nil, nil
	}
	var files []pack.BuildFile
	seen := make(map[string]bool)
	for _, g := range globs {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(projectDir, g))
		if err != nil {
			return nil, fmt.Errorf("invalid include glob %q: %w", g, err)
		}
		for _, abs := range matches {
			rel, err := filepath.Rel(projectDir, abs)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if ignore != nil && ignore.Match(rel) {
				continue
			}
			if seen[rel] {
				continue
			}
			info, err := os.Lstat(abs)
			if err != nil {
				return nil, err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				return nil, fmt.Errorf("include path %q is a symlink", rel)
			}
			if info.IsDir() {
				err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if d.IsDir() {
						return nil
					}
					r, err := filepath.Rel(projectDir, path)
					if err != nil {
						return err
					}
					r = filepath.ToSlash(r)
					if ignore != nil && ignore.Match(r) {
						return nil
					}
					if seen[r] {
						return nil
					}
					seen[r] = true
					fi, err := d.Info()
					if err != nil {
						return err
					}
					files = append(files, pack.BuildFile{RelPath: r, AbsPath: path, Info: fi})
					return nil
				})
				if err != nil {
					return nil, err
				}
				continue
			}
			seen[rel] = true
			files = append(files, pack.BuildFile{RelPath: rel, AbsPath: abs, Info: info})
		}
	}
	return files, nil
}

func sha256Hex(data []byte) string {
	return hashutil.SHA256Hex(data)
}
