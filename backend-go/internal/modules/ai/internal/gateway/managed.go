package teachergateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	aiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

func streamManaged(ctx context.Context, provider aiconfig.Provider, in Request, emit func(Event)) error {
	client, err := llmcloud.Default()
	if err != nil {
		return err
	}
	maxOutput := provider.ManagedMaxOutput
	if in.MaxOutputTokens > 0 && in.MaxOutputTokens < maxOutput {
		maxOutput = in.MaxOutputTokens
	}
	payload := struct {
		Prompt           *promptpack.Reference `json:"prompt,omitempty"`
		PromptExposureID string                `json:"promptExposureId,omitempty"`
		Model            string                `json:"model"`
		RequestID        string                `json:"requestId"`
		System           string                `json:"system,omitempty"`
		TurnInstructions string                `json:"turnInstructions,omitempty"`
		Messages         []Message             `json:"messages"`
		Tools            []Tool                `json:"tools,omitempty"`
		MaxOutputTokens  int                   `json:"maxOutputTokens"`
		ReasoningEffort  string                `json:"reasoningEffort,omitempty"`
		ShowReasoning    *bool                 `json:"showReasoning,omitempty"`
	}{
		Model: provider.Model, RequestID: idgen.New("infer"), System: in.System, Prompt: in.Prompt, PromptExposureID: in.PromptExposureID,
		TurnInstructions: in.TurnInstructions, Messages: in.Messages, Tools: in.Tools,
		MaxOutputTokens: maxOutput, ReasoningEffort: in.ReasoningEffort, ShowReasoning: in.ShowReasoning,
	}
	settlementReceived := false
	err = client.Stream(ctx, payload, func(raw json.RawMessage) error {
		var event Event
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		if event.Type == "credit-settlement" && event.CreditSettlement != nil {
			if settlementReceived {
				return nil
			}
			settlementReceived = true
			event.CreditSettlement.RequestID = payload.RequestID
		}
		emit(event)
		return nil
	})
	if !settlementReceived {
		var remote *llmcloud.RemoteError
		switch {
		case errors.As(err, &remote) && remote.CreditSettlement != nil:
			remote.CreditSettlement.RequestID = payload.RequestID
			emit(Event{Type: "credit-settlement", CreditSettlement: remote.CreditSettlement})
		case err == nil || (!errors.Is(err, llmcloud.ErrLoginRequired) && (remote == nil || remote.Status >= http.StatusInternalServerError)):
			emit(Event{Type: "credit-settlement", CreditSettlement: &CreditSettlement{Pending: true, RequestID: payload.RequestID}})
		}
	}
	return err
}
