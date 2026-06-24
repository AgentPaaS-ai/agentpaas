// Package dockerclient provides a Docker client factory that mirrors the
// Docker CLI's endpoint discovery order. client.FromEnv alone only honors
// explicit environment variables (DOCKER_HOST) and does NOT read Docker's
// context store (~/.docker/contexts/meta), which is how Docker Desktop,
// colima, and Podman set their socket on macOS/Linux. Without this resolver,
// a daemon/pack process that inherits an environment without DOCKER_HOST
// cannot reach a context-backed Docker daemon even though `docker ps` works.
//
// Resolution order (matches `docker context show` semantics):
//  1. DOCKER_HOST environment variable (explicit override — highest priority).
//  2. Docker context store: ~/.docker/config.json "currentContext" →
//     ~/.docker/contexts/meta/<digest>/meta.json "Endpoints.docker.Host".
//  3. Platform default socket.
package dockerclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/docker/docker/client"
)

// defaultSocket returns the platform-default Docker daemon socket path.
func defaultSocket() string {
	if runtime.GOOS == "windows" {
		return "npipe:////./pipe/docker_engine"
	}
	return "unix:///var/run/docker.sock"
}

// contextMeta mirrors the subset of the Docker context meta.json we need.
type contextMeta struct {
	Name      string `json:"Name"`
	Endpoints struct {
		Docker struct {
			Host           string `json:"Host"`
			SkipTLSVerify  bool   `json:"SkipTLSVerify"`
			CACertPath     string `json:"CACertPath"`
			CertPath       string `json:"CertPath"`
			KeyPath        string `json:"KeyPath"`
		} `json:"docker"`
	} `json:"Endpoints"`
}

// resolveHost returns the Docker daemon endpoint, honoring the resolution
// order documented in the package comment.
func resolveHost() (host string, source string, err error) {
	// 1. Explicit env override.
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return h, "DOCKER_HOST", nil
	}

	// 2. Docker context store.
	if h, ctxName, errCtx := fromContextStore(); errCtx == nil && h != "" {
		return h, "context:" + ctxName, nil
	}

	// 3. Platform default.
	return defaultSocket(), "default", nil
}

// fromContextStore reads ~/.docker/config.json for the current context name,
// then resolves its endpoint host from the context meta directory.
func fromContextStore() (host string, contextName string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}

	cfgPath := filepath.Join(home, ".docker", "config.json")
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		return "", "", err
	}

	var cfg struct {
		CurrentContext string `json:"currentContext"`
	}
	if err := json.Unmarshal(cfgBytes, &cfg); err != nil {
		return "", "", fmt.Errorf("parse %s: %w", cfgPath, err)
	}
	if cfg.CurrentContext == "" {
		return "", "", nil // no context configured
	}

	// Context metas are stored in digest-named directories. Scan for the
	// matching Name field rather than hashing the context name ourselves.
	// The meta file is named "meta" on some Docker versions and "meta.json"
	// on others (colima/Docker Desktop), so try both.
	metaDir := filepath.Join(home, ".docker", "contexts", "meta")
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return "", cfg.CurrentContext, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var data []byte
		for _, name := range []string{"meta", "meta.json"} {
			data, err = os.ReadFile(filepath.Join(metaDir, e.Name(), name))
			if err == nil {
				break
			}
		}
		if err != nil {
			continue
		}
		var m contextMeta
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.Name == cfg.CurrentContext {
			return m.Endpoints.Docker.Host, cfg.CurrentContext, nil
		}
	}
	return "", cfg.CurrentContext, fmt.Errorf("context %q not found in store", cfg.CurrentContext)
}

// New returns a Docker client configured with the resolved endpoint, plus
// API version negotiation. TLS settings are applied when the resolved host
// uses TLS (tcp:// with certs configured in the context). It is safe to
// call from both the CLI process and the daemon subprocess.
func New() (*client.Client, error) {
	host, _, err := resolveHost()
	if err != nil {
		return nil, fmt.Errorf("resolve Docker host: %w", err)
	}
	opts := []client.Opt{
		client.WithHost(host),
		client.WithAPIVersionNegotiation(),
	}

	// If host is a tcp:// endpoint with TLS, honor DOCKER_TLS_VERIFY and the
	// standard cert path env vars. unix:// and npipe:// don't use TLS.
	if u, err := url.Parse(host); err == nil && u.Scheme == "tcp" {
		if v := os.Getenv("DOCKER_TLS_VERIFY"); v != "" {
			certDir := os.Getenv("DOCKER_CERT_PATH")
			if certDir == "" {
				certDir = filepath.Join(os.Getenv("HOME"), ".docker")
			}
			opts = append(opts,
				client.WithTLSClientConfig(
					filepath.Join(certDir, "ca.pem"),
					filepath.Join(certDir, "cert.pem"),
					filepath.Join(certDir, "key.pem"),
				),
			)
		}
	}

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("create Docker client (host=%s): %w", host, err)
	}
	return cli, nil
}

// Ping verifies that the Docker daemon at the resolved endpoint is reachable
// within the given timeout. Returns a descriptive error if not. Used by the
// daemon startup and `agent doctor` to give the user actionable feedback.
func Ping(timeout time.Duration) error {
	cli, err := New()
	if err != nil {
		return err
	}
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// If the host is a unix socket, check it's actually present and
	// reachable at the filesystem level first — gives a much clearer
	// error than a raw dial timeout.
	host, _, _ := resolveHost()
	if u, err := url.Parse(host); err == nil && u.Scheme == "unix" {
		sockPath := u.Path
		if _, err := os.Stat(sockPath); err != nil {
			return fmt.Errorf("Docker socket not found at %s (context/colima/Docker Desktop may not be running): %w", sockPath, err)
		}
		// Quick dial to confirm it's a live socket.
		c, err := net.DialTimeout("unix", sockPath, 2*time.Second)
		if err != nil {
			return fmt.Errorf("cannot connect to Docker socket %s: %w", sockPath, err)
		}
		_ = c.Close()
	}

	if _, err := cli.Ping(ctx); err != nil {
		return fmt.Errorf("Docker daemon ping failed: %w", err)
	}
	return nil
}

// ErrDaemonUnreachable is returned when the Docker daemon cannot be reached.
var ErrDaemonUnreachable = errors.New("Docker daemon unreachable")

// ExportHostToEnv resolves the Docker endpoint (same order as New) and, if it
// was discovered from the Docker context store rather than an explicit
// DOCKER_HOST env var, writes it to os.Setenv("DOCKER_HOST", ...). This makes
// the resolved endpoint visible to child processes that only honor DOCKER_HOST
// (syft, cosign, buildx, docker CLI itself). It is a no-op when DOCKER_HOST is
// already set in the environment or the resolved host equals the platform
// default socket. Call this once at daemon startup before spawning any
// Docker-dependent external tool.
func ExportHostToEnv() error {
	host, source, err := resolveHost()
	if err != nil {
		return err
	}
	// Only export if we resolved via context store — avoid clobbering an
	// explicit DOCKER_HOST or redundantly setting the default.
	if source == "DOCKER_HOST" || source == "default" {
		return nil
	}
	return os.Setenv("DOCKER_HOST", host)
}
