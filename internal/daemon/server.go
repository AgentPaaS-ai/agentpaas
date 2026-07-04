package daemon

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/audit"
	"github.com/parvezsyed/agentpaas/internal/dashboard"
	"github.com/parvezsyed/agentpaas/internal/home"
	"github.com/parvezsyed/agentpaas/internal/otel"
	"github.com/parvezsyed/agentpaas/internal/trigger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// defaultShutdownTimeout is the maximum time to wait for in-flight RPCs
	// to complete before force-stopping the server.
	defaultShutdownTimeout = 10 * time.Second
)

// Daemon is the AgentPaaS control daemon. It binds a gRPC server to a Unix
// domain socket and serves the ControlService API.
//
// Use New() to create, Start() to begin serving, and Stop() or HandleSignal()
// for graceful shutdown.
type Daemon struct {
	paths   *home.HomePaths
	version VersionInfo

	mu            sync.Mutex
	server        *grpc.Server
	listener      net.Listener
	ready         bool
	started       bool
	stopped       bool
	lockFile      *os.File
	lockIno       lockIno
	pidFile       string
	auditIndexer  *audit.SQLiteIndexer
	confirmations *ConfirmationStore
	control       *controlServer
	dashboard     *dashboard.Server
	dashboardAddr string
	otelStore     *otel.Store
	eventBus      *trigger.EventBus
	triggerServer *trigger.Server
	triggerCancel context.CancelFunc
	cronScheduler *trigger.CronScheduler

	// allowRoot bypasses the root-user check. Used only for tests.
	allowRoot bool
}

// lockIno holds the inode of the lock file at the time the flock was
// acquired. It is used to detect whether the lock file has been deleted
// and recreated (which would create a new inode and bypass the flock).
type lockIno struct {
	dev uint64
	ino uint64
}

// Option configures the Daemon.
type Option func(*Daemon)

// WithAllowRoot allows the daemon to run as root. This is intended for
// integration tests and containerized environments where running as root
// is expected.
func WithAllowRoot() Option {
	return func(d *Daemon) {
		d.allowRoot = true
	}
}

// WithDashboard sets the dashboard listen address. Pass an empty string to
// disable the dashboard server.
func WithDashboard(addr string) Option {
	return func(d *Daemon) {
		d.dashboardAddr = addr
	}
}

// New creates a new Daemon bound to the given home directory paths.
//
// It acquires an exclusive flock on the lock file to prevent multiple daemon
// instances. If the lock cannot be acquired (EWOULDBLOCK), New returns an
// error indicating that another daemon is already running.
//
// Callers must call Start() to begin serving, and Stop() (or HandleSignal)
// to shut down gracefully.
func New(paths *home.HomePaths, version VersionInfo, opts ...Option) (*Daemon, error) {
	d := &Daemon{
		paths:         paths,
		version:       version,
		confirmations: NewConfirmationStore(),
	}

	for _, opt := range opts {
		opt(d)
	}

	// Acquire exclusive flock on the lock file.
	lockFile, err := os.OpenFile(paths.Lock, os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("daemon: cannot open lock file %s: %w", paths.Lock, err)
	}

	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lockFile.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("daemon: lock file %s is held by another process — daemon is already running", paths.Lock)
		}
		return nil, fmt.Errorf("daemon: flock error on %s: %w", paths.Lock, err)
	}

	// Verify the lock file inode hasn't been replaced between Open and Flock.
	// If the file was deleted and a new one created at the same path, the
	// FD's inode will differ from the path's inode, and the flock on the
	// old inode provides no protection against a second daemon.
	li, err := lockFileInode(lockFile)
	if err != nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("daemon: cannot stat lock file %s: %w", paths.Lock, err)
	}
	pathFi, err := os.Stat(paths.Lock)
	if err != nil {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("daemon: cannot stat lock file path %s: %w", paths.Lock, err)
	}
	if li.dev != uint64(pathFi.Sys().(*syscall.Stat_t).Dev) || li.ino != pathFi.Sys().(*syscall.Stat_t).Ino {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
		return nil, fmt.Errorf("daemon: lock file %s was replaced (inode mismatch) — refusing to start", paths.Lock)
	}

	d.lockFile = lockFile
	d.lockIno = li
	return d, nil
}

// Start binds the Unix socket, starts the gRPC server, and begins serving.
//
// Before binding, it:
//  1. Calls home.Ensure() to set up the home directory layout.
//  2. Calls home.ValidatePermissions() to check security invariants.
//  3. Calls home.CleanStale() to remove stale socket/lock/pid files.
//  4. Checks that the daemon is not running as root (unless WithAllowRoot).
//  5. Writes the PID file.
//  6. Binds the Unix socket with mode 0600.
//  7. Registers the ControlService gRPC server.
//
// After Start() returns, the daemon is listening but not yet ready to serve
// requests. Call Ready() to transition to the ready state.
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.started {
		return fmt.Errorf("daemon: already started")
	}
	if d.stopped {
		return fmt.Errorf("daemon: already stopped")
	}

	// Check root.
	if !d.allowRoot {
		if err := CheckRoot(os.Getuid(), false); err != nil {
			return err
		}
	}

	// Ensure home directory exists with correct permissions.
	if err := home.Ensure(d.paths); err != nil {
		return fmt.Errorf("daemon: home setup: %w", err)
	}

	// After Ensure, verify the lock file hasn't been replaced. Ensure may
	// recreate runtime files that were deleted, and a recreated lock file
	// would have a new inode with no flock held.
	if err := d.reacquireLock(); err != nil {
		return fmt.Errorf("daemon: lock file verification after Ensure: %w", err)
	}

	// Validate permissions before serving.
	if err := home.ValidatePermissions(d.paths); err != nil {
		return fmt.Errorf("daemon: permission check: %w", err)
	}

	// Clean stale runtime files.
	if err := home.CleanStale(d.paths); err != nil {
		return fmt.Errorf("daemon: stale file cleanup: %w", err)
	}

	// Write PID file.
	pidData := []byte(strconv.Itoa(os.Getpid()) + "\n")
	if err := os.WriteFile(d.paths.PID, pidData, 0600); err != nil {
		return fmt.Errorf("daemon: cannot write PID file %s: %w", d.paths.PID, err)
	}
	d.pidFile = d.paths.PID

	// Remove stale socket file if present (though CleanStale should have done this).
	_ = os.Remove(d.paths.Socket)

	// Bind Unix socket.
	ln, err := net.Listen("unix", d.paths.Socket)
	if err != nil {
		_ = d.cleanupFiles()
		return fmt.Errorf("daemon: cannot bind Unix socket %s: %w", d.paths.Socket, err)
	}
	d.listener = ln

	// Set socket permissions to 0600.
	_ = os.Chmod(d.paths.Socket, 0600)

	auditIndex, err := audit.NewSQLiteIndexer(filepath.Join(d.paths.State, "audit.db"))
	if err != nil {
		_ = ln.Close()
		_ = d.cleanupFiles()
		return fmt.Errorf("daemon: open audit index: %w", err)
	}
	d.auditIndexer = auditIndex

	auditPath := filepath.Join(d.paths.State, "audit.jsonl")
	checkpointPath := filepath.Join(d.paths.State, "audit.jsonl.checkpoints")
	keyPath := filepath.Join(d.paths.State, "audit-checkpoint-key.der")
	keyDER, checkpointPubKey, err := audit.LoadOrGenerateCheckpointKey(keyPath)
	if err != nil {
		_ = auditIndex.Close()
		_ = ln.Close()
		_ = d.cleanupFiles()
		return fmt.Errorf("daemon: load checkpoint signing key: %w", err)
	}
	log.Printf("daemon: audit checkpoint key loaded (encrypted at rest)")
	checkpointCadence := audit.DefaultCheckpointCadence
	if raw := strings.TrimSpace(os.Getenv("AGENTPAAS_AUDIT_CHECKPOINT_CADENCE")); raw != "" {
		if n, parseErr := strconv.ParseInt(raw, 10, 64); parseErr == nil && n > 0 {
			checkpointCadence = n
		}
	}
	auditWriter, err := audit.NewAuditWriterWithCheckpoints(auditPath, checkpointPath, checkpointCadence, keyDER)
	if err != nil {
		_ = auditIndex.Close()
		_ = ln.Close()
		_ = d.cleanupFiles()
		return fmt.Errorf("daemon: open audit writer: %w", err)
	}
	// Rebuild the index from the existing chain (if any) for recovery.
	if err := auditIndex.Rebuild(auditPath); err != nil {
		// Non-fatal: index will be empty, dashboard queries return empty.
		fmt.Fprintf(os.Stderr, "daemon: audit index rebuild: %v\n", err)
	}

	dashboardAddr := d.dashboardAddr
	if dashboardAddr == "" {
		dashboardAddr = os.Getenv("AGENTPAAS_DASHBOARD_ADDR")
	}
	if dashboardAddr == "" {
		dashboardAddr = "127.0.0.1:8090"
	}
	if dashboardAddr != "off" && dashboardAddr != "disabled" {
		// Create otel store for timeline/logs/cost data.
		otelStore, err := otel.NewStore(ctx, filepath.Join(d.paths.State, "otel.db"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "daemon: otel store: %v\n", err)
		} else {
			d.otelStore = otelStore
		}

		// Create event bus for live run timeline events.
		d.eventBus = trigger.NewEventBus()

		d.dashboard = dashboard.NewServerWithAudit(
			dashboardAddr,
			"",
			d.otelStore,
			nil, // ResourceManager wired below after control server is created
			d.auditIndexer,
		)
		d.dashboard.SetEventBus(d.eventBus)
		if pubDER, marshalErr := x509.MarshalPKIXPublicKey(checkpointPubKey); marshalErr == nil {
			d.dashboard.SetAuditTrustAnchor(pubDER)
		}
		// Capture the dashboard reference before starting the goroutine
		// to avoid a data race with Stop() which sets d.dashboard = nil.
		dash := d.dashboard
		go func() {
			if err := dash.ListenAndServe(); err != nil {
				fmt.Fprintf(os.Stderr, "daemon: dashboard error: %v\n", err)
			}
		}()
	}

	// Create gRPC server with readiness interceptor.
	d.server = grpc.NewServer(
		grpc.UnaryInterceptor(d.readinessInterceptor),
		grpc.StreamInterceptor(d.readinessStreamInterceptor),
	)

	// Register stub ControlService handlers.
	controlServer := &controlServer{
		version:               d.version,
		auditIndex:            d.auditIndexer,
		auditWriter:           auditWriter,
		homePaths:             d.paths,
		eventBus:              d.eventBus,
		auditCheckpointPubKey: checkpointPubKey,
		auditCheckpointsPath:  checkpointPath,
	}
	attachConfirmationStore(controlServer, d.confirmations)
	d.control = controlServer
	if d.dashboard != nil {
		if rt, err := controlServer.getOrCreateRuntime(); err == nil {
			d.dashboard.SetResourceManager(NewDockerResourceManager(rt))
		}
	}
	controlv1.RegisterControlServiceServer(d.server, controlServer)

	// Start trigger server for external invocations (loopback-only for P1).
	triggerGRPCAddr := os.Getenv("AGENTPAAS_TRIGGER_GRPC_ADDR")
	if triggerGRPCAddr == "" {
		triggerGRPCAddr = "127.0.0.1:7718"
	}
	triggerRESTAddr := os.Getenv("AGENTPAAS_TRIGGER_REST_ADDR")
	if triggerRESTAddr == "" {
		triggerRESTAddr = "127.0.0.1:7717"
	}

	triggerAPIKey := os.Getenv("AGENTPAAS_TRIGGER_API_KEY")
	triggerExpose := os.Getenv("AGENTPAAS_TRIGGER_EXPOSE")
	exposeTrigger := triggerExpose == "1" || strings.EqualFold(triggerExpose, "true")
	if exposeTrigger && triggerAPIKey == "" {
		fmt.Fprintf(os.Stderr, "--expose requires AGENTPAAS_TRIGGER_API_KEY to be set\n")
		return fmt.Errorf("--expose requires AGENTPAAS_TRIGGER_API_KEY to be set")
	}

	triggerCfg := trigger.ServerConfig{
		GRPCAddr: triggerGRPCAddr,
		RESTAddr: triggerRESTAddr,
		EventBus: d.eventBus,
		Audit:    auditWriter,
		Exposed:  exposeTrigger,
	}
	if triggerAPIKey != "" {
		keyHash := sha256.Sum256([]byte(triggerAPIKey))
		keyID := "env-" + hex.EncodeToString(keyHash[:8])
		triggerCfg.Authenticator = trigger.NewAPIKeyAuthenticator(map[string]*trigger.APIKeyMeta{
			triggerAPIKey: {
				ID:     keyID,
				Scopes: []string{"trigger"},
			},
		})
		fmt.Fprintf(os.Stderr, "daemon: trigger server API key authentication enabled\n")
	}

	triggerSrv, err := trigger.New(triggerCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: trigger server init: %v\n", err)
	} else {
		triggerSrv.SetInvokeFunc(func(ctx context.Context, agentName string, payload []byte) (string, error) {
			resp, err := controlServer.Run(ctx, &controlv1.RunRequest{
				AgentName:      agentName,
				TriggerPayload: payload,
			})
			if err != nil {
				return "", err
			}
			return resp.GetRunId(), nil
		})
		triggerCtx, triggerCancel := context.WithCancel(context.Background())
		if err := triggerSrv.Start(triggerCtx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: trigger server start: %v\n", err)
			triggerCancel()
		} else {
			d.triggerServer = triggerSrv
			d.triggerCancel = triggerCancel
		}
	}

	// Start cron scheduler for scheduled agent invocations.
	cronStatePath := filepath.Join(d.paths.State, "cron-schedules.json")
	triggerSvc := trigger.NewTriggerService(auditWriter, trigger.DefaultMaxPayload, d.eventBus, nil)
	triggerSvc.SetInvokeFunc(func(ctx context.Context, agentName string, payload []byte) (string, error) {
		resp, err := controlServer.Run(ctx, &controlv1.RunRequest{AgentName: agentName, TriggerPayload: payload})
		if err != nil {
			return "", err
		}
		return resp.GetRunId(), nil
	})
	cronCfg := trigger.CronConfig{
		Audit:          auditWriter,
		StatePath:      cronStatePath,
		TriggerService: triggerSvc,
	}
	d.cronScheduler = trigger.NewCronScheduler(cronCfg)
	controlServer.cronScheduler = d.cronScheduler
	d.cronScheduler.Start()
	fmt.Fprintf(os.Stderr, "daemon: cron scheduler started (state: %s)\n", cronStatePath)

	reconcileCtx, reconcileCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer reconcileCancel()
	controlServer.reconcileOrphanedContainers(reconcileCtx)

	d.started = true

	// Start serving in background.
	go func() {
		if err := d.server.Serve(ln); err != nil && err != grpc.ErrServerStopped {
			fmt.Fprintf(os.Stderr, "daemon: gRPC server error: %v\n", err)
		}
	}()

	return nil
}

// Ready transitions the daemon to the ready state. After this call, RPCs
// are processed normally instead of returning Unavailable.
//
// The daemon is intentionally NOT ready immediately after Start() so that
// the caller can perform post-boot initialization before accepting RPCs.
func (d *Daemon) Ready() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.ready = true
}

// IsReady returns whether the daemon is in the ready state.
func (d *Daemon) IsReady() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.ready
}

// Stop gracefully shuts down the daemon. It:
//  1. Stops accepting new RPCs (GracefulStop).
//  2. Waits up to the default shutdown timeout for in-flight RPCs to drain.
//  3. Force-stops if the timeout expires.
//  4. Removes the PID file.
//  5. Releases the lock file.
//
// Stop is idempotent and safe to call multiple times.
func (d *Daemon) Stop(ctx context.Context) error {
	d.mu.Lock()
	alreadyStopped := d.stopped
	d.stopped = true
	d.mu.Unlock()

	if alreadyStopped {
		return nil
	}

	// GracefulStop stops accepting new connections/shutdowns and blocks
	// until all pending RPCs are finished.
	done := make(chan struct{})
	go func() {
		if d.server != nil {
			d.server.GracefulStop()
		}
		close(done)
	}()

	// Wait for graceful shutdown or timeout.
	select {
	case <-done:
		// All RPCs finished in time.
	case <-ctx.Done():
		// Timeout expired — force stop.
		if d.server != nil {
			d.server.Stop()
		}
	}

	// Close the listener if still open.
	if d.listener != nil {
		_ = d.listener.Close()
	}
	if d.auditIndexer != nil {
		_ = d.auditIndexer.Close()
		d.auditIndexer = nil
	}
	if d.otelStore != nil {
		_ = d.otelStore.Close()
		d.otelStore = nil
	}
	if d.control != nil && d.control.auditWriter != nil {
		_ = d.control.auditWriter.Close()
	}
	if d.control != nil {
		detachConfirmationStore(d.control)
		d.control = nil
	}
	// Read dashboard under lock to avoid racing with the goroutine in
	// Start() that reads it without holding the mutex.
	d.mu.Lock()
	dash := d.dashboard
	d.dashboard = nil
	d.mu.Unlock()
	if dash != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = dash.Shutdown(shutdownCtx)
		shutdownCancel()
	}

	if d.triggerCancel != nil {
		d.triggerCancel()
	}
	if d.triggerServer != nil {
		d.triggerServer.Stop()
	}
	if d.cronScheduler != nil {
		d.cronScheduler.Stop()
	}

	// Clean up files.
	_ = d.cleanupFiles()

	// Release the flock.
	if d.lockFile != nil {
		_ = syscall.Flock(int(d.lockFile.Fd()), syscall.LOCK_UN)
		_ = d.lockFile.Close()
		d.lockFile = nil
	}

	return nil
}

// HandleSignal registers a signal handler that triggers graceful shutdown
// when the given signal is received. The signal is sent to sigCh by the
// caller (or by os/signal).
//
// This is a test-friendly wrapper: instead of using signal.Notify directly,
// it accepts a channel that the test or main goroutine feeds signals into.
func (d *Daemon) HandleSignal(sig os.Signal, sigCh chan os.Signal) error {
	switch sig {
	case syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP:
	default:
		return fmt.Errorf("daemon: unsupported signal %v", sig)
	}

	go func() {
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		_ = d.Stop(ctx)
	}()

	return nil
}

// cleanupFiles removes the PID file and socket file left behind by the daemon.
func (d *Daemon) cleanupFiles() error {
	var firstErr error

	if d.pidFile != "" {
		if err := os.Remove(d.pidFile); err != nil && !os.IsNotExist(err) {
			firstErr = err
		}
		d.pidFile = ""
	}

	if d.paths != nil && d.paths.Socket != "" {
		if err := os.Remove(d.paths.Socket); err != nil && !os.IsNotExist(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

// readinessInterceptor is a gRPC unary interceptor that returns Unavailable
// if the daemon is not yet ready.
func (d *Daemon) readinessInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if !d.IsReady() {
		return nil, status.Error(codes.Unavailable, "daemon is starting up — not ready to serve requests")
	}
	return handler(ctx, req)
}

// readinessStreamInterceptor is a gRPC stream interceptor that returns
// Unavailable if the daemon is not yet ready.
func (d *Daemon) readinessStreamInterceptor(srv interface{}, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	if !d.IsReady() {
		return status.Error(codes.Unavailable, "daemon is starting up — not ready to serve requests")
	}
	return handler(srv, stream)
}

// CheckRoot checks whether the daemon is running as root and returns an error
// if it is, unless allowRoot is true. This protects against accidental
// privilege escalation.
func CheckRoot(uid int, allowRoot bool) error {
	if uid == 0 && !allowRoot {
		return fmt.Errorf("daemon: refusing to run as root — use --allow-root-for-test to bypass (not recommended for production)")
	}
	return nil
}

// lockFileInode returns the device and inode number of the open file.
func lockFileInode(f *os.File) (lockIno, error) {
	fi, err := f.Stat()
	if err != nil {
		return lockIno{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return lockIno{}, fmt.Errorf("unexpected file info type %T", fi.Sys())
	}
	return lockIno{dev: uint64(st.Dev), ino: st.Ino}, nil
}

// reacquireLock checks whether the lock file on disk still refers to the
// same inode as the open FD. If the inode differs (because the file was
// deleted and recreated), it re-opens the new file, re-acquires the flock,
// and updates the daemon's lockFile and lockIno fields.
//
// NOTE: d.mu MUST be held by the caller.
func (d *Daemon) reacquireLock() error {
	li, err := lockFileInode(d.lockFile)
	if err != nil {
		return fmt.Errorf("daemon: cannot stat lock file FD: %w", err)
	}
	pathFi, err := os.Stat(d.paths.Lock)
	if err != nil {
		if os.IsNotExist(err) {
			// Lock file was deleted and not recreated — we still hold
			// the flock on the old inode, but there's nothing to do.
			return nil
		}
		return fmt.Errorf("daemon: cannot stat lock file path %s: %w", d.paths.Lock, err)
	}
	pathSt := pathFi.Sys().(*syscall.Stat_t)

	// If inodes match, the lock file hasn't been replaced.
	if li.dev == uint64(pathSt.Dev) && li.ino == pathSt.Ino {
		return nil
	}

	// The lock file was replaced — open the new file and re-acquire the flock.
	newFile, err := os.OpenFile(d.paths.Lock, os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("daemon: cannot re-open replaced lock file %s: %w", d.paths.Lock, err)
	}

	if err := syscall.Flock(int(newFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = newFile.Close()
		if err == syscall.EWOULDBLOCK {
			return fmt.Errorf("daemon: lock file %s replaced and already held by another process", d.paths.Lock)
		}
		return fmt.Errorf("daemon: flock error on replaced lock file %s: %w", d.paths.Lock, err)
	}

	// Verify the new file's inode is stable.
	newLI, err := lockFileInode(newFile)
	if err != nil {
		_ = syscall.Flock(int(newFile.Fd()), syscall.LOCK_UN)
		_ = newFile.Close()
		return fmt.Errorf("daemon: cannot stat new lock file FD: %w", err)
	}
	newPathFi, err := os.Stat(d.paths.Lock)
	if err != nil {
		_ = syscall.Flock(int(newFile.Fd()), syscall.LOCK_UN)
		_ = newFile.Close()
		return fmt.Errorf("daemon: cannot stat new lock file path: %w", err)
	}
	newPathSt := newPathFi.Sys().(*syscall.Stat_t)
	if newLI.dev != uint64(newPathSt.Dev) || newLI.ino != newPathSt.Ino {
		_ = syscall.Flock(int(newFile.Fd()), syscall.LOCK_UN)
		_ = newFile.Close()
		return fmt.Errorf("daemon: new lock file was also replaced during re-acquisition")
	}

	// Release the old lock and swap.
	_ = syscall.Flock(int(d.lockFile.Fd()), syscall.LOCK_UN)
	_ = d.lockFile.Close()
	d.lockFile = newFile
	d.lockIno = newLI

	return nil
}
