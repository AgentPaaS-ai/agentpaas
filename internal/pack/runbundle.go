package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/dockerclient"
	"github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const (
	// RunBundleName is the deterministic tar stored beside agent.lock.
	// Written only for an explicit --target linux/amd64 pack.
	RunBundleName = "run.bundle.tar"

	// RuntimeIDPy311R1 is the first runtime id: the Python 3.11 base and
	// the harness contract that loads a run bundle.
	RuntimeIDPy311R1 = "py311-r1"

	linuxAMD64Target = "linux/amd64"

	// Locked requirements file copied into the image by the pack Dockerfile.
	runBundleReqRel = "agentpaas/requirements.lock"

	maxRunBundleFile    = 256 * 1024 * 1024
	maxRunBundleTotal   = 2 * 1024 * 1024 * 1024
	maxRunBundleHeaders = 500_000
)

// RunBundleConfig controls writing the run bundle taken from a built image.
type RunBundleConfig struct {
	// ProjectDir is the agent project the image was built from.
	ProjectDir string
	// Platform is the explicit pack target. Empty means a host-architecture pack,
	// which writes no bundle. Only linux/amd64 writes one.
	Platform string
	// LockPath is the agent.lock path. The tar is written beside it.
	LockPath string
	// ImageRef is the built image tag or digest. Ignored when imageTar is set.
	ImageRef string
	// SourceDateEpoch is the fixed mtime written into every tar header.
	SourceDateEpoch time.Time

	// imageTar is a container-export tar of the built image. Tests set it.
	// Production leaves it nil and exports ImageRef from Docker.
	imageTar io.Reader
}

// runBundleStash carries the tar from BuildImage to CreateAgentLock and
// RecordDeployment in the same process. Host-architecture packs never store one.
var runBundleStash sync.Map

// WriteRunBundle writes a deterministic tar of the image roots beside the lock.
// A host-architecture pack (platform other than linux/amd64) writes nothing.
// The tar is not the .agentpaas export format. Header rules match the hardened
// bundle writer (sorted names, SOURCE_DATE_EPOCH, uid/gid 0) without importing
// internal/bundle, which already imports this package.
func WriteRunBundle(ctx context.Context, cfg RunBundleConfig) error {
	if cfg.Platform != linuxAMD64Target {
		return nil
	}
	data, err := assembleRunBundle(ctx, cfg)
	if err != nil {
		return err
	}
	return writeRunBundleFile(cfg.LockPath, data)
}

func assembleRunBundle(ctx context.Context, cfg RunBundleConfig) ([]byte, error) {
	if err := validateProjectDir(cfg.ProjectDir); err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	if !filepath.IsAbs(cfg.ProjectDir) {
		return nil, fmt.Errorf("write run bundle: project directory must be absolute: %s", cfg.ProjectDir)
	}
	if err := rejectSymlinkPath(cfg.ProjectDir, false); err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	if err := rejectProjectSymlinks(cfg.ProjectDir); err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	ignore, err := LoadIgnore(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	projectFiles, err := CollectBuildFiles(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}

	rc, err := openImageTar(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	defer func() { _ = rc.Close() }() // best-effort close

	entries, err := readImageExport(rc)
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	selected, rels, err := selectRunBundleEntries(entries, projectFiles)
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	selected, err = rewriteHardlinks(selected)
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	if err := rejectEscapingLinks(selected, rels); err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	if err := rejectBundleSecrets(ctx, selected, projectScanRels(projectFiles)); err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	epoch := cfg.SourceDateEpoch
	if epoch.IsZero() {
		epoch = time.Unix(0, 0)
	}
	data, err := writeDeterministicRunTar(selected, epoch.UTC())
	if err != nil {
		return nil, fmt.Errorf("write run bundle: %w", err)
	}
	return data, nil
}

func storeRunBundle(digest string, data []byte) {
	if strings.TrimSpace(digest) == "" || len(data) == 0 {
		return
	}
	cp := make([]byte, len(data))
	copy(cp, data)
	runBundleStash.Store(digest, cp)
}

func peekRunBundle(digest string) ([]byte, bool) {
	if strings.TrimSpace(digest) == "" {
		return nil, false
	}
	v, ok := runBundleStash.Load(digest)
	if !ok {
		return nil, false
	}
	data, ok := v.([]byte)
	if !ok || len(data) == 0 {
		return nil, false
	}
	return data, true
}

func takeRunBundle(digest string) ([]byte, bool) {
	if strings.TrimSpace(digest) == "" {
		return nil, false
	}
	v, ok := runBundleStash.LoadAndDelete(digest)
	if !ok {
		return nil, false
	}
	data, ok := v.([]byte)
	if !ok || len(data) == 0 {
		return nil, false
	}
	return data, true
}

func rejectProjectSymlinks(projectDir string) error {
	return filepath.WalkDir(projectDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", p, err)
		}
		info, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", p, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		target, err := os.Readlink(p)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", p, err)
		}
		resolved := target
		if !filepath.IsAbs(target) {
			resolved = filepath.Join(filepath.Dir(p), target)
		}
		resolved = filepath.Clean(resolved)
		rel, relErr := filepath.Rel(projectDir, resolved)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("symlink %s points outside the project", p)
		}
		return fmt.Errorf("symlinks are not allowed: %s", p)
	})
}

type imgEntry struct {
	name     string
	typeflag byte
	body     []byte
	link     string
}

func openImageTar(ctx context.Context, cfg RunBundleConfig) (io.ReadCloser, error) {
	if cfg.imageTar != nil {
		return io.NopCloser(cfg.imageTar), nil
	}
	if strings.TrimSpace(cfg.ImageRef) == "" {
		return nil, errors.New("image ref is required")
	}
	return exportImageTar(ctx, cfg.ImageRef)
}

// exportImageTar exports a built image as a tar of its filesystem.
// Symlinks are preserved (not followed) so a link cannot pull outside bytes in.
var exportImageTar = exportImageTarDocker

func exportImageTarDocker(ctx context.Context, imageRef string) (io.ReadCloser, error) {
	cli, err := dockerclient.New()
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	defer func() { _ = cli.Close() }() // best-effort close

	plat := ocispec.Platform{OS: "linux", Architecture: "amd64"}
	// Name the image once: the Docker client refuses a create that sets both
	// Image and Config.Image, before any request is sent.
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Image:    imageRef,
		Platform: &plat,
	})
	if err != nil {
		return nil, fmt.Errorf("create container for image export: %w", err)
	}
	remove := func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = cli.ContainerRemove(cleanCtx, created.ID, client.ContainerRemoveOptions{Force: true}) // best-effort remove
	}

	exported, err := cli.ContainerExport(ctx, created.ID, client.ContainerExportOptions{})
	if err != nil {
		remove()
		return nil, fmt.Errorf("export image %s: %w", imageRef, err)
	}
	return &exportCloser{ReadCloser: exported, close: remove}, nil
}

type exportCloser struct {
	io.ReadCloser
	close func()
}

func (e *exportCloser) Close() error {
	err := e.ReadCloser.Close()
	if e.close != nil {
		e.close()
	}
	return err
}

func readImageExport(r io.Reader) (map[string]imgEntry, error) {
	tr := tar.NewReader(r)
	out := make(map[string]imgEntry)
	var total int64
	headers := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read image tar: %w", err)
		}
		headers++
		if headers > maxRunBundleHeaders {
			return nil, fmt.Errorf("image tar exceeds entry cap %d", maxRunBundleHeaders)
		}
		raw := hdr.Name
		if hdr.Typeflag == tar.TypeDir {
			// Docker's container export names directories with a trailing slash.
			raw = strings.TrimSuffix(raw, "/")
		}
		name, err := cleanImageRel(raw)
		if err != nil {
			return nil, err
		}
		if name == "" || name == "." {
			if err := discardEntry(tr, hdr); err != nil {
				return nil, err
			}
			continue
		}
		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("duplicate image path %s", name)
		}
		if hdr.Size < 0 || hdr.Size > maxRunBundleFile {
			return nil, fmt.Errorf("image entry %s exceeds size cap", name)
		}
		flag := hdr.Typeflag
		if flag == 0 {
			flag = tar.TypeReg // legacy regular-file flag
		}
		ent := imgEntry{name: name, typeflag: flag, link: filepath.ToSlash(hdr.Linkname)}
		keep := imageEntryCandidate(name)
		switch flag {
		case tar.TypeReg:
			if keep {
				body, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
				if err != nil {
					return nil, fmt.Errorf("read image file %s: %w", name, err)
				}
				if int64(len(body)) != hdr.Size {
					return nil, fmt.Errorf("short read of image file %s", name)
				}
				total += int64(len(body))
				if total > maxRunBundleTotal {
					return nil, fmt.Errorf("image roots exceed total size cap")
				}
				ent.body = body
			} else if err := discardN(tr, hdr.Size); err != nil {
				return nil, err
			}
		case tar.TypeDir:
			// Directories are synthesized when the tar is written.
		case tar.TypeSymlink:
			if keep {
				ent.typeflag = tar.TypeSymlink
			}
		case tar.TypeLink:
			// A hardlink linkname is another member, not a path. Never store it.
			// rewriteHardlinks copies bytes only when that member was selected.
			if hdr.Size > 0 {
				if err := discardN(tr, hdr.Size); err != nil {
					return nil, err
				}
			}
			if !keep {
				break
			}
			member, err := hardlinkMember(name, hdr.Linkname)
			if err != nil {
				return nil, err
			}
			ent.typeflag = tar.TypeLink
			ent.link = member
		default:
			if keep {
				return nil, fmt.Errorf("image entry %s has unsupported type %d", name, hdr.Typeflag)
			}
			if err := discardEntry(tr, hdr); err != nil {
				return nil, err
			}
			continue
		}
		if keep {
			out[name] = ent
		}
	}
}

func imageEntryCandidate(name string) bool {
	if name == runBundleReqRel || name == "agentpaas" {
		return true
	}
	return name == "app" || strings.HasPrefix(name, "app/")
}

func discardEntry(tr *tar.Reader, hdr *tar.Header) error {
	if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == 0 {
		return discardN(tr, hdr.Size)
	}
	return nil
}

func discardN(r io.Reader, n int64) error {
	if n <= 0 {
		return nil
	}
	_, err := io.Copy(io.Discard, io.LimitReader(r, n))
	if err != nil {
		return fmt.Errorf("discard image entry: %w", err)
	}
	return nil
}

func cleanImageRel(name string) (string, error) {
	name = filepath.ToSlash(name)
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return "", nil
	}
	if strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("path rejected: %s", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", fmt.Errorf("path must not contain '..': %s", name)
		}
		if part == "" || part == "." {
			return "", fmt.Errorf("path rejected: %s", name)
		}
	}
	return name, nil
}

func selectRunBundleEntries(entries map[string]imgEntry, projectFiles []BuildFile) ([]imgEntry, []string, error) {
	selected := make(map[string]imgEntry)
	lock, ok := entries[runBundleReqRel]
	if !ok {
		return nil, nil, errors.New("locked requirements file not in image")
	}
	selected[runBundleReqRel] = lock

	rels := make([]string, 0, len(projectFiles))
	for _, file := range projectFiles {
		rel := filepath.ToSlash(file.RelPath)
		if rel == "" || strings.HasPrefix(rel, "python/") || strings.HasPrefix(rel, "deps/") {
			continue
		}
		rels = append(rels, rel)
		name := path.Join("app", rel)
		ent, ok := entries[name]
		if !ok {
			return nil, nil, fmt.Errorf("project file %s not in image", rel)
		}
		selected[name] = ent
	}
	for name, ent := range entries {
		if underPrefix(name, "app/python") || underPrefix(name, "app/deps") {
			selected[name] = ent
		}
	}
	out := make([]imgEntry, 0, len(selected))
	for _, ent := range selected {
		if ent.typeflag == tar.TypeDir {
			continue
		}
		out = append(out, ent)
	}
	return out, rels, nil
}

func underPrefix(name, root string) bool {
	return name == root || strings.HasPrefix(name, root+"/")
}

func rejectEscapingLinks(entries []imgEntry, projectRels []string) error {
	for _, ent := range entries {
		if ent.typeflag == tar.TypeLink {
			return fmt.Errorf("hardlink %s is not allowed in the run bundle", ent.name)
		}
		if ent.typeflag != tar.TypeSymlink {
			continue
		}
		target, err := resolveImageLink(ent.name, ent.link)
		if err != nil {
			return err
		}
		if !linkStaysInTree(target, projectRels) {
			return fmt.Errorf("symlink %s points outside the run bundle", ent.name)
		}
	}
	return nil
}

func resolveImageLink(from, link string) (string, error) {
	link = filepath.ToSlash(strings.TrimSpace(link))
	if link == "" {
		return "", fmt.Errorf("symlink %s has an empty target", from)
	}
	if strings.ContainsRune(link, 0) {
		return "", fmt.Errorf("symlink %s has a rejected target", from)
	}
	var raw string
	if strings.HasPrefix(link, "/") {
		raw = strings.TrimPrefix(link, "/")
	} else {
		dir := path.Dir(from)
		if dir == "." {
			raw = link
		} else {
			raw = dir + "/" + link
		}
	}
	if _, err := cleanImageRel(raw); err != nil {
		return "", fmt.Errorf("symlink %s points outside the run bundle: %w", from, err)
	}
	cleaned := path.Clean(raw)
	cleaned = filepath.ToSlash(cleaned)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("symlink %s points outside the run bundle", from)
	}
	return cleaned, nil
}

// hardlinkMember is the archive member a TypeLink names. It is not a
// filesystem path relative to the link, so "etc/passwd" stays "etc/passwd".
func hardlinkMember(from, link string) (string, error) {
	link = strings.TrimSpace(link)
	if link == "" {
		return "", fmt.Errorf("hardlink %s has an empty target", from)
	}
	member, err := cleanImageRel(link)
	if err != nil || member == "" || member == "." {
		return "", fmt.Errorf("hardlink %s points outside the run bundle", from)
	}
	return member, nil
}

// rewriteHardlinks turns a hardlink into a regular file when its member is
// already a selected regular file. Any other target is an escape: the linkname
// is not copied into the bundle.
func rewriteHardlinks(entries []imgEntry) ([]imgEntry, error) {
	files := make([]imgEntry, 0, len(entries))
	pending := make(map[string]imgEntry)
	byName := make(map[string]imgEntry, len(entries))
	var total int64
	for _, ent := range entries {
		if ent.typeflag == tar.TypeLink {
			if _, dup := pending[ent.name]; dup {
				return nil, fmt.Errorf("hardlink %s points outside the run bundle", ent.name)
			}
			pending[ent.name] = ent
			continue
		}
		files = append(files, ent)
		if ent.typeflag == tar.TypeReg || ent.typeflag == 0 {
			byName[ent.name] = ent
			total += int64(len(ent.body))
		}
	}
	for len(pending) > 0 {
		progress := false
		for name, ent := range pending {
			if _, waiting := pending[ent.link]; waiting {
				continue
			}
			target, ok := byName[ent.link]
			if !ok || (target.typeflag != tar.TypeReg && target.typeflag != 0) {
				return nil, fmt.Errorf("hardlink %s points outside the run bundle", name)
			}
			total += int64(len(target.body))
			if total > maxRunBundleTotal {
				return nil, fmt.Errorf("image roots exceed total size cap")
			}
			rewritten := imgEntry{name: name, typeflag: tar.TypeReg, body: target.body}
			byName[name] = rewritten
			files = append(files, rewritten)
			delete(pending, name)
			progress = true
		}
		if !progress {
			for name := range pending {
				return nil, fmt.Errorf("hardlink %s points outside the run bundle", name)
			}
		}
	}
	return files, nil
}

func linkStaysInTree(target string, projectRels []string) bool {
	target = strings.TrimSuffix(filepath.ToSlash(target), "/")
	// A nil project list is the scan of an already-written bundle: every
	// path under app/ is inside the three roots. The writer passes the
	// real project list and is stricter.
	if projectRels == nil && (target == "app" || strings.HasPrefix(target, "app/")) {
		return true
	}
	switch {
	case target == runBundleReqRel:
		return true
	case target == "app" || underPrefix(target, "app/python") || underPrefix(target, "app/deps"):
		return true
	}
	for _, rel := range projectRels {
		allowed := path.Join("app", rel)
		if target == allowed {
			return true
		}
		dir := path.Dir(allowed)
		for dir != "." && dir != "" {
			if target == dir {
				return true
			}
			dir = path.Dir(dir)
		}
	}
	return false
}

func projectScanRels(files []BuildFile) []string {
	rels := make([]string, 0, len(files))
	for _, file := range files {
		rel := filepath.ToSlash(file.RelPath)
		if rel == "" || rel == "." {
			continue
		}
		rels = append(rels, rel)
	}
	return rels
}

func rejectBundleSecrets(ctx context.Context, entries []imgEntry, projectRels []string) error {
	for _, ent := range entries {
		if ent.typeflag == tar.TypeSymlink || ent.typeflag == tar.TypeLink {
			continue
		}
		if ContainsExportSentinel(ent.body) {
			return fmt.Errorf("secret detected in run bundle %s", ent.name)
		}
	}
	// Fail closed like runGitleaks: no scanner means no bundle is written.
	if _, err := exec.LookPath("gitleaks"); err != nil {
		return fmt.Errorf("secret scan failed: gitleaks not available: %w", err)
	}
	project := make(map[string]struct{}, len(projectRels))
	for _, rel := range projectRels {
		rel = filepath.ToSlash(rel)
		if rel == "" || rel == "." {
			continue
		}
		project[path.Join("app", rel)] = struct{}{}
	}
	var strict, deps []imgEntry
	for _, ent := range entries {
		if ent.typeflag != tar.TypeReg && ent.typeflag != 0 {
			continue
		}
		// Dependency files under app/deps/ use the second config. A project
		// file at the same path, including one under deps/, stays on the first.
		if underPrefix(ent.name, "app/deps") {
			if _, isProject := project[ent.name]; !isProject {
				deps = append(deps, ent)
				continue
			}
		}
		strict = append(strict, ent)
	}
	if err := scanBundleEntries(ctx, strict, gitleaksDefaultConfig); err != nil {
		return err
	}
	return scanBundleEntries(ctx, deps, gitleaksDepsConfig)
}

func scanBundleEntries(ctx context.Context, entries []imgEntry, config []byte) error {
	if len(entries) == 0 {
		return nil
	}
	tmp, err := os.MkdirTemp("", "agentpaas-runbundle-scan-*")
	if err != nil {
		return fmt.Errorf("create secret scan dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }() // best-effort remove
	if err := materializeRunBundle(tmp, entries); err != nil {
		return err
	}
	findings, err := runGitleaks(ctx, tmp, config)
	if err != nil {
		return fmt.Errorf("secret scan failed: %w", err)
	}
	if len(findings) > 0 {
		f := findings[0]
		return fmt.Errorf("secret detected in run bundle %s (%s)", f.File, f.Rule)
	}
	return nil
}

func materializeRunBundle(dir string, entries []imgEntry) error {
	for _, ent := range entries {
		if ent.typeflag == tar.TypeSymlink || ent.typeflag == tar.TypeLink || ent.typeflag == tar.TypeDir {
			continue
		}
		dest := filepath.Join(dir, filepath.FromSlash(ent.name))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("stage %s: %w", ent.name, err)
		}
		if err := os.WriteFile(dest, ent.body, 0o600); err != nil {
			return fmt.Errorf("stage %s: %w", ent.name, err)
		}
	}
	return nil
}

// scanRunBundleFile is the pack secret scan over a written run bundle.
func scanRunBundleFile(ctx context.Context, bundlePath string) ([]SecretFinding, error) {
	if err := rejectSymlinkPath(bundlePath, false); err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	f, err := os.Open(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	defer func() { _ = f.Close() }() // best-effort close
	entries, err := readImageExport(f)
	if err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	selected := make([]imgEntry, 0, len(entries))
	for _, ent := range entries {
		if ent.typeflag == tar.TypeDir {
			continue
		}
		selected = append(selected, ent)
	}
	selected, err = rewriteHardlinks(selected)
	if err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	if err := rejectEscapingLinks(selected, nil); err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	var findings []SecretFinding
	for _, ent := range selected {
		if ContainsExportSentinel(ent.body) {
			findings = append(findings, SecretFinding{
				File:   ent.name,
				Line:   1,
				Rule:   "export-sentinel",
				Secret: maskSecret("SENTINEL_EXPORT_SECRET"),
			})
		}
	}
	tmp, err := os.MkdirTemp("", "agentpaas-runbundle-scan-*")
	if err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }() // best-effort remove
	if err := materializeRunBundle(tmp, selected); err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	leaked, err := runGitleaks(ctx, tmp, gitleaksDefaultConfig)
	if err != nil {
		return nil, fmt.Errorf("scan run bundle: %w", err)
	}
	findings = append(findings, leaked...)
	return findings, nil
}

type runTarEntry struct {
	name     string
	body     []byte
	link     string
	dir      bool
	linkType byte
}

func writeDeterministicRunTar(entries []imgEntry, epoch time.Time) ([]byte, error) {
	dirs := map[string]struct{}{}
	files := make([]runTarEntry, 0, len(entries))
	for _, ent := range entries {
		if ent.typeflag == tar.TypeDir {
			continue
		}
		item := runTarEntry{name: ent.name, body: ent.body, link: ent.link, linkType: ent.typeflag}
		files = append(files, item)
		dir := path.Dir(ent.name)
		for dir != "." && dir != "" {
			dirs[dir] = struct{}{}
			dir = path.Dir(dir)
		}
	}
	out := make([]runTarEntry, 0, len(files)+len(dirs))
	for dir := range dirs {
		out = append(out, runTarEntry{name: dir + "/", dir: true})
	}
	out = append(out, files...)
	sortRunTar(out)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, ent := range out {
		hdr := &tar.Header{
			Name:    ent.name,
			Uid:     0,
			Gid:     0,
			Uname:   "",
			Gname:   "",
			ModTime: epoch,
			Format:  tar.FormatUSTAR,
		}
		if ent.dir {
			hdr.Typeflag = tar.TypeDir
			hdr.Mode = 0o755
			hdr.Size = 0
		} else if ent.linkType == tar.TypeSymlink {
			hdr.Typeflag = tar.TypeSymlink
			hdr.Linkname = ent.link
			hdr.Mode = 0o777
			hdr.Size = 0
		} else if ent.linkType != tar.TypeReg && ent.linkType != 0 {
			return nil, fmt.Errorf("hardlink %s is not allowed in the run bundle", ent.name)
		} else {
			hdr.Typeflag = tar.TypeReg
			hdr.Mode = 0o644
			hdr.Size = int64(len(ent.body))
		}
		if len(hdr.Name) > 100 {
			hdr.Format = tar.FormatPAX
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("write tar header %s: %w", ent.name, err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := tw.Write(ent.body); err != nil {
				return nil, fmt.Errorf("write tar body %s: %w", ent.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	return buf.Bytes(), nil
}

func sortRunTar(entries []runTarEntry) {
	// Local sort avoids importing sort in a way that hides the comparison.
	for i := 1; i < len(entries); i++ {
		j := i
		for j > 0 && entries[j].name < entries[j-1].name {
			entries[j], entries[j-1] = entries[j-1], entries[j]
			j--
		}
	}
}

func writeRunBundleFile(lockPath string, data []byte) error {
	if strings.TrimSpace(lockPath) == "" {
		return errors.New("write run bundle: lock path is required")
	}
	dir := filepath.Dir(lockPath)
	if err := validateSecurePath(dir, true); err != nil {
		return fmt.Errorf("write run bundle: %w", err)
	}
	dest := filepath.Join(dir, RunBundleName)
	if err := validateSecurePath(dest, false); err != nil {
		return fmt.Errorf("write run bundle: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".run.bundle-*.tar")
	if err != nil {
		return fmt.Errorf("write run bundle: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName) // best-effort remove
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close() // best-effort close
		return fmt.Errorf("write run bundle: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write run bundle: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("write run bundle: %w", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("write run bundle: %w", err)
	}
	cleanup = false
	return nil
}
