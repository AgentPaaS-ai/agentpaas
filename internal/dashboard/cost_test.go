package dashboard

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/otel"
)

func TestComputeCost_KnownModel(t *testing.T) {
	got := ComputeCost("openrouter", "openai/gpt-4o", 1000, 500)

	assertFloatEqual(t, got.InputCost, 0.005)
	assertFloatEqual(t, got.OutputCost, 0.0075)
	assertFloatEqual(t, got.TotalCost, 0.0125)
	if got.Estimated {
		t.Fatal("Estimated = true, want false")
	}
	if got.PriceTableVersion != PriceTableVersion {
		t.Fatalf("PriceTableVersion = %q, want %q", got.PriceTableVersion, PriceTableVersion)
	}
}

func TestComputeCost_UnknownModel(t *testing.T) {
	got := ComputeCost("unknown", "unknown-model", 1000, 500)

	if !got.Estimated {
		t.Fatal("Estimated = false, want true")
	}
	assertFloatEqual(t, got.InputCost, 0.003)
	assertFloatEqual(t, got.OutputCost, 0.0015)
	assertFloatEqual(t, got.TotalCost, 0.0045)
}

func TestComputeCost_ZeroTokens(t *testing.T) {
	got := ComputeCost("openrouter", "openai/gpt-4o", 0, 0)

	if got.InputCost != 0 || got.OutputCost != 0 || got.TotalCost != 0 {
		t.Fatalf("costs = input %f output %f total %f, want zero", got.InputCost, got.OutputCost, got.TotalCost)
	}
	if got.TotalTokens != 0 {
		t.Fatalf("TotalTokens = %d, want 0", got.TotalTokens)
	}
}

func TestComputeCost_SubscriptionModel(t *testing.T) {
	got := ComputeCost("xai", "grok-4.3", 1000, 500)

	if got.Estimated {
		t.Fatal("Estimated = true, want false")
	}
	if got.InputCost != 0 || got.OutputCost != 0 || got.TotalCost != 0 {
		t.Fatalf("subscription costs = input %f output %f total %f, want zero", got.InputCost, got.OutputCost, got.TotalCost)
	}
}

func TestLookupPrice_CaseInsensitive(t *testing.T) {
	lower := lookupPrice("openrouter", "openai/gpt-4o")
	mixed := lookupPrice("OpenRouter", "OpenAI/GPT-4O")

	if lower != mixed {
		t.Fatalf("case-insensitive lookup mismatch: lower=%#v mixed=%#v", lower, mixed)
	}
}

func TestServeRunCost_NoStore(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+timelineTraceID(20).String()+"/cost", nil)
	req.SetPathValue("runID", timelineTraceID(20).String())
	resp := httptest.NewRecorder()

	(&Server{}).ServeRunCost(resp, req)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusNotFound, resp.Body.String())
	}
}

func TestServeRunCost_InvalidRunID(t *testing.T) {
	store := openTimelineTestStore(t, context.Background())
	defer func() { _ = store.Close() }()
	req := httptest.NewRequest(http.MethodGet, "/api/runs/../cost", nil)
	req.SetPathValue("runID", "..")
	resp := httptest.NewRecorder()

	(&Server{store: store}).ServeRunCost(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

func TestTimelineCostFields_PriceTableVersion(t *testing.T) {
	now := time.Now().UTC()
	event, err := spanToTimelineEvent(timelineTraceID(24).String(), otel.SpanRecord{
		TraceID:    timelineTraceID(24).String(),
		SpanID:     timelineSpanID(1).String(),
		Name:       "llm timeline cost",
		StartTime:  now,
		EndTime:    now.Add(10 * time.Millisecond),
		Attributes: `{"llm.provider":"openrouter","llm.model":"openai/gpt-4o","llm.input_tokens":1000,"llm.output_tokens":500}`,
	})
	if err != nil {
		t.Fatalf("span to timeline event: %v", err)
	}
	var row LLMTimelineRow
	if err := json.Unmarshal(event.Data, &row); err != nil {
		t.Fatalf("decode llm row: %v", err)
	}
	assertFloatEqual(t, row.Cost, 0.0125)
	assertFloatEqual(t, row.TotalCost, 0.0125)
	if row.PriceTableVersion != PriceTableVersion {
		t.Fatalf("PriceTableVersion = %q, want %q", row.PriceTableVersion, PriceTableVersion)
	}
	if row.Estimated {
		t.Fatal("Estimated = true, want false")
	}
}

func TestServeRunCost_Aggregation(t *testing.T) {
	ctx := context.Background()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := timelineTraceID(21).String()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(1), "llm one", map[string]any{
		"llm.provider":      "openrouter",
		"llm.model":         "openai/gpt-4o",
		"llm.input_tokens":  int64(1000),
		"llm.output_tokens": int64(500),
	})
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(2), "llm two", map[string]any{
		"llm.provider":      "openrouter",
		"llm.model":         "openai/gpt-4o",
		"llm.input_tokens":  int64(100),
		"llm.output_tokens": int64(50),
	})
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(3), "llm unknown", map[string]any{
		"llm.provider":      "unknown",
		"llm.model":         "fallback-model",
		"llm.input_tokens":  int64(10),
		"llm.output_tokens": int64(5),
	})

	got := requestRunCost(t, store, runID)

	if len(got.ByModel) != 2 {
		t.Fatalf("ByModel length = %d, want 2: %#v", len(got.ByModel), got.ByModel)
	}
	if got.TotalInputTokens != 1110 || got.TotalOutputTokens != 555 {
		t.Fatalf("totals = input %d output %d, want input 1110 output 555", got.TotalInputTokens, got.TotalOutputTokens)
	}
	assertFloatEqual(t, got.ByModel[0].TotalCost, 0.01375)
	assertFloatEqual(t, got.ByModel[1].TotalCost, 0.000045)
	assertFloatEqual(t, got.TotalCost, 0.013795)
	if !got.Estimated {
		t.Fatal("Estimated = false, want true when any model uses fallback pricing")
	}
}

func TestServeRunCost_Sanitized(t *testing.T) {
	ctx := context.Background()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := timelineTraceID(22).String()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(1), "llm xss", map[string]any{
		"llm.provider":      "unknown",
		"llm.model":         "<script>alert(1)</script>",
		"llm.input_tokens":  int64(1),
		"llm.output_tokens": int64(1),
	})

	body := requestRunCostBody(t, store, runID)

	if strings.Contains(body, "<script>") {
		t.Fatalf("response contains raw script tag: %s", body)
	}
	if !strings.Contains(body, `\u0026lt;script\u0026gt;`) && !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("response does not contain escaped script tag: %s", body)
	}
}

func TestServeRunCost_PriceTableVersion(t *testing.T) {
	ctx := context.Background()
	store := openTimelineTestStore(t, ctx)
	defer func() { _ = store.Close() }()
	runID := timelineTraceID(23).String()
	ingestTimelineSpan(t, ctx, store, runID, timelineSpanID(1), "llm price table", map[string]any{
		"llm.provider":      "openrouter",
		"llm.model":         "openai/gpt-4o",
		"llm.input_tokens":  int64(1),
		"llm.output_tokens": int64(1),
	})

	got := requestRunCost(t, store, runID)

	if got.PriceTableVersion != PriceTableVersion {
		t.Fatalf("PriceTableVersion = %q, want %q", got.PriceTableVersion, PriceTableVersion)
	}
	if len(got.ByModel) != 1 || got.ByModel[0].PriceTableVersion != PriceTableVersion {
		t.Fatalf("model price table version missing: %#v", got.ByModel)
	}
}

func requestRunCost(t *testing.T, store *otel.Store, runID string) RunCostView {
	t.Helper()
	var got RunCostView
	if err := json.Unmarshal([]byte(requestRunCostBody(t, store, runID)), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

func requestRunCostBody(t *testing.T, store *otel.Store, runID string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/cost", nil)
	req.SetPathValue("runID", runID)
	resp := httptest.NewRecorder()

	(&Server{store: store}).ServeRunCost(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	return resp.Body.String()
}

func assertFloatEqual(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.0000001 {
		t.Fatalf("float = %.12f, want %.12f", got, want)
	}
}
