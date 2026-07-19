package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/port"
)

// DockerMeteringSink records measurements and provides usage summaries.
// In the Docker adapter, measurements are collected from Docker stats
// and the existing audit/cost-estimate sources.
type DockerMeteringSink struct {
	mu           sync.Mutex
	measurements []port.Measurement
}

var _ port.MeteringSink = (*DockerMeteringSink)(nil)

func (m *DockerMeteringSink) Record(_ context.Context, measurement port.Measurement) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if measurement.Timestamp.IsZero() {
		measurement.Timestamp = time.Now()
	}
	m.measurements = append(m.measurements, measurement)
	return nil
}

func (m *DockerMeteringSink) Query(_ context.Context, filter port.MeasurementFilter) ([]port.Measurement, error) {
	if filter.TenantID == "" {
		return nil, fmt.Errorf("metering query requires a tenant ID")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []port.Measurement
	for _, mm := range m.measurements {
		if mm.TenantID != filter.TenantID {
			continue
		}
		if filter.RunID != "" && mm.RunID != filter.RunID {
			continue
		}
		if filter.Type != "" && mm.Type != filter.Type {
			continue
		}
		if !filter.Since.IsZero() && mm.Timestamp.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && mm.Timestamp.After(filter.Until) {
			continue
		}
		out = append(out, mm)
	}
	return out, nil
}

func (m *DockerMeteringSink) Summary(_ context.Context, tenantID string, since, until time.Time) (*port.UsageSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	summary := &port.UsageSummary{TenantID: tenantID}
	for _, mm := range m.measurements {
		if mm.TenantID != tenantID {
			continue
		}
		if !since.IsZero() && mm.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && mm.Timestamp.After(until) {
			continue
		}
		switch mm.Type {
		case port.MeterCPU:
			summary.TotalCPUSeconds += mm.Value
		case port.MeterMemory:
			summary.TotalMemoryMBHours += mm.Value
		case port.MeterStorage:
			summary.TotalStorageMB += mm.Value
		case port.MeterNetwork:
			summary.TotalNetworkBytes += mm.Value
		case port.MeterModel:
			summary.TotalModelTokens += mm.Value
		case port.MeterTool:
			summary.TotalToolCalls += mm.Value
		}
		summary.TotalCostUSDMicros += mm.CostUSDMicros
	}
	return summary, nil
}
