package port

import (
	"context"
	"time"
)

// MeteringSink records and aggregates tenant usage.
type MeteringSink interface {
	Record(context.Context, Measurement) error
	Query(context.Context, MeasurementFilter) ([]Measurement, error)
	Summary(context.Context, string, time.Time, time.Time) (*UsageSummary, error)
}

// Measurement attributes a usage quantity to a run and tenant.
type Measurement struct {
	TenantID      string
	RunID         string
	AttemptID     string
	Type          MeasurementType
	Value         int64
	Unit          string
	CostUSDMicros int64
	Timestamp     time.Time
}

// MeasurementType identifies a usage category.
type MeasurementType string

const (
	MeterCPU     MeasurementType = "cpu"
	MeterMemory  MeasurementType = "memory"
	MeterStorage MeasurementType = "storage"
	MeterNetwork MeasurementType = "network"
	MeterModel   MeasurementType = "model"
	MeterTool    MeasurementType = "tool"
)

// MeasurementFilter selects measurements.
type MeasurementFilter struct {
	TenantID string
	RunID    string
	Type     MeasurementType
	Since    time.Time
	Until    time.Time
}

// UsageSummary aggregates tenant usage.
type UsageSummary struct {
	TenantID           string
	TotalCPUSeconds    int64
	TotalMemoryMBHours int64
	TotalStorageMB     int64
	TotalNetworkBytes  int64
	TotalModelTokens   int64
	TotalToolCalls     int64
	TotalCostUSDMicros int64
}
