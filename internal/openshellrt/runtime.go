package openshellrt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

// Runtime is the local OpenShell gateway the daemon drives through the SDK.
type Runtime struct {
	client  *osv1.Client
	cmd     *exec.Cmd
	cliPath string
	version string
}

// Dial connects to an already-running local gateway. It does not start one.
func Dial(ctx context.Context) (*Runtime, error) {
	client, err := osv1.NewClient(osv1.Config{
		Address: GatewayURL,
		Auth:    osv1.NoAuth(),
	})
	if err != nil {
		return nil, err
	}
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	health, err := client.Health().Check(hctx)
	if err != nil || health == nil || !health.Healthy {
		_ = client.Close()
		if err == nil {
			err = errors.New("gateway unhealthy")
		}
		return nil, err
	}
	return &Runtime{client: client, version: health.Version}, nil
}

// EnsureLocal installs the pinned N-1 binaries if needed, starts the local
// gateway when it is not already healthy, and returns an SDK client.
func EnsureLocal(ctx context.Context, home, binDir string) (*Runtime, error) {
	if binDir == "" {
		binDir = InstallDir(home)
	}
	if err := EnsureInstalled(binDir); err != nil {
		return nil, err
	}
	if rt, err := Dial(ctx); err == nil {
		rt.cliPath = filepath.Join(binDir, "openshell")
		return rt, nil
	}
	socketPath, err := dockerSocket()
	if err != nil {
		return nil, fmt.Errorf("openshell docker socket: %w", err)
	}
	cfgDir := filepath.Join(home, "openshell")
	cfgPath, err := writeGatewayConfig(cfgDir, socketPath)
	if err != nil {
		return nil, err
	}
	stateDir := filepath.Join(home, "openshell", "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	gw := filepath.Join(binDir, "openshell-gateway")
	cmd := exec.Command(gw, "--disable-tls", "--port", "17670", "--config", cfgPath, "--log-level", "info")
	cmd.Env = append(os.Environ(),
		"OPENSHELL_DISABLE_TLS=1",
		"XDG_STATE_HOME="+stateDir,
		"XDG_CONFIG_HOME="+cfgDir,
	)
	logPath := filepath.Join(cfgDir, "gateway.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return nil, fmt.Errorf("start openshell gateway: %w", err)
	}
	go func() { _ = cmd.Wait() }()

	var rt *Runtime
	deadline := time.Now().Add(45 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			_ = cmd.Process.Kill()
			return nil, ctx.Err()
		}
		rt, last = Dial(ctx)
		if last == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if last != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("openshell gateway did not become ready: %w", last)
	}
	rt.cmd = cmd
	rt.cliPath = filepath.Join(binDir, "openshell")
	_ = registerCLI(binDir)
	return rt, nil
}

func registerCLI(binDir string) error {
	cli := filepath.Join(binDir, "openshell")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, "gateway", "add", GatewayURL, "--local", "--name", "agentpaas")
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "already") && !strings.Contains(strings.ToLower(string(out)), "exists") {
		return fmt.Errorf("register gateway: %w", err)
	}
	_ = exec.CommandContext(ctx, cli, "gateway", "select", "agentpaas").Run()
	return nil
}

// Close releases the SDK client. The gateway process keeps running so the
// next daemon start can reuse it.
func (r *Runtime) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

func (r *Runtime) Address() string {
	if r == nil {
		return ""
	}
	return GatewayAddr
}

func (r *Runtime) CLIVersion() string {
	if r == nil || r.cliPath == "" {
		return PinVersion
	}
	ver, err := binaryVersion(r.cliPath)
	if err != nil || ver == "" {
		return PinVersion
	}
	return ver
}

func (r *Runtime) GatewayVersion(ctx context.Context) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("openshell client is nil")
	}
	health, err := r.client.Health().Check(ctx)
	if err != nil {
		return "", err
	}
	if health == nil || !health.Healthy {
		return "", errors.New("openshell gateway unhealthy")
	}
	if health.Version != "" {
		return health.Version, nil
	}
	return PinVersion, nil
}

// EnsureBroker registers the keychain secret with the OpenShell provider.
// The secret is not logged and is not placed in the sandbox environment.
func (r *Runtime) EnsureBroker(ctx context.Context, credID, secret string) (string, error) {
	if r == nil || r.client == nil {
		return "", errors.New("openshell client is nil")
	}
	if secret == "" {
		return "", fmt.Errorf("broker credential %q is empty", credID)
	}
	if err := r.ensureProfile(ctx); err != nil {
		return "", err
	}
	name := providerName(credID)
	_, err := r.client.Providers().Ensure(ctx, workspaceDefault, &osv1.Provider{
		Name: name,
		Type: profileID,
		Spec: osv1.ProviderSpec{
			Credentials: map[string]string{"api_key": secret},
		},
	})
	if err != nil {
		return "", fmt.Errorf("openshell provider %s: %w", name, err)
	}
	return name, nil
}

func (r *Runtime) ensureProfile(ctx context.Context) error {
	if _, err := r.client.Providers().Profiles().Get(ctx, workspaceDefault, profileID); err == nil {
		return nil
	}
	res, err := r.client.Providers().Profiles().Import(ctx, workspaceDefault, []osv1.ProfileImportItem{{
		Profile: brokerProfile(),
		Source:  "agentpaas",
	}})
	if err != nil {
		return fmt.Errorf("import openshell profile: %w", err)
	}
	if res != nil && !res.Imported {
		return fmt.Errorf("%s", formatDiag("openshell profile rejected", res.Diagnostics))
	}
	return nil
}

// SandboxRequest is the agent sandbox the daemon asks OpenShell to create.
type SandboxRequest struct {
	Name        string
	Image       string
	Env         map[string]string
	Provider    string
	Rules       []EgressRule
	BrokerHost  string
	HarnessPort uint32
}

// SandboxHandle is a running OpenShell sandbox and the loopback service URL.
type SandboxHandle struct {
	Name       string
	ServiceURL string
}

// CreateAgentSandbox creates a sandbox, waits until it is ready, and returns
// the service URL the daemon invokes. The agent environment in req.Env must
// not contain a secret value.
func (r *Runtime) CreateAgentSandbox(ctx context.Context, req SandboxRequest) (*SandboxHandle, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("openshell client is nil")
	}
	if req.HarnessPort == 0 {
		req.HarnessPort = 8080
	}
	spec := &osv1.SandboxSpec{
		Template:    &osv1.SandboxTemplate{Image: req.Image},
		Environment: req.Env,
		Policy:      SandboxPolicy(req.Rules, req.Provider, req.BrokerHost),
	}
	if req.Provider != "" {
		spec.Providers = []string{req.Provider}
	}
	sb, err := r.client.Sandboxes().Create(ctx, workspaceDefault, req.Name, spec, map[string]string{
		"agentpaas": "1",
		"runtime":   "openshell",
	}, osv1.CreateOptions{ServiceExposures: []osv1.ServiceExposure{{
		TargetPort: req.HarnessPort,
	}}})
	if err != nil {
		return nil, fmt.Errorf("openshell sandbox create: %w", err)
	}
	ready, err := r.client.Sandboxes().WaitReady(ctx, workspaceDefault, sb.Name)
	if err != nil {
		detail := err.Error()
		if got, gerr := r.client.Sandboxes().Get(ctx, workspaceDefault, sb.Name); gerr == nil && got != nil {
			for _, c := range got.Status.Conditions {
				if c.Message != "" || c.Reason != "" {
					detail += "; " + c.Type + " " + c.Reason + " " + c.Message
				}
			}
		}
		_ = r.DeleteSandbox(context.Background(), sb.Name)
		return nil, fmt.Errorf("openshell sandbox ready: %s", detail)
	}
	url := ""
	if ready != nil && ready.ServiceURLs != nil {
		url = ready.ServiceURLs[""]
	}
	if url == "" && sb.ServiceURLs != nil {
		url = sb.ServiceURLs[""]
	}
	if url == "" {
		_ = r.DeleteSandbox(context.Background(), sb.Name)
		return nil, errors.New("openshell sandbox has no service URL")
	}
	return &SandboxHandle{Name: sb.Name, ServiceURL: url}, nil
}

// DeleteSandbox removes a sandbox. Missing sandboxes are not an error.
func (r *Runtime) DeleteSandbox(ctx context.Context, name string) error {
	if r == nil || r.client == nil || name == "" {
		return nil
	}
	_, err := r.client.Sandboxes().Delete(ctx, workspaceDefault, name, osv1.DeleteOptions{AllowMissing: true})
	return err
}

// GatewayListening reports whether the loopback gateway port accepts TCP.
func GatewayListening() bool {
	c, err := net.DialTimeout("tcp", GatewayAddr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

var linkOnce sync.Once

// LinkBeside copies or links the pinned CLI into dir so PATH finds `openshell`.
func LinkBeside(fromDir, destDir string) {
	linkOnce.Do(func() {
		for _, name := range []string{"openshell", "openshell-gateway"} {
			src := filepath.Join(fromDir, name)
			dst := filepath.Join(destDir, name)
			if src == dst {
				continue
			}
			if _, err := os.Stat(src); err != nil {
				continue
			}
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			if err := os.Symlink(src, dst); err != nil {
				in, err := os.Open(src)
				if err != nil {
					continue
				}
				out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, 0o755)
				if err != nil {
					_ = in.Close()
					continue
				}
				_, _ = ioCopy(out, in)
				_ = out.Close()
				_ = in.Close()
			}
		}
	})
}

func ioCopy(dst, src *os.File) (int64, error) {
	return dst.ReadFrom(src)
}
