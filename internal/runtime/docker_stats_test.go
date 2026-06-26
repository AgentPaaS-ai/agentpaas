package runtime

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestStats_ParsesCPUAndMemory(t *testing.T) {
	const payload = `{
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

	stats, err := parseContainerStatsJSON([]byte(payload))
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
			"cpu_usage": { "total_usage": 0 },
			"system_cpu_usage": 0,
			"online_cpus": 1
		},
		"precpu_stats": {
			"cpu_usage": { "total_usage": 0 },
			"system_cpu_usage": 0
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