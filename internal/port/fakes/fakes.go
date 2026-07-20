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

// FakeWorkloadRuntime.Prepare prepares fake workload runtime.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeWorkloadRuntime) Prepare(_ context.Context, r port.PrepareRequest) (port.WorkloadID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PrepareCalls = append(f.PrepareCalls, r)
	return f.PrepareResult, f.PrepareErr
}
// FakeWorkloadRuntime.Start starts fake workload runtime.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeWorkloadRuntime) Start(_ context.Context, id port.WorkloadID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StartCalls = append(f.StartCalls, id)
	return f.StartErr
}
// FakeWorkloadRuntime.Signal signals fake workload runtime.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeWorkloadRuntime) Signal(_ context.Context, id port.WorkloadID, s port.WorkloadSignal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SignalCalls = append(f.SignalCalls, s)
	return f.SignalErr
}
// FakeWorkloadRuntime.Fence fence.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeWorkloadRuntime) Fence(_ context.Context, id port.WorkloadID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.FenceCalls = append(f.FenceCalls, id)
	return f.FenceErr
}
// FakeWorkloadRuntime.Stop stops fake workload runtime.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeWorkloadRuntime) Stop(_ context.Context, id port.WorkloadID, _ *time.Duration) error { // best-effort cleanup
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StopCalls = append(f.StopCalls, id)
	return f.StopErr
}
// FakeWorkloadRuntime.Inspect inspects fake workload runtime.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeWorkloadRuntime) Inspect(_ context.Context, id port.WorkloadID) (port.WorkloadStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.InspectCalls = append(f.InspectCalls, id)
	return f.InspectResult, f.InspectErr
}
// FakeWorkloadRuntime.Cleanup cleans up fake workload runtime.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeTransactionalStateStore.CasDeployment compare-and-swaps deployment.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) CasDeployment(_ context.Context, v port.DeploymentState, _ int64) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.DeploymentCalls = append(f.DeploymentCalls, v)
	return f.Err
}
// FakeTransactionalStateStore.GetDeployment returns the deployment.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) GetDeployment(context.Context, string, string) (*port.DeploymentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetDeploymentResult, f.Err
}
// FakeTransactionalStateStore.CasRun compare-and-swaps run.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) CasRun(_ context.Context, v port.RunState, _ int64) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RunCalls = append(f.RunCalls, v)
	return f.Err
}
// FakeTransactionalStateStore.GetRun returns the run.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) GetRun(context.Context, string, string) (*port.RunState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetRunResult, f.Err
}
// FakeTransactionalStateStore.CasAttempt compare-and-swaps attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) CasAttempt(_ context.Context, v port.AttemptState, _ int64) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AttemptCalls = append(f.AttemptCalls, v)
	return f.Err
}
// FakeTransactionalStateStore.GetAttempt returns the attempt.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) GetAttempt(context.Context, string, string) (*port.AttemptState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetAttemptResult, f.Err
}
// FakeTransactionalStateStore.CasWorkflow compare-and-swaps workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) CasWorkflow(_ context.Context, v port.WorkflowState, _ int64) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.WorkflowCalls = append(f.WorkflowCalls, v)
	return f.Err
}
// FakeTransactionalStateStore.GetWorkflow returns the workflow.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) GetWorkflow(context.Context, string, string) (*port.WorkflowState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.GetWorkflowResult, f.Err
}
// FakeTransactionalStateStore.ListDeployments lists the deployments.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeTransactionalStateStore) ListDeployments(context.Context, string) ([]*port.DeploymentState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Deployments, f.Err
}
// FakeTransactionalStateStore.ListRuns lists the runs.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeEventStore.Append appends fake event store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeEventStore) Append(_ context.Context, e port.Event) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e.Sequence = int64(len(f.Events) + 1)
	f.Events = append(f.Events, e)
	return e.Sequence, f.Err
}
// FakeEventStore.Subscribe subscribes fake event store.
//
// It returns an error if the operation fails or inputs are invalid.
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
// FakeEventStore.Read reads fake event store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeEventStore) Read(context.Context, string, string, int64, int) ([]port.Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.Event(nil), f.Events...), f.Err
}
// FakeEventStore.LatestSequence latest sequence.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeArtifactStore.Commit commits fake artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeArtifactStore) Commit(_ context.Context, r port.CommitArtifactRequest) (port.ArtifactID, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CommitCalls = append(f.CommitCalls, r)
	return f.ArtifactID, f.Digest, f.Err
}
// FakeArtifactStore.Authorize authorizes fake artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeArtifactStore) Authorize(_ context.Context, id port.ArtifactID, _ string) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AuthorizeCalls = append(f.AuthorizeCalls, id)
	return f.Err
}
// FakeArtifactStore.Stream streams fake artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeArtifactStore) Stream(_ context.Context, id port.ArtifactID) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.StreamCalls = append(f.StreamCalls, id)
	return io.NopCloser(strings.NewReader(f.Content)), f.Err
}
// FakeArtifactStore.RangeRead range read.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeArtifactStore) RangeRead(_ context.Context, id port.ArtifactID, _ int64, _ int64) ([]byte, error) { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RangeReadCalls = append(f.RangeReadCalls, id)
	return []byte(f.Content), f.Err
}
// FakeArtifactStore.Verify verifies fake artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeArtifactStore) Verify(_ context.Context, id port.ArtifactID, _ string) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, id)
	return f.Err
}
// FakeArtifactStore.Retain retain.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeArtifactStore) Retain(_ context.Context, id port.ArtifactID, _ port.RetentionPolicy) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RetainCalls = append(f.RetainCalls, id)
	return f.Err
}
// FakeArtifactStore.Delete deletes fake artifact store.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeEgressEnforcer.Apply applies fake egress enforcer.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeEgressEnforcer) Apply(_ context.Context, id string, _ port.CommSnapshot) error { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ApplyCalls = append(f.ApplyCalls, id)
	return f.Err
}
// FakeEgressEnforcer.Check checks fake egress enforcer.
func (f *FakeEgressEnforcer) Check(_ context.Context, id, d string) port.Decision {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.CheckCalls = append(f.CheckCalls, id+":"+d)
	return f.Decision
}
// FakeEgressEnforcer.Remove removes fake egress enforcer.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeEgressEnforcer) Remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RemoveCalls = append(f.RemoveCalls, id)
	return f.Err
}

// FakeIngressEnforcer records enforcement calls.
type FakeIngressEnforcer struct{ FakeEgressEnforcer }

// FakeIngressEnforcer.Apply applies fake ingress enforcer.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeIngressEnforcer) Apply(ctx context.Context, id string, s port.CommSnapshot) error {
	return f.FakeEgressEnforcer.Apply(ctx, id, s)
}
// FakeIngressEnforcer.Check checks fake ingress enforcer.
func (f *FakeIngressEnforcer) Check(ctx context.Context, id, s string) port.Decision {
	return f.FakeEgressEnforcer.Check(ctx, id, s)
}
// FakeIngressEnforcer.Remove removes fake ingress enforcer.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeSecretBroker.Apply applies fake secret broker.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeSecretBroker) Apply(_ context.Context, r port.ApplyCredentialRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ApplyCalls = append(f.ApplyCalls, r)
	return f.Err
}
// FakeSecretBroker.Revoke revokes fake secret broker.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeSecretBroker) Revoke(_ context.Context, w, c string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RevokeCalls = append(f.RevokeCalls, w+":"+c)
	return f.Err
}
// FakeSecretBroker.List lists fake secret broker.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakePackageStore.Resolve resolves fake package store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakePackageStore) Resolve(_ context.Context, t, r string) (*port.PackageResolution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ResolveCalls = append(f.ResolveCalls, t+":"+r)
	return f.Resolution, f.Err
}
// FakePackageStore.Verify verifies fake package store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakePackageStore) Verify(_ context.Context, d string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, d)
	return f.Err
}
// FakePackageStore.List lists fake package store.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeMeteringSink.Record records fake metering sink.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeMeteringSink) Record(_ context.Context, m port.Measurement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Measurements = append(f.Measurements, m)
	return f.Err
}
// FakeMeteringSink.Query queries fake metering sink.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeMeteringSink) Query(context.Context, port.MeasurementFilter) ([]port.Measurement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]port.Measurement(nil), f.Measurements...), f.Err
}
// FakeMeteringSink.Summary summary.
//
// It returns an error if the operation fails or inputs are invalid.
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

// FakeClock.Now now.
func (f *FakeClock) Now() time.Time    { f.mu.Lock(); defer f.mu.Unlock(); return f.NowValue }
// FakeClock.Monotonic monotonic.
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

// FakeLeaseStore.Acquire acquires fake lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeLeaseStore) Acquire(_ context.Context, r port.LeaseRequest) (port.LeaseID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.AcquireCalls = append(f.AcquireCalls, r)
	return f.LeaseID, f.Err
}
// FakeLeaseStore.Renew renews fake lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeLeaseStore) Renew(_ context.Context, id port.LeaseID, _ time.Duration) (time.Time, error) { // intentionally ignored (reviewed)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.RenewCalls = append(f.RenewCalls, id)
	return f.Expiry, f.Err
}
// FakeLeaseStore.Release releases fake lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeLeaseStore) Release(_ context.Context, id port.LeaseID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ReleaseCalls = append(f.ReleaseCalls, id)
	return f.Err
}
// FakeLeaseStore.Verify verifies fake lease store.
//
// It returns an error if the operation fails or inputs are invalid.
func (f *FakeLeaseStore) Verify(_ context.Context, id port.LeaseID) (port.LeaseStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.VerifyCalls = append(f.VerifyCalls, id)
	return f.Status, f.Err
}
// FakeLeaseStore.Revoke revokes fake lease store.
//
// It returns an error if the operation fails or inputs are invalid.
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
