package port

import (
	"context"
	"time"
)

// WorkloadRuntime manages exact image digests under resource and activation policies.
type WorkloadRuntime interface {
	Prepare(context.Context, PrepareRequest) (WorkloadID, error)
	Start(context.Context, WorkloadID) error
	Signal(context.Context, WorkloadID, WorkloadSignal) error
	Fence(context.Context, WorkloadID) error
	Stop(context.Context, WorkloadID, *time.Duration) error
	Inspect(context.Context, WorkloadID) (WorkloadStatus, error)
	Cleanup(context.Context, WorkloadID) error
}

// PrepareRequest describes a workload sandbox to prepare.
type PrepareRequest struct {
	TenantID             string
	ImageDigest          string
	RunID                string
	AttemptID            string
	ResourcePolicy       ResourcePolicy
	ActivationPolicy     ActivationPolicy
	EgressSnapshotDigest string
	CredentialBindings   []CredentialBinding
}

// ResourcePolicy limits workload resources.
type ResourcePolicy struct {
	CPUShares int64
	MemoryMB  int64
	PIDsLimit int64
	DiskMB    int64
}

// ActivationPolicy controls workload activation.
type ActivationPolicy struct {
	Mode         ActivationMode
	IdleTimeoutS int64
}

// ActivationMode selects workload activation behavior.
type ActivationMode string

const (
	ActivationOnDemand ActivationMode = "on_demand"
	ActivationWarm     ActivationMode = "warm"
	ActivationResident ActivationMode = "resident"
)

// WorkloadSignal is a signal sent to a workload.
type WorkloadSignal string

const (
	SignalTERM WorkloadSignal = "SIGTERM"
	SignalINT  WorkloadSignal = "SIGINT"
	SignalKILL WorkloadSignal = "SIGKILL"
)

// WorkloadStatus reports lifecycle and resource status.
type WorkloadStatus struct {
	ID        WorkloadID
	State     WorkloadState
	StartedAt *time.Time
	PID       int
	IP        string
}

// WorkloadState is a workload lifecycle state.
type WorkloadState string

const (
	WorkloadPrepared WorkloadState = "prepared"
	WorkloadRunning  WorkloadState = "running"
	WorkloadFenced   WorkloadState = "fenced"
	WorkloadStopped  WorkloadState = "stopped"
	WorkloadCleaned  WorkloadState = "cleaned"
)

// CredentialBinding identifies a credential without carrying its value.
type CredentialBinding struct {
	CredentialID string
	MountPath    string
	Header       string
}
