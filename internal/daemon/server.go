package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/home"
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

	mu       sync.Mutex
	server   *grpc.Server
	listener net.Listener
	ready    bool
	started  bool
	stopped  bool
	lockFile *os.File
	pidFile  string

	// allowRoot bypasses the root-user check. Used only for tests.
	allowRoot bool
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
		paths:   paths,
		version: version,
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

	d.lockFile = lockFile
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

	// Create gRPC server with readiness interceptor.
	d.server = grpc.NewServer(
		grpc.UnaryInterceptor(d.readinessInterceptor),
		grpc.StreamInterceptor(d.readinessStreamInterceptor),
	)

	// Register stub ControlService handlers.
	controlv1.RegisterControlServiceServer(d.server, &stubControlServer{
		version: d.version,
	})

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
	case syscall.SIGTERM, syscall.SIGINT:
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
