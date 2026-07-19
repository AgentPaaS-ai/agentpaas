package port

import (
	"context"
	"time"
)

// Clock provides wall-clock and monotonic time.
type Clock interface {
	Now() time.Time
	Monotonic() uint64
}

// LeaseStore manages fenced workload leases.
type LeaseStore interface {
	Acquire(context.Context, LeaseRequest) (LeaseID, error)
	Renew(context.Context, LeaseID, time.Duration) (time.Time, error)
	Release(context.Context, LeaseID) error
	Verify(context.Context, LeaseID) (LeaseStatus, error)
	Revoke(context.Context, LeaseID) error
}

// LeaseID identifies a lease.
type LeaseID string

// LeaseRequest describes a lease to acquire.
type LeaseRequest struct {
	TenantID   string
	WorkloadID string
	AttemptID  string
	TTL        time.Duration
}

// LeaseStatus reports lease validity.
type LeaseStatus struct {
	ID      LeaseID
	Valid   bool
	Expiry  time.Time
	Revoked bool
}
