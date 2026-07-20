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
	"log"
	"net"
	"net/http"
	"sort"
	"strconv"
	"time"

	triggerv1 "github.com/AgentPaaS-ai/agentpaas/api/trigger/v1"
	"github.com/AgentPaaS-ai/agentpaas/internal/audit"
	"github.com/AgentPaaS-ai/agentpaas/internal/port"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
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
		_ = s.grpcServer.Serve(grpcListener) // intentionally ignored (reviewed)
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
		_ = s.restServer.Serve(restListener) // intentionally ignored (reviewed)
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
		_ = r.Body.Close() // best-effort close

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
	_ = json.NewEncoder(w).Encode(map[string]any{ // best-effort encode to client
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
			return fmt.Errorf("line number jsonmarshaler new decoder: %w", err)
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

// SetEventStore wires a durable EventStore on the underlying TriggerService so
// InvokeStream uses the real durable admission path (durable run_created,
// subscribe, bridge real execution events) instead of the in-memory EventBus
// fallback. Production (the daemon) wires a DurableEventStore pointing at
// ~/.agentpaas/state/events/ so InvokeStream events survive reconnection and
// are replayable after restart.
func (s *Server) SetEventStore(store port.EventStore) {
	s.triggerService.SetEventStore(store)
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
		_ = s.restServer.Shutdown(ctx) // best-effort cleanup
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
	// eventStore is the durable EventStore used by InvokeStream when set. If
	// nil, InvokeStream falls back to the in-memory EventBus (legacy path).
	// T03 wires this so InvokeStream events survive reconnection; T04
	// replaces the synthetic admission path with the real durable one.
	eventStore port.EventStore
	// triggerTenant is the tenant ID under which trigger-originated runs are
	// appended to the EventStore. Defaults to defaultTriggerTenant.
	triggerTenant string
	runStore      *RunStore

	cancelGracePeriod time.Duration

	invokeFunc func(ctx context.Context, agentName string, payload []byte) (string, error)
}

// defaultTriggerTenant is the tenant ID used for runs admitted by the
// TriggerService itself (Invoke/InvokeStream) when the caller does not
// present a tenant-scoped identity. Events appended by InvokeStream use this
// tenant so they are durable and replayable after restart.
const defaultTriggerTenant = "trigger"

// NewTriggerService creates the trigger service implementation.
func NewTriggerService(a audit.AuditAppender, maxPayload int, deps ...any) *TriggerService {
	if maxPayload == 0 {
		maxPayload = DefaultMaxPayload
	}
	var store *IdempotencyStore
	var bus *EventBus
	var eventStore port.EventStore
	for _, dep := range deps {
		switch typed := dep.(type) {
		case *EventBus:
			bus = typed
		case *IdempotencyStore:
			store = typed
		case port.EventStore:
			// *DurableEventStore satisfies port.EventStore, so this case
			// matches both the interface and the concrete type. Do not add a
			// separate *DurableEventStore case — SA4020 flags it as
			// unreachable since the interface case matches first.
			eventStore = typed
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
		eventStore:        eventStore,
		triggerTenant:     defaultTriggerTenant,
		runStore:          NewRunStore(),
		cancelGracePeriod: CancelGracePeriod,
	}
}

// SetEventStore wires a durable EventStore. When set, InvokeStream appends
// synthetic events to it (durable) instead of the in-memory EventBus.
func (s *TriggerService) SetEventStore(store port.EventStore) {
	s.eventStore = store
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
		caller, _ := CallerFromContext(ctx) // optional caller identity
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
//
// When a durable EventStore is wired (production), InvokeStream uses the REAL
// admission path: it runs the same admission logic as Invoke (payload check,
// runID generation, idempotency, runStore.Register), appends a durable
// run_created event, subscribes to the durable store, and — when invokeFunc is
// set — starts the real execution path and bridges its EventBus events into
// the durable store so the subscription delivers them to the client. The run
// reaches terminal via the real execution path, NOT inline: InvokeStream never
// manufactures success when a durable store is configured.
//
// When no durable EventStore is configured (EventBus fallback), the legacy
// synthetic admission path is retained for backward compatibility with
// existing tests, with a log warning. Production always wires the durable
// store (see daemon wiring), so the synthetic path is test-only.
func (s *TriggerService) InvokeStream(req *triggerv1.InvokeRequest, stream triggerv1.TriggerService_InvokeStreamServer) error {
	ctx := stream.Context()
	if len(req.GetPayload()) > s.maxPayload {
		return status.Errorf(codes.InvalidArgument, "payload exceeds %d bytes", s.maxPayload)
	}

	runID, err := generateRunID()
	if err != nil {
		return status.Errorf(codes.Internal, "generate run id: %v", err)
	}
	idempotencyReplay := false
	if s.idempotency != nil && req.GetIdempotencyKey() != "" {
		caller, _ := CallerFromContext(ctx) // optional caller identity
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
			idempotencyReplay = true
		case IdempotencyConflict:
			return status.Error(codes.AlreadyExists, "idempotency key conflict: different payload")
		case IdempotencyNew:
		}
	}

	// Real durable admission path: durable EventStore is the source of truth.
	// The run is admitted (registered + run_created appended) and the client
	// stream is fed from the durable subscription. The terminal event is
	// produced by the real execution path (invokeFunc -> EventBus -> bridge ->
	// durable store), never manufactured inline by InvokeStream.
	if s.eventStore != nil {
		return s.invokeStreamDurable(ctx, req, stream, runID, idempotencyReplay)
	}

	// EventBus fallback: synthetic admission path. Retained for backward
	// compatibility with tests that do not wire a durable EventStore.
	// Production always wires the durable store, so this path is test-only.
	log.Printf("trigger: InvokeStream using synthetic EventBus fallback (no durable EventStore wired); run will not be replayable after restart")
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

// invokeStreamDurable implements the real admission path backed by the durable
// EventStore. It admits the run, subscribes to the durable store, starts the
// real execution path (invokeFunc) when available, and bridges EventBus
// lifecycle events into the durable store so the subscription delivers them to
// the client. The terminal event comes from the real execution path, not
// inline.
func (s *TriggerService) invokeStreamDurable(
	ctx context.Context,
	req *triggerv1.InvokeRequest,
	stream triggerv1.TriggerService_InvokeStreamServer,
	runID string,
	idempotencyReplay bool,
) error {
	tenant := s.triggerTenant
	agentName := req.GetAgentName()

	// Admit the run in the runStore. On idempotency replay the run was already
	// registered by the original invocation; Register is idempotent (returns
	// the existing entry), so this is safe to call unconditionally.
	s.runStore.Register(runID, agentName)

	// On a fresh (non-replay) admission, append the durable run_created event.
	// On replay, the original invocation already appended it; we must NOT
	// append a duplicate (the durable store would record two run_created
	// events with different sequences, corrupting replay).
	if !idempotencyReplay {
		if _, err := s.eventStore.Append(ctx, port.Event{
			TenantID:  tenant,
			RunID:     runID,
			Type:      string(EventRunCreated),
			Payload:   []byte(agentName),
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return status.Errorf(codes.Internal, "append run_created: %v", err)
		}
	}

	// Subscribe to the durable store BEFORE starting execution so no live
	// events are missed. Existing events (including run_created, and on replay
	// any prior terminal event) are replayed from cursor 0.
	subCh, err := s.eventStore.Subscribe(ctx, tenant, runID, 0)
	if err != nil {
		return status.Errorf(codes.Internal, "subscribe durable: %v", err)
	}

	// Start the real execution path when invokeFunc is wired and this is not an
	// idempotency replay (a replay must NOT re-execute). A bridge goroutine
	// carries EventBus lifecycle events for the run into the durable store so
	// the subscription delivers them to the client. The bridge exits when it
	// observes a terminal EventBus event.
	if !idempotencyReplay && s.invokeFunc != nil {
		// Register the run on the EventBus so the bridge can subscribe before
		// invokeFunc publishes. invokeFunc returns the canonical runID used by
		// the real execution path; the EventBus events are published under it.
		actualRunID, invokeErr := s.invokeFunc(ctx, agentName, req.GetPayload())
		if invokeErr != nil {
			// Real execution failed to start. Record a terminal failure in the
			// durable store so the client stream and replay observe it; never
			// manufacture success.
			s.runStore.MarkFinished(runID, triggerv1.RunStatus_RUN_STATUS_FAILED)
			if _, aErr := s.eventStore.Append(ctx, port.Event{
				TenantID:  tenant,
				RunID:     runID,
				Type:      string(EventRunFailed),
				Payload:   []byte(invokeErr.Error()),
				Timestamp: time.Now().UTC(),
			}); aErr != nil {
				return status.Errorf(codes.Internal, "invoke failed: %v; and append run_failed: %v", invokeErr, aErr)
			}
			// Fall through to stream the terminal from the subscription.
		} else {
			// The real execution path publishes lifecycle events to the
			// EventBus under actualRunID. Bridge them into the durable store
			// under (tenant, runID) so the subscription delivers them.
			s.bridgeEventBusToStore(ctx, actualRunID, runID, tenant)
		}
	}

	// Stream events from the durable subscription to the client until a
	// terminal event arrives (or the client context is cancelled). On
	// idempotency replay, the subscription replays the already-committed
	// terminal event and returns immediately.
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-subCh:
			if !open {
				return nil
			}
			run := durableEventToRun(event, runID, agentName)
			if err := stream.Send(&triggerv1.InvokeResponse{Run: run}); err != nil {
				return err
			}
			// Mirror the runStore terminal status from the durable event so
			// GetRun/ListRuns observe the real terminal (not a stale PENDING).
			switch EventType(event.Type) {
			case EventRunSucceeded:
				s.runStore.MarkFinished(runID, triggerv1.RunStatus_RUN_STATUS_SUCCEEDED)
			case EventRunFailed:
				s.runStore.MarkFinished(runID, triggerv1.RunStatus_RUN_STATUS_FAILED)
			case EventRunCancelled:
				s.runStore.MarkFinished(runID, triggerv1.RunStatus_RUN_STATUS_CANCELLED)
			}
			if isTerminalEventType(event.Type) {
				return nil
			}
		}
	}
}

// bridgeEventBusToStore subscribes to the EventBus for eventBusRunID and
// appends each lifecycle event to the durable EventStore under (tenant,
// storeRunID). It exits when a terminal EventBus event is observed (and after
// appending that terminal to the store). This bridges the real execution
// path's EventBus publications into the durable store so the InvokeStream
// subscription — and replay after restart — observes the real lifecycle.
//
// The bridge runs in its own goroutine so InvokeStream can stream events to
// the client concurrently. It is best-effort with respect to the client
// stream: if the client disconnects (ctx cancelled), the bridge stops
// appending; the durable store retains whatever was appended before
// cancellation, and the real execution path's terminal (if it arrives later)
// is appended by the execution path itself in production.
func (s *TriggerService) bridgeEventBusToStore(ctx context.Context, eventBusRunID, storeRunID, tenant string) {
	busCh, cancel := s.eventBus.Subscribe(eventBusRunID, 0)
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-busCh:
				if !open {
					return
				}
				payload := eventBusPayloadToBytes(event.Data)
				if _, err := s.eventStore.Append(ctx, port.Event{
					TenantID:  tenant,
					RunID:     storeRunID,
					Type:      string(event.Type),
					Payload:   payload,
					Timestamp: event.Timestamp,
				}); err != nil {
					// A lost lifecycle event is an audit gap; log it so an
					// operator can reconcile. The real execution path's
					// terminal is still observable via the EventBus in
					// production (dashboard), but the durable replay will be
					// incomplete.
					log.Printf("trigger: bridgeEventBusToStore append failed for tenant=%q run=%q type=%s: %v",
						tenant, storeRunID, event.Type, err)
				}
				if event.IsTerminal() {
					return
				}
			}
		}
	}()
}

// eventBusPayloadToBytes serializes an EventBus event's Data field to a byte
// payload for the durable store. EventBus Data is a map[string]any (or nil);
// we JSON-encode it for a stable, replayable representation. Encoding errors
// are extremely unlikely (the data is already JSON-marshalable in practice)
// and fall back to nil payload rather than dropping the event.
func eventBusPayloadToBytes(data any) []byte {
	if data == nil {
		return nil
	}
	b, err := json.Marshal(data)
	if err != nil {
		return nil
	}
	return b
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
		return "", fmt.Errorf("generate run id: %w", err)
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

// isTerminalEventType reports whether a durable event type string represents a
// terminal lifecycle transition (succeeded/failed/cancelled).
func isTerminalEventType(eventType string) bool {
	switch EventType(eventType) {
	case EventRunSucceeded, EventRunFailed, EventRunCancelled:
		return true
	}
	return false
}

// durableEventToRun converts a port.Event (from the durable EventStore) into a
// triggerv1.Run for streaming to the client. Mirrors eventToRun but operates
// on the durable event shape (Sequence instead of EventID).
func durableEventToRun(event port.Event, runID, agentName string) *triggerv1.Run {
	run := &triggerv1.Run{
		RunId:     runID,
		AgentName: agentName,
	}
	switch EventType(event.Type) {
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
	if event.Sequence <= 1 {
		run.CreatedAt = ts
	} else if isTerminalEventType(event.Type) {
		run.FinishedAt = ts
	}
	return run
}
