package llm

var (
	openAIEndpoint    = "https://api.openai.com/v1/chat/completions"
	anthropicEndpoint = "https://api.anthropic.com/v1/messages"
	xaiEndpoint       = "https://api.x.ai/v1/chat/completions"
)

// SetTestEndpoints overrides provider endpoints for testing. Returns a restore function.
func SetTestEndpoints(openAI, anthropic, xai string) func() {
	origOpenAI := openAIEndpoint
	origAnthropic := anthropicEndpoint
	origXAI := xaiEndpoint
	openAIEndpoint = openAI
	anthropicEndpoint = anthropic
	xaiEndpoint = xai
	return func() {
		openAIEndpoint = origOpenAI
		anthropicEndpoint = origAnthropic
		xaiEndpoint = origXAI
	}
}
