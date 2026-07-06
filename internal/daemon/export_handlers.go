package daemon

import (
	"context"
	"path/filepath"
	"strings"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/export"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *controlServer) ExportPreview(ctx context.Context, req *controlv1.ExportPreviewRequest) (*controlv1.ExportPreviewResponse, error) {
	projectDir := req.GetAgentProjectPath()
	if projectDir == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_project_path is required")
	}
	if s.homePaths == nil {
		return nil, status.Error(codes.FailedPrecondition, "daemon home paths not configured")
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "resolve project path: %v", err)
	}
	ks, err := s.openIdentityStore()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "identity keystore: %v", err)
	}
	prev, err := export.Preview(ctx, export.Config{
		Home:           s.homePaths.Home,
		ProjectDir:     absProject,
		IncludeGlobs:   req.GetIncludeGlobs(),
		PublisherStore: ks,
	})
	if err != nil {
		return nil, mapExportError(err)
	}
	var files []*controlv1.ExportFileEntry
	for _, f := range prev.Files {
		files = append(files, &controlv1.ExportFileEntry{
			Path:   f.Path,
			Digest: f.Digest,
			Bytes:  f.Bytes,
			Extra:  f.Extra,
		})
	}
	return &controlv1.ExportPreviewResponse{
		AgentName:    prev.AgentName,
		AgentVersion: prev.AgentVersion,
		Files:        files,
	}, nil
}

func (s *controlServer) Export(ctx context.Context, req *controlv1.ExportRequest) (*controlv1.ExportResponse, error) {
	projectDir := req.GetAgentProjectPath()
	if projectDir == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_project_path is required")
	}
	if req.GetOutputPath() == "" {
		return nil, status.Error(codes.InvalidArgument, "output_path is required")
	}
	if !req.GetConfirmed() {
		return nil, status.Error(codes.FailedPrecondition, "export not confirmed; review manifest and pass confirmed=true")
	}
	if s.homePaths == nil {
		return nil, status.Error(codes.FailedPrecondition, "daemon home paths not configured")
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "resolve project path: %v", err)
	}
	ks, err := s.openIdentityStore()
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "identity keystore: %v", err)
	}
	result, err := export.Run(ctx, export.Config{
		Home:           s.homePaths.Home,
		ProjectDir:     absProject,
		OutputPath:     req.GetOutputPath(),
		WithImage:      req.GetWithImage(),
		IncludeGlobs:   req.GetIncludeGlobs(),
		SkipConfirm:    true,
		PublisherStore: ks,
		Audit:          &exportAuditSink{server: s},
	})
	if err != nil {
		s.recordAudit("bundle_export_verification_failed", "cli", map[string]interface{}{
			"error": err.Error(),
		})
		return nil, mapExportError(err)
	}
	return &controlv1.ExportResponse{
		BundleDigest:         result.BundleDigest,
		PublisherFingerprint: result.PublisherFingerprint,
		FileCount:            int32(result.FileCount),
		TotalBytes:           result.TotalBytes,
		OutputPath:           result.OutputPath,
	}, nil
}

type exportAuditSink struct {
	server *controlServer
}

func (e *exportAuditSink) Append(record audit.AuditRecord) error {
	if e.server.auditWriter == nil {
		return nil
	}
	return e.server.auditWriter.Append(record)
}

func mapExportError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if exportErrIsPrecondition(msg) {
		return status.Errorf(codes.FailedPrecondition, "%v", err)
	}
	return status.Errorf(codes.Internal, "export failed: %v", err)
}

func exportErrIsPrecondition(msg string) bool {
	for _, sub := range []string{
		"not deployed",
		"publisher identity",
		"without publisher",
		"source changed",
		"export blocked",
		"secret",
		"denied",
		"repack",
		"run agentpaas pack",
	} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}