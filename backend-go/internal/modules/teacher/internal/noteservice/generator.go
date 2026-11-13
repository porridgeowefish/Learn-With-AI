package noteservice

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/xmz14/lll/backend-go/internal/idgen"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"

	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"

	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/notestore"
	usage "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/usagestore"
	"strings"
)

var ErrProviderNotConfigured = errors.New("note provider is not configured")

func Generate(ctx context.Context, provider, model string, turns []notestore.NoteTurn) (string, notestore.NoteUsage, error) {
	cfg, err := ai.LoadConfig()
	if err != nil {
		return "", notestore.NoteUsage{}, err
	}
	if cfg == nil || provider == "" || cfg.FindExact(provider) == nil {
		return "", notestore.NoteUsage{}, ErrProviderNotConfigured
	}
	p := cfg.FindExact(provider)
	if model == "" {
		model = p.Model
	}
	if !p.HasConnection() || model == "" {
		return "", notestore.NoteUsage{}, ErrProviderNotConfigured
	}
	if p.Kind == ai.ManagedKind && !notestore.AllManagedTurns(turns) {
		return "", notestore.NoteUsage{}, ErrProviderNotConfigured
	}
	data, err := json.Marshal(turns)
	if err != nil {
		return "", notestore.NoteUsage{}, err
	}
	promptCtx := ai.CloudPromptContext(ctx, "auxiliary/notes")
	notePrompt, err := ai.RenderPromptContext(promptCtx, "auxiliary/notes", nil)
	if err != nil {
		return "", notestore.NoteUsage{}, err
	}
	var content strings.Builder
	reported := notestore.NoteUsage{}
	request := ai.GatewayRequest{BindingName: "notes", ProviderID: provider, Model: model, System: notePrompt, Messages: []ai.GatewayMessage{{Role: "user", Content: string(data)}}}
	request.Prompt = ai.ManagedPromptReference(promptCtx)
	err = (ai.Configured{}).Stream(ctx, request, func(e ai.GatewayEvent) {
		if e.Type == "text-delta" {
			content.WriteString(e.Delta)
		}
		if e.Type == "usage" {
			reported.UsageReported = reported.UsageReported || len(e.Usage) > 0
			if n, ok := e.Usage["inputTokens"]; ok {
				reported.InputTokens = n
			}
			if n, ok := e.Usage["outputTokens"]; ok {
				reported.OutputTokens = n
			}
			if value := usage.OptionalTokens(e.Usage, "cacheReadTokens"); value != nil {
				reported.CacheReadTokens = value
			}
			if value := usage.OptionalTokens(e.Usage, "cacheWriteTokens"); value != nil {
				reported.CacheWriteTokens = value
			}
			if value := usage.OptionalTokens(e.Usage, "cacheMissTokens"); value != nil {
				reported.CacheMissTokens = value
			}
		}
	})
	if err == nil && strings.TrimSpace(content.String()) == "" {
		err = errors.New("empty note")
	}
	return strings.TrimSpace(content.String()), reported, err
}
func recordUsage(slug string, job notestore.NoteJob, reported notestore.NoteUsage) error {
	c, err := conversation.New(slug)
	if err != nil {
		return err
	}
	meta, err := c.Meta()
	if err != nil {
		return err
	}
	unit, err := c.Unit()
	if err != nil {
		return err
	}
	return usage.AppendNote(slug, usage.Record{ConversationID: meta.ID, UnitID: unit.UnitID, ResponseID: idgen.New("noteattempt"), ProviderID: job.ProviderID, InputTokens: reported.InputTokens, OutputTokens: reported.OutputTokens, UsageReported: reported.UsageReported, CacheReadTokens: reported.CacheReadTokens, CacheWriteTokens: reported.CacheWriteTokens, CacheMissTokens: reported.CacheMissTokens})
}
