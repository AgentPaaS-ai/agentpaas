package cli

import (
	"fmt"
	"net"
	"strings"
	"time"

	controlv1 "github.com/AgentPaaS-ai/agentpaas/api/control/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// ConnectToDaemon dials the daemon's Unix socket and returns a ControlService
// client and the underlying gRPC connection.
//
// If the daemon is not running, the returned error is clear and actionable,
// suggesting the user run 'agent daemon start'.
func ConnectToDaemon(socketPath string) (controlv1.ControlServiceClient, *grpc.ClientConn, error) {
	if socketPath == "" {
		return nil, nil, fmt.Errorf("socket path is empty")
	}

	conn, err := grpc.NewClient(fmt.Sprintf("unix://%s", socketPath),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithNoProxy(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot connect to daemon at %s: %w", socketPath, err)
	}

	// Try a quick dial to see if the socket is live.
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	c, derr := dialer.Dial("unix", socketPath)
	if derr != nil {
		_ = conn.Close() // best-effort close
		errMsg := derr.Error()
		if strings.Contains(errMsg, "connection refused") ||
			strings.Contains(errMsg, "no such file or directory") ||
			strings.Contains(errMsg, "connect: no such file") {
			return nil, nil, fmt.Errorf(
				"daemon is not running (connection refused at %s)\nRun 'agent daemon start' to start the daemon",
				socketPath,
			)
		}
		return nil, nil, fmt.Errorf("cannot connect to daemon at %s: %w", socketPath, derr)
	}
	_ = c.Close() // best-effort close

	client := controlv1.NewControlServiceClient(conn)
	return client, conn, nil
}