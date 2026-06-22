package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/docker/docker/client"
)

const (
	defaultPythonBaseImage = "gcr.io/distroless/python3-debian12@sha256:2fdb05402a2cf21cf78fdb3ba4c5db167241e9e498140f5bf689d7efb773731f"
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
	// HarnessPath is the path to the pre-built harness binary to embed as PID 1.
	// If empty, uses the standard harness binary location.
	HarnessPath string
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

type buildFile struct {
	relPath string
	absPath string
	info    fs.FileInfo
}

// BuildImage builds a deterministic OCI image for the agent project.
func BuildImage(ctx context.Context, cfg BuildConfig) (*BuildResult, error) {
	if err := validateBuildConfig(&cfg); err != nil {
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

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = cli.Close() }()

	sourceDateEpoch := fmt.Sprint(cfg.SourceDateEpoch.Unix())
	buildResp, err := cli.ImageBuild(ctx, buildCtx, build.ImageBuildOptions{
		Tags:       []string{cfg.ImageTag},
		Remove:     true,
		NoCache:    true,
		PullParent: true,
		BuildArgs: map[string]*string{
			"SOURCE_DATE_EPOCH": &sourceDateEpoch,
		},
		Version: build.BuilderBuildKit,
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
	if _, err := io.Copy(io.Discard, buildResp.Body); err != nil {
		return nil, fmt.Errorf("drain build output: %w", err)
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
	files, err := collectBuildFiles(projectDir, ignore)
	if err != nil {
		return "", err
	}

	h := sha256.New()
	for _, file := range files {
		if _, err := io.WriteString(h, file.relPath); err != nil {
			return "", fmt.Errorf("hash path %s: %w", file.relPath, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash separator: %w", err)
		}
		if _, err := io.WriteString(h, fmt.Sprint(file.info.Size())); err != nil {
			return "", fmt.Errorf("hash size %s: %w", file.relPath, err)
		}
		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash separator: %w", err)
		}

		data, err := readProjectFile(file.absPath)
		if err != nil {
			return "", err
		}
		if _, err := h.Write(data); err != nil {
			return "", fmt.Errorf("hash content %s: %w", file.relPath, err)
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

func collectBuildFiles(projectDir string, ignore *IgnoreMatcher) ([]buildFile, error) {
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

	var files []buildFile
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

		files = append(files, buildFile{
			relPath: rel,
			absPath: path,
			info:    info,
		})

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
	files, err := collectBuildFiles(projectDir, ignore)
	if err != nil {
		return err
	}

	tw := tar.NewWriter(dst)
	defer func() { _ = tw.Close() }()
	for _, file := range files {
		if err := addFileToTar(tw, file.absPath, filepath.ToSlash(filepath.Join(prefix, file.relPath)), file.info, timestamp); err != nil {
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
	rootfs, err := createRootfsArchive(cfg, ignore, deps)
	if err != nil {
		return nil, err
	}
	if err := addBytesToTar(tw, "rootfs.tar", rootfs, 0o644, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close build context tar: %w", err)
	}

	return bytes.NewReader(buf.Bytes()), nil
}

func createRootfsArchive(cfg BuildConfig, ignore *IgnoreMatcher, deps []string) ([]byte, error) {
	buf := new(bytes.Buffer)
	tw := tar.NewWriter(buf)
	defer func() { _ = tw.Close() }()

	if err := addDirToTar(tw, "agentpaas", 0o755, 0, 0, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}
	if err := addDirToTar(tw, "app", 0o755, 0, 0, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}

	harnessInfo, err := os.Lstat(cfg.HarnessPath)
	if err != nil {
		return nil, fmt.Errorf("inspect harness %s: %w", cfg.HarnessPath, err)
	}
	if harnessInfo.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlinks are not allowed: %s", cfg.HarnessPath)
	}
	if err := addFileToTarWithOwner(tw, cfg.HarnessPath, "agentpaas/harness", harnessInfo, 0o555, 0, 0, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}
	if err := addBytesToTarWithOwner(tw, "agentpaas/requirements.lock", []byte(strings.Join(deps, "\n")+"\n"), 0o644, 0, 0, cfg.SourceDateEpoch); err != nil {
		return nil, err
	}

	files, err := collectBuildFiles(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, err
	}
	dirs := projectDirs(files)
	for _, dir := range dirs {
		if err := addDirToTar(tw, filepath.ToSlash(filepath.Join("app", dir)), 0o755, cfg.NonRootUID, cfg.NonRootUID, cfg.SourceDateEpoch); err != nil {
			return nil, err
		}
	}
	for _, file := range files {
		if err := addFileToTarWithOwner(tw, file.absPath, filepath.ToSlash(filepath.Join("app", file.relPath)), file.info, int64(file.info.Mode().Perm()), cfg.NonRootUID, cfg.NonRootUID, cfg.SourceDateEpoch); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close rootfs tar: %w", err)
	}

	return buf.Bytes(), nil
}

func projectDirs(files []buildFile) []string {
	dirs := make(map[string]struct{})
	for _, file := range files {
		dir := filepath.Dir(file.relPath)
		for dir != "." && dir != string(filepath.Separator) {
			dirs[dir] = struct{}{}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	out := make([]string, 0, len(dirs))
	for dir := range dirs {
		out = append(out, dir)
	}
	sort.Strings(out)

	return out
}

func renderDockerfile(cfg BuildConfig, deps []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s\n", cfg.BaseImage)
	fmt.Fprintf(&b, "ENV SOURCE_DATE_EPOCH=%d\n", cfg.SourceDateEpoch.Unix())
	b.WriteString("ADD rootfs.tar /\n")
	b.WriteString("WORKDIR /app\n")
	if len(deps) > 0 {
		b.WriteString("ENV AGENTPAAS_DEPS_LOCKED=/agentpaas/requirements.lock\n")
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
	return addFileToTarWithOwner(tw, filePath, tarPath, info, mode, 0, 0, timestamp)
}

func addFileToTarWithOwner(tw *tar.Writer, filePath string, tarPath string, info fs.FileInfo, mode int64, uid int, gid int, timestamp time.Time) error {
	_ = info
	if err := rejectSymlinkPath(filePath, false); err != nil {
		return err
	}
	data, err := readProjectFile(filePath)
	if err != nil {
		return err
	}

	return addBytesToTarWithOwner(tw, tarPath, data, mode, uid, gid, timestamp)
}

func addBytesToTar(tw *tar.Writer, name string, data []byte, mode int64, timestamp time.Time) error {
	return addBytesToTarWithOwner(tw, name, data, mode, 0, 0, timestamp)
}

func addBytesToTarWithOwner(tw *tar.Writer, name string, data []byte, mode int64, uid int, gid int, timestamp time.Time) error {
	header := &tar.Header{
		Typeflag: tar.TypeReg,
		Name:     filepath.ToSlash(name),
		Size:     int64(len(data)),
		Mode:     mode,
		Uid:      uid,
		Gid:      gid,
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

func addDirToTar(tw *tar.Writer, name string, mode int64, uid int, gid int, timestamp time.Time) error {
	header := &tar.Header{
		Typeflag: tar.TypeDir,
		Name:     filepath.ToSlash(name) + "/",
		Mode:     mode,
		Uid:      uid,
		Gid:      gid,
		ModTime:  timestamp.UTC(),
		Format:   tar.FormatUSTAR,
	}
	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
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
