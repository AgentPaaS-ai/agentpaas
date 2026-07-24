package pack

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestCreatedAtRoundtripStability(t *testing.T) {
	tests := []struct {
		name string
		t    time.Time
	}{
		{"epoch_zero_nanos", time.Unix(1_700_000_000, 0).UTC()},
		{"date_with_nanos", time.Date(2024, 1, 1, 0, 0, 0, 123456789, time.UTC)},
		{"date_with_trailing_nanos", time.Date(2024, 1, 1, 0, 0, 0, 100000000, time.UTC)},
		{"source_date_epoch", testTime()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate lockCanonicalMap: put time.Time directly in map
			m := map[string]interface{}{
				"created_at": tt.t,
			}
			before, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			// Simulate ReadAgentLock: unmarshal into struct
			var decoded struct {
				CreatedAt time.Time `json:"created_at"`
			}
			if err := json.Unmarshal(before, &decoded); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			// Re-create map from decoded value (simulates canonicalJSON in verify)
			m2 := map[string]interface{}{
				"created_at": decoded.CreatedAt,
			}
			after, err := json.Marshal(m2)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}

			if !bytes.Equal(before, after) {
				t.Fatalf("drift:\n  BEFORE: %s\n  AFTER:  %s", before, after)
			}
			t.Logf("stable: %s", before)
		})
	}
}
