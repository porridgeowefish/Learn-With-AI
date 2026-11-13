package teacherservice

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

func compareFirstAnswer(ctx context.Context, conversation *conversationstore.Store, history teacherHistory, learner conversationstore.Message, responseID, messageID string, emit func(StreamFrame)) (bool, error) {
	if learner.Role != "learner" || ctx.Value(skipMemoryObservation{}) != nil || strings.TrimSpace(messageText(learner)) == "" {
		return false, nil
	}
	for _, entry := range history.messages {
		if entry.Seq < history.learnerSeq && entry.Message.Role == "learner" && entry.Message.OperationID != "starter-methodology-v1-question" {
			return false, nil
		}
	}
	client, err := llmcloud.Default()
	if err != nil {
		return false, nil
	}
	comparisonID, err := client.StartComparison(ctx, learner.OperationID)
	if err != nil || comparisonID == "" {
		return false, nil
	}
	emit(StreamFrame{Type: "comparison-started", Data: map[string]any{"comparisonId": comparisonID}})
	question := messageText(learner)
	type arm struct {
		id       string
		text     string
		complete bool
	}
	left := arm{id: "a"}
	right := arm{id: "b"}
	var group sync.WaitGroup
	var output sync.Mutex
	for _, side := range []*arm{&left, &right} {
		group.Go(func() {
			var reply strings.Builder
			err := client.StreamComparison(ctx, comparisonID, side.id, question, func(raw json.RawMessage) error {
				var event ai.GatewayEvent
				if err := json.Unmarshal(raw, &event); err != nil {
					return err
				}
				if event.Type == "text-delta" {
					reply.WriteString(event.Delta)
					output.Lock()
					emit(StreamFrame{Type: "comparison-delta", Data: map[string]any{"arm": side.id, "delta": event.Delta}})
					output.Unlock()
				}
				return nil
			})
			side.text = reply.String()
			side.complete = err == nil && strings.TrimSpace(side.text) != ""
		})
	}
	group.Wait()
	message := conversationstore.Message{ID: messageID, Role: "teacher", Status: "completed", CreatedAt: time.Now().UTC(), Blocks: []conversationstore.Block{}}
	switch {
	case left.complete && right.complete && ctx.Err() == nil:
		message.Comparison = &conversationstore.AnswerComparison{ID: comparisonID, A: left.text, B: right.text}
	case left.complete:
		message.Blocks = append(message.Blocks, conversationstore.Block{Type: "markdown", Source: left.text})
	case right.complete:
		message.Blocks = append(message.Blocks, conversationstore.Block{Type: "markdown", Source: right.text})
	default:
		message.Status = "failed"
		message.FailureCode = "comparison_unavailable"
		if ctx.Err() != nil {
			message.Status, message.FailureCode = "interrupted", "interrupted"
		}
		partial := left.text
		if partial == "" {
			partial = right.text
		}
		if partial != "" {
			message.Blocks = append(message.Blocks, conversationstore.Block{Type: "markdown", Source: partial})
		}
	}
	event, err := conversation.RecordMessageWithEvidence(responseID, message)
	if err != nil {
		return true, err
	}
	if _, err = conversation.Append("teacher-response-finished", map[string]any{"responseId": responseID, "messageId": messageID, "status": message.Status}); err != nil {
		return true, err
	}
	if message.Status == "completed" {
		emit(StreamFrame{Type: "message-completed", Data: map[string]any{"messageId": messageID, "latestSeq": event.Seq}})
	} else {
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": messageID, "code": message.FailureCode, "partialPreserved": len(message.Blocks) > 0}})
	}
	return true, nil
}
