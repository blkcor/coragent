package agent

import (
	"github.com/blkcor/coragent/internal/core"
	"github.com/blkcor/coragent/internal/provider"
)

// RichProvider is an optional additive provider extension. Implementing only
// Provider remains valid; when this extension is present Coragent selects it for
// the round without issuing a probe or a duplicate request.
type (
	RichProvider          = core.RichProvider
	RichProviderEventType = core.RichProviderEventType
	RichProviderEvent     = core.RichProviderEvent
	RichReplyEnded        = core.RichReplyEnded
)

const (
	RichProviderTextDelta             = core.RichProviderTextDelta
	RichProviderReasoningSummaryDelta = core.RichProviderReasoningSummaryDelta
	RichProviderToolCall              = core.RichProviderToolCall
	RichProviderUsage                 = core.RichProviderUsage
	RichProviderWarning               = core.RichProviderWarning
	RichProviderReplyEnded            = core.RichProviderReplyEnded
	RichProviderError                 = core.RichProviderError
)

// NewOpenAIProvider constructs a Provider that speaks the OpenAI-compatible
// streaming protocol against the given endpoint. It lets an SDK consumer build a
// model backend while importing only this package.
func NewOpenAIProvider(baseURL, apiKey, model string) Provider {
	return provider.NewOpenAIProvider(baseURL, apiKey, model)
}
