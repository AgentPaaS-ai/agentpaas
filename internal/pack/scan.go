package pack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
)

const secretAllowPatternEventType = "secret_scan_allow_pattern"

var contextSizeWarningThreshold int64 = 100 * 1024 * 1024

// AuditAppender is implemented by audit sinks that accept audit records.
type AuditAppender = audit.AuditAppender

// ScanConfig controls the secret scan process.
type ScanConfig struct {
	// ProjectDir is the agent project directory to scan.
	ProjectDir string
	// Ignore is the .agentpaasignore matcher (from T01).
	Ignore *IgnoreMatcher
	// AllowPatterns are regex patterns for secrets that are explicitly allowed.
	// Each requires a successful audit append (see AuditAppend below).
	AllowPatterns []string
	// AuditAppend is called to record an allow-pattern justification.
	// If nil, --allow-secret-pattern aborts with an error.
	AuditAppend AuditAppender
}

// SecretFinding represents a detected secret in the source or build context.
type SecretFinding struct {
	// File is the relative path from ProjectDir.
	File string `json:"file"`
	// Line is the 1-based line number.
	Line int `json:"line"`
	// Rule is the gitleaks rule ID that matched.
	Rule string `json:"rule"`
	// Secret is the masked secret value (first 4 chars + *** + last 4 chars).
	Secret string `json:"secret"`

	rawSecret string
}

// ScanResult holds the outcome of a secret scan.
type ScanResult struct {
	// Findings from source tree scan.
	SourceFindings []SecretFinding `json:"source_findings"`
	// Findings from build context scan (after .agentpaasignore applied).
	ContextFindings []SecretFinding `json:"context_findings"`
	// ContextSize is the total build context size in bytes.
	ContextSize int64 `json:"context_size"`
	// ContextSizeWarning is true if ContextSize > 100MB.
	ContextSizeWarning bool `json:"context_size_warning"`
	// BundleFindings are secrets in a run bundle stored in the project directory.
	// The pack writer also scans image bytes before the bundle is stored.
	BundleFindings []SecretFinding `json:"bundle_findings,omitempty"`
}

type gitleaksFinding struct {
	File      string `json:"File"`
	StartLine int    `json:"StartLine"`
	RuleID    string `json:"RuleID"`
	Secret    string `json:"Secret"`
}

// ScanSecrets runs gitleaks over the source tree and effective build context.
// Steps:
// 1. Run gitleaks on the full source tree (no ignore filtering).
// 2. Run gitleaks on the effective build context (after .agentpaasignore).
// 3. Compute build context size (sum of non-ignored file sizes).
// 4. Warn if context >100MB.
// 5. Apply allow-patterns (with audit append requirement).
// Returns ScanResult with findings. Does NOT fail itself — caller checks findings.
func ScanSecrets(ctx context.Context, cfg ScanConfig) (*ScanResult, error) {
	if err := validateProjectDir(cfg.ProjectDir); err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	if !filepath.IsAbs(cfg.ProjectDir) {
		return nil, fmt.Errorf("project directory must be absolute: %s", cfg.ProjectDir)
	}
	if err := rejectSymlinkPath(cfg.ProjectDir, false); err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	ignore := cfg.Ignore
	if ignore == nil {
		var err error
		ignore, err = LoadIgnore(cfg.ProjectDir)
		if err != nil {
			return nil, fmt.Errorf("scan secrets: %w", err)
		}
	}

	sourceDir, sourceCleanup, err := sourceTreeWithoutRunBundle(cfg.ProjectDir)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	defer sourceCleanup()
	sourceFindings, err := runGitleaks(ctx, sourceDir, gitleaksDefaultConfig)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	contextDir, cleanup, err := materializeBuildContext(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	defer cleanup()

	contextFindings, err := runGitleaks(ctx, contextDir, gitleaksDefaultConfig)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	contextFindings = rewriteFindingFiles(contextFindings, contextDir, cfg.ProjectDir)

	contextSize, err := computeContextSize(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}

	sourceFindings, err = applyAllowPatterns(sourceFindings, cfg.AllowPatterns, cfg.AuditAppend)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}
	contextFindings, err = applyAllowPatterns(contextFindings, cfg.AllowPatterns, cfg.AuditAppend)
	if err != nil {
		return nil, fmt.Errorf("scan secrets: %w", err)
	}

	var bundleFindings []SecretFinding
	bundlePath := filepath.Join(cfg.ProjectDir, RunBundleName)
	if info, statErr := os.Lstat(bundlePath); statErr == nil && info.Mode().IsRegular() {
		bundleFindings, err = scanRunBundleFile(ctx, bundlePath)
		if err != nil {
			return nil, fmt.Errorf("scan secrets: %w", err)
		}
		bundleFindings, err = applyAllowPatterns(bundleFindings, cfg.AllowPatterns, cfg.AuditAppend)
		if err != nil {
			return nil, fmt.Errorf("scan secrets: %w", err)
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("scan secrets: %w", statErr)
	}

	return &ScanResult{
		SourceFindings:     sourceFindings,
		ContextFindings:    contextFindings,
		BundleFindings:     bundleFindings,
		ContextSize:        contextSize,
		ContextSizeWarning: contextSize > contextSizeWarningThreshold,
	}, nil
}

// ScanDirectorySecrets runs gitleaks on the exact directory tree (export fileset).
func ScanDirectorySecrets(ctx context.Context, dir string) ([]SecretFinding, error) {
	return runGitleaks(ctx, dir, gitleaksDefaultConfig)
}

// ContainsExportSentinel reports whether data contains the red-team sentinel string.
func ContainsExportSentinel(data []byte) bool {
	return strings.Contains(string(data), "SENTINEL_EXPORT_SECRET")
}

// runGitleaks executes one pinned gitleaks invocation. The child sees a
// pack-owned config outside the scanned tree, no GITLEAKS_* variables, an
// empty ignore directory as both --gitleaks-ignore-path and the working
// directory, and --ignore-gitleaks-allow. No baseline is passed. The verdict
// then depends only on the scanned bytes and config.
func runGitleaks(ctx context.Context, dir string, config []byte) ([]SecretFinding, error) {
	if err := rejectSymlinkPath(dir, false); err != nil {
		return nil, fmt.Errorf("run gitleaks: %w", err)
	}
	if strings.TrimSpace(string(config)) == "" {
		return nil, errors.New("run gitleaks: pack config is empty")
	}
	source := dir
	if !filepath.IsAbs(source) {
		abs, err := filepath.Abs(source)
		if err != nil {
			return nil, fmt.Errorf("run gitleaks: %w", err)
		}
		source = abs
	}

	cfgDir, err := os.MkdirTemp("", "agentpaas-gitleaks-config-*")
	if err != nil {
		return nil, fmt.Errorf("run gitleaks: %w", err)
	}
	defer func() { _ = os.RemoveAll(cfgDir) }() // best-effort remove
	cfgPath := filepath.Join(cfgDir, "config.toml")
	if err := os.WriteFile(cfgPath, config, 0o600); err != nil {
		return nil, fmt.Errorf("run gitleaks: %w", err)
	}
	ignoreDir, err := os.MkdirTemp("", "agentpaas-gitleaks-ignore-*")
	if err != nil {
		return nil, fmt.Errorf("run gitleaks: %w", err)
	}
	defer func() { _ = os.RemoveAll(ignoreDir) }() // best-effort remove

	scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Export scans a staged directory, which is intentionally not a Git
	// repository. --no-git makes gitleaks inspect the filesystem contents
	// instead of silently scanning zero commits and returning [].
	cmd := exec.CommandContext(scanCtx, "gitleaks", "detect",
		"--no-git",
		"--source", source,
		"--report-format", "json",
		"--report-path", "-",
		"--config", cfgPath,
		"--ignore-gitleaks-allow",
		"--gitleaks-ignore-path", ignoreDir,
	)
	cmd.Dir = ignoreDir
	cmd.Env = envWithoutGitleaks()
	output, err := cmd.Output()
	if scanCtx.Err() != nil {
		return nil, fmt.Errorf("gitleaks scan failed: %w", scanCtx.Err())
	}
	if err != nil && len(strings.TrimSpace(string(output))) == 0 {
		return nil, fmt.Errorf("gitleaks scan failed: %w", err)
	}

	findings, parseErr := parseGitleaksOutput(output, source)
	if parseErr == nil {
		return findings, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gitleaks scan failed: %w", err)
	}
	if len(strings.TrimSpace(string(output))) == 0 {
		return nil, nil
	}

	return nil, parseErr
}

// envWithoutGitleaks is the process environment with every GITLEAKS_* variable removed.
func envWithoutGitleaks() []string {
	src := os.Environ()
	dst := make([]string, 0, len(src))
	for _, kv := range src {
		name, _, found := strings.Cut(kv, "=")
		if found && strings.HasPrefix(name, "GITLEAKS_") {
			continue
		}
		dst = append(dst, kv)
	}
	return dst
}

// maskSecret masks a secret value: first 4 chars + *** + last 4 chars.
// If secret is <12 chars, returns "****" (fully masked).
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) < 12 {
		return "****"
	}

	return s[:4] + "***" + s[len(s)-4:]
}

// computeContextSize sums file sizes in projectDir, respecting .agentpaasignore.
// Symlink-safe.
func computeContextSize(projectDir string, ignore *IgnoreMatcher) (int64, error) {
	files, err := CollectBuildFiles(projectDir, ignore)
	if err != nil {
		return 0, fmt.Errorf("compute context size: %w", err)
	}

	var size int64
	for _, file := range files {
		size += file.Info.Size()
	}

	return size, nil
}

// applyAllowPatterns filters out findings matching any allow-pattern.
// For each filtered finding, if AuditAppend is non-nil, appends an audit record.
// If AuditAppend is nil and a pattern would filter a finding, returns error.
func applyAllowPatterns(findings []SecretFinding, patterns []string, auditAppend AuditAppender) ([]SecretFinding, error) {
	if len(findings) == 0 || len(patterns) == 0 {
		return findings, nil
	}

	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile allow-secret-pattern: %w", err)
		}
		compiled = append(compiled, re)
	}

	filtered := make([]SecretFinding, 0, len(findings))
	for _, finding := range findings {
		pattern := matchingAllowPattern(finding, compiled, patterns)
		if pattern == "" {
			filtered = append(filtered, finding)
			continue
		}
		if auditAppend == nil {
			return nil, errors.New("allow-secret-pattern requires audit append")
		}
		if err := auditAppend.Append(allowPatternAuditRecord(finding, pattern)); err != nil {
			return nil, fmt.Errorf("append allow-secret-pattern audit record: %w", err)
		}
	}

	return filtered, nil
}

// HasSecrets returns true if any findings exist (source or context).
func (r *ScanResult) HasSecrets() bool {
	return r != nil && (len(r.SourceFindings) > 0 || len(r.ContextFindings) > 0 || len(r.BundleFindings) > 0)
}

// FailClosed returns true if the scan should fail (secrets found that are
// not allow-patterned with audit append).
func (r *ScanResult) FailClosed() bool {
	return r.HasSecrets()
}

func parseGitleaksOutput(output []byte, baseDir string) ([]SecretFinding, error) {
	if len(strings.TrimSpace(string(output))) == 0 {
		return nil, nil
	}

	var raw []gitleaksFinding
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse gitleaks output: %w", err)
	}

	findings := make([]SecretFinding, 0, len(raw))
	for _, item := range raw {
		file := filepath.ToSlash(item.File)
		if filepath.IsAbs(item.File) {
			rel, err := safeRelPath(baseDir, item.File)
			if err != nil {
				return nil, fmt.Errorf("parse gitleaks output: %w", err)
			}
			file = rel
		}
		if file == ".." || strings.HasPrefix(file, "../") || strings.Contains(file, "/../") {
			return nil, fmt.Errorf("gitleaks reported path outside scan directory: %s", item.File)
		}
		findings = append(findings, SecretFinding{
			File:      file,
			Line:      item.StartLine,
			Rule:      item.RuleID,
			Secret:    maskSecret(item.Secret),
			rawSecret: item.Secret,
		})
	}

	return findings, nil
}

func sourceTreeWithoutRunBundle(projectDir string) (string, func(), error) {
	bundlePath := filepath.Join(projectDir, RunBundleName)
	info, err := os.Lstat(bundlePath)
	if err != nil || !info.Mode().IsRegular() {
		return projectDir, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "agentpaas-source-scan-*")
	if err != nil {
		return "", nil, fmt.Errorf("stage source scan: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmp) } // best-effort remove
	err = filepath.WalkDir(projectDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(projectDir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if rel == RunBundleName {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dest := filepath.Join(tmp, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o600)
	})
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("stage source scan: %w", err)
	}
	return tmp, cleanup, nil
}

func materializeBuildContext(projectDir string, ignore *IgnoreMatcher) (string, func(), error) {
	files, err := CollectBuildFiles(projectDir, ignore)
	if err != nil {
		return "", nil, fmt.Errorf("materialize build context: %w", err)
	}

	dir, err := os.MkdirTemp("", "agentpaas-build-context-*")
	if err != nil {
		return "", nil, fmt.Errorf("create build context temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) } // best-effort remove

	for _, file := range files {
		if file.RelPath == RunBundleName {
			continue
		}
		dst := filepath.Join(dir, filepath.FromSlash(file.RelPath))
		rel, err := safeRelPath(dir, dst)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("materialize build context: %w", err)
		}
		if rel != file.RelPath {
			cleanup()
			return "", nil, fmt.Errorf("invalid build context path: %s", file.RelPath)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("create build context directory: %w", err)
		}
		data, err := readProjectFile(file.AbsPath)
		if err != nil {
			cleanup()
			return "", nil, fmt.Errorf("materialize build context: %w", err)
		}
		if err := os.WriteFile(dst, data, file.Info.Mode().Perm()); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("write build context file %s: %w", file.RelPath, err)
		}
	}

	return dir, cleanup, nil
}

func rewriteFindingFiles(findings []SecretFinding, contextDir string, projectDir string) []SecretFinding {
	_ = contextDir // intentionally ignored (reviewed)
	_ = projectDir // intentionally ignored (reviewed)
	return findings
}

func matchingAllowPattern(finding SecretFinding, compiled []*regexp.Regexp, patterns []string) string {
	target := finding.rawSecret
	if target == "" {
		target = finding.Secret
	}
	for i, re := range compiled {
		if re.MatchString(target) {
			return patterns[i]
		}
	}

	return ""
}

func allowPatternAuditRecord(finding SecretFinding, pattern string) audit.AuditRecord {
	return audit.AuditRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		EventType:      secretAllowPatternEventType,
		DeploymentMode: "local",
		Actor:          "agentpaas",
		Payload: map[string]interface{}{
			"file":    finding.File,
			"line":    finding.Line,
			"rule":    finding.Rule,
			"secret":  finding.Secret,
			"pattern": pattern,
		},
	}
}
