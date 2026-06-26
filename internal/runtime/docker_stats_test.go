package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/client"
)

const validStatsPayload = `{
		"cpu_stats": {
			"cpu_usage": { "total_usage": 150000000 },
			"system_cpu_usage": 2000000000,
			"online_cpus": 2
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": 100000000 },
			"system_cpu_usage": 1000000000
		},
		"memory_stats": {
			"usage": 52428800,
			"limit": 1073741824
		},
		"pids_stats": {
			"current": 7
		}
	}`

func newDockerRuntimeWithStatsHandler(t *testing.T, handler http.HandlerFunc) *DockerRuntime {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cli, err := client.NewClientWithOpts(
		client.WithHost(srv.URL),
		client.WithHTTPClient(srv.Client()),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("client.NewClientWithOpts() error = %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	return &DockerRuntime{cli: cli}
}

type errReadCloser struct {
	err error
}

func (e errReadCloser) Read([]byte) (int, error) { return 0, e.err }
func (e errReadCloser) Close() error             { return nil }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestStats_ParsesCPUAndMemory(t *testing.T) {
	stats, err := parseContainerStatsJSON([]byte(validStatsPayload))
	if err != nil {
		t.Fatalf("parseContainerStatsJSON() error = %v", err)
	}

	wantCPU := 10.0 // (50M / 1B) * 2 * 100
	if math.Abs(stats.CPUPercent-wantCPU) > 0.001 {
		t.Errorf("CPUPercent = %f, want %f", stats.CPUPercent, wantCPU)
	}
	if math.Abs(stats.MemoryMB-50.0) > 0.001 {
		t.Errorf("MemoryMB = %f, want 50.0", stats.MemoryMB)
	}
	if stats.PIDs != 7 {
		t.Errorf("PIDs = %d, want 7", stats.PIDs)
	}
}

func TestStats_CPUDeltaUint64NoInt64Overflow(t *testing.T) {
	big := uint64(math.MaxInt64) + 5000
	pre := uint64(math.MaxInt64) - 1000
	sysBig := uint64(math.MaxInt64) + 8000
	sysPre := uint64(math.MaxInt64) - 2000

	payload := fmt.Sprintf(`{
		"cpu_stats": {
			"cpu_usage": { "total_usage": %d },
			"system_cpu_usage": %d,
			"online_cpus": 2
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": %d },
			"system_cpu_usage": %d
		},
		"memory_stats": { "usage": 0, "limit": 0 },
		"pids_stats": { "current": 1 }
	}`, big, sysBig, pre, sysPre)

	stats, err := parseContainerStatsJSON([]byte(payload))
	if err != nil {
		t.Fatalf("parseContainerStatsJSON() error = %v", err)
	}

	wantCPU := (float64(big-pre) / float64(sysBig-sysPre)) * 2.0 * 100.0
	if math.Abs(stats.CPUPercent-wantCPU) > 0.001 {
		t.Errorf("CPUPercent = %f, want %f", stats.CPUPercent, wantCPU)
	}
}

func TestStats_CPUPercentCalculation(t *testing.T) {
	tests := []struct {
		name        string
		cpuDelta    int64
		systemDelta int64
		onlineCPUs  uint32
		want        float64
	}{
		{
			name:        "normal case",
			cpuDelta:    50_000_000,
			systemDelta: 1_000_000_000,
			onlineCPUs:  2,
			want:        10.0,
		},
		{
			name:        "zero system delta",
			cpuDelta:    50_000_000,
			systemDelta: 0,
			onlineCPUs:  2,
			want:        0,
		},
		{
			name:        "zero online cpus",
			cpuDelta:    50_000_000,
			systemDelta: 1_000_000_000,
			onlineCPUs:  0,
			want:        0,
		},
		{
			name:        "negative delta",
			cpuDelta:    -1,
			systemDelta: 1_000_000_000,
			onlineCPUs:  2,
			want:        0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeCPUPercent(tc.cpuDelta, tc.systemDelta, tc.onlineCPUs)
			if math.Abs(got-tc.want) > 0.001 {
				t.Errorf("computeCPUPercent() = %f, want %f", got, tc.want)
			}
		})
	}
}

func TestStats_MemoryCalculation(t *testing.T) {
	const payload = `{
		"cpu_stats": {
			"cpu_usage": { "total_usage": 200000000 },
			"system_cpu_usage": 3000000000,
			"online_cpus": 1
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": 100000000 },
			"system_cpu_usage": 1000000000
		},
		"memory_stats": {
			"usage": 104857600,
			"limit": 1073741824
		},
		"pids_stats": {
			"current": 1
		}
	}`

	stats, err := parseContainerStatsJSON([]byte(payload))
	if err != nil {
		t.Fatalf("parseContainerStatsJSON() error = %v", err)
	}
	if math.Abs(stats.MemoryMB-100.0) > 0.001 {
		t.Errorf("MemoryMB = %f, want 100.0", stats.MemoryMB)
	}
}

func TestStats_ParseZeroPrecpuReturnsNotReady(t *testing.T) {
	const payload = `{
		"cpu_stats": {
			"cpu_usage": { "total_usage": 150000000 },
			"system_cpu_usage": 2000000000,
			"online_cpus": 2
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": 0 },
			"system_cpu_usage": 0
		},
		"memory_stats": { "usage": 1, "limit": 1 },
		"pids_stats": { "current": 1 }
	}`

	_, err := parseContainerStatsJSON([]byte(payload))
	if !errors.Is(err, ErrStatsNotReady) {
		t.Errorf("parseContainerStatsJSON() error = %v, want ErrStatsNotReady", err)
	}
}

func TestStats_ParseMissingCPUStatsReturnsError(t *testing.T) {
	const payload = `{
		"cpu_stats": {
			"cpu_usage": { "total_usage": 0 },
			"system_cpu_usage": 0,
			"online_cpus": 2
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": 100000000 },
			"system_cpu_usage": 1000000000
		},
		"memory_stats": { "usage": 1, "limit": 1 },
		"pids_stats": { "current": 1 }
	}`

	_, err := parseContainerStatsJSON([]byte(payload))
	if err == nil {
		t.Fatal("parseContainerStatsJSON() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing or zero cpu_stats/precpu_stats") {
		t.Errorf("parseContainerStatsJSON() error = %v, want missing/zero cpu_stats message", err)
	}
}

func TestStats_EmptyID(t *testing.T) {
	d := &DockerRuntime{}
	stats, err := d.Stats(context.Background(), "")
	if !errors.Is(err, ErrContainerNotFound) {
		t.Errorf("Stats(empty) error = %v, want ErrContainerNotFound", err)
	}
	if stats.CPUPercent != 0 || stats.MemoryMB != 0 || stats.PIDs != 0 {
		t.Errorf("Stats() = %+v, want zero-value ContainerStats", stats)
	}
}

func TestStats_DelegationGuard(t *testing.T) {
	want := ContainerStats{CPUPercent: 42.0, MemoryMB: 128.0, PIDs: 5}
	mock := &mockRuntimeDriver{
		statsFunc: func(_ context.Context, id ContainerID) (ContainerStats, error) {
			if string(id) != "cid-stats" {
				t.Errorf("Stats id = %q, want cid-stats", id)
			}
			return want, nil
		},
	}

	d := NewDockerRuntimeWithDriver(mock)
	stats, err := d.Stats(context.Background(), "cid-stats")
	if err != nil {
		t.Fatalf("Stats() error = %v", err)
	}
	if stats != want {
		t.Errorf("Stats() = %+v, want %+v", stats, want)
	}
}

func TestStats_ContainerStatsAPIError(t *testing.T) {
	wantErr := errors.New("docker boom")
	d := newDockerRuntimeWithStatsHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, wantErr.Error(), http.StatusInternalServerError)
	})

	_, err := d.Stats(context.Background(), "cid-1")
	if err == nil {
		t.Fatal("Stats() error = nil, want API error")
	}
	if !strings.Contains(err.Error(), "container stats") {
		t.Errorf("Stats() error = %v, want container stats wrapper", err)
	}
}

func TestStats_ReadAllFailure(t *testing.T) {
	readErr := errors.New("read failed")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	base := srv.Client().Transport
	if base == nil {
		base = http.DefaultTransport
	}
	httpClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "/containers/") && strings.HasSuffix(req.URL.Path, "/stats") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       errReadCloser{err: readErr},
					Header:     make(http.Header),
					Request:    req,
				}, nil
			}
			return base.RoundTrip(req)
		}),
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost(srv.URL),
		client.WithHTTPClient(httpClient),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("client.NewClientWithOpts() error = %v", err)
	}
	t.Cleanup(func() { _ = cli.Close() })

	rt := &DockerRuntime{cli: cli}
	_, err = rt.Stats(context.Background(), "cid-1")
	if err == nil {
		t.Fatal("Stats() error = nil, want read error")
	}
	if !strings.Contains(err.Error(), "read container stats") {
		t.Errorf("Stats() error = %v, want read container stats wrapper", err)
	}
	if !strings.Contains(err.Error(), readErr.Error()) {
		t.Errorf("Stats() error = %v, want read failure cause", err)
	}
}

func TestStats_ParseFailureInsideStats(t *testing.T) {
	d := newDockerRuntimeWithStatsHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not-json`))
	})

	_, err := d.Stats(context.Background(), "cid-1")
	if err == nil {
		t.Fatal("Stats() error = nil, want parse error")
	}
	if !strings.Contains(err.Error(), "parse container stats JSON") {
		t.Errorf("Stats() error = %v, want parse container stats JSON", err)
	}
}

func TestStats_ConcurrentNoRace(t *testing.T) {
	d := newDockerRuntimeWithStatsHandler(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, validStatsPayload)
	})

	const goroutines = 50
	const iters = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	errCh := make(chan error, goroutines*iters)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				stats, err := d.Stats(context.Background(), "cid-1")
				if err != nil {
					errCh <- err
					continue
				}
				if stats.PIDs != 7 {
					errCh <- fmt.Errorf("PIDs = %d, want 7", stats.PIDs)
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}