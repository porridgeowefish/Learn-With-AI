package teacherservice

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

var resolveComparisonClient = llmcloud.Default

func compareFirstAnswer(ctx context.Context, slug, providerID string, conversation *conversationstore.Store, history teacherHistory, learner conversationstore.Message, responseID, messageID string, emit func(StreamFrame)) (bool, error) {
	if ctx.Value(skipMemoryObservation{}) != nil || (learner.Role != "learner" && !history.opening) {
		return false, nil
	}
	operation, err := conversation.FirstAnswerComparisonOperation(false)
	if err != nil || operation == "" {
		return false, nil
	}
	client, err := resolveComparisonClient()
	if err != nil {
		return false, nil
	}
	input, err := firstAnswerInput(slug, providerID, conversation, history, learner)
	if err != nil {
		return false, nil
	}
	assignment, err := client.StartComparison(ctx, operation)
	if err != nil {
		if ctx.Err() == nil {
			var remote *llmcloud.RemoteError
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				log.Print("teacher comparison assignment timed out")
			case errors.As(err, &remote):
				log.Printf("teacher comparison assignment failed (HTTP %d)", remote.Status)
			default:
				log.Print("teacher comparison assignment request failed")
			}
		}
		return false, nil
	}
	if assignment.ID == "" {
		return false, nil
	}
	claimed, err := conversation.FirstAnswerComparisonOperation(true)
	if err != nil || claimed == "" {
		return false, nil
	}
	return true, streamComparison(ctx, client, conversation, assignment, input, responseID, messageID, emit)
}

func firstAnswerInput(slug, providerID string, conversation *conversationstore.Store, history teacherHistory, learner conversationstore.Message) (llmcloud.ComparisonInput, error) {
	project, err := projects.ReadProjectState(slug)
	if err != nil {
		return llmcloud.ComparisonInput{}, err
	}
	guideline, err := conversation.LearningGuideline()
	if err != nil {
		return llmcloud.ComparisonInput{}, err
	}
	question := messageText(learner)
	if history.opening {
		question = teacherOpeningPrompt
	} else if question == "" {
		question = "请根据本次学习主题和我提供的内容开始讲解。"
	}
	contextText := "学习项目：" + limitComparisonText(project.Title, 1024)
	if ai.IsManagedProvider(providerID) {
		instructions, err := conversation.SystemPrompt()
		if err != nil {
			return llmcloud.ComparisonInput{}, err
		}
		if text := strings.TrimSpace(instructions); text != "" {
			contextText += "\n\n学习者对当前项目的要求：\n" + limitComparisonText(text, 8<<10)
		}
		if scope := softScopeAppendix(slug); scope != "" {
			contextText += "\n\n项目范围：\n" + limitComparisonText(scope, 3<<10)
		}
		// Preserve relevant text after a previous turn failed without giving an
		// answer. Keep original media and other project history local.
		var earlier strings.Builder
		for _, entry := range history.uncompacted() {
			if entry.Seq >= history.learnerSeq || entry.Message.Role != "learner" || entry.Message.OperationID == "starter-methodology-v1-question" {
				continue
			}
			if text := messageText(entry.Message); text != "" {
				earlier.WriteString(limitComparisonText(text, 2<<10))
				earlier.WriteByte('\n')
			}
			if earlier.Len() >= 2<<10 {
				break
			}
		}
		if earlier.Len() > 0 {
			contextText += "\n\n先前尚未获得回复的学习者消息：\n" + limitComparisonText(earlier.String(), 2<<10)
		}
		var media strings.Builder
		for _, block := range learner.Blocks {
			if (block.Type == "chat-image" || block.Type == "chat-audio") && block.Source != "" {
				media.WriteString(limitComparisonText(block.Source, 2<<10))
				media.WriteByte('\n')
			}
		}
		if media.Len() > 0 {
			contextText += "\n\n本条消息附带的文字：\n" + limitComparisonText(media.String(), 1024)
		}
	}
	return llmcloud.ComparisonInput{Question: question, Context: limitComparisonText(contextText, 16<<10), Mode: guideline.LearningMode, Opening: history.opening}, nil
}

func limitComparisonText(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	for !utf8.RuneStart(value[maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

func streamComparison(ctx context.Context, client *llmcloud.Client, conversation *conversationstore.Store, assignment llmcloud.ComparisonAssignment, input llmcloud.ComparisonInput, responseID, messageID string, emit func(StreamFrame)) error {
	comparisonID := assignment.ID
	emit(StreamFrame{Type: "comparison-started", Data: map[string]any{"comparisonId": comparisonID}})
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
			err := client.StreamComparison(ctx, comparisonID, side.id, input, func(raw json.RawMessage) error {
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
		return err
	}
	if _, err = conversation.Append("teacher-response-finished", map[string]any{"responseId": responseID, "messageId": messageID, "status": message.Status, "model": assignment.Model}); err != nil {
		return err
	}
	if message.Status == "completed" {
		emit(StreamFrame{Type: "message-completed", Data: map[string]any{"messageId": messageID, "latestSeq": event.Seq}})
	} else {
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": messageID, "code": message.FailureCode, "partialPreserved": len(message.Blocks) > 0}})
	}
	return nil
}
