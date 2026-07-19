package port

import (
	"context"
	"time"
)

// TransactionalStateStore provides tenant-scoped compare-and-swap state.
type TransactionalStateStore interface {
	CasDeployment(context.Context, DeploymentState, int64) error
	GetDeployment(context.Context, string, string) (*DeploymentState, error)
	CasRun(context.Context, RunState, int64) error
	GetRun(context.Context, string, string) (*RunState, error)
	CasAttempt(context.Context, AttemptState, int64) error
	GetAttempt(context.Context, string, string) (*AttemptState, error)
	CasWorkflow(context.Context, WorkflowState, int64) error
	GetWorkflow(context.Context, string, string) (*WorkflowState, error)
	ListDeployments(context.Context, string) ([]*DeploymentState, error)
	ListRuns(context.Context, string, string) ([]*RunState, error)
}

// DeploymentState is durable deployment state.
type DeploymentState struct {
	TenantID       string
	DeploymentID   string
	PackageName    string
	PackageVersion string
	Generation     int64
	Status         string
	ImageDigest    string
	PolicyDigest   string
	CreatedAt      time.Time
}

// RunState is durable invocation state.
type RunState struct {
	TenantID     string
	RunID        string
	WorkflowID   string
	DeploymentID string
	Generation   int64
	Status       string
	CreatedAt    time.Time
}

// AttemptState is durable attempt state.
type AttemptState struct {
	TenantID    string
	AttemptID   string
	RunID       string
	Generation  int64
	Status      string
	LeaseID     string
	LeaseExpiry *time.Time
	CreatedAt   time.Time
}

// WorkflowState is durable workflow state.
type WorkflowState struct {
	TenantID   string
	WorkflowID string
	Generation int64
	Status     string
	CreatedAt  time.Time
}
