package pack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/parvezsyed/agentpaas/internal/audit"
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
		return nil, err
	}
	if !filepath.IsAbs(cfg.ProjectDir) {
		return nil, fmt.Errorf("project directory must be absolute: %s", cfg.ProjectDir)
	}
	if err := rejectSymlinkPath(cfg.ProjectDir, false); err != nil {
		return nil, err
	}
	ignore := cfg.Ignore
	if ignore == nil {
		var err error
		ignore, err = LoadIgnore(cfg.ProjectDir)
		if err != nil {
			return nil, err
		}
	}

	sourceFindings, err := runGitleaks(ctx, cfg.ProjectDir)
	if err != nil {
		return nil, err
	}
	contextDir, cleanup, err := materializeBuildContext(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	contextFindings, err := runGitleaks(ctx, contextDir)
	if err != nil {
		return nil, err
	}
	contextFindings = rewriteFindingFiles(contextFindings, contextDir, cfg.ProjectDir)

	contextSize, err := computeContextSize(cfg.ProjectDir, ignore)
	if err != nil {
		return nil, err
	}

	sourceFindings, err = applyAllowPatterns(sourceFindings, cfg.AllowPatterns, cfg.AuditAppend)
	if err != nil {
		return nil, err
	}
	contextFindings, err = applyAllowPatterns(contextFindings, cfg.AllowPatterns, cfg.AuditAppend)
	if err != nil {
		return nil, err
	}

	return &ScanResult{
		SourceFindings:     sourceFindings,
		ContextFindings:    contextFindings,
		ContextSize:        contextSize,
		ContextSizeWarning: contextSize > contextSizeWarningThreshold,
	}, nil
}

// runGitleaks executes gitleaks on a directory and returns findings.
// Uses exec.Command("gitleaks", "detect", "--source", dir, "--report-format", "json", "--report-path", "-").
// Parses JSON output from stdout.
// Returns empty list if gitleaks finds nothing.
func runGitleaks(ctx context.Context, dir string) ([]SecretFinding, error) {
	if err := rejectSymlinkPath(dir, false); err != nil {
		return nil, err
	}

	scanCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(scanCtx, "gitleaks", "detect", "--source", dir, "--report-format", "json", "--report-path", "-")
	output, err := cmd.Output()
	if scanCtx.Err() != nil {
		return nil, fmt.Errorf("gitleaks scan failed: %w", scanCtx.Err())
	}

	findings, parseErr := parseGitleaksOutput(output, dir)
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
	files, err := collectBuildFiles(projectDir, ignore)
	if err != nil {
		return 0, err
	}

	var size int64
	for _, file := range files {
		size += file.info.Size()
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
	return r != nil && (len(r.SourceFindings) > 0 || len(r.ContextFindings) > 0)
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
				return nil, err
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

func materializeBuildContext(projectDir string, ignore *IgnoreMatcher) (string, func(), error) {
	files, err := collectBuildFiles(projectDir, ignore)
	if err != nil {
		return "", nil, err
	}

	dir, err := os.MkdirTemp("", "agentpaas-build-context-*")
	if err != nil {
		return "", nil, fmt.Errorf("create build context temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	for _, file := range files {
		dst := filepath.Join(dir, filepath.FromSlash(file.relPath))
		rel, err := safeRelPath(dir, dst)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if rel != file.relPath {
			cleanup()
			return "", nil, fmt.Errorf("invalid build context path: %s", file.relPath)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("create build context directory: %w", err)
		}
		data, err := readProjectFile(file.absPath)
		if err != nil {
			cleanup()
			return "", nil, err
		}
		if err := os.WriteFile(dst, data, file.info.Mode().Perm()); err != nil {
			cleanup()
			return "", nil, fmt.Errorf("write build context file %s: %w", file.relPath, err)
		}
	}

	return dir, cleanup, nil
}

func rewriteFindingFiles(findings []SecretFinding, contextDir string, projectDir string) []SecretFinding {
	_ = contextDir
	_ = projectDir
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
