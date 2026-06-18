package daemon

import (
	"context"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
)

// stubControlServer implements the ControlServiceServer interface by embedding
// UnimplementedControlServiceServer and overriding only the Doctor method with
// a stub response. All other RPCs return Unimplemented via the embedded default
// implementations.
//
// This lets the daemon start, accept connections, and respond to the Doctor
// diagnostic RPC while the remaining methods await real implementations.
type stubControlServer struct {
	controlv1.UnimplementedControlServiceServer

	version VersionInfo
}

// compile-time interface check.
var _ controlv1.ControlServiceServer = (*stubControlServer)(nil)

// Doctor returns a stub diagnostic response with version info and a single
// "ok" check indicating the daemon skeleton is running.
func (s *stubControlServer) Doctor(ctx context.Context, req *controlv1.DoctorRequest) (*controlv1.DoctorResponse, error) {
	checks := []*controlv1.CheckResult{
		{
			Name:    "version",
			Status:  "ok",
			Message: s.version.String(),
		},
		{
			Name:    "daemon_skeleton",
			Status:  "ok",
			Message: "Daemon skeleton is running. Stub implementation — full methods pending.",
		},
	}

	return &controlv1.DoctorResponse{
		Checks:        checks,
		OverallStatus: "ok",
	}, nil
}