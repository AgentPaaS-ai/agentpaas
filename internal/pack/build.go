package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/build"
	"github.com/AgentPaaS-ai/agentpaas/internal/dockerclient"
)

const (
	defaultPythonBaseImage = "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
	defaultBuilderImage    = "python:3.11-slim"
	defaultNonRootUID      = 64000
)

// BuildConfig controls the image build process.
type BuildConfig struct {
	// ProjectDir is the agent project directory to build.
	ProjectDir string
	// Runtime is the detected/explicit runtime type.
	Runtime RuntimeType
	// BaseImage is the distroless base image ref (digest-pinned).
	// Default: "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
	BaseImage string
	// BuilderImage is the image used to install Python deps in a multi-stage build.
	// Default: "python:3.11-slim"
	BuilderImage string
	// HarnessPath is the path to the pre-built harness binary to embed as PID 1.
	// If empty, uses the standard harness binary location.
	HarnessPath string
	// SDKDir is the path to the Python SDK directory to embed. If empty, uses the standard location.
	SDKDir string
	// SourceDateEpoch is the fixed timestamp for reproducible builds.
	// Default: time.Unix(0, 0) (epoch).
	SourceDateEpoch time.Time
	// NonRootUID is the uid for the non-root user. Default: 64000.
	NonRootUID int
	// ImageTag is the tag for the built image (e.g. "agentpaas/myagent:0.1.0").
	ImageTag string
}

// BuildResult holds the outcome of an image build.
type BuildResult struct {
	ImageDigest string    `json:"image_digest"`
	ImageRef    string    `json:"image_ref"`
	BuildTime   time.Time `json:"build_time"`
	// BuildInputDigest is the SHA-256 over the canonical build context
	// (sorted file list + file contents). Same input -> same digest -> same image.
	BuildInputDigest string `json:"build_input_digest"`
	// DepsLocked is the resolved/locked dependency list from uv.
	DepsLocked []string `json:"deps_locked"`
}

// BuildFile represents a collected file from the build context.
type BuildFile struct {
	RelPath string
	AbsPath string
	Info    fs.FileInfo
}

// BuildImage builds a deterministic OCI image for the agent project.
func BuildImage(ctx context.Context, cfg BuildConfig) (*BuildResult, error) {
	if err := validateBuildConfig(&cfg); err != nil {
		return nil, err
	}

	// Enforce LLM egress policy at pack time.
	// If the agent has an LLM provider configured, the provider's domain
	// MUST be present in the egress policy. Otherwise, the agent will fail
	// at runtime when it tries to call the LLM API through the gateway.
	agentConfig, err := LoadAgentYAML(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	policyFile, err := LoadPolicy(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	if err := ValidateLLMEgress(agentConfig, policyFile); err != nil {
		return nil, err
	}

	ignore, err := LoadIgnore(cfg.ProjectDir)
	if err != nil {
		return nil, err
	}

	inputDigest, err := ComputeBuildInputDigest(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, err
	}

	deps, err := ResolveDependencies(ctx, cfg.ProjectDir, cfg.Runtime)
	if err != nil {
		return nil, err
	}

	buildCtx, err := createDockerBuildContext(cfg, ignore, deps)
	if err != nil {
		return nil, err
	}

	cli, err := dockerclient.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = cli.Close() }()

	buildResp, err := cli.ImageBuild(ctx, buildCtx, build.ImageBuildOptions{
		Tags:       []string{cfg.ImageTag},
		Remove:     true,
		NoCache:    true,
		PullParent: true,
		Labels: map[string]string{
			"org.opencontainers.image.created":     cfg.SourceDateEpoch.UTC().Format(time.RFC3339),
			"org.agentpaas.build_input_digest":     inputDigest,
			"org.agentpaas.source_date_epoch":      fmt.Sprint(cfg.SourceDateEpoch.Unix()),
			"org.opencontainers.image.base.name":   cfg.BaseImage,
			"org.opencontainers.image.revision":    inputDigest,
			"org.opencontainers.image.description": "AgentPaaS Python agent image",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build image: %w", err)
	}
	defer func() { _ = buildResp.Body.Close() }()

	// Read and check the build output stream. Docker ImageBuild returns a
	// streaming JSON response where each line is either {"stream":"..."} for
	// progress or {"errorDetail":{"message":"..."}} for build failures.
	// Discarding it (io.Copy to io.Discard) silently swallows build errors,
	// causing ImageInspect to fail with "no such image" or, worse, return
	// a stale image's digest if one exists with the same tag.
	dec := json.NewDecoder(buildResp.Body)
	for {
		var msg struct {
			Stream      string          `json:"stream,omitempty"`
			ErrorDetail json.RawMessage `json:"errorDetail,omitempty"`
			Error       string          `json:"error,omitempty"`
		}
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("build cancelled (context deadline): %w", err)
			}
			return nil, fmt.Errorf("read build output: %w", err)
		}
		if msg.Error != "" {
			return nil, fmt.Errorf("docker build failed: %s", msg.Error)
		}
		if len(msg.ErrorDetail) > 0 && string(msg.ErrorDetail) != "null" {
			return nil, fmt.Errorf("docker build failed: %s", string(msg.ErrorDetail))
		}
	}

	inspect, err := cli.ImageInspect(ctx, cfg.ImageTag)
	if err != nil {
		return nil, fmt.Errorf("inspect built image %q: %w", cfg.ImageTag, err)
	}

	return &BuildResult{
		ImageDigest:      imageDigest(inspect.ID, inspect.RepoDigests),
		ImageRef:         cfg.ImageTag,
		BuildTime:        cfg.SourceDateEpoch.UTC(),
		BuildInputDigest: inputDigest,
		DepsLocked:       deps,
	}, nil
}

// ComputeBuildInputDigest computes SHA-256 over the canonical build context.
// Canonical = sorted file paths (relative to ProjectDir, forward slashes) +
// each file's content. Respects .agentpaasignore exclusions.
// Symlink-safe: uses os.Lstat, rejects symlinks.
func ComputeBuildInputDigest(projectDir string, ignore *IgnoreMatcher) (string, error) {
	files, err := CollectBuildFiles(projectDir, ignore)
	if err != nil {
		return "", err
	}
	return ComputeBuildInputDigestFromFiles(files)
}

// ComputeBuildInputDigestFromFiles computes SHA-256 over the canonical build
// context represented by sorted BuildFile entries (path, size, content).
// Shared by pack and bundle verification — do not duplicate this logic.
func ComputeBuildInputDigestFromFiles(files []BuildFile) (string, error) {
	h := sha256.New()
	for _, file := range files {
		if _, err := io.WriteString(h, file.RelPath); err != nil {
			return "", fmt.Errorf("hash path %s: %w", file.RelPath, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash separator: %w", err)
		}
		if _, err := io.WriteString(h, fmt.Sprint(file.Info.Size())); err != nil {
			return "", fmt.Errorf("hash size %s: %w", file.RelPath, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash separator: %w", err)
		}

		data, err := readProjectFile(file.AbsPath)
		if err != nil {
			return "", err
		}
		if _, err := h.Write(data); err != nil {
			return "", fmt.Errorf("hash content %s: %w", file.RelPath, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash separator: %w", err)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// ResolveDependencies runs uv to lock dependencies.
// For requirements.txt: `uv pip compile requirements.txt -o /tmp/locked.txt`
// For pyproject.toml: `uv lock`
// Returns the list of locked package@version strings.
// Returns error with verbatim uv output on conflict.
func ResolveDependencies(ctx context.Context, projectDir string, runtime RuntimeType) ([]string, error) {
	_ = runtime
	if err := validateProjectDir(projectDir); err != nil {
		return nil, err
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return nil, err
	}

	reqPath := filepath.Join(projectDir, "requirements.txt")
	if err := rejectSymlinkPath(reqPath, false); err == nil {
		return resolveRequirements(ctx, projectDir, reqPath)
	} else if !errors.Is(unwrappedErr(err), fs.ErrNotExist) {
		return nil, err
	}

	pyprojectPath := filepath.Join(projectDir, "pyproject.toml")
	if err := rejectSymlinkPath(pyprojectPath, false); err == nil {
		return resolvePyproject(ctx, projectDir)
	} else if !errors.Is(unwrappedErr(err), fs.ErrNotExist) {
		return nil, err
	}

	return nil, nil
}

// CreateBuildContext creates a deterministic tar reader of the build context.
// Files are added in sorted path order. Uses ignore matcher to exclude files.
// Symlink-safe.
func CreateBuildContext(projectDir string, ignore *IgnoreMatcher) (io.Reader, error) {
	buf := new(bytes.Buffer)
	if err := writeProjectFilesToTar(buf, projectDir, "", ignore, time.Unix(0, 0)); err != nil {
		return nil, err
	}

	return bytes.NewReader(buf.Bytes()), nil
}

// defaultBaseImage returns the default distroless base for the runtime.
func defaultBaseImage(runtime RuntimeType) string {
	switch runtime {
	case RuntimePython, RuntimeLangGraph, RuntimeCrewAI:
		return defaultPythonBaseImage
	default:
		return defaultPythonBaseImage
	}
}

func validateBuildConfig(cfg *BuildConfig) error {
	if cfg == nil {
		return errors.New("build config is required")
	}
	if err := validateProjectDir(cfg.ProjectDir); err != nil {
		return err
	}
	if !filepath.IsAbs(cfg.ProjectDir) {
		return fmt.Errorf("project directory must be absolute: %s", cfg.ProjectDir)
	}
	if err := rejectSymlinkPath(cfg.ProjectDir, false); err != nil {
		return err
	}
	if cfg.Runtime == "" {
		cfg.Runtime = RuntimePython
	}
	if cfg.BaseImage == "" {
		cfg.BaseImage = defaultBaseImage(cfg.Runtime)
	}
	if cfg.BuilderImage == "" {
		cfg.BuilderImage = defaultBuilderImage
	}
	if cfg.SourceDateEpoch.IsZero() {
		cfg.SourceDateEpoch = time.Unix(0, 0)
	}
	cfg.SourceDateEpoch = cfg.SourceDateEpoch.UTC()
	if cfg.NonRootUID == 0 {
		cfg.NonRootUID = defaultNonRootUID
	}
	if cfg.NonRootUID < 1 {
		return fmt.Errorf("non-root uid must be positive: %d", cfg.NonRootUID)
	}
	if strings.TrimSpace(cfg.ImageTag) == "" {
		return errors.New("image tag is required")
	}
	if cfg.HarnessPath == "" {
		harnessPath, err := exec.LookPath("agentpaas-harness")
		if err != nil {
			return fmt.Errorf("harness path is required: %w", err)
		}
		cfg.HarnessPath = harnessPath
	}
	if !filepath.IsAbs(cfg.HarnessPath) {
		absPath, err := filepath.Abs(cfg.HarnessPath)
		if err != nil {
			return fmt.Errorf("resolve harness path %s: %w", cfg.HarnessPath, err)
		}
		cfg.HarnessPath = absPath
	}
	if err := rejectSymlinkPath(cfg.HarnessPath, false); err != nil {
		return err
	}
	if cfg.SDKDir == "" {
		harnessDir := filepath.Dir(cfg.HarnessPath)
		candidate := filepath.Join(filepath.Dir(harnessDir), "python")
		if info, err := os.Stat(filepath.Join(candidate, "agentpaas_sdk")); err == nil && info.IsDir() {
			cfg.SDKDir = candidate
		}
	}
	if cfg.SDKDir != "" {
		if err := rejectSymlinkPath(cfg.SDKDir, false); err != nil {
			return err
		}
	}

	return nil
}

func resolveRequirements(ctx context.Context, projectDir string, reqPath string) ([]string, error) {
	lockedFile, err := os.CreateTemp("", "agentpaas-locked-*.txt")
	if err != nil {
		return nil, fmt.Errorf("create locked requirements file: %w", err)
	}
	lockedPath := lockedFile.Name()
	defer func() { _ = lockedFile.Close() }()
	defer func() { _ = os.Remove(lockedPath) }()

	if err := rejectSymlinkPath(reqPath, false); err != nil {
		return nil, err
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "uv", "pip", "compile", reqPath, "-o", lockedPath)
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv pip compile failed:\n%s", string(output))
	}
	if err := cmdCtx.Err(); err != nil {
		return nil, fmt.Errorf("uv pip compile failed: %w\n%s", err, string(output))
	}

	data, err := readProjectFile(lockedPath)
	if err != nil {
		return nil, err
	}

	return parseLockedRequirements(data), nil
}

func resolvePyproject(ctx context.Context, projectDir string) ([]string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, "uv", "lock")
	cmd.Dir = projectDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("uv lock failed:\n%s", string(output))
	}
	if err := cmdCtx.Err(); err != nil {
		return nil, fmt.Errorf("uv lock failed: %w\n%s", err, string(output))
	}

	lockPath := filepath.Join(projectDir, "uv.lock")
	data, err := readProjectFile(lockPath)
	if err != nil {
		return nil, err
	}

	return parseUVLock(data), nil
}

func parseLockedRequirements(data []byte) []string {
	var deps []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		name, version, ok := strings.Cut(line, "==")
		if !ok {
			continue
		}
		version = strings.Fields(version)[0]
		deps = append(deps, strings.ToLower(strings.TrimSpace(name))+"@"+version)
	}
	sort.Strings(deps)

	return deps
}

func parseUVLock(data []byte) []string {
	var deps []string
	var name string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name = ") {
			name = trimTOMLString(strings.TrimPrefix(line, "name = "))
			continue
		}
		if name == "" || !strings.HasPrefix(line, "version = ") {
			continue
		}
		version := trimTOMLString(strings.TrimPrefix(line, "version = "))
		if version != "" {
			deps = append(deps, strings.ToLower(name)+"@"+version)
		}
		name = ""
	}
	sort.Strings(deps)

	return deps
}

func trimTOMLString(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"`)
	value = strings.Trim(value, `'`)

	return value
}

// CollectBuildFiles walks projectDir and returns all regular files that are
// not excluded by the ignore matcher. Files are returned sorted by relative path.
// Symlinks are rejected. If ignore is nil, it falls back to LoadIgnore(projectDir).
func CollectBuildFiles(projectDir string, ignore *IgnoreMatcher) ([]BuildFile, error) {
	if err := validateProjectDir(projectDir); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(projectDir) {
		return nil, fmt.Errorf("project directory must be absolute: %s", projectDir)
	}
	if err := rejectSymlinkPath(projectDir, false); err != nil {
		return nil, err
	}
	if ignore == nil {
		var err error
		ignore, err = LoadIgnore(projectDir)
		if err != nil {
			return nil, err
		}
	}

	var files []BuildFile
	err := filepath.WalkDir(projectDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not allowed: %s", path)
		}

		rel, err := safeRelPath(projectDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if ignore.Match(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() || ignore.Match(rel) {
			return nil
		}

		files = append(files, BuildFile{
			RelPath: rel,
			AbsPath: path,
			Info:    info,
		})

		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})

	return files, nil
}

type sdkFile struct {
	absPath string
	relPath string
	info    fs.FileInfo
}

func collectSDKFiles(sdkDir string) ([]sdkFile, error) {
	var files []sdkFile
	err := filepath.WalkDir(sdkDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".pyc") {
			return nil
		}
		relPath, err := filepath.Rel(sdkDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files = append(files, sdkFile{absPath: path, relPath: relPath, info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].relPath < files[j].relPath
	})
	return files, nil
}

func writeProjectFilesToTar(dst io.Writer, projectDir string, prefix string, ignore *IgnoreMatcher, timestamp time.Time) error {
	files, err := CollectBuildFiles(projectDir, ignore)
	if err != nil {
		return err
	}

	tw := tar.NewWriter(dst)
	defer func() { _ = tw.Close() }()
	for _, file := range files {
		if err := addFileToTar(tw, file.AbsPath, filepath.ToSlash(filepath.Join(prefix, file.RelPath)), file.Info, timestamp); err != nil {
			return err
		}
	}

	return nil
}

func createDockerBuildContext(cfg BuildConfig, ignore *IgnoreMatcher, deps []string) (io.Reader, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	defer func() { _ = tw.Close() }()

	if err := addBytesToTar(tw, "Dockerfile", []byte(renderDockerfile(cfg, deps)), 0o644, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}
	// Write the lock file in pip-compatible format (package==version).
	// The deps slice uses package@version internally, but pip install -r
	// requires == format (or >=). Convert back.
	var pipDeps []string
	for _, d := range deps {
		pipDeps = append(pipDeps, strings.Replace(d, "@", "==", 1))
	}
	if err := addBytesToTar(tw, "agentpaas-locked.txt", []byte(strings.Join(pipDeps, "\n")+"\n"), 0o644, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}

	harnessInfo, err := os.Lstat(cfg.HarnessPath)
	if err != nil {
		return nil, fmt.Errorf("inspect harness %s: %w", cfg.HarnessPath, err)
	}
	if harnessInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinks are not allowed: %s", cfg.HarnessPath)
	}
	if err := addFileToTarWithMode(tw, cfg.HarnessPath, "harness", harnessInfo, 0o555, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}

	if cfg.SDKDir != "" {
		sdkFiles, err := collectSDKFiles(cfg.SDKDir)
		if err != nil {
			return nil, fmt.Errorf("collect SDK files: %w", err)
		}
		for _, file := range sdkFiles {
			if err := addFileToTar(tw, file.absPath, filepath.ToSlash(filepath.Join("python", file.relPath)), file.info, cfg.SourceDateEpoch); err != nil {
				return nil, err
			}
		}
	}

	files, err := CollectBuildFiles(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		if err := addFileToTar(tw, file.AbsPath, filepath.ToSlash(filepath.Join("project", file.RelPath)), file.Info, cfg.SourceDateEpoch); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close build context tar: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func renderDockerfile(cfg BuildConfig, deps []string) string {
	var b strings.Builder
	if len(deps) > 0 {
		fmt.Fprintf(&b, "FROM %s AS builder\n", cfg.BuilderImage)
		b.WriteString("WORKDIR /build\n")
		b.WriteString("COPY agentpaas-locked.txt /tmp/requirements.lock\n")
		b.WriteString("RUN pip install --no-cache-dir --target=/build/deps -r /tmp/requirements.lock\n")
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "FROM %s\n", cfg.BaseImage)
	fmt.Fprintf(&b, "ENV SOURCE_DATE_EPOCH=%d\n", cfg.SourceDateEpoch.Unix())
	b.WriteString("WORKDIR /app\n")
	b.WriteString("COPY --chown=0:0 harness /agentpaas/harness\n")
	b.WriteString("COPY --chown=0:0 agentpaas-locked.txt /agentpaas/requirements.lock\n")
	fmt.Fprintf(&b, "COPY --chown=%d:%d project/ /app/\n", cfg.NonRootUID, cfg.NonRootUID)
	fmt.Fprintf(&b, "COPY --chown=%d:%d python/ /app/python/\n", cfg.NonRootUID, cfg.NonRootUID)
	if len(deps) > 0 {
		fmt.Fprintf(&b, "COPY --chown=%d:%d --from=builder /build/deps /app/deps\n", cfg.NonRootUID, cfg.NonRootUID)
		b.WriteString("ENV AGENTPAAS_DEPS_LOCKED=/agentpaas/requirements.lock\n")
		b.WriteString("ENV PYTHONPATH=/app/deps\n")
	}
	fmt.Fprintf(&b, "USER %d:%d\n", cfg.NonRootUID, cfg.NonRootUID)
	b.WriteString("ENTRYPOINT [\"/agentpaas/harness\"]\n")

	return b.String()
}

func addFileToTar(tw *tar.Writer, filePath string, tarPath string, info fs.FileInfo, timestamp time.Time) error {
	mode := info.Mode().Perm()
	if mode == 0 {
		mode = 0o644
	}

	return addFileToTarWithMode(tw, filePath, tarPath, info, int64(mode), timestamp)
}

func addFileToTarWithMode(tw *tar.Writer, filePath string, tarPath string, info fs.FileInfo, mode int64, timestamp time.Time) error {
	_ = info
	if err := rejectSymlinkPath(filePath, false); err != nil {
		return err
	}
	data, err := readProjectFile(filePath)
	if err != nil {
		return err
	}

	return addBytesToTar(tw, tarPath, data, mode, timestamp)
}

func addBytesToTar(tw *tar.Writer, name string, data []byte, mode int64, timestamp time.Time) error {
	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.ToSlash(name),
		Size:     int64(len(data)),
		Mode:     mode,
		Uid:      0,
		Gid:      0,
		ModTime:  timestamp.UTC(),
		Format:   tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar content %s: %w", name, err)
	}

	return nil
}

func safeRelPath(projectDir string, path string) (string, error) {
	rel, err := filepath.Rel(projectDir, path)
	if err != nil {
		return "", fmt.Errorf("resolve relative path %s: %w", path, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes project directory: %s", path)
	}
	if strings.Contains(filepath.ToSlash(rel), "../") {
		return "", fmt.Errorf("path traversal is not allowed: %s", rel)
	}

	return filepath.ToSlash(rel), nil
}

func imageDigest(id string, repoDigests []string) string {
	if len(repoDigests) > 0 {
		_, digest, ok := strings.Cut(repoDigests[0], "@")
		if ok {
			return digest
		}
	}

	return strings.TrimPrefix(id, "sha256:")
}

func unwrappedErr(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
