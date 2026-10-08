package openshellrt

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/dockerclient"
)

// Status is what doctor and version report. It never includes a secret.
type Status struct {
	CLIPath        string
	CLIVersion     string
	GatewayAddress string
	GatewayReady   bool
	GatewayDetail  string
}

// Probe reports the pinned OpenShell CLI and whether the local gateway answers.
func Probe() Status {
	st := Status{GatewayAddress: GatewayAddr}
	path, err := LookBinary("openshell")
	if err != nil {
		st.CLIVersion = "not found"
		st.GatewayDetail = "openshell binary not found"
		return st
	}
	st.CLIPath = path
	ver, err := binaryVersion(path)
	if err != nil {
		st.CLIVersion = "unknown"
		st.GatewayDetail = err.Error()
	} else {
		st.CLIVersion = ver
	}
	if ready, detail := gatewayReady(); ready {
		st.GatewayReady = true
		st.GatewayDetail = detail
	} else if st.GatewayDetail == "" {
		st.GatewayDetail = detail
	}
	return st
}

// LookBinary finds the official openshell CLI next to this process, in dir, or on PATH.
func LookBinary(name string) (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("%s not found", name)
}

func binaryVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version: %w", filepath.Base(path), err)
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", errors.New("empty version")
	}
	return fields[len(fields)-1], nil
}

func gatewayReady() (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rt, err := Dial(ctx)
	if err != nil {
		return false, "gateway not ready"
	}
	defer func() { _ = rt.Close() }()
	ver, err := rt.GatewayVersion(ctx)
	if err != nil {
		return false, "gateway not ready"
	}
	if ver == "" {
		ver = PinVersion
	}
	return true, ver
}

// InstallDir is the directory make build and the daemon share for the pinned binaries.
func InstallDir(home string) string {
	if home != "" {
		return filepath.Join(home, "openshell", PinVersion)
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "bin"
}

// gatewayGOOS is the host OS used when writing gateway.toml. Tests override
// it so darwin and non-darwin configs are both checked without depending
// on the machine.
var gatewayGOOS = runtime.GOOS

// writeGatewayConfig writes a schema-v2 gateway file that uses the host Docker
// socket (colima or Docker Desktop) and plaintext loopback. No secrets.
// On darwin the Docker driver runs inside a VM, so the supervisor must dial
// the host gateway through host.docker.internal. Native Linux stays on the
// driver's 127.0.0.1 default. bind_address is unchanged.
func writeGatewayConfig(dir, socketPath string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	jwtDir := filepath.Join(dir, "jwt")
	if err := os.MkdirAll(jwtDir, 0o700); err != nil {
		return "", err
	}
	signing := filepath.Join(jwtDir, "signing.pem")
	public := filepath.Join(jwtDir, "public.pem")
	kid := filepath.Join(jwtDir, "kid")
	if _, err := os.Stat(signing); err != nil {
		if err := generateJWTKeys(signing, public, kid); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, "gateway.toml")
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "[openshell]\nversion = 2\n\n")
	fmt.Fprintf(&buf, "[openshell.gateway]\nbind_address = %q\ncompute_driver = \"docker\"\nname = \"agentpaas\"\n\n", GatewayAddr)
	fmt.Fprintf(&buf, "[openshell.gateway.gateway_jwt]\nsigning_key_path = %q\npublic_key_path = %q\nkid_path = %q\ngateway_id = \"agentpaas\"\n\n", signing, public, kid)
	fmt.Fprintf(&buf, "[openshell.gateway.auth]\nallow_unauthenticated_users = true\n\n")
	fmt.Fprintf(&buf, "[openshell.drivers.docker]\nsocket_path = %q\nimage_pull_policy = \"if_not_present\"\nsandbox_label = \"agentpaas\"\n", socketPath)
	if gatewayGOOS == "darwin" {
		port, err := gatewayListenPort(GatewayAddr)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&buf, "grpc_endpoint = %q\n", "http://host.docker.internal:"+port)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func gatewayListenPort(addr string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("gateway address %q: %w", addr, err)
	}
	if port == "" {
		return "", fmt.Errorf("gateway address %q has no port", addr)
	}
	return port, nil
}

func generateJWTKeys(signing, public, kid string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate openshell jwt key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("encode openshell jwt key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("encode openshell jwt public key: %w", err)
	}
	if err := os.WriteFile(signing, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(public, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644); err != nil {
		return err
	}
	return os.WriteFile(kid, []byte("agentpaas\n"), 0o644)
}

func dockerSocket() (string, error) {
	p, err := dockerclient.SocketPath()
	if err != nil {
		return "", err
	}
	if p == "" {
		return "", errors.New("docker socket path is empty")
	}
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("docker socket %s: %w", p, err)
	}
	return p, nil
}
