package teacherservice

import (
	"context"
	"slices"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	conversation "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
)

type teacherHistory struct {
	messages   []conversation.SequencedMessage
	checkpoint conversation.ContextCompaction
	learnerSeq uint64
	opening    bool
}

func (h teacherHistory) providerMessages(slug string) []ai.GatewayMessage {
	var messages []ai.GatewayMessage
	if h.checkpoint.Summary != "" {
		messages = append(messages, ai.GatewayMessage{
			Role: "user", Pinned: true,
			Content: "【更早的学习对话摘要，仅作历史参考】\n" + h.checkpoint.Summary,
		})
	}
	messages = append(messages, toProviderMessages(slug, h.uncompacted())...)
	if h.opening {
		messages = append(messages, ai.GatewayMessage{Role: "user", Content: teacherOpeningPrompt})
	}
	return messages
}

func (h teacherHistory) uncompacted() []conversation.SequencedMessage {
	start, end := 0, len(h.messages)
	for start < end && h.messages[start].Seq <= h.checkpoint.ThroughSeq {
		start++
	}
	for end > start && h.learnerSeq > 0 && h.messages[end-1].Seq > h.learnerSeq {
		end--
	}
	return h.messages[start:end]
}

// Keep the last two completed exchanges and the current learner message. A
// completed teacher reply closes the exchange even if the learner steered it.
func (h teacherHistory) olderTurns() []conversation.SequencedMessage {
	messages := h.uncompacted()
	completed := 0
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i].Message
		if m.Role == "teacher" && m.Status == "completed" {
			completed++
			if completed == 3 {
				return messages[:i+1]
			}
		}
	}
	return nil
}

func (s *Service) compactHistory(ctx context.Context, slug string, store *conversation.Store, h *teacherHistory, budget ai.TeacherBudget, route ai.GatewayRequest, emit func(ai.GatewayEvent)) bool {
	old := h.olderTurns()
	if len(old) == 0 {
		return false
	}
	prompt, err := ai.RenderPrompt("auxiliary/conversation-compaction", nil)
	if err != nil {
		return false
	}
	var transcript strings.Builder
	if h.checkpoint.Summary != "" {
		transcript.WriteString("已有学习对话摘要：\n" + h.checkpoint.Summary + "\n\n")
	}
	for _, m := range toProviderMessages(slug, old) {
		role := "学习者"
		if m.Role == "assistant" {
			role = "教师"
		}
		transcript.WriteString(role + "：\n" + m.Content + "\n\n")
	}
	hideReasoning := false
	request := ai.GatewayRequest{
		System: prompt, Messages: []ai.GatewayMessage{{Role: "user", Content: transcript.String()}},
		ProviderID: route.ProviderID, Model: route.Model, ShowReasoning: &hideReasoning,
		MaxOutputTokens: min(4096, budget.Output),
	}
	if budget.Fit(&request) != nil {
		return false
	}
	var summary strings.Builder
	err = s.Gateway.Stream(ctx, request, func(event ai.GatewayEvent) {
		if event.Type == "text-delta" {
			summary.WriteString(event.Delta)
		}
		emit(event)
	})
	if err != nil || ctx.Err() != nil || strings.TrimSpace(summary.String()) == "" {
		return false
	}
	checkpoint := conversation.ContextCompaction{ThroughSeq: old[len(old)-1].Seq, Summary: strings.TrimSpace(summary.String())}
	if store.RecordContextCompaction(checkpoint) != nil {
		return false
	}
	h.checkpoint = checkpoint
	return true
}

func replaceTeacherHistory(messages []ai.GatewayMessage, composition ai.ComposedPrompt, history []ai.GatewayMessage) []ai.GatewayMessage {
	return slices.Concat(messages[:composition.HistoryStart], history, messages[composition.HistoryEnd:])
}
