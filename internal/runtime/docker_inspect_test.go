package runtime

import (
	"context"
	"net/netip"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// inspectContainer adapts the moby v0.6 client inspect result to the
// container record tests already assert on.
func inspectContainer(ctx context.Context, cli *client.Client, id string) (container.InspectResponse, error) {
	res, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return container.InspectResponse{}, err
	}
	return res.Container, nil
}

func ipString(addr netip.Addr) string {
	if !addr.IsValid() {
		return ""
	}
	return addr.String()
}

func inspectContainerRaw(ctx context.Context, cli *client.Client, id string) ([]byte, error) {
	res, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return nil, err
	}
	return res.Raw, nil
}
