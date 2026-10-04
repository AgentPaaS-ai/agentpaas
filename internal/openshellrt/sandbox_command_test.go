package openshellrt

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	osv1 "github.com/NVIDIA/OpenShell/sdk/go/openshell/v1"
)

// stubOpenShell records the sandbox spec CreateAgentSandbox hands to
// Sandboxes().Create. Other client methods are unused on this path.
type stubOpenShell struct {
	osv1.ClientInterface
	sandboxes osv1.SandboxInterface
}

func (s *stubOpenShell) Sandboxes() osv1.SandboxInterface { return s.sandboxes }

type recordingSandboxes struct {
	osv1.SandboxInterface
	created bool
	command []string
	image   string
}

func (s *recordingSandboxes) Create(_ context.Context, _, name string, spec *osv1.SandboxSpec, _ map[string]string, _ ...osv1.CreateOptions) (*osv1.Sandbox, error) {
	s.created = true
	if spec != nil {
		s.command = append([]string(nil), spec.Command...)
		if spec.Template != nil {
			s.image = spec.Template.Image
		}
	}
	return &osv1.Sandbox{
		Name:        name,
		ServiceURLs: map[string]string{"": "http://127.0.0.1:9"},
	}, nil
}

func (s *recordingSandboxes) WaitReady(_ context.Context, _, name string, _ ...osv1.WaitOptions) (*osv1.Sandbox, error) {
	return &osv1.Sandbox{
		Name:        name,
		ServiceURLs: map[string]string{"": "http://127.0.0.1:9"},
	}, nil
}

func TestCreateAgentSandboxCommandIsHarnessNotShell(t *testing.T) {
	const image = "localhost:5001/agentpaas/o02-golden:0.1.0"
	rec := &recordingSandboxes{}
	rt := &Runtime{client: &stubOpenShell{sandboxes: rec}}

	_, err := rt.CreateAgentSandbox(context.Background(), SandboxRequest{
		Name:  "ap-o02",
		Image: image,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.created {
		t.Fatal("Sandboxes().Create was not called")
	}
	if rec.image != image {
		t.Fatalf("image = %q, want %q; do not change the packed image", rec.image, image)
	}
	if len(rec.command) == 0 {
		t.Fatal("empty sandbox command resolves to a login shell at /bin/sh; want [/agentpaas/harness]")
	}
	want := []string{"/agentpaas/harness"}
	if !reflect.DeepEqual(rec.command, want) {
		t.Fatalf("sandbox command = %#v, want %#v", rec.command, want)
	}
	for _, arg := range rec.command {
		if isShellExecutable(arg) {
			t.Fatalf("sandbox command must not be a shell, got %#v", rec.command)
		}
	}
}

func isShellExecutable(arg string) bool {
	switch filepath.Base(arg) {
	case "sh", "bash", "dash", "zsh", "ash", "csh", "ksh", "fish":
		return true
	default:
		return false
	}
}
