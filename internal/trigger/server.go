package trigger

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	triggerv1 "github.com/parvezsyed/agentpaas/api/trigger/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	EventBus         *EventBus
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
	if cfg.EventBus == nil {
		cfg.EventBus = NewEventBus()
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

	s.triggerService = NewTriggerService(cfg.Audit, cfg.MaxPayloadBytes, cfg.EventBus, cfg.IdempotencyStore)
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

	mux := newRESTGatewayMux()
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if err := triggerv1.RegisterTriggerServiceHandlerFromEndpoint(parent, mux, s.cfg.GRPCAddr, opts); err != nil {
		s.grpcServer.Stop()
		return fmt.Errorf("register gateway: %w", err)
	}

	handler := jsonValidationMiddleware(mux)
	if s.cfg.EventBus != nil {
		sseHandler := NewSSEHandler(s.cfg.EventBus)
		gatewayHandler := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/v1/trigger/events" && r.Method == http.MethodGet {
				runID := r.URL.Query().Get("run_id")
				if runID == "" {
					http.Error(w, "run_id required", http.StatusBadRequest)
					return
				}
				if s.cfg.Authenticator != nil {
					ctx := r.Context()
					if token := bearerToken(r.Header.Get("Authorization")); token != "" {
						ctx = WithAuthToken(ctx, token)
					}
					if caller, method, err := s.cfg.Authenticator.Authenticate(ctx); err == nil {
						ctx = WithCaller(ctx, caller, method)
					}
					r = r.WithContext(ctx)
				}
				sseHandler.ServeSSE(w, r, runID)
				return
			}
			gatewayHandler.ServeHTTP(w, r)
		})
	}
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

func newRESTGatewayMux() *runtime.ServeMux {
	jsonMarshaler := &lineNumberJSONMarshaler{JSONPb: &runtime.JSONPb{}}
	return runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, jsonMarshaler),
		runtime.WithMarshalerOption("application/json", jsonMarshaler),
	)
}

func jsonValidationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodPut {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeRESTJSONError(w, http.StatusBadRequest, "failed to read body")
			return
		}
		_ = r.Body.Close()

		if len(bytes.TrimSpace(body)) == 0 {
			writeRESTJSONError(w, http.StatusBadRequest, "request body is required")
			return
		}
		if line, column, ok := rawNullByteLocation(body); ok {
			writeRESTJSONError(w, http.StatusBadRequest, fmt.Sprintf("request body contains null bytes at line %d, column %d", line, column))
			return
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		next.ServeHTTP(w, r)
	})
}

func writeRESTJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    int(codes.InvalidArgument),
		"message": msg,
	})
}

type lineNumberJSONMarshaler struct {
	*runtime.JSONPb
}

func (m *lineNumberJSONMarshaler) NewDecoder(r io.Reader) runtime.Decoder {
	return runtime.DecoderFunc(func(v interface{}) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}

		var raw json.RawMessage
		if err := json.NewDecoder(bytes.NewReader(data)).Decode(&raw); err != nil {
			return jsonErrorWithLine(data, err)
		}
		if line, column, ok := jsonNullStringLocation(data); ok {
			return status.Error(codes.InvalidArgument, fmt.Sprintf("request body contains null bytes at line %d, column %d", line, column))
		}
		return m.Unmarshal(raw, v)
	})
}

func jsonNullStringLocation(data []byte) (int, int, bool) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0, 0, false
	}
	if !valueContainsNullString(value) {
		return 0, 0, false
	}
	return escapedNullByteLocation(data)
}

func valueContainsNullString(value any) bool {
	switch typed := value.(type) {
	case string:
		return bytes.IndexByte([]byte(typed), 0x00) >= 0
	case []any:
		for _, item := range typed {
			if valueContainsNullString(item) {
				return true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if bytes.IndexByte([]byte(key), 0x00) >= 0 || valueContainsNullString(item) {
				return true
			}
		}
	}
	return false
}

func rawNullByteLocation(data []byte) (int, int, bool) {
	offset := bytes.IndexByte(data, 0x00)
	if offset < 0 {
		return 0, 0, false
	}
	line, column := lineColumnAtOffset(data, offset+1)
	return line, column, true
}

func escapedNullByteLocation(data []byte) (int, int, bool) {
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		i++
		for i < len(data) {
			switch data[i] {
			case '\\':
				if isEscapedNullByte(data[i:]) {
					line, column := lineColumnAtOffset(data, i+1)
					return line, column, true
				}
				if i+1 < len(data) {
					i += 2
					continue
				}
			case '"':
				goto nextString
			}
			i++
		}
	nextString:
	}
	return 0, 0, false
}

func isEscapedNullByte(data []byte) bool {
	if len(data) < 6 || data[0] != '\\' || data[1] != 'u' {
		return false
	}
	for _, b := range data[2:6] {
		if b != '0' {
			return false
		}
	}
	return true
}

func jsonErrorWithLine(data []byte, err error) error {
	line, column := jsonErrorLineColumn(data, err)
	return fmt.Errorf("%w at line %d, column %d", err, line, column)
}

func jsonErrorLineColumn(data []byte, err error) (int, int) {
	offset := len(data) + 1
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) && syntaxErr.Offset > 0 {
		offset = int(syntaxErr.Offset)
	}
	if offset < 1 {
		offset = 1
	}
	if offset > len(data)+1 {
		offset = len(data) + 1
	}

	return lineColumnAtOffset(data, offset)
}

func lineColumnAtOffset(data []byte, offset int) (int, int) {
	line := 1 + bytes.Count(data[:offset-1], []byte("\n"))
	lineStart := bytes.LastIndexByte(data[:offset-1], '\n')
	column := offset
	if lineStart >= 0 {
		column = offset - lineStart - 1
	}
	return line, column
}

// SetInvokeFunc wires Invoke to call the daemon's Run handler.
// The fn receives the agent name and the user's trigger payload bytes
// (from InvokeRequest.payload) so the payload reaches the agent's
// handle_invoke() instead of being dropped.
func (s *Server) SetInvokeFunc(fn func(ctx context.Context, agentName string, payload []byte) (string, error)) {
	s.triggerService.SetInvokeFunc(fn)
}

// SetInvokeFunc wires Invoke to the given run handler.
func (s *TriggerService) SetInvokeFunc(fn func(ctx context.Context, agentName string, payload []byte) (string, error)) {
	s.invokeFunc = fn
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
	eventBus    *EventBus
	runStore    *RunStore

	cancelGracePeriod time.Duration

	invokeFunc func(ctx context.Context, agentName string, payload []byte) (string, error)
}

// NewTriggerService creates the trigger service implementation.
func NewTriggerService(a audit.AuditAppender, maxPayload int, deps ...any) *TriggerService {
	if maxPayload == 0 {
		maxPayload = DefaultMaxPayload
	}
	var store *IdempotencyStore
	var bus *EventBus
	for _, dep := range deps {
		switch typed := dep.(type) {
		case *EventBus:
			bus = typed
		case *IdempotencyStore:
			store = typed
		}
	}
	if bus == nil {
		bus = NewEventBus()
	}
	return &TriggerService{
		audit:             a,
		maxPayload:        maxPayload,
		idempotency:       store,
		eventBus:          bus,
		runStore:          NewRunStore(),
		cancelGracePeriod: CancelGracePeriod,
	}
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

	if s.invokeFunc != nil {
		actualRunID, err := s.invokeFunc(ctx, req.GetAgentName(), req.GetPayload())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "invoke agent: %v", err)
		}
		runID = actualRunID
		run := &triggerv1.Run{
			RunId:     runID,
			AgentName: req.GetAgentName(),
			Status:    triggerv1.RunStatus_RUN_STATUS_RUNNING,
		}
		entry := s.runStore.Register(runID, req.GetAgentName())
		run.CreatedAt = entry.toRun().GetCreatedAt()
		s.runStore.MarkStarted(runID)
		return &triggerv1.InvokeResponse{Run: run}, nil
	}

	run := &triggerv1.Run{
		RunId:     runID,
		AgentName: req.GetAgentName(),
		Status:    triggerv1.RunStatus_RUN_STATUS_PENDING,
	}
	entry := s.runStore.Register(runID, req.GetAgentName())
	run.CreatedAt = entry.toRun().GetCreatedAt()
	return &triggerv1.InvokeResponse{Run: run}, nil
}

// InvokeStream triggers a run and streams lifecycle updates.
func (s *TriggerService) InvokeStream(req *triggerv1.InvokeRequest, stream triggerv1.TriggerService_InvokeStreamServer) error {
	ctx := stream.Context()
	if len(req.GetPayload()) > s.maxPayload {
		return status.Errorf(codes.InvalidArgument, "payload exceeds %d bytes", s.maxPayload)
	}

	runID, err := generateRunID()
	if err != nil {
		return status.Errorf(codes.Internal, "generate run id: %v", err)
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
			return status.Errorf(codes.Internal, "check idempotency: %v", err)
		}
		switch result {
		case IdempotencyReplayed:
			runID = entry.RunID
		case IdempotencyConflict:
			return status.Error(codes.AlreadyExists, "idempotency key conflict: different payload")
		case IdempotencyNew:
		}
	}

	s.runStore.Register(runID, req.GetAgentName())
	s.eventBus.RegisterRun(runID)
	s.eventBus.Publish(runID, EventRunCreated, map[string]string{"agent": req.GetAgentName()})
	s.runStore.MarkFinished(runID, triggerv1.RunStatus_RUN_STATUS_SUCCEEDED)
	s.eventBus.Publish(runID, EventRunSucceeded, map[string]string{"agent": req.GetAgentName()})

	ch, cancel := s.eventBus.Subscribe(runID, 0)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-ch:
			if !open {
				return nil
			}
			run := eventToRun(&event, runID, req.GetAgentName())
			if err := stream.Send(&triggerv1.InvokeResponse{Run: run}); err != nil {
				return err
			}
			if event.IsTerminal() {
				return nil
			}
		}
	}
}

// GetRun retrieves a run by ID.
func (s *TriggerService) GetRun(_ context.Context, req *triggerv1.GetRunRequest) (*triggerv1.Run, error) {
	if req.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	entry, ok := s.runStore.Get(req.GetRunId())
	if !ok {
		return nil, status.Errorf(codes.NotFound, "run %s not found", req.GetRunId())
	}
	return entry.toRun(), nil
}

// ListRuns lists known runs.
func (s *TriggerService) ListRuns(_ context.Context, req *triggerv1.ListRunsRequest) (*triggerv1.ListRunsResponse, error) {
	start := 0
	if req.GetPageToken() != "" {
		offset, err := strconv.Atoi(req.GetPageToken())
		if err != nil || offset < 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid page_token")
		}
		start = offset
	}
	pageSize := int(req.GetPageSize())
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 100 {
		pageSize = 100
	}

	entries := s.runStore.List()
	sort.Slice(entries, func(i, j int) bool {
		return runSortKey(entries[i]) < runSortKey(entries[j])
	})

	runs := make([]*triggerv1.Run, 0, len(entries))
	for _, entry := range entries {
		run := entry.toRun()
		if req.GetAgentName() != "" && run.GetAgentName() != req.GetAgentName() {
			continue
		}
		if req.GetStatus() != triggerv1.RunStatus_RUN_STATUS_UNSPECIFIED && run.GetStatus() != req.GetStatus() {
			continue
		}
		runs = append(runs, run)
	}
	if start >= len(runs) {
		return &triggerv1.ListRunsResponse{}, nil
	}
	end := start + pageSize
	if end > len(runs) {
		end = len(runs)
	}
	resp := &triggerv1.ListRunsResponse{Runs: runs[start:end]}
	if end < len(runs) {
		resp.NextPageToken = strconv.Itoa(end)
	}
	return resp, nil
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

func eventToRun(event *RunEvent, runID, agentName string) *triggerv1.Run {
	run := &triggerv1.Run{
		RunId:     runID,
		AgentName: agentName,
	}
	switch event.Type {
	case EventRunCreated:
		run.Status = triggerv1.RunStatus_RUN_STATUS_PENDING
	case EventRunStarted:
		run.Status = triggerv1.RunStatus_RUN_STATUS_RUNNING
	case EventRunSucceeded:
		run.Status = triggerv1.RunStatus_RUN_STATUS_SUCCEEDED
	case EventRunFailed:
		run.Status = triggerv1.RunStatus_RUN_STATUS_FAILED
	case EventRunCancelled:
		run.Status = triggerv1.RunStatus_RUN_STATUS_CANCELLED
	case EventRunProgress, EventHeartbeat:
		run.Status = triggerv1.RunStatus_RUN_STATUS_RUNNING
	}

	ts := timestamppb.New(event.Timestamp)
	if event.EventID <= 1 {
		run.CreatedAt = ts
	} else if event.IsTerminal() {
		run.FinishedAt = ts
	}
	return run
}
