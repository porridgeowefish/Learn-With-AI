package teacherservice

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
	conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/websearch"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

var resolveComparisonClient = llmcloud.Default

type comparisonPreparation struct {
	composition         ai.ComposedPrompt
	tools               []ai.Tool
	searcher            websearch.Searcher
	turnContext, effort string
	budget              ai.TeacherBudget
}

func (s *Service) compareFirstAnswer(ctx context.Context, slug, providerID string, conversation *conversationstore.Store, history teacherHistory, learner conversationstore.Message, responseID, messageID string, prepared comparisonPreparation, emit func(StreamFrame)) (bool, error) {
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
	var runTool comparisonToolFactory
	if assignment.Protocol >= 2 {
		runTool, err = s.prepareComparisonTeaching(ctx, slug, providerID, responseID, conversation, history, prepared, &input)
		if err != nil {
			log.Print("composed comparison input unavailable")
			return false, nil
		}
	}
	claimed, err := conversation.FirstAnswerComparisonOperation(true)
	if err != nil || claimed == "" {
		return false, nil
	}
	return streamComparison(ctx, client, conversation, assignment, input, responseID, messageID, emit, runTool)
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

type comparisonToolFactory func(func(StreamFrame)) (*responseTools, *responseSources, error)

func (s *Service) prepareComparisonTeaching(ctx context.Context, slug, providerID, responseID string, conversation *conversationstore.Store, history teacherHistory, prepared comparisonPreparation, input *llmcloud.ComparisonInput) (comparisonToolFactory, error) {
	teaching := &llmcloud.ComparisonTeaching{TeacherIndex: -1, ReasoningEffort: prepared.effort}
	var messages []ai.GatewayMessage
	var tools []ai.Tool
	var reading *responseSources
	if ai.IsManagedProvider(providerID) {
		messages = slices.Clone(prepared.composition.Messages)
		tools = prepared.tools
		for _, fragment := range prepared.composition.Fragments {
			switch {
			case fragment.Resource == "teaching/teacher":
				teaching.TeacherIndex = fragment.MessageIndex
			case fragment.SourceID == "guideline":
				guideline, err := conversation.LearningGuideline()
				if err != nil {
					return nil, err
				}
				if guideline.Version == 2 && guideline.Guideline == "prompt:modes/"+input.Mode {
					teaching.ModeIndex = &fragment.MessageIndex
					teaching.ModeText, err = ai.RenderPromptContext(ctx, "modes/"+input.Mode, nil)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		if history.opening {
			index := prepared.composition.HistoryEnd - 1
			teaching.OpeningIndex = &index
		}
		limit := prepared.budget.SourceLimit(ai.GatewayRequest{Messages: messages, Tools: tools, System: prepared.turnContext})
		if slices.ContainsFunc(tools, func(tool ai.Tool) bool { return isSourceTool(tool.Name) }) {
			reading = newResponseSources(ctx, slug, limit)
			reading.allowed = map[string]bool{}
			for _, tool := range tools {
				reading.allowed[tool.Name] = true
			}
			prepared.turnContext += reading.bootstrap(ctx, input.Question, min(2500, limit))
		}
		if strings.TrimSpace(prepared.turnContext) != "" {
			text, err := ai.RenderPrompt("teaching/context/turn", map[string]string{"content": prepared.turnContext})
			if err != nil {
				return nil, err
			}
			messages = append(messages, ai.GatewayMessage{Role: "user", Content: text})
		}
	} else {
		// A custom connection never sends its local history, saved instructions,
		// preference, memory or source content to the comparison service.
		for _, fragment := range prepared.composition.Fragments {
			if fragment.Resource == "" || fragment.Resource == "tools/sources/instructions" || fragment.Resource == "tools/web-search/instructions" {
				continue
			}
			if fragment.Resource == "teaching/teacher" {
				teaching.TeacherIndex = len(messages)
			}
			messages = append(messages, prepared.composition.Messages[fragment.MessageIndex])
		}
		if input.Mode != "" {
			modeText, err := ai.RenderPromptContext(ctx, "modes/"+input.Mode, nil)
			if err != nil {
				return nil, err
			}
			index := len(messages)
			teaching.ModeIndex, teaching.ModeText = &index, modeText
			messages = append(messages, ai.GatewayMessage{Role: "user", Content: modeText})
		}
		messages = append(messages, ai.GatewayMessage{Role: "user", Content: input.Context})
		if input.Opening {
			index := len(messages)
			teaching.OpeningIndex = &index
		}
		messages = append(messages, ai.GatewayMessage{Role: "user", Content: input.Question})
		for _, tool := range prepared.tools {
			if tool.Name == visualTool.Name {
				tools = append(tools, tool)
			}
		}
		prepared.searcher = nil
	}
	if teaching.TeacherIndex < 0 || len(messages) > 128 {
		return nil, errors.New("invalid shared teaching composition")
	}
	var err error
	teaching.Messages, err = json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	teaching.Tools, err = json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	input.Context = ""
	input.Teaching = teaching
	if payload, err := json.Marshal(input); err != nil || len(payload) > 500<<10 {
		return nil, errors.New("shared teaching composition exceeds the comparison limit")
	}
	return func(emit func(StreamFrame)) (*responseTools, *responseSources, error) {
		var armReading *responseSources
		if reading != nil {
			copy := *reading
			copy.citations = maps.Clone(reading.citations)
			copy.coverage = make(map[string]*conversationstore.ReadingCoverage, len(reading.coverage))
			for key, coverage := range reading.coverage {
				value := *coverage
				value.CoveredRanges = slices.Clone(coverage.CoveredRanges)
				value.UnresolvedTopics = slices.Clone(coverage.UnresolvedTopics)
				copy.coverage[key] = &value
			}
			armReading = &copy
		}
		runtime, err := bindResponseTools(tools, armReading, prepared.searcher, s, slug, responseID, emit)
		return runtime, armReading, err
	}, nil
}

func streamComparison(ctx context.Context, client *llmcloud.Client, conversation *conversationstore.Store, assignment llmcloud.ComparisonAssignment, input llmcloud.ComparisonInput, responseID, messageID string, emit func(StreamFrame), factories ...comparisonToolFactory) (bool, error) {
	comparisonID := assignment.ID
	emit(StreamFrame{Type: "comparison-started", Data: map[string]any{"comparisonId": comparisonID}})
	type arm struct {
		id        string
		text      string
		complete  bool
		failure   string
		citations []sources.SourceCitation
		coverage  []conversationstore.ReadingCoverage
	}
	left := arm{id: "a"}
	right := arm{id: "b"}
	var group sync.WaitGroup
	var output sync.Mutex
	emitSafely := func(frame StreamFrame) {
		output.Lock()
		defer output.Unlock()
		emit(frame)
	}
	for _, side := range []*arm{&left, &right} {
		group.Go(func() {
			var reply strings.Builder
			var runtime *responseTools
			var reading *responseSources
			if len(factories) > 0 && factories[0] != nil {
				var bindErr error
				runtime, reading, bindErr = factories[0](emitSafely)
				if bindErr != nil {
					side.failure = "comparison_tool_failed"
					return
				}
			}
			err := client.StreamComparison(ctx, comparisonID, side.id, input, func(raw json.RawMessage) error {
				var event struct {
					ai.GatewayEvent
					ResultID string
					Room     int
				}
				if err := json.Unmarshal(raw, &event); err != nil {
					return err
				}
				if event.Type == "text-delta" {
					reply.WriteString(event.Delta)
					emitSafely(StreamFrame{Type: "comparison-delta", Data: map[string]any{"arm": side.id, "delta": event.Delta}})
				}
				if event.Type == "comparison-reset" {
					reply.Reset()
					emitSafely(StreamFrame{Type: "comparison-delta", Data: map[string]any{"arm": side.id, "replace": true}})
				}
				if event.Type == "comparison-tool-call" {
					if runtime == nil || event.ToolCall == nil || event.ResultID == "" || event.Room < 0 || event.Room > 4000 {
						return errors.New("invalid comparison tool call")
					}
					result := runtime.execute(ctx, *event.ToolCall, event.Room)
					if result.Text == "" {
						encoded, _ := json.Marshal(sourceError(result.Code))
						result.Text = string(encoded)
					}
					return client.SubmitComparisonToolResult(ctx, event.ResultID, result.Text, result.Final || runtime.webCalls >= 2)
				}
				return nil
			})
			side.text = reply.String()
			side.complete = err == nil && strings.TrimSpace(side.text) != ""
			var remote *llmcloud.RemoteError
			if err != nil {
				side.failure = providerFailureCode(err)
			} else if !side.complete {
				side.failure = "comparison_empty_output"
			}
			if errors.As(err, &remote) {
				switch remote.Code {
				case "comparison_output_limit", "comparison_empty_output", "comparison_tool_failed", "model_input_limit", "inference_timeout":
					side.failure = remote.Code
				}
			}
			if side.complete && reading != nil {
				side.text, side.citations, side.coverage = reading.verifiedAnswer(ctx, side.text)
				if side.text != reply.String() {
					emitSafely(StreamFrame{Type: "comparison-delta", Data: map[string]any{"arm": side.id, "replace": true, "delta": side.text}})
				}
			}
		})
	}
	group.Wait()
	if !left.complete && !right.complete && ctx.Err() == nil {
		log.Printf("teacher comparison fallback: id=%s a=%s b=%s", comparisonID, left.failure, right.failure)
		emit(StreamFrame{Type: "message-reset", Data: map[string]any{"messageId": messageID, "code": "comparison_fallback"}})
		return false, nil
	}
	message := conversationstore.Message{ID: messageID, Role: "teacher", Status: "completed", CreatedAt: time.Now().UTC(), Blocks: []conversationstore.Block{}}
	switch {
	case left.complete && right.complete && ctx.Err() == nil:
		message.Comparison = &conversationstore.AnswerComparison{ID: comparisonID, A: left.text, B: right.text, CitationsA: left.citations, CitationsB: right.citations, CoverageA: left.coverage, CoverageB: right.coverage}
	case left.complete:
		message.Blocks = append(message.Blocks, conversationstore.Block{Type: "markdown", Source: left.text})
		message.SourceCitations, message.ReadingCoverage = left.citations, left.coverage
	case right.complete:
		message.Blocks = append(message.Blocks, conversationstore.Block{Type: "markdown", Source: right.text})
		message.SourceCitations, message.ReadingCoverage = right.citations, right.coverage
	default:
		message.Status, message.FailureCode = "interrupted", "interrupted"
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
	if _, err = conversation.Append("teacher-response-finished", map[string]any{"responseId": responseID, "messageId": messageID, "status": message.Status, "model": assignment.Model}); err != nil {
		return true, err
	}
	if message.Status == "completed" {
		emit(StreamFrame{Type: "message-completed", Data: map[string]any{"messageId": messageID, "latestSeq": event.Seq}})
	} else {
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": messageID, "code": message.FailureCode, "partialPreserved": len(message.Blocks) > 0}})
	}
	return true, nil
}
