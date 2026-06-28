package agent

import "github.com/blkcor/coragent/internal/provider"

// NewOpenAIProvider constructs a Provider that speaks the OpenAI-compatible
// streaming protocol against the given endpoint. It lets an SDK consumer build a
// model backend while importing only this package.
func NewOpenAIProvider(baseURL, apiKey, model string) Provider {
	return provider.NewOpenAIProvider(baseURL, apiKey, model)
}
