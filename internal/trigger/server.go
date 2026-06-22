package trigger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const (
	DefaultGRPCPort   = 7718
	DefaultRESTPort   = 7717
	DefaultMaxPayload = 1 << 20
)

// ServerConfig configures the Trigger API server.
type ServerConfig struct {
	GRPCAddr        string
	RESTAddr        string
	Exposed         bool
	Authenticator   Authenticator
	CORS            *CORSMiddleware
	Audit           audit.AuditAppender
	MaxPayloadBytes int
	// IdempotencyStore handles idempotency key replay/conflict.
	IdempotencyStore *IdempotencyStore
}

// Server is the Trigger API server, serving gRPC and REST gateway endpoints.
type Server struct {
	cfg ServerConfig

	grpcServer   *grpc.Server
	grpcListener net.Listener
	restServer   *http.Server
	restListener net.Listener

	triggerService *TriggerService
}

// New creates a new Trigger API server.
func New(cfg ServerConfig) (*Server, error) {
	if cfg.GRPCAddr == "" {
		cfg.GRPCAddr = fmt.Sprintf("127.0.0.1:%d", DefaultGRPCPort)
	}
	if cfg.RESTAddr == "" {
		cfg.RESTAddr = fmt.Sprintf("127.0.0.1:%d", DefaultRESTPort)
	}
	if cfg.Exposed {
		if cfg.Authenticator == nil {
			return nil, fmt.Errorf("--expose requires API key authentication configured")
		}
		if apiKeys, ok := cfg.Authenticator.(*APIKeyAuthenticator); ok && apiKeys.configuredKeys() == 0 {
			return nil, fmt.Errorf("--expose requires API key authentication configured")
		}
	}
	if cfg.MaxPayloadBytes == 0 {
		cfg.MaxPayloadBytes = DefaultMaxPayload
	}

	s := &Server{cfg: cfg}

	var opts []grpc.ServerOption
	if cfg.Authenticator != nil {
		opts = append(opts,
			grpc.UnaryInterceptor(AuthInterceptor(cfg.Authenticator)),
			grpc.StreamInterceptor(AuthStreamInterceptor(cfg.Authenticator)),
		)
	}
	s.grpcServer = grpc.NewServer(opts...)

	s.triggerService = NewTriggerService(cfg.Audit, cfg.MaxPayloadBytes, cfg.IdempotencyStore)
	triggerv1.RegisterTriggerServiceServer(s.grpcServer, s.triggerService)

	return s, nil
}

// Start begins serving gRPC and REST.
func (s *Server) Start(parent context.Context) error {
	grpcListener, err := net.Listen("tcp", s.cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", s.cfg.GRPCAddr, err)
	}
	s.grpcListener = grpcListener
	s.cfg.GRPCAddr = grpcListener.Addr().String()
	go func() {
		_ = s.grpcServer.Serve(grpcListener)
	}()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := triggerv1.RegisterTriggerServiceHandlerFromEndpoint(parent, mux, s.cfg.GRPCAddr, opts); err != nil {
		s.grpcServer.Stop()
		return fmt.Errorf("register gateway: %w", err)
	}

	var handler http.Handler = mux
	if s.cfg.CORS != nil {
		handler = s.cfg.CORS.Wrap(handler)
	}

	restListener, err := net.Listen("tcp", s.cfg.RESTAddr)
	if err != nil {
		s.grpcServer.Stop()
		return fmt.Errorf("listen REST %s: %w", s.cfg.RESTAddr, err)
	}
	s.restListener = restListener
	s.cfg.RESTAddr = restListener.Addr().String()
	s.restServer = &http.Server{
		Addr:              s.cfg.RESTAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = s.restServer.Serve(restListener)
	}()

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.restServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.restServer.Shutdown(ctx)
	}
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}
}

// TriggerService implements triggerv1.TriggerServiceServer.
type TriggerService struct {
	triggerv1.UnimplementedTriggerServiceServer

	audit       audit.AuditAppender
	maxPayload  int
	idempotency *IdempotencyStore
}

// NewTriggerService creates the trigger service implementation.
func NewTriggerService(a audit.AuditAppender, maxPayload int, stores ...*IdempotencyStore) *TriggerService {
	if maxPayload == 0 {
		maxPayload = DefaultMaxPayload
	}
	var store *IdempotencyStore
	if len(stores) > 0 {
		store = stores[0]
	}
	return &TriggerService{audit: a, maxPayload: maxPayload, idempotency: store}
}

// Invoke triggers an agent run. T01 returns a pending stub run.
func (s *TriggerService) Invoke(ctx context.Context, req *triggerv1.InvokeRequest) (*triggerv1.InvokeResponse, error) {
	if len(req.GetPayload()) > s.maxPayload {
		return nil, status.Errorf(codes.InvalidArgument, "payload exceeds %d bytes", s.maxPayload)
	}

	runID, err := generateRunID()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "generate run id: %v", err)
	}
	if s.idempotency != nil && req.GetIdempotencyKey() != "" {
		caller, _ := CallerFromContext(ctx)
		requestHash := CanonicalRequestHash(
			string(caller),
			req.GetAgentName(),
			invokeMetadataValue(req, "agent_lock_digest"),
			req.GetPayload(),
			req.GetContentType(),
			invokeAPIVersion(req),
		)
		result, entry, err := s.idempotency.CheckOrReserve(ctx, req.GetIdempotencyKey(), runID, requestHash, string(caller), req.GetAgentName())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "check idempotency: %v", err)
		}
		switch result {
		case IdempotencyReplayed:
			runID = entry.RunID
		case IdempotencyConflict:
			return nil, status.Error(codes.AlreadyExists, "idempotency key conflict: different payload")
		case IdempotencyNew:
		}
	}

	run := &triggerv1.Run{
		RunId:     runID,
		AgentName: req.GetAgentName(),
		Status:    triggerv1.RunStatus_RUN_STATUS_PENDING,
	}
	return &triggerv1.InvokeResponse{Run: run}, nil
}

// InvokeStream triggers a run and streams updates. T01 sends one pending stub.
func (s *TriggerService) InvokeStream(req *triggerv1.InvokeRequest, stream triggerv1.TriggerService_InvokeStreamServer) error {
	if len(req.GetPayload()) > s.maxPayload {
		return status.Errorf(codes.InvalidArgument, "payload exceeds %d bytes", s.maxPayload)
	}
	run := &triggerv1.Run{
		RunId:     "run-stub-stream",
		AgentName: req.GetAgentName(),
		Status:    triggerv1.RunStatus_RUN_STATUS_PENDING,
	}
	return stream.Send(&triggerv1.InvokeResponse{Run: run})
}

// GetRun retrieves a run by ID. T01 leaves storage unimplemented.
func (s *TriggerService) GetRun(context.Context, *triggerv1.GetRunRequest) (*triggerv1.Run, error) {
	return nil, status.Error(codes.Unimplemented, "GetRun not implemented in T01 stub")
}

// CancelRun cancels a run. T01 leaves run control unimplemented.
func (s *TriggerService) CancelRun(context.Context, *triggerv1.CancelRunRequest) (*triggerv1.Run, error) {
	return nil, status.Error(codes.Unimplemented, "CancelRun not implemented in T01 stub")
}

// ListRuns lists runs. T01 returns an empty page.
func (s *TriggerService) ListRuns(context.Context, *triggerv1.ListRunsRequest) (*triggerv1.ListRunsResponse, error) {
	return &triggerv1.ListRunsResponse{}, nil
}

func generateRunID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "run-" + hex.EncodeToString(b[:]), nil
}

func invokeMetadataValue(req *triggerv1.InvokeRequest, key string) string {
	if req.GetMetadata() == nil {
		return ""
	}
	return req.GetMetadata()[key]
}

func invokeAPIVersion(req *triggerv1.InvokeRequest) string {
	if version := invokeMetadataValue(req, "api_version"); version != "" {
		return version
	}
	return "trigger.v1"
}
