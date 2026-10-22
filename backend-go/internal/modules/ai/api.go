// Package ai owns model connections, protocols and shared request budgeting.
package ai

import (
	"context"
	aiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	"github.com/xmz14/lll/backend-go/internal/modules/ai/internal/budget"
	gateway "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/gateway"
	providers "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/providers"
)

type Gateway = gateway.Gateway
type GatewayMessage = gateway.Message
type Tool = gateway.Tool
type ToolCall = gateway.ToolCall
type GatewayEvent = gateway.Event
type GatewayRequest = gateway.Request
type Configured = gateway.Configured
type ProviderConfig = aiconfig.Provider
type Config = aiconfig.Config
type Binding = aiconfig.Binding
type Provider = providers.Provider
type AIMessage = providers.Message
type AIFrame = providers.Frame
type Budget = budget.Budget

var LoadConfig = aiconfig.Load
var SaveConfig = aiconfig.Save
var UseConfigPathForTest = aiconfig.UseConfigPathForTest
var BudgetForProvider = budget.ForProvider
var EstimateTextTokens = budget.EstimateTextTokens
var TruncateTokens = budget.TruncateTokens
var CheckGenerationRequest = budget.CheckGenerationRequest
var GenerationOutputTokens = budget.GenerationOutputTokens
var GenerationThinkingTokens = budget.GenerationThinkingTokens

func Stream(ctx context.Context, p Provider, system string, messages []AIMessage, emit func(AIFrame)) error {
	return providers.Stream(ctx, p, system, messages, emit)
}
func Complete(ctx context.Context, p Provider, system string, messages []AIMessage) (string, error) {
	return providers.Complete(ctx, p, system, messages)
}

var ErrContextBudgetExhausted = budget.ErrContextBudgetExhausted
