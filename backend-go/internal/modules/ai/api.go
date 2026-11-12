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
type CreditSettlement = gateway.CreditSettlement
type GatewayRequest = gateway.Request
type UpstreamRateLimit = gateway.UpstreamRateLimit
type ChatGPTHTTPError = gateway.ChatGPTHTTPError

var ErrUpstreamAuthRejected = gateway.ErrUpstreamAuthRejected
var ErrChatGPTConnection = gateway.ErrChatGPTConnection

type Configured = gateway.Configured
type ProviderConfig = aiconfig.Provider
type Config = aiconfig.Config
type Binding = aiconfig.Binding
type Provider = providers.Provider
type AIMessage = providers.Message
type AIFrame = providers.Frame
type Budget = budget.Budget
type TeacherBudget = budget.TeacherBudget

const ManagedKind = aiconfig.ManagedKind
const ManagedProviderPrefix = aiconfig.ManagedPrefix

var LoadConfig = aiconfig.Load
var IsManagedProvider = aiconfig.IsManagedProvider
var ReasoningCapabilities = gateway.ReasoningCapabilities
var SaveConfig = aiconfig.Save
var UseConfigPathForTest = aiconfig.UseConfigPathForTest
var BudgetForProvider = budget.ForProvider
var EstimateTextTokens = budget.EstimateTextTokens
var TruncateTokens = budget.TruncateTokens
var CheckGenerationRequest = budget.CheckGenerationRequest
var GenerationOutputTokens = budget.GenerationOutputTokens
var GenerationThinkingTokens = budget.GenerationThinkingTokens

// StreamUpstream sends a request whose concurrency is managed by the caller.
func StreamUpstream(ctx context.Context, p ProviderConfig, request GatewayRequest, emit func(GatewayEvent)) error {
	return gateway.StreamUpstream(ctx, p, request, emit)
}

func Stream(ctx context.Context, p Provider, system string, messages []AIMessage, emit func(AIFrame)) error {
	return providers.Stream(ctx, p, system, messages, emit)
}
func Complete(ctx context.Context, p Provider, system string, messages []AIMessage) (string, error) {
	return providers.Complete(ctx, p, system, messages)
}

var ErrContextBudgetExhausted = budget.ErrContextBudgetExhausted
var IsContextOverflow = gateway.IsContextOverflow

func WithBackgroundRequest(ctx context.Context) context.Context { return gateway.WithBackground(ctx) }
