package dashboard

import (
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PriceTableVersion is the version of the built-in P1 price table.
const PriceTableVersion = "p1-v1.0"

// PriceEntry maps a provider/model pair to per-token pricing in USD.
type PriceEntry struct {
	Provider       string  `json:"provider"`
	Model          string  `json:"model"`
	InputPerToken  float64 `json:"input_per_token"`
	OutputPerToken float64 `json:"output_per_token"`
	Estimated      bool    `json:"estimated"`
}

// CostView is the sanitized API response for cost data.
type CostView struct {
	Provider          string  `json:"provider"`
	Model             string  `json:"model"`
	InputTokens       int64   `json:"input_tokens"`
	OutputTokens      int64   `json:"output_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	InputCost         float64 `json:"input_cost"`
	OutputCost        float64 `json:"output_cost"`
	TotalCost         float64 `json:"total_cost"`
	PriceTableVersion string  `json:"price_table_version"`
	Estimated         bool    `json:"estimated"`
	ComputedAt        string  `json:"computed_at"`
}

// RunCostView aggregates cost across all spans in a run.
type RunCostView struct {
	RunID             string     `json:"run_id"`
	TotalCost         float64    `json:"total_cost"`
	TotalInputTokens  int64      `json:"total_input_tokens"`
	TotalOutputTokens int64      `json:"total_output_tokens"`
	ByModel           []CostView `json:"by_model"`
	PriceTableVersion string     `json:"price_table_version"`
	Estimated         bool       `json:"estimated"`
}

var builtInPriceTable = []PriceEntry{
	{Provider: "openrouter", Model: "anthropic/claude-sonnet-4", InputPerToken: 0.000003, OutputPerToken: 0.000015},
	{Provider: "openrouter", Model: "anthropic/claude-opus-4.7", InputPerToken: 0.000015, OutputPerToken: 0.000075},
	{Provider: "openrouter", Model: "openai/gpt-4o", InputPerToken: 0.000005, OutputPerToken: 0.000015},
	{Provider: "openrouter", Model: "deepseek/deepseek-v4-flash", InputPerToken: 0.00000011, OutputPerToken: 0.00000028},
	{Provider: "openrouter", Model: "deepseek/deepseek-v4-pro", InputPerToken: 0.00000027, OutputPerToken: 0.0000011},
	{Provider: "xai", Model: "grok-4.3", InputPerToken: 0, OutputPerToken: 0},
	{Provider: "z-ai", Model: "glm-5.2", InputPerToken: 0.0000002, OutputPerToken: 0.0000008},
}

const fallbackPricePerToken = 0.000003

type spanCostInputs struct {
	provider     string
	model        string
	inputTokens  int64
	outputTokens int64
}

type costAggregate struct {
	provider     string
	model        string
	inputTokens  int64
	outputTokens int64
}

func lookupPrice(provider, model string) PriceEntry {
	for _, entry := range builtInPriceTable {
		if strings.EqualFold(entry.Provider, provider) && strings.EqualFold(entry.Model, model) {
			return entry
		}
	}
	return PriceEntry{
		Provider:       provider,
		Model:          model,
		InputPerToken:  fallbackPricePerToken,
		OutputPerToken: fallbackPricePerToken,
		Estimated:      true,
	}
}

// ComputeCost calculates cost from token counts and provider/model.
func ComputeCost(provider, model string, inputTokens, outputTokens int64) CostView {
	if inputTokens < 0 {
		inputTokens = 0
	}
	if outputTokens < 0 {
		outputTokens = 0
	}
	entry := lookupPrice(provider, model)
	inputCost := roundCost(float64(inputTokens) * entry.InputPerToken)
	outputCost := roundCost(float64(outputTokens) * entry.OutputPerToken)
	return CostView{
		Provider:          sanitizeString(provider, maxAttributeValueLen),
		Model:             sanitizeString(model, maxAttributeValueLen),
		InputTokens:       inputTokens,
		OutputTokens:      outputTokens,
		TotalTokens:       inputTokens + outputTokens,
		InputCost:         inputCost,
		OutputCost:        outputCost,
		TotalCost:         roundCost(inputCost + outputCost),
		PriceTableVersion: PriceTableVersion,
		Estimated:         entry.Estimated,
		ComputedAt:        time.Now().UTC().Format(time.RFC3339),
	}
}

// ServeRunCost handles GET /api/runs/{runID}/cost.
func (s *Server) ServeRunCost(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeJSONError(w, http.StatusNotFound, "cost data unavailable (no store)")
		return
	}
	runID := strings.TrimSpace(r.PathValue("runID"))
	if runID == "" {
		runID = runIDFromRunAPIPath(r.URL.Path, "cost")
	}
	if !validTimelineRunID(runID) {
		writeJSONError(w, http.StatusBadRequest, "invalid run id")
		return
	}

	spans, err := s.store.QuerySpans(r.Context(), runID, 0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query cost spans failed")
		return
	}

	aggregates := make(map[string]*costAggregate)
	for _, span := range spans {
		inputs, ok := costInputsFromAttributes(span.Attributes)
		if !ok {
			continue
		}
		key := costAggregateKey(inputs.provider, inputs.model)
		aggregate := aggregates[key]
		if aggregate == nil {
			aggregate = &costAggregate{provider: inputs.provider, model: inputs.model}
			aggregates[key] = aggregate
		}
		aggregate.inputTokens += inputs.inputTokens
		aggregate.outputTokens += inputs.outputTokens
	}

	view := RunCostView{
		RunID:             sanitizeString(runID, maxAttributeValueLen),
		ByModel:           make([]CostView, 0, len(aggregates)),
		PriceTableVersion: PriceTableVersion,
	}
	for _, aggregate := range aggregates {
		cost := ComputeCost(aggregate.provider, aggregate.model, aggregate.inputTokens, aggregate.outputTokens)
		view.TotalInputTokens += cost.InputTokens
		view.TotalOutputTokens += cost.OutputTokens
		view.TotalCost = roundCost(view.TotalCost + cost.TotalCost)
		view.Estimated = view.Estimated || cost.Estimated
		view.ByModel = append(view.ByModel, cost)
	}
	sort.Slice(view.ByModel, func(i, j int) bool {
		left := strings.ToLower(view.ByModel[i].Provider + "\x00" + view.ByModel[i].Model)
		right := strings.ToLower(view.ByModel[j].Provider + "\x00" + view.ByModel[j].Model)
		return left < right
	})

	writeJSON(w, http.StatusOK, view)
}

func costInputsFromAttributes(raw string) (spanCostInputs, bool) {
	var attrs map[string]any
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return spanCostInputs{}, false
	}
	inputTokens, hasInput := tokenAttr(attrs, "llm.input_tokens", "gen_ai.usage.input_tokens")
	outputTokens, hasOutput := tokenAttr(attrs, "llm.output_tokens", "gen_ai.usage.output_tokens")
	if !hasInput && !hasOutput {
		return spanCostInputs{}, false
	}
	provider := stringAttr(attrs, "llm.provider", "gen_ai.system")
	model := stringAttr(attrs, "llm.model", "gen_ai.request.model")
	return spanCostInputs{
		provider:     provider,
		model:        model,
		inputTokens:  inputTokens,
		outputTokens: outputTokens,
	}, true
}

func costAggregateKey(provider, model string) string {
	return strings.ToLower(provider) + "\x00" + strings.ToLower(model)
}

func stringAttr(attrs map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attrs[key]; ok {
			switch v := value.(type) {
			case string:
				return v
			case json.Number:
				return v.String()
			default:
				return strings.TrimSpace(strings.Trim(strings.ReplaceAll(strings.TrimSpace(jsonValueString(v)), "\x00", ""), `"`))
			}
		}
	}
	return ""
}

func tokenAttr(attrs map[string]any, keys ...string) (int64, bool) {
	for _, key := range keys {
		value, ok := attrs[key]
		if !ok {
			continue
		}
		tokens, ok := int64Attr(value)
		if tokens < 0 {
			tokens = 0
		}
		return tokens, ok
	}
	return 0, false
}

func int64Attr(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int:
		return int64(v), true
	case float64:
		return int64(v), true
	case json.Number:
		i, err := v.Int64()
		if err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(v.String(), 64)
		if err != nil {
			return 0, false
		}
		return int64(f), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err == nil {
			return i, true
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

func roundCost(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}
