package dashboard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/parvezsyed/agentpaas/internal/audit"
)

// AuditSearchView is the sanitized API response for audit record search.
type AuditSearchView struct {
	TotalCount int               `json:"total_count"`
	Records    []AuditRecordView `json:"records"`
	SeqRange   [2]int64          `json:"seq_range"`
	Indexed    bool              `json:"indexed"`
}

// AuditRecordView is a sanitized audit record for API display.
type AuditRecordView struct {
	Seq            int64             `json:"seq"`
	Timestamp      string            `json:"timestamp"`
	EventType      string            `json:"event_type"`
	DeploymentMode string            `json:"deployment_mode"`
	Actor          string            `json:"actor"`
	Payload        map[string]string `json:"payload"`
}

// AuditVerifyView is the sanitized API response for audit verification.
type AuditVerifyView struct {
	Verified               bool     `json:"verified"`
	AuditRecordCount       int64    `json:"audit_record_count"`
	AuditHeadSeq           int64    `json:"audit_head_seq"`
	AuditHeadHash          string   `json:"audit_head_hash"`
	CheckpointCount        int      `json:"checkpoint_count"`
	LatestAnchorSeq        int64    `json:"latest_anchor_seq"`
	LatestAnchorHash       string   `json:"latest_anchor_hash"`
	TrustAnchorFingerprint string   `json:"trust_anchor_fingerprint"`
	VerificationCommand    string   `json:"verification_command"`
	IssuesCount            int      `json:"issues_count"`
	IssueSummaries         []string `json:"issue_summaries,omitempty"`
}

type auditExportRequest struct {
	AuditPath      string `json:"audit_path"`
	CheckpointPath string `json:"checkpoint_path"`
	BundleDir      string `json:"bundle_dir"`
}

// ServeAuditSearch handles GET /api/audit/search.
func (s *Server) ServeAuditSearch(w http.ResponseWriter, r *http.Request) {
	if s.auditIndexer == nil {
		writeJSONError(w, http.StatusNotFound, "audit index unavailable")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	eventType := strings.TrimSpace(r.URL.Query().Get("event_type"))
	limit, ok := parseAuditSearchInt(w, r.URL.Query().Get("limit"), 100, 1, 1000, "limit")
	if !ok {
		return
	}
	offset, ok := parseAuditSearchInt(w, r.URL.Query().Get("offset"), 0, 0, 1_000_000, "offset")
	if !ok {
		return
	}

	records, err := s.queryAuditRecords(eventType)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query audit index failed")
		return
	}
	records = filterAuditRecords(records, query)
	seqRange := auditSeqRange(records)
	total := len(records)
	records = paginateAuditRecords(records, limit, offset)

	views := make([]AuditRecordView, 0, len(records))
	for _, record := range records {
		views = append(views, auditRecordView(record))
	}
	writeJSON(w, http.StatusOK, AuditSearchView{
		TotalCount: total,
		Records:    views,
		SeqRange:   seqRange,
		Indexed:    true,
	})
}

// ServeAuditExport handles POST /api/audit/export.
func (s *Server) ServeAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req auditExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	auditPath, err := resolveDashboardReadPath(strings.TrimSpace(req.AuditPath), "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid audit path")
		return
	}
	checkpointPath, err := resolveDashboardReadPath(strings.TrimSpace(req.CheckpointPath), "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid checkpoint path")
		return
	}
	bundleDir, err := resolveDashboardWriteDir(strings.TrimSpace(req.BundleDir))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid bundle dir")
		return
	}

	manifest, err := audit.ExportAuditBundle(bundleDir, &audit.ExportBundleOptions{
		AuditPath:      auditPath,
		CheckpointPath: checkpointPath,
		SigningKey:     s.auditSigningKey,
		PubKeyDER:      append([]byte(nil), s.auditPubKeyDER...),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "export audit bundle failed")
		return
	}
	sanitizeBundleManifest(manifest)
	writeJSON(w, http.StatusOK, manifest)
}

// ServeAuditVerify handles GET /api/audit/verify?audit=<path>&checkpoints=<path>.
func (s *Server) ServeAuditVerify(w http.ResponseWriter, r *http.Request) {
	auditPath := strings.TrimSpace(r.URL.Query().Get("audit"))
	checkpointsPath := strings.TrimSpace(r.URL.Query().Get("checkpoints"))
	if auditPath == "" || checkpointsPath == "" {
		writeJSONError(w, http.StatusBadRequest, "both 'audit' and 'checkpoints' query params required")
		return
	}
	resolvedAuditPath, err := resolveDashboardReadPath(auditPath, "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid path")
		return
	}
	resolvedCheckpointsPath, err := resolveDashboardReadPath(checkpointsPath, "")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid path")
		return
	}

	result, err := audit.VerifyAuditChain(resolvedAuditPath, resolvedCheckpointsPath, nil)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "verify audit chain failed")
		return
	}
	writeJSON(w, http.StatusOK, s.auditVerifyView(result, resolvedAuditPath, resolvedCheckpointsPath))
}

func (s *Server) queryAuditRecords(eventType string) ([]audit.AuditRecord, error) {
	if eventType != "" {
		records, err := s.auditIndexer.QueryByEventType(eventType, 0)
		if err != nil {
			return nil, err
		}
		return records, nil
	}
	count, err := s.auditIndexer.RecordCount()
	if err != nil {
		return nil, err
	}
	records := make([]audit.AuditRecord, 0, count)
	for seq := int64(1); seq <= int64(count); seq++ {
		record, err := s.auditIndexer.QueryBySeq(seq)
		if err != nil {
			continue
		}
		records = append(records, *record)
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Seq < records[j].Seq
	})
	return records, nil
}

func parseAuditSearchInt(w http.ResponseWriter, raw string, defaultValue int, minValue int, maxValue int, name string) (int, bool) {
	if strings.TrimSpace(raw) == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		writeJSONError(w, http.StatusBadRequest, "invalid "+name)
		return 0, false
	}
	return value, true
}

func filterAuditRecords(records []audit.AuditRecord, query string) []audit.AuditRecord {
	if query == "" {
		return records
	}
	query = strings.ToLower(query)
	filtered := make([]audit.AuditRecord, 0, len(records))
	for _, record := range records {
		if strings.Contains(strings.ToLower(record.EventType), query) ||
			strings.Contains(strings.ToLower(record.Actor), query) ||
			strings.Contains(strings.ToLower(record.DeploymentMode), query) ||
			strings.Contains(strings.ToLower(payloadString(record.Payload)), query) {
			filtered = append(filtered, record)
		}
	}
	return filtered
}

func paginateAuditRecords(records []audit.AuditRecord, limit int, offset int) []audit.AuditRecord {
	if offset >= len(records) {
		return []audit.AuditRecord{}
	}
	end := offset + limit
	if end > len(records) {
		end = len(records)
	}
	return records[offset:end]
}

func auditSeqRange(records []audit.AuditRecord) [2]int64 {
	if len(records) == 0 {
		return [2]int64{}
	}
	minSeq := records[0].Seq
	maxSeq := records[0].Seq
	for _, record := range records[1:] {
		if record.Seq < minSeq {
			minSeq = record.Seq
		}
		if record.Seq > maxSeq {
			maxSeq = record.Seq
		}
	}
	return [2]int64{minSeq, maxSeq}
}

func auditRecordView(record audit.AuditRecord) AuditRecordView {
	return AuditRecordView{
		Seq:            record.Seq,
		Timestamp:      sanitizeString(record.Timestamp, maxAttributeValueLen),
		EventType:      sanitizeString(record.EventType, maxAttributeValueLen),
		DeploymentMode: sanitizeString(record.DeploymentMode, maxAttributeValueLen),
		Actor:          sanitizeString(record.Actor, maxAttributeValueLen),
		Payload:        sanitizeJSONMap(payloadString(record.Payload), maxAttributeValueLen),
	}
}

func payloadString(payload map[string]interface{}) string {
	if len(payload) == 0 {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	return string(data)
}

func (s *Server) auditVerifyView(result *audit.VerificationResult, auditPath string, checkpointPath string) AuditVerifyView {
	issues := make([]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		if issue == nil {
			continue
		}
		issues = append(issues, sanitizeString(issue.Error(), maxAttributeValueLen))
	}
	fingerprint := "unsigned"
	if len(s.auditTrustAnchorDER) > 0 {
		fingerprint = sha256Fingerprint(s.auditTrustAnchorDER)
	} else if len(s.auditPubKeyDER) > 0 {
		fingerprint = sha256Fingerprint(s.auditPubKeyDER)
	}
	return AuditVerifyView{
		Verified:               len(result.Issues) == 0,
		AuditRecordCount:       result.AuditRecordCount,
		AuditHeadSeq:           result.AuditHeadSeq,
		AuditHeadHash:          sanitizeHexDigest(result.AuditHeadHash),
		CheckpointCount:        result.CheckpointCount,
		LatestAnchorSeq:        result.LatestAnchorSeq,
		LatestAnchorHash:       sanitizeHexDigest(result.LatestAnchorHash),
		TrustAnchorFingerprint: sanitizeHexDigest(fingerprint),
		VerificationCommand: sanitizeString(
			fmt.Sprintf("agentpaas audit verify --audit %q --checkpoints %q", auditPath, checkpointPath),
			maxAttributeValueLen,
		),
		IssuesCount:    len(result.Issues),
		IssueSummaries: issues,
	}
}

func sanitizeBundleManifest(manifest *audit.BundleManifest) {
	if manifest == nil {
		return
	}
	manifest.ExportTimestamp = sanitizeString(manifest.ExportTimestamp, maxAttributeValueLen)
	manifest.AuditHeadHash = sanitizeHexDigest(manifest.AuditHeadHash)
	manifest.LatestCpHash = sanitizeHexDigest(manifest.LatestCpHash)
	manifest.PubKeyFingerprint = sanitizeHexDigest(manifest.PubKeyFingerprint)
	manifest.ManifestHash = sanitizeHexDigest(manifest.ManifestHash)
}

func sha256Fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sanitizeHexDigest(value string) string {
	sanitized := sanitizeString(value, maxAttributeValueLen)
	if sanitized != "[REDACTED]" || !isHexDigest(value) {
		return sanitized
	}
	return value
}

func isHexDigest(value string) bool {
	if value == "" || len(value)%2 != 0 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
			continue
		}
		return false
	}
	return true
}

func resolveDashboardReadPath(rawPath string, baseDir string) (string, error) {
	path, err := cleanDashboardPath(rawPath, baseDir)
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkPath(path, false); err != nil {
		return "", err
	}
	return path, nil
}

func resolveDashboardWriteDir(rawPath string) (string, error) {
	path, err := cleanDashboardPath(rawPath, "")
	if err != nil {
		return "", err
	}
	if err := rejectSymlinkPath(path, true); err != nil {
		return "", err
	}
	return path, nil
}

func cleanDashboardPath(rawPath string, baseDir string) (string, error) {
	if rawPath == "" || strings.Contains(rawPath, "\x00") || strings.ContainsAny(rawPath, "\n\r") {
		return "", fmt.Errorf("invalid path")
	}
	if strings.Contains(rawPath, "..") {
		return "", fmt.Errorf("invalid path")
	}
	var path string
	if filepath.IsAbs(rawPath) {
		path = filepath.Clean(rawPath)
	} else {
		if baseDir == "" {
			return "", fmt.Errorf("relative path without base dir")
		}
		path = filepath.Join(baseDir, rawPath)
	}
	if !filepath.IsAbs(path) || isSystemDashboardPath(path) {
		return "", fmt.Errorf("invalid path")
	}
	if baseDir != "" {
		base := filepath.Clean(baseDir)
		rel, err := filepath.Rel(base, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path escapes base dir")
		}
	}
	return path, nil
}

func isSystemDashboardPath(path string) bool {
	for _, prefix := range []string{"/etc", "/usr", "/bin"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func rejectSymlinkPath(path string, allowMissingLeaf bool) error {
	cleaned := filepath.Clean(path)
	parts := strings.Split(strings.TrimPrefix(cleaned, string(filepath.Separator)), string(filepath.Separator))
	current := string(filepath.Separator)
	for i, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			if allowMissingLeaf && i == len(parts)-1 && os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component rejected")
		}
	}
	return nil
}
