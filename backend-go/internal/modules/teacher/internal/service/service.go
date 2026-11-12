package teacherservice

import (
	"context"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/learningprofile"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/continuity"

	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	sourcestore "github.com/xmz14/lll/backend-go/internal/modules/sources"

	conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"

	notestore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/notestore"
	usagestore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/usagestore"
	websearch "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/websearch"
)

type skipMemoryObservation struct{}

// Use the strictest supported search provider limit for every tool request.
const maxSearchQueryRunes = 70

type StreamFrame struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

type TurnInput struct {
	Options                   *learningprofile.TurnOptions `json:"options,omitempty"`
	OperationID               string
	Content                   string
	AttachmentRefs            []string
	ConversationAttachmentIDs []string
	ProviderID                string
}

// StreamOpening uses a system trigger, never a fabricated learner statement.
// The HTTP lifecycle lock and persisted opening claim own idempotency.
func (s *Service) StreamOpening(ctx context.Context, slug, providerID string, emit func(StreamFrame)) error {
	conversation, err := conversationstore.New(slug)
	if err != nil {
		return err
	}
	trigger := conversationstore.Message{ID: "teacher-opening", Role: "system", OperationID: "teacher-opening-v1"}
	return s.streamTeacherResponse(ctx, slug, conversation, trigger, 0, idgen.New("resp"), idgen.New("msg"), providerID, "", emit)
}

type Service struct {
	Summaries       *continuity.Service
	Gateway         ai.Gateway
	OnCompleted     func(projectSlug, responseID string)
	OnSourceReading func(map[string]any)
	OnSourceUpdated func(string, sourcestore.Source)
}

// resolveSearcher is a seam for tests; production always resolves from config.
var resolveSearcher = websearch.Configured

func New(gateway ai.Gateway) *Service {
	if gateway == nil {
		gateway = ai.Configured{}
	}
	return &Service{Gateway: gateway}
}

func (s *Service) StreamTurn(ctx context.Context, slug string, in TurnInput, emit func(StreamFrame)) error {
	in.Content = strings.TrimSpace(in.Content)
	if in.OperationID == "" || (in.Content == "" && len(in.ConversationAttachmentIDs) == 0) || len([]byte(in.Content)) > 64<<10 {
		return errors.New("invalid learner turn")
	}
	conversation, err := conversationstore.New(slug)
	if err != nil {
		return err
	}
	meta, metaErr := conversation.Meta()
	if metaErr != nil {
		return metaErr
	}
	notes, notesErr := notestore.New(slug)
	if notesErr == nil {
		if err := notes.Initialize(meta.LatestSeq); err != nil {
			println("teacher notes initialization:", slug, err.Error())
		}
	}
	learnerBlocks := []conversationstore.Block{{Type: "markdown", Source: in.Content}}
	mediaBlocks, err := conversation.AttachmentBlocks(in.ConversationAttachmentIDs)
	if err != nil {
		return err
	}
	learnerBlocks = append(learnerBlocks, mediaBlocks...)
	for _, ref := range in.AttachmentRefs {
		learnerBlocks = append(learnerBlocks, conversationstore.Block{Type: "attachment", ArtifactRef: ref})
	}
	learner, learnerEvent, err := conversation.AppendMessage("learner", "completed", in.OperationID, learnerBlocks)
	if err != nil {
		return err
	}
	if prior, found, err := conversation.ResponseForLearner(learner.ID); err != nil {
		return err
	} else if found {
		emit(StreamFrame{Type: "turn-accepted", Data: map[string]any{"learnerMessageId": learner.ID, "responseId": prior.ResponseID}})
		if prior.Active || prior.Message == nil {
			return errors.New("teacher response is already active")
		}
		emit(StreamFrame{Type: "message-started", Data: map[string]any{"teacherMessageId": prior.TeacherMessageID}})
		for _, block := range prior.Message.Blocks {
			switch block.Type {
			case "markdown":
				emit(StreamFrame{Type: "text-delta", Data: map[string]any{"blockId": block.ID, "delta": block.Source}})
			case "reasoning-summary":
				emit(StreamFrame{Type: "reasoning-summary-delta", Data: map[string]any{"blockId": block.ID, "delta": block.Source}})
			}
		}
		meta, _ := conversation.Meta()
		emit(StreamFrame{Type: "message-completed", Data: map[string]any{"messageId": prior.TeacherMessageID, "latestSeq": meta.LatestSeq}})
		return nil
	}
	return s.streamTeacherResponse(ctx, slug, conversation, learner, learnerEvent.Seq, idgen.New("resp"), idgen.New("msg"), in.ProviderID, "", emit, in.Options)
}

// StreamPromotedTurn runs a turn for a queue item that was already recorded as
// a learner message by the store's promotion step. steering=true adds the
// mid-generation guidance appendix for interrupt-and-redirect turns.
func (s *Service) StreamPromotedTurn(ctx context.Context, slug string, promoted conversationstore.PromotedTurn, steering bool, emit func(StreamFrame)) error {
	conversation, err := conversationstore.New(slug)
	if err != nil {
		return err
	}
	responseID, teacherMessageID := idgen.New("resp"), idgen.New("msg")
	steerNote := ""
	if steering {
		steerNote = messageText(promoted.Learner)
		_, _ = conversation.Append("steering-note", map[string]any{"learnerMessageId": promoted.Learner.ID, "responseId": responseID})
	}
	return s.streamTeacherResponse(ctx, slug, conversation, promoted.Learner, promoted.LearnerSeq, responseID, teacherMessageID, promoted.Item.ProviderID, steerNote, emit, promoted.Item.Options)
}

// RegenerateResponse supersedes an old response and immediately re-runs the
// same triggering learner message. The old reply stays in the event log but is
// hidden from every projection.
func (s *Service) RegenerateResponse(ctx context.Context, slug, oldResponseID, providerID string, emit func(StreamFrame), options ...*learningprofile.TurnOptions) error {
	conversation, err := conversationstore.New(slug)
	if err != nil {
		return err
	}
	info, found, err := conversation.ResponseByID(oldResponseID)
	if err != nil || !found || info.Superseded || info.Active {
		return errors.New("response cannot be regenerated")
	}
	if latest, ok, latestErr := conversation.LatestResponseID(); latestErr != nil || !ok || latest != oldResponseID {
		return errors.New("response cannot be regenerated: not the latest response")
	}
	learner, foundLearner, err := conversation.FindMessage(info.TriggeringLearnerMessageID)
	if info.TriggeringLearnerMessageID == "teacher-opening" {
		learner = conversationstore.Message{ID: "teacher-opening", Role: "system", OperationID: "teacher-opening-v1"}
		foundLearner, err = true, nil
	}
	if err != nil || !foundLearner || (learner.Role != "learner" && learner.Role != "system") {
		return errors.New("triggering learner message missing")
	}
	var learnerSeq uint64
	sequenced, err := conversation.SequencedMessages()
	if err != nil {
		return err
	}
	for _, item := range sequenced {
		if item.Message.ID == learner.ID {
			learnerSeq = item.Seq
			break
		}
	}
	responseID, teacherMessageID := idgen.New("resp"), idgen.New("msg")
	if err := conversation.SupersedeResponse(oldResponseID, responseID); err != nil {
		return err
	}
	return s.streamTeacherResponse(context.WithValue(ctx, skipMemoryObservation{}, true), slug, conversation, learner, learnerSeq, responseID, teacherMessageID, providerID, "", emit, options...)
}

// streamTeacherResponse is the shared streaming core for every turn origin:
// direct send, queued promotion, steering, and regeneration.
func (s *Service) streamTeacherResponse(ctx context.Context, slug string, conversation *conversationstore.Store, learner conversationstore.Message, learnerSeq uint64, responseID, teacherMessageID, providerID, steerNote string, emit func(StreamFrame), choices ...*learningprofile.TurnOptions) error {
	profile, profileErr := learningprofile.Read()
	if profileErr != nil {
		return profileErr
	}
	options := learningprofile.TurnOptions{Effort: profile.Settings.Effort}
	explicitOptions := len(choices) > 0 && choices[0] != nil
	if explicitOptions {
		options = *choices[0]
	}
	effectiveProviderID, effectiveModel := providerID, ""
	if cfg, err := ai.LoadConfig(); err == nil && cfg != nil {
		p := cfg.Resolve("teacher")
		if providerID != "" {
			p = cfg.Find(providerID)
		}
		if p != nil {
			effectiveProviderID, effectiveModel = p.ID, p.Model
			if !explicitOptions {
				caps := ai.ReasoningCapabilities(*p)
				options.Effort = ""
				for _, v := range caps {
					if v == profile.Settings.Effort {
						options.Effort = v
					}
				}
				if options.Effort == "" && len(caps) > 0 {
					options.Effort = "medium"
				}
			}
		}
	}
	notes, notesErr := notestore.New(slug)
	if notesErr == nil && learnerSeq > 0 {
		if err := notes.Initialize(learnerSeq - 1); err != nil {
			println("teacher notes initialization:", slug, err.Error())
		}
	}
	learnerText := messageText(learner)
	textBlockID, reasoningBlockID := idgen.New("blk"), idgen.New("blk")
	emit(StreamFrame{Type: "turn-accepted", Data: map[string]any{"learnerMessageId": learner.ID, "responseId": responseID}})
	if _, err := conversation.Append("teacher-response-started", map[string]any{"responseId": responseID, "teacherMessageId": teacherMessageID, "triggeringLearnerMessageId": learner.ID, "reasoningEffort": options.Effort, "showReasoning": profile.Settings.ShowReasoning, "webSearch": options.WebSearch && profile.Settings.Search.Enabled}); err != nil {
		return err
	}
	emit(StreamFrame{Type: "message-started", Data: map[string]any{"teacherMessageId": teacherMessageID}})

	promptCtx := ai.ManagedPromptContext(ctx, effectiveProviderID, "teacher/general")
	sequenced, checkpoint, err := conversation.ContextHistory(learnerSeq)
	history := teacherHistory{messages: sequenced, checkpoint: checkpoint, learnerSeq: learnerSeq, opening: learner.Role == "system"}
	var composition ai.ComposedPrompt
	if err == nil {
		composition, err = composeTeacherHistory(promptCtx, slug, conversation, map[string]string{"webSearchEnabled": strconv.FormatBool(options.WebSearch && profile.Settings.Search.Enabled), "sourcesEnabled": "true"}, history)
	}
	turnInstructions, providerMessages := "", composition.Messages
	if err != nil {
		message := conversationstore.Message{ID: teacherMessageID, Role: "teacher", Status: "failed", FailureCode: "context_preparation_failed", Blocks: []conversationstore.Block{}, CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()}
		_, _ = conversation.RecordMessage(message)
		_, _ = conversation.Append("teacher-response-finished", map[string]any{"responseId": responseID, "messageId": teacherMessageID, "status": "failed"})
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": teacherMessageID, "code": message.FailureCode, "partialPreserved": false}})
		return err
	}
	turnContext := learningprofile.Context(slug)
	if steerNote != "" {
		steering, e := ai.RenderPrompt("teaching/runtime/steering", map[string]string{"content": steerNote})
		if e != nil {
			return e
		}
		turnContext += "\n\n" + steering
	}

	var tools []ai.Tool
	searcher := resolveSearcher()
	if !options.WebSearch || !profile.Settings.Search.Enabled {
		searcher = nil
	}
	if searcher != nil {
		tools = append(tools, ai.Tool{Name: "search_web", Description: promptText("tools/web-search/description"), Schema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "maxLength": maxSearchQueryRunes, "description": "不超过70个字符的简短检索词，用最可能命中的自然语言关键词。"},
			},
			"required": []string{"query"},
		}})
	}

	budget := ai.BudgetForProvider(effectiveProviderID)
	sourceDefinitions, sourceDefinitionErr := sourcestore.CheckedTools()
	if sourceDefinitionErr != nil {
		return sourceDefinitionErr
	}
	tools = append(tools, sourceDefinitions...)
	tools = append(tools, visualTool)
	selectedNames := composition.Tools
	selected := map[string]bool{}
	for _, name := range selectedNames {
		selected[name] = true
	}
	available := tools
	tools = nil
	for _, tool := range available {
		if selected[tool.Name] {
			tools = append(tools, tool)
		}
	}
	usage := map[string]int{}
	var creditUsage *conversationstore.CreditUsage
	recordCredit := func(receipt *ai.CreditSettlement) {
		if !ai.IsManagedProvider(effectiveProviderID) || receipt == nil || receipt.Units < 0 {
			return
		}
		if creditUsage == nil {
			creditUsage = &conversationstore.CreditUsage{}
		}
		creditUsage.Units += receipt.Units
		creditUsage.Pending = creditUsage.Pending || receipt.Pending
		if receipt.Pending && receipt.RequestID != "" {
			creditUsage.PendingRequests = append(creditUsage.PendingRequests, receipt.RequestID)
		}
	}
	var providerErr error
	var lessonEndID string
	allowSources := selected["list_source_documents"]
	var plannedCalls []ai.ToolCall
	if learner.Role == "learner" && ctx.Err() == nil {
		var plan toolIntent
		var intentUsage map[string]int
		var intentErr error
		if explicitTeachingEnd(learnerText) || noMoreTeachingQuestions(learnerText, providerMessages) {
			plan.EndDecision = "confirm_end"
			plan.Evidence = learnerText
		} else {
			plan, intentUsage, intentErr = s.inferToolIntent(ctx, effectiveProviderID, effectiveModel, learnerText, providerMessages, recordCredit)
		}
		for k, v := range intentUsage {
			usage[k] += v
		}
		if len(intentUsage) > 0 {
			emit(StreamFrame{Type: "usage", Data: map[string]any{"usage": intentUsage}})
		}
		if intentErr != nil {
			if s.Summaries != nil && ctx.Value(skipMemoryObservation{}) == nil {
				if err := continuity.Begin(slug, learnerSeq); err != nil {
					return err
				}
			}
			_, _ = conversation.Append("tool-intent-evaluated", map[string]any{"responseId": responseID, "status": "fallback"})
			turnInstructions += "\n" + promptText("teaching/runtime/intent-unavailable")
		} else {
			if s.Summaries != nil && ctx.Value(skipMemoryObservation{}) == nil {
				if confirmedTeachingEnd(plan, learnerText, providerMessages) {
					job, summaryErr := s.Summaries.EnqueueConfirmed(ctx, slug, learner.ID, teacherMessageID, learnerSeq, effectiveProviderID)
					if summaryErr == nil {
						lessonEndID = job.ID
						ending, resourceErr := ai.RenderPrompt("teaching/runtime/end-confirmed", nil)
						if resourceErr != nil {
							return resourceErr
						}
						turnInstructions += "\n\n" + ending
						_, _ = conversation.Append("teaching-end-confirmed", map[string]any{"summaryId": job.ID, "confirmationMessageId": learner.ID, "status": job.Status})
					} else {
						if err := continuity.Begin(slug, learnerSeq); err != nil {
							return err
						}
						_, _ = conversation.Append("teaching-summary-failed", map[string]any{"confirmationMessageId": learner.ID, "error": summaryErr.Error()})
						failureInstruction, resourceErr := ai.RenderPrompt("teaching/runtime/end-confirmation-failed", nil)
						if resourceErr != nil {
							return resourceErr
						}
						turnInstructions += "\n\n" + failureInstruction
					}
				} else if err := continuity.Begin(slug, learnerSeq); err != nil {
					return err
				}
			}
			filtered := make([]ai.Tool, 0, len(tools))
			for _, tool := range tools {
				if plan.allows(tool.Name) {
					filtered = append(filtered, tool)
				}
			}
			tools = filtered
			allowSources = allowSources && plan.allows("list_source_documents")
			var unavailable []string
			plannedCalls, unavailable = plan.calls(tools)
			_, _ = conversation.Append("tool-intent-evaluated", map[string]any{"responseId": responseID, "status": "evaluated", "required": plan.Required, "forbidden": plan.Forbidden, "unavailable": unavailable})
			if len(unavailable) > 0 {
				notice, e := ai.RenderPrompt("teaching/runtime/tool-unavailable", map[string]string{"tools": strings.Join(unavailable, ", ")})
				if e != nil {
					return e
				}
				turnInstructions += "\n" + notice
			}
		}
	}

	// Count the dynamic appendix here. It is added at the end of the actual request.
	initial := ai.GatewayRequest{System: turnContext, TurnInstructions: turnInstructions, Messages: providerMessages, Tools: tools, ProviderID: effectiveProviderID, Model: effectiveModel, MaxOutputTokens: budget.Output}
	compactionTried := false
	compact := func(messages []ai.GatewayMessage) ([]ai.GatewayMessage, bool) {
		compactionTried = true
		if !s.compactHistory(ctx, slug, conversation, &history, budget, initial, func(event ai.GatewayEvent) {
			switch event.Type {
			case "usage":
				for k, v := range event.Usage {
					usage[k] += v
				}
				emit(StreamFrame{Type: "usage", Data: map[string]any{"usage": event.Usage}})
			case "credit-settlement":
				recordCredit(event.CreditSettlement)
			}
		}) {
			return messages, false
		}
		return replaceTeacherHistory(messages, composition, history.providerMessages(slug)), true
	}
	if budget.ShouldCompact(initial) && len(history.olderTurns()) > 0 {
		providerMessages, _ = compact(providerMessages)
		initial.Messages = providerMessages
	}
	budgetErr := budget.Fit(&initial)
	sourceLimit := budget.SourceLimit(initial)
	reading := newResponseSources(ctx, slug, sourceLimit)
	reading.allowed = map[string]bool{}
	for _, tool := range tools {
		reading.allowed[tool.Name] = true
	}
	if reading.reader != nil && s.OnSourceUpdated != nil {
		reading.reader.OnUpdated = func(source sourcestore.Source) { s.OnSourceUpdated(slug, source) }
	}
	if budgetErr == nil && allowSources {
		bootstrap := reading.bootstrap(ctx, learnerText, min(2500, sourceLimit))
		turnContext += bootstrap
		if strings.Contains(learnerText, "继续") || strings.EqualFold(strings.TrimSpace(learnerText), "continue") {
			if messages, e := conversation.AllMessages(); e == nil {
				turnContext += reading.continuation(ctx, messages)
			}
		}
	}
	if strings.TrimSpace(turnContext) != "" {
		// Keep changing memory, source snapshots and retrieval evidence after
		// stable instructions and history, once per turn (also across tool rounds).
		reference, e := ai.RenderPrompt("teaching/context/turn", map[string]string{"content": turnContext})
		if e != nil {
			return e
		}
		providerMessages = append(providerMessages, ai.GatewayMessage{Role: "user", Content: reference})
	}
	var text, reasoning strings.Builder
	recoveredUnexpectedTool := false
	roundMessages := providerMessages
	runtimeTools, bindErr := bindResponseTools(tools, reading, searcher, s, slug, responseID, emit)
	if bindErr != nil {
		return bindErr
	}
	toolRounds := 0

	for requestNumber := 0; requestNumber < 8; requestNumber++ {
		if ctx.Err() != nil {
			providerErr = ctx.Err()
			break
		}
		if budgetErr != nil {
			providerErr = budgetErr
			break
		}
		if latest, e := learningprofile.Read(); e != nil || !latest.Settings.Search.Enabled {
			filtered := tools[:0]
			for _, tool := range tools {
				if tool.Name != "search_web" {
					filtered = append(filtered, tool)
				}
			}
			tools = filtered
		}
		request := ai.GatewayRequest{ReasoningEffort: options.Effort, ShowReasoning: &profile.Settings.ShowReasoning, TurnInstructions: turnInstructions, Messages: roundMessages, Tools: tools, ProviderID: effectiveProviderID, Model: effectiveModel, MaxOutputTokens: budget.Output}
		request.Prompt = ai.ManagedPromptReference(promptCtx)
		request.PromptExposureID = responseID
		if providerErr = budget.Fit(&request); providerErr != nil {
			break
		}
		roundTextStart := text.Len()
		var pending []ai.ToolCall
		var roundThinking []json.RawMessage
		var roundProviderContent, roundProviderReasoning strings.Builder
		roundUsage := map[string]int{}
		plannedRound := requestNumber == 0 && len(plannedCalls) > 0
		if plannedRound {
			pending = plannedCalls
		} else {
			providerErr = s.Gateway.Stream(ctx, request, func(event ai.GatewayEvent) {
				switch event.Type {
				case "text-delta":
					text.WriteString(event.Delta)
					emit(StreamFrame{Type: "text-delta", Data: map[string]any{"blockId": textBlockID, "delta": event.Delta}})
				case "reasoning-summary-delta":
					if !profile.Settings.ShowReasoning {
						break
					}
					reasoning.WriteString(event.Delta)
					emit(StreamFrame{Type: "reasoning-summary-delta", Data: map[string]any{"blockId": reasoningBlockID, "delta": event.Delta}})
				case "provider-thinking-block":
					roundThinking = append(roundThinking, append(json.RawMessage(nil), event.ThinkingBlock...))
				case "provider-content-delta":
					roundProviderContent.WriteString(event.Delta)
				case "provider-reasoning-delta":
					roundProviderReasoning.WriteString(event.Delta)
				case "usage":
					for k, v := range event.Usage {
						roundUsage[k] = v
					}
					emit(StreamFrame{Type: "usage", Data: map[string]any{"usage": event.Usage}})
				case "credit-settlement":
					recordCredit(event.CreditSettlement)
				case "tool-call-ready":
					if event.ToolCall != nil {
						call := *event.ToolCall
						if call.ProviderCallID == "" {
							call.ProviderCallID = call.CallID
						}
						pending = append(pending, call)
					}
				}
			})
		}
		for k, v := range roundUsage {
			usage[k] += v
		}
		if requestNumber == 0 && !plannedRound && !compactionTried && ai.IsContextOverflow(providerErr) &&
			text.Len() == 0 && reasoning.Len() == 0 && len(pending) == 0 && ctx.Err() == nil {
			if compacted, ok := compact(roundMessages); ok {
				roundMessages = compacted
				continue
			}
		}
		if len(pending) == 0 {
			break
		}
		streamErr := providerErr
		unexpected := false
		for _, call := range pending {
			offered := false
			for _, available := range tools {
				if available.Name == call.ToolName {
					offered = true
					break
				}
			}
			if !offered && !plannedRound {
				unexpected = true
			}
		}
		if unexpected {
			text.Reset()
			reasoning.Reset()
			emit(StreamFrame{Type: "message-reset", Data: map[string]any{"messageId": teacherMessageID, "code": "unexpected_tool_request"}})
			for _, call := range pending {
				_, _ = conversation.Append("tool-call-requested", map[string]any{"callId": call.CallID, "responseId": responseID, "toolName": call.ToolName})
				_, _ = conversation.Append("tool-result-recorded", map[string]any{"callId": call.CallID, "status": "rejected", "code": "tool_not_offered"})
			}
			if recoveredUnexpectedTool || requestNumber == 7 {
				providerErr = errors.New("provider repeatedly requested an unavailable tool")
				break
			}
			recoveredUnexpectedTool = true
			tools = nil
			// Discard the complete invalid provider candidate, including its tool history.
			// Prior valid exchanges are kept; this candidate never becomes teaching text.
			turnInstructions += "\n" + promptText("teaching/runtime/no-more-tools")
			continue
		}
		assistant := ai.GatewayMessage{Role: "assistant", Content: text.String()[roundTextStart:], ThinkingBlocks: roundThinking, ToolCalls: pending, ProviderContent: roundProviderContent.String(), ReasoningContent: roundProviderReasoning.String()}
		if !plannedRound {
			roundMessages = append(roundMessages, assistant)
		}
		limited := toolRounds >= 6 || requestNumber >= 6
		// Reserve a bounded result for every pending ID before any body is loaded.
		resultRoom := budget.Available(ai.GatewayRequest{TurnInstructions: turnInstructions, Messages: roundMessages, Tools: tools}) - 256*len(pending) - 512
		forceFinal := limited
		for _, call := range pending {
			_, _ = conversation.Append("tool-call-requested", map[string]any{"callId": call.CallID, "responseId": responseID, "toolName": call.ToolName, "arguments": call.Arguments})
			resultText := ""
			resultStatus := "executed"
			code := ""
			if limited {
				code = "tool_limit_reached"
				forceFinal = true
			} else if ctx.Err() != nil {
				code = "cancelled"
			} else {
				outcome := runtimeTools.execute(ctx, call, resultRoom)
				resultText, code = outcome.Text, outcome.Code
				forceFinal = forceFinal || outcome.Final
			}
			if resultText == "" {
				encoded, _ := json.Marshal(sourceError(code))
				resultText = string(encoded)
			}
			if code != "" {
				resultStatus = "failed"
			}
			_, _ = conversation.Append("tool-result-recorded", map[string]any{"callId": call.CallID, "status": resultStatus, "code": code})
			if plannedRound {
				// These results were requested by the application policy, not a
				// provider assistant. Do not fabricate signed/reasoning tool history.
				wrapped, e := ai.RenderPrompt("teaching/runtime/planner-tool-result", map[string]string{"tool": call.ToolName, "content": resultText})
				if e != nil {
					return e
				}
				roundMessages = append(roundMessages, ai.GatewayMessage{Role: "user", Content: wrapped})
			} else {
				roundMessages = append(roundMessages, ai.GatewayMessage{Role: "tool", ToolCallID: call.ProviderCallID, Content: resultText})
			}
			resultRoom -= ai.EstimateTextTokens(resultText) + 32
		}
		toolRounds++
		if streamErr != nil {
			providerErr = streamErr
			break
		}
		if runtimeTools.webCalls >= 2 {
			forceFinal = true
		}
		if forceFinal || requestNumber >= 6 {
			tools = nil
			turnInstructions += "\n" + promptText("teaching/runtime/tools-finished")
		}
	}
	status := "completed"
	if providerErr != nil {
		if errors.Is(providerErr, context.Canceled) {
			status = "interrupted"
		} else {
			status = "failed"
		}
	}
	var blocks []conversationstore.Block
	if reasoning.Len() > 0 {
		blocks = append(blocks, conversationstore.Block{ID: reasoningBlockID, Type: "reasoning-summary", Source: reasoning.String()})
	}
	if text.Len() > 0 {
		blocks = append(blocks, conversationstore.Block{ID: textBlockID, Type: "markdown", Source: text.String()})
	}
	message := conversationstore.Message{ID: teacherMessageID, Role: "teacher", Status: status, Blocks: blocks,
		CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(), CreditUsage: creditUsage}
	if providerErr != nil {
		message.FailureCode = providerFailureCode(providerErr)
	}
	if status == "completed" {
		message.LessonEnded = lessonEndID != ""
		message.LessonEndID = lessonEndID
		if message.LessonEnded && ai.IsManagedProvider(effectiveProviderID) {
			if history, err := conversation.AllMessages(); err == nil {
				for i := len(history) - 1; i >= 0; i-- {
					if prior := history[i]; prior.Role == "teacher" && prior.Status == "completed" {
						message.LessonRatingExposureID = prior.TeachingExposureID
						if prior.LessonEndID == lessonEndID {
							message.LessonRatingExposureID = prior.LessonRatingExposureID
						}
						break
					}
				}
			}
		} else if !message.LessonEnded && ai.ManagedPromptIsExperimental(promptCtx) {
			message.TeachingExposureID = responseID
		}
		answer, citations, coverage := reading.verifiedAnswer(ctx, text.String())
		message.SourceCitations, message.ReadingCoverage = citations, coverage
		if answer != text.String() {
			for i := range message.Blocks {
				if message.Blocks[i].Type == "markdown" {
					message.Blocks[i].Source = answer
				}
			}
			emit(StreamFrame{Type: "message-reset", Data: map[string]any{"messageId": teacherMessageID, "code": "source_citation_verification"}})
			emit(StreamFrame{Type: "text-delta", Data: map[string]any{"blockId": textBlockID, "delta": answer}})
		}
	}
	finalEvent, persistErr := conversation.RecordMessageWithEvidence(responseID, message)
	if persistErr != nil {
		return persistErr
	}
	if _, err := conversation.Append("teacher-response-finished", map[string]any{"responseId": responseID, "messageId": teacherMessageID, "status": status, "providerId": effectiveProviderID, "model": effectiveModel}); err != nil {
		return err
	}
	if meta, metaErr := conversation.Meta(); metaErr == nil {
		if unit, unitErr := conversation.Unit(); unitErr == nil {
			_ = usagestore.Append(slug, usagestore.Record{ConversationID: meta.ID, UnitID: unit.UnitID, ResponseID: responseID, ProviderID: effectiveProviderID, InputTokens: usage["inputTokens"], OutputTokens: usage["outputTokens"], UsageReported: len(usage) > 0, CacheReadTokens: usagestore.OptionalTokens(usage, "cacheReadTokens"), CacheWriteTokens: usagestore.OptionalTokens(usage, "cacheWriteTokens"), CacheMissTokens: usagestore.OptionalTokens(usage, "cacheMissTokens")})
		}
	}
	if providerErr != nil {
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": teacherMessageID, "code": message.FailureCode, "partialPreserved": len(blocks) > 0, "creditUsage": creditUsage}})
		return providerErr
	}
	latestSeq := finalEvent.Seq
	if meta, e := conversation.Meta(); e == nil {
		latestSeq = meta.LatestSeq
	}
	emit(StreamFrame{Type: "message-completed", Data: map[string]any{"messageId": teacherMessageID, "latestSeq": latestSeq, "sourceCitations": message.SourceCitations, "readingCoverage": message.ReadingCoverage, "creditUsage": creditUsage}})
	if learner.Role == "learner" && ctx.Value(skipMemoryObservation{}) != true {
		learningprofile.Observe(slug, learner.ID, learnerText, effectiveProviderID, s.Gateway)
	}
	if s.OnCompleted != nil && learner.Role != "system" {
		s.OnCompleted(slug, responseID)
	}
	return nil
}

func (s *Service) sourceReading(data map[string]any, emit func(StreamFrame)) {
	if s.OnSourceReading != nil {
		s.OnSourceReading(data)
	}
	emit(StreamFrame{Type: "source-reading", Data: data})
}

func messageText(message conversationstore.Message) string {
	var out strings.Builder
	for _, block := range message.Blocks {
		if block.Type == "markdown" {
			out.WriteString(block.Source)
			out.WriteByte('\n')
		}
	}
	return strings.TrimSpace(out.String())
}

var quotedProposalText = regexp.MustCompile("(?s)```.*?```|`[^`]*`|“[^”]*”|‘[^’]*’|「[^」]*」|\"[^\"]*\"")

func providerFailureCode(err error) string {
	if errors.Is(err, ai.ErrContextBudgetExhausted) || ai.IsContextOverflow(err) {
		return "context_budget_exhausted"
	}
	if errors.Is(err, context.Canceled) {
		return "interrupted"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "teacher_request_timeout"
	}
	if errors.Is(err, ai.ErrChatGPTConnection) {
		return "chatgpt_connection_failed"
	}
	var limited *ai.UpstreamRateLimit
	if errors.As(err, &limited) {
		return "teacher_provider_rate_limited"
	}
	var rejected *ai.ChatGPTHTTPError
	if errors.As(err, &rejected) {
		switch rejected.Status {
		case 400, 413, 422:
			return "chatgpt_request_rejected"
		case 403, 404:
			return "chatgpt_model_unavailable"
		}
	}
	return "teacher_provider_unavailable"
}
