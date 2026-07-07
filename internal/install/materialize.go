package install

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/bundle"
	"github.com/AgentPaaS-ai/agentpaas/internal/pack"
	"github.com/AgentPaaS-ai/agentpaas/internal/trust"
)

// ErrMaterializeFailed is returned when install materialization fails (state rolled back).
var ErrMaterializeFailed = errors.New("install materialization failed")

// ErrDepsUnlockedRefused is returned when uv.lock is missing and operator did not allow unlocked deps.
var ErrDepsUnlockedRefused = errors.New("missing uv.lock requires --allow-unlocked-deps in non-interactive mode")

// MaterializeOpts configures state materialization + image acquisition.
type MaterializeOpts struct {
	StateRoot string
	Bundle    *bundle.Bundle
	BundlePath string
	BundleDigest string
	Manifest  InstallManifest

	PreferImage       bool
	AllowUnlockedDeps bool
	IsTTY             bool
	PrintWarn         func(msg string)
	PromptUnlocked    func(prompt string) (string, error)

	Builder ImageBuilder
	Loader  ImageLoader

	// PostVerifyHook runs after files are written but before atomic rename (tests).
	PostWriteHook func(stagingDir string) error

	Audit audit.AuditAppender
}

// MaterializeResult holds the final installed path and manifest written on disk.
type MaterializeResult struct {
	AgentRef      string
	InstalledPath string
	Manifest      InstallManifest
}

// MaterializeInstall writes ~/.agentpaas/state/agents/<name>@<pub8>/ atomically.
func MaterializeInstall(ctx context.Context, opts MaterializeOpts) (*MaterializeResult, error) {
	if opts.Bundle == nil || opts.Bundle.Lock == nil {
		return nil, fmt.Errorf("materialize requires verified bundle")
	}
	if opts.StateRoot == "" {
		return nil, fmt.Errorf("materialize requires StateRoot")
	}
	lock := opts.Bundle.Lock
	manifest := opts.Manifest
	manifest.PublisherFingerprint = trust.NormalizeFingerprint(manifest.PublisherFingerprint)
	if manifest.AgentName == "" {
		manifest.AgentName = lock.AgentName
	}
	if manifest.AgentVersion == "" {
		manifest.AgentVersion = lock.AgentVersion
	}
	ref, err := InstalledAgentRefDirName(manifest.AgentName, manifest.PublisherFingerprint)
	if err != nil {
		return nil, err
	}
	if err := CheckAliasUnique(opts.StateRoot, manifest.Alias, ref); err != nil {
		return nil, err
	}
	finalDir, err := InstalledAgentPath(opts.StateRoot, manifest.AgentName, manifest.PublisherFingerprint)
	if err != nil {
		return nil, err
	}
	agentsParent, err := ensureAgentsParent(opts.StateRoot)
	if err != nil {
		return nil, err
	}

	stagingDir, err := os.MkdirTemp(agentsParent, ".tmp-"+ref+"-*")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(stagingDir)
		}
	}()

	if err := writeInstalledTree(ctx, stagingDir, opts, lock, &manifest); err != nil {
		return nil, err
	}
	if opts.PostWriteHook != nil {
		if err := opts.PostWriteHook(stagingDir); err != nil {
			_ = rollbackInstalled(finalDir, stagingDir)
			return nil, fmt.Errorf("%w: post-write hook: %v — please file a bug report at https://github.com/AgentPaaS-ai/agentpaas/issues", ErrMaterializeFailed, err)
		}
	}
	if verr := verifyInstalledAtDir(stagingDir, opts.Audit, manifest.AgentName); verr != nil {
		_ = os.RemoveAll(stagingDir)
		stagingActive = false
		return nil, fmt.Errorf("%w: %v — please file a bug report at https://github.com/AgentPaaS-ai/agentpaas/issues", ErrMaterializeFailed, verr)
	}
	if err := atomicReplaceInstalledDir(stagingDir, finalDir); err != nil {
		return nil, err
	}
	stagingActive = false
	return &MaterializeResult{
		AgentRef:      ref,
		InstalledPath: finalDir,
		Manifest:      manifest,
	}, nil
}

func writeInstalledTree(ctx context.Context, stagingDir string, opts MaterializeOpts, lock *pack.AgentLock, manifest *InstallManifest) error {
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return err
	}
	lockBytes := opts.Bundle.LockJSON
	if len(lockBytes) == 0 {
		var err error
		lockBytes, err = pack.LockfileCanonicalJSON(lock)
		if err != nil {
			return fmt.Errorf("marshal lock: %w", err)
		}
	}
	if err := writeInstalledFile(stagingDir, installedLockName, lockBytes, 0o600); err != nil {
		return err
	}
	if err := writeInstalledFile(stagingDir, installedPolicyName, opts.Bundle.PolicyYAML, 0o600); err != nil {
		return err
	}
	if err := writeInstalledFile(stagingDir, installedSBOMName, opts.Bundle.SBOM, 0o600); err != nil {
		return err
	}
	sourceDir := filepath.Join(stagingDir, installedSourceDir)
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		return err
	}
	if err := opts.Bundle.ExtractSource(sourceDir); err != nil {
		return fmt.Errorf("extract source: %w", err)
	}
	parent := ParentBundleRef{Digest: opts.BundleDigest, Path: opts.BundlePath}
	parentRaw, _ := json.MarshalIndent(parent, "", "  ")
	if err := writeInstalledFile(stagingDir, installedParentBundleRef, parentRaw, 0o600); err != nil {
		return err
	}
	manifest.ParentBundleRef = &parent
	manifest.InstalledAt = time.Now().UTC()

	digest, mode, depsUnlocked, err := acquireImage(ctx, stagingDir, opts, lock)
	if err != nil {
		return err
	}
	manifest.InstallMode = mode
	manifest.LocalImageDigest = digest
	manifest.DepsUnlockedRebuild = depsUnlocked

	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writeInstalledFile(stagingDir, installedManifestName, manifestRaw, 0o600); err != nil {
		return err
	}
	digestLine := []byte(normalizeImageDigest(digest) + "\n")
	if err := writeInstalledFile(stagingDir, installedLocalImageDigestFile, digestLine, 0o600); err != nil {
		return err
	}
	return enforceInstalledPerms(stagingDir)
}

func acquireImage(ctx context.Context, stagingDir string, opts MaterializeOpts, lock *pack.AgentLock) (digest, mode string, depsUnlocked bool, err error) {
	sourceDir := filepath.Join(stagingDir, installedSourceDir)
	if opts.PreferImage {
		if opts.Bundle.Manifest == nil || opts.Bundle.Manifest.Contents.Image == nil {
			return "", "", false, fmt.Errorf("bundle has no prebuilt image; omit --prefer-image to rebuild")
		}
		img := opts.Bundle.Manifest.Contents.Image
		if err := CheckPrebuiltPlatform(img.Platform); err != nil {
			return "", "", false, err
		}
		if opts.Loader == nil {
			return "", "", false, fmt.Errorf("image loader not configured")
		}
		tmpOCI, err := os.MkdirTemp("", "agentpaas-bundle-image-*")
		if err != nil {
			return "", "", false, err
		}
		defer func() { _ = os.RemoveAll(tmpOCI) }()
		if err := opts.Bundle.ExtractImage(tmpOCI); err != nil {
			return "", "", false, fmt.Errorf("extract image: %w", err)
		}
		want := img.Digest
		if want == "" {
			want = lock.ImageDigest
		}
		got, err := opts.Loader.Load(ctx, tmpOCI, want)
		if err != nil {
			return "", "", false, err
		}
		return normalizeImageDigest(got), "prebuilt-image", false, nil
	}
	if !SourceHasUVLock(sourceDir) {
		depsUnlocked = true
		if err := warnMissingUVLock(opts); err != nil {
			return "", "", false, err
		}
	}
	if opts.Builder == nil {
		return "", "", false, fmt.Errorf("image builder not configured")
	}
	got, err := opts.Builder.Build(ctx, sourceDir, lock.AgentName)
	if err != nil {
		return "", "", false, fmt.Errorf("image build: %w", err)
	}
	return normalizeImageDigest(got), "local-rebuild", depsUnlocked, nil
}

func warnMissingUVLock(opts MaterializeOpts) error {
	msg := "WARNING: uv.lock missing in bundle source; rebuild uses unlocked dependencies (deps_unlocked_rebuild=true)"
	if opts.PrintWarn != nil {
		opts.PrintWarn(msg)
	}
	if opts.IsTTY {
		if opts.PromptUnlocked == nil {
			return ErrDepsUnlockedRefused
		}
		resp, err := opts.PromptUnlocked(msg + "\nContinue without locked deps? [type 'yes']: ")
		if err != nil {
			return err
		}
		if !strings.EqualFold(strings.TrimSpace(resp), "yes") {
			return ErrDepsUnlockedRefused
		}
		return nil
	}
	if !opts.AllowUnlockedDeps {
		return ErrDepsUnlockedRefused
	}
	return nil
}

func writeInstalledFile(dir, name string, data []byte, mode os.FileMode) error {
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

func enforceInstalledPerms(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.Chmod(path, 0o700)
		}
		return os.Chmod(path, 0o600)
	})
}

func atomicReplaceInstalledDir(stagingDir, finalDir string) error {
	if err := os.RemoveAll(finalDir); err != nil && !os.IsNotExist(err) {
		trash := finalDir + ".trash-" + fmt.Sprintf("%d", time.Now().UnixNano())
		if rerr := os.Rename(finalDir, trash); rerr == nil {
			_ = os.RemoveAll(trash)
		} else {
			return fmt.Errorf("replace installed dir: %w", err)
		}
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		return fmt.Errorf("rename staged install: %w", err)
	}
	return nil
}

func rollbackInstalled(finalDir, stagingDir string) error {
	_ = os.RemoveAll(stagingDir)
	_ = os.RemoveAll(finalDir)
	return nil
}

