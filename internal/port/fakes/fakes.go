// Package fakes provides thread-safe configurable implementations of port contracts.
package fakes

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// FakeWorkloadRuntime records workload calls and returns configured results.
type FakeWorkloadRuntime struct {
	mu                                                 sync.Mutex
	PrepareCalls                                       []port.PrepareRequest
	StartCalls                                         []port.WorkloadID
	SignalCalls                                        []port.WorkloadSignal
	FenceCalls                                         []port.WorkloadID
	StopCalls                                          []port.WorkloadID
	InspectCalls                                       []port.WorkloadID
	CleanupCalls                                       []port.WorkloadID
	PrepareResult                                      port.WorkloadID
	PrepareErr                                         error
	InspectResult                                      port.WorkloadStatus
	InspectErr                                         error
	StartErr, SignalErr, FenceErr, StopErr, CleanupErr error
}

func (f *FakeWorkloadRuntime) Prepare(_ context.Context, r port.PrepareRequest) (port.WorkloadID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PrepareCalls = append(f.PrepareCalls, r)
	return f.PrepareResult, f.PrepareErr
}
func (f *FakeWorkloadRuntime) Start(_ context.Context, id port.WorkloadID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StartCalls = append(f.StartCalls, id)
	return f.StartErr
}
func (f *FakeWorkloadRuntime) Signal(_ context.Context, id port.WorkloadID, s port.WorkloadSignal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SignalCalls = append(f.SignalCalls, s)
	return f.SignalErr
}
func (f *FakeWorkloadRuntime) Fence(_ context.Context, id port.WorkloadID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FenceCalls = append(f.FenceCalls, id)
	return f.FenceErr
}
func (f *FakeWorkloadRuntime) Stop(_ context.Context, id port.WorkloadID, _ *time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StopCalls = append(f.StopCalls, id)
	return f.StopErr
}
func (f *FakeWorkloadRuntime) Inspect(_ context.Context, id port.WorkloadID) (port.WorkloadStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InspectCalls = append(f.InspectCalls, id)
	return f.InspectResult, f.InspectErr
}
func (f *FakeWorkloadRuntime) Cleanup(_ context.Context, id port.WorkloadID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CleanupCalls = append(f.CleanupCalls, id)
	return f.CleanupErr
}

// FakeTransactionalStateStore records state calls and returns configured results.
type FakeTransactionalStateStore struct {
	mu                  sync.Mutex
	DeploymentCalls     []port.DeploymentState
	RunCalls            []port.RunState
	AttemptCalls        []port.AttemptState
	WorkflowCalls       []port.WorkflowState
	GetDeploymentResult *port.DeploymentState
	GetRunResult        *port.RunState
	GetAttemptResult    *port.AttemptState
	GetWorkflowResult   *port.WorkflowState
	Deployments         []*port.DeploymentState
	Runs                []*port.RunState
	Err                 error
}

func (f *FakeTransactionalStateStore) CasDeployment(_ context.Context, v port.DeploymentState, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeploymentCalls = append(f.DeploymentCalls, v)
	return f.Err
}
func (f *FakeTransactionalStateStore) GetDeployment(context.Context, string, string) (*port.DeploymentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetDeploymentResult, f.Err
}
func (f *FakeTransactionalStateStore) CasRun(_ context.Context, v port.RunState, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RunCalls = append(f.RunCalls, v)
	return f.Err
}
func (f *FakeTransactionalStateStore) GetRun(context.Context, string, string) (*port.RunState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetRunResult, f.Err
}
func (f *FakeTransactionalStateStore) CasAttempt(_ context.Context, v port.AttemptState, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AttemptCalls = append(f.AttemptCalls, v)
	return f.Err
}
func (f *FakeTransactionalStateStore) GetAttempt(context.Context, string, string) (*port.AttemptState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetAttemptResult, f.Err
}
func (f *FakeTransactionalStateStore) CasWorkflow(_ context.Context, v port.WorkflowState, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.WorkflowCalls = append(f.WorkflowCalls, v)
	return f.Err
}
func (f *FakeTransactionalStateStore) GetWorkflow(context.Context, string, string) (*port.WorkflowState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetWorkflowResult, f.Err
}
func (f *FakeTransactionalStateStore) ListDeployments(context.Context, string) ([]*port.DeploymentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Deployments, f.Err
}
func (f *FakeTransactionalStateStore) ListRuns(context.Context, string, string) ([]*port.RunState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Runs, f.Err
}

// FakeEventStore records event calls.
type FakeEventStore struct {
	mu              sync.Mutex
	Events          []port.Event
	SubscribeEvents []port.Event
	Err             error
}

func (f *FakeEventStore) Append(_ context.Context, e port.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e.Sequence = int64(len(f.Events) + 1)
	f.Events = append(f.Events, e)
	return e.Sequence, f.Err
}
func (f *FakeEventStore) Subscribe(context.Context, string, string, int64) (<-chan port.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := make(chan port.Event, len(f.SubscribeEvents))
	for _, e := range f.SubscribeEvents {
		c <- e
	}
	close(c)
	return c, f.Err
}
func (f *FakeEventStore) Read(context.Context, string, string, int64, int) ([]port.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.Event(nil), f.Events...), f.Err
}
func (f *FakeEventStore) LatestSequence(context.Context, string, string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return int64(len(f.Events)), f.Err
}

// FakeArtifactStore records artifact calls and returns configurable results.
type FakeArtifactStore struct {
	mu             sync.Mutex
	CommitCalls    []port.CommitArtifactRequest
	AuthorizeCalls []port.ArtifactID
	StreamCalls    []port.ArtifactID
	RangeReadCalls []port.ArtifactID
	VerifyCalls    []port.ArtifactID
	RetainCalls    []port.ArtifactID
	DeleteCalls    []port.ArtifactID
	ArtifactID     port.ArtifactID
	Digest         string
	Content        string
	Err            error
}

func (f *FakeArtifactStore) Commit(_ context.Context, r port.CommitArtifactRequest) (port.ArtifactID, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CommitCalls = append(f.CommitCalls, r)
	return f.ArtifactID, f.Digest, f.Err
}
func (f *FakeArtifactStore) Authorize(_ context.Context, id port.ArtifactID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AuthorizeCalls = append(f.AuthorizeCalls, id)
	return f.Err
}
func (f *FakeArtifactStore) Stream(_ context.Context, id port.ArtifactID) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StreamCalls = append(f.StreamCalls, id)
	return io.NopCloser(strings.NewReader(f.Content)), f.Err
}
func (f *FakeArtifactStore) RangeRead(_ context.Context, id port.ArtifactID, _ int64, _ int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RangeReadCalls = append(f.RangeReadCalls, id)
	return []byte(f.Content), f.Err
}
func (f *FakeArtifactStore) Verify(_ context.Context, id port.ArtifactID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, id)
	return f.Err
}
func (f *FakeArtifactStore) Retain(_ context.Context, id port.ArtifactID, _ port.RetentionPolicy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RetainCalls = append(f.RetainCalls, id)
	return f.Err
}
func (f *FakeArtifactStore) Delete(_ context.Context, id port.ArtifactID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeleteCalls = append(f.DeleteCalls, id)
	return f.Err
}

// FakeEgressEnforcer records enforcement calls.
type FakeEgressEnforcer struct {
	mu          sync.Mutex
	ApplyCalls  []string
	CheckCalls  []string
	RemoveCalls []string
	Decision    port.Decision
	Err         error
}

func (f *FakeEgressEnforcer) Apply(_ context.Context, id string, _ port.CommSnapshot) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ApplyCalls = append(f.ApplyCalls, id)
	return f.Err
}
func (f *FakeEgressEnforcer) Check(_ context.Context, id, d string) port.Decision {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CheckCalls = append(f.CheckCalls, id+":"+d)
	return f.Decision
}
func (f *FakeEgressEnforcer) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RemoveCalls = append(f.RemoveCalls, id)
	return f.Err
}

// FakeIngressEnforcer records enforcement calls.
type FakeIngressEnforcer struct{ FakeEgressEnforcer }

func (f *FakeIngressEnforcer) Apply(ctx context.Context, id string, s port.CommSnapshot) error {
	return f.FakeEgressEnforcer.Apply(ctx, id, s)
}
func (f *FakeIngressEnforcer) Check(ctx context.Context, id, s string) port.Decision {
	return f.FakeEgressEnforcer.Check(ctx, id, s)
}
func (f *FakeIngressEnforcer) Remove(ctx context.Context, id string) error {
	return f.FakeEgressEnforcer.Remove(ctx, id)
}

// FakeSecretBroker records credential IDs, never credential values.
type FakeSecretBroker struct {
	mu          sync.Mutex
	ApplyCalls  []port.ApplyCredentialRequest
	RevokeCalls []string
	Credentials []string
	Err         error
}

func (f *FakeSecretBroker) Apply(_ context.Context, r port.ApplyCredentialRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ApplyCalls = append(f.ApplyCalls, r)
	return f.Err
}
func (f *FakeSecretBroker) Revoke(_ context.Context, w, c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RevokeCalls = append(f.RevokeCalls, w+":"+c)
	return f.Err
}
func (f *FakeSecretBroker) List(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.Credentials...), f.Err
}

// FakePackageStore records package calls.
type FakePackageStore struct {
	mu           sync.Mutex
	Resolution   *port.PackageResolution
	Packages     []port.PackageMetadata
	ResolveCalls []string
	VerifyCalls  []string
	Err          error
}

func (f *FakePackageStore) Resolve(_ context.Context, t, r string) (*port.PackageResolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ResolveCalls = append(f.ResolveCalls, t+":"+r)
	return f.Resolution, f.Err
}
func (f *FakePackageStore) Verify(_ context.Context, d string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, d)
	return f.Err
}
func (f *FakePackageStore) List(context.Context, string) ([]port.PackageMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Packages, f.Err
}

// FakeMeteringSink records measurements.
type FakeMeteringSink struct {
	mu            sync.Mutex
	Measurements  []port.Measurement
	SummaryResult *port.UsageSummary
	Err           error
}

func (f *FakeMeteringSink) Record(_ context.Context, m port.Measurement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Measurements = append(f.Measurements, m)
	return f.Err
}
func (f *FakeMeteringSink) Query(context.Context, port.MeasurementFilter) ([]port.Measurement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.Measurement(nil), f.Measurements...), f.Err
}
func (f *FakeMeteringSink) Summary(context.Context, string, time.Time, time.Time) (*port.UsageSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.SummaryResult, f.Err
}

// FakeClock provides configurable time values.
type FakeClock struct {
	mu             sync.Mutex
	NowValue       time.Time
	MonotonicValue uint64
}

func (f *FakeClock) Now() time.Time    { f.mu.Lock(); defer f.mu.Unlock(); return f.NowValue }
func (f *FakeClock) Monotonic() uint64 { f.mu.Lock(); defer f.mu.Unlock(); return f.MonotonicValue }

// FakeLeaseStore records lease operations.
type FakeLeaseStore struct {
	mu           sync.Mutex
	AcquireCalls []port.LeaseRequest
	RenewCalls   []port.LeaseID
	ReleaseCalls []port.LeaseID
	VerifyCalls  []port.LeaseID
	RevokeCalls  []port.LeaseID
	LeaseID      port.LeaseID
	Status       port.LeaseStatus
	Expiry       time.Time
	Err          error
}

func (f *FakeLeaseStore) Acquire(_ context.Context, r port.LeaseRequest) (port.LeaseID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AcquireCalls = append(f.AcquireCalls, r)
	return f.LeaseID, f.Err
}
func (f *FakeLeaseStore) Renew(_ context.Context, id port.LeaseID, _ time.Duration) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RenewCalls = append(f.RenewCalls, id)
	return f.Expiry, f.Err
}
func (f *FakeLeaseStore) Release(_ context.Context, id port.LeaseID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ReleaseCalls = append(f.ReleaseCalls, id)
	return f.Err
}
func (f *FakeLeaseStore) Verify(_ context.Context, id port.LeaseID) (port.LeaseStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, id)
	return f.Status, f.Err
}
func (f *FakeLeaseStore) Revoke(_ context.Context, id port.LeaseID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RevokeCalls = append(f.RevokeCalls, id)
	return f.Err
}

var _ port.WorkloadRuntime = (*FakeWorkloadRuntime)(nil)
var _ port.TransactionalStateStore = (*FakeTransactionalStateStore)(nil)
var _ port.EventStore = (*FakeEventStore)(nil)
var _ port.ArtifactStore = (*FakeArtifactStore)(nil)
var _ port.EgressEnforcer = (*FakeEgressEnforcer)(nil)
var _ port.IngressEnforcer = (*FakeIngressEnforcer)(nil)
var _ port.SecretBroker = (*FakeSecretBroker)(nil)
var _ port.PackageStore = (*FakePackageStore)(nil)
var _ port.MeteringSink = (*FakeMeteringSink)(nil)
var _ port.Clock = (*FakeClock)(nil)
var _ port.LeaseStore = (*FakeLeaseStore)(nil)
