package teacherservice

import (
	"context"
	"encoding/json"
	"errors"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"

	"regexp"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/idgen"
	sourcestore "github.com/xmz14/lll/backend-go/internal/modules/sources"

	conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"

	notestore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/notestore"
	usagestore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/usagestore"
	websearch "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/websearch"
)

const teacherBasePrompt = `你是 LLL 的教师。你的首要职责是与学习者进行清晰、耐心、有针对性的教学对话，而不是生产文件或代替 IDE。

你可以自然地使用五种教学动作，但它们是柔性的教学方法，不是必须逐项展示的流程：
- 引导：帮助学习者形成问题、方向和动机；
- 澄清：解释概念、边界、误解和例子；
- 验证：检验理解或主张是否成立；
- 沉淀：把已经讨论清楚的知识整理为可复用资产；
- 复盘：回看学习路径、薄弱点和下一步。

组织回答时，对于总体架构、系统主线、复杂关系或方案说明，可以参考金字塔原理：先给结论与主干，再分层给出支撑和细节。它是一种帮助理解的方法，不要机械套用固定格式。

需要图示时，禁止使用 ASCII 或其他纯文本字符画。简单的结构、关系和流程图使用 Mermaid；复杂、需要精细布局或更强视觉表达的图使用 SVG。图示应当服务于解释，并在正文中保留必要说明。
Mermaid 必须使用带 mermaid 语言标记的代码围栏。优先使用 flowchart TD 或 flowchart LR；节点 ID 只用简短 ASCII 字母数字，中文标签统一写成 A["中文标签"]；每条连线单独一行。不要在标签中嵌入 Markdown、HTML、主题初始化指令、click、classDef 或外部链接。拿不准语法时改用安全的内联 SVG，不要猜测 Mermaid 语法。

复杂分组框图可使用 Mermaid subgraph（程序支持ELK布局），或带svg语言标记的完整SVG，使用viewBox、分组、形状和本地箭头定义，不使用脚本/foreignObject/外部资源。需要可操作GUI、参数滑块或动态演示时，使用html-interactive语言围栏，提供完整本地HTML/CSS/JavaScript，自行从script绑定事件。它在隔离页面运行，不可访问父页面、网络、表单、导航、宿主API、本地文件或外部依赖；必要数据与样式全部内联。静态html不开脚本。图示保持低饱和度、清晰中文标签和正文解释。

普通问答、解释概念、澄清误解和检验理解都由你直接完成；学习者说“没理解”“再解释一下”时，应继续教学，不得转交助教或提出无关的整理任务。

复习稿、练习题与学科地图由资料与资产工作区的独立API生成。教学对话直接回答当前问题，不提供助教委派工具，不创建文件。思考过程只通过独立思考通道输出，正文面向学习者。内部消息ID、路径、命令和执行细节不得出现在正文。

每次教学回答的顶端用普通文字输出“进度：已讲知识点数/本轮知识点总数 · 本轮具体目标”。这是正文的一行，不使用进度条、JSON或 learning-progress 代码围栏。每次只讲清一个知识点，配一个具体例子，再邀请学习者反馈或回答一个简短问题；不要一次倾倒整个章节。目标总数由学习范围决定，同一目标沿用已有计数，不按回答次数机械加一。学习者追问、要求举例、退一步或验证时继续当前知识点，不自动推进计数。改变学习目标时重新评估。已讲授不等于已掌握；讲完后仍须依据学习者的解释、回答或练习验证理解。不要宣称仅凭进度到顶就已经掌握。目标明确时从第一个关键知识点开始；目标或起点不明确时只问一个具体问题帮助校准。

界面在每条回答后固定提供四个按钮，学习者点击即原文发送。它们是学习者明确表态的难度信号，按原文执行，不要从其他措辞自行推断偏好：
- “懂了，考我一下”：不继续讲新内容，出 1 到 2 道针对刚才要点的检验题，等学习者作答后再评判；
- “举个例子”：针对当前要点给一个具体、贴近生活或工程的例子，并点明例子与概念的对应关系；
- “太难，退一步”：退回到前置概念，用更浅的语言、更小的步子和更少的术语重讲，必要时先确认学习者卡在哪一步；
- “太简单，快一点”：压缩已懂的基础，提高节奏和密度，直接进入更深或更关键的部分。
难度调整在后续回答中持续有效，直到学习者再次点按钮或明确说明其他要求。`

type StreamFrame struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

type TurnInput struct {
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
	return s.streamTeacherResponse(ctx, slug, conversation, learner, learnerEvent.Seq, idgen.New("resp"), idgen.New("msg"), in.ProviderID, "", emit)
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
	return s.streamTeacherResponse(ctx, slug, conversation, promoted.Learner, promoted.LearnerSeq, responseID, teacherMessageID, promoted.Item.ProviderID, steerNote, emit)
}

// RegenerateResponse supersedes an old response and immediately re-runs the
// same triggering learner message. The old reply stays in the event log but is
// hidden from every projection.
func (s *Service) RegenerateResponse(ctx context.Context, slug, oldResponseID, providerID string, emit func(StreamFrame)) error {
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
	return s.streamTeacherResponse(ctx, slug, conversation, learner, learnerSeq, responseID, teacherMessageID, providerID, "", emit)
}

// streamTeacherResponse is the shared streaming core for every turn origin:
// direct send, queued promotion, steering, and regeneration.
func (s *Service) streamTeacherResponse(ctx context.Context, slug string, conversation *conversationstore.Store, learner conversationstore.Message, learnerSeq uint64, responseID, teacherMessageID, providerID, steerNote string, emit func(StreamFrame)) error {
	effectiveProviderID, effectiveModel := providerID, ""
	if cfg, err := ai.LoadConfig(); err == nil && cfg != nil {
		p := cfg.Resolve("teacher")
		if providerID != "" {
			p = cfg.Find(providerID)
		}
		if p != nil {
			effectiveProviderID, effectiveModel = p.ID, p.Model
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
	if _, err := conversation.Append("teacher-response-started", map[string]any{"responseId": responseID, "teacherMessageId": teacherMessageID, "triggeringLearnerMessageId": learner.ID}); err != nil {
		return err
	}
	emit(StreamFrame{Type: "message-started", Data: map[string]any{"teacherMessageId": teacherMessageID}})

	systemPrompt, providerMessages, err := buildTeacherContext(ctx, slug, conversation, providerID)
	if err != nil {
		message := conversationstore.Message{ID: teacherMessageID, Role: "teacher", Status: "failed", Blocks: []conversationstore.Block{}, CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()}
		_, _ = conversation.RecordMessage(message)
		_, _ = conversation.Append("teacher-response-finished", map[string]any{"responseId": responseID, "messageId": teacherMessageID, "status": "failed"})
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": teacherMessageID, "code": "context_preparation_failed", "partialPreserved": false}})
		return err
	}
	if steerNote != "" {
		systemPrompt += "\n\n【生成中引导】学习者在你上一条回复未完成时插入了：" + steerNote + "\n上一条 interrupted 消息是中断稿：吸收其中仍然有效的部分，按引导方向继续教学。"
	}
	if learner.Role == "system" {
		providerMessages = append(providerMessages, ai.GatewayMessage{Role: "user", Content: "新的学习任务已创建。请依据任务目标、学习范围和已有偏好主动开场：先简短询问有无特殊要求，说明可以不填，且可以随时通过教师页的‘附加信息’修改并保存在当前对话中。若已保存要求，确认会采用即可，不重复询问。不等待填写，继续简要说明将学什么，从第一个关键知识点开始，用一个具体例子引入，最后留一个简短问题了解起点。不要等待学习者问候，不要一次讲完整课程。"})
	}

	var tools []ai.Tool
	searcher := resolveSearcher()
	if searcher != nil {
		tools = append(tools, ai.Tool{Name: "search_web", Description: "检索公开网络信息。适用于时效性强或需要事实核对的问题；搜索结果是外部信息，回答中需辨析可靠性并给出来源链接。", Schema: map[string]any{
			"type": "object", "additionalProperties": false,
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "maxLength": 256, "description": "面向搜索引擎的检索词，用最可能命中的自然语言关键词。"},
			},
			"required": []string{"query"},
		}})
		systemPrompt += "\n\n你可以使用 search_web 工具检索公开网络信息（一次回答最多调用 2 次）。仅在需要时效性信息或事实核对时使用；搜索结果来自外部站点，需要辨析可靠性与时效，并在回答中给出来源链接。"
	}

	budget := ai.BudgetForProvider(effectiveProviderID)
	tools = append(tools, sourcestore.Tools()...)
	systemPrompt += sourceToolInstructions
	initial := ai.GatewayRequest{System: systemPrompt, Messages: providerMessages, Tools: tools, ProviderID: effectiveProviderID, Model: effectiveModel, MaxOutputTokens: budget.Output}
	budgetErr := budget.TrimInitial(&initial)
	providerMessages = initial.Messages
	sourceLimit := min(12000, budget.Window/4, max(0, budget.Available(initial)-512))
	reading := newResponseSources(ctx, slug, sourceLimit)
	if reading.reader != nil && s.OnSourceUpdated != nil {
		reading.reader.OnUpdated = func(source sourcestore.Source) { s.OnSourceUpdated(slug, source) }
	}
	if budgetErr == nil {
		bootstrap := reading.bootstrap(ctx, learnerText, min(2500, sourceLimit))
		systemPrompt += bootstrap
		if strings.Contains(learnerText, "继续") || strings.EqualFold(strings.TrimSpace(learnerText), "continue") {
			if messages, e := conversation.AllMessages(); e == nil {
				systemPrompt += reading.continuation(ctx, messages)
			}
		}
	}
	var text, reasoning strings.Builder
	usage := map[string]int{}
	var providerErr error
	recoveredUnexpectedTool := false
	roundMessages := providerMessages
	sourceCalls, webCalls, toolRounds := 0, 0, 0
	for requestNumber := 0; requestNumber < 8; requestNumber++ {
		if budgetErr != nil {
			providerErr = budgetErr
			break
		}
		request := ai.GatewayRequest{System: systemPrompt, Messages: roundMessages, Tools: tools, ProviderID: effectiveProviderID, Model: effectiveModel, MaxOutputTokens: budget.Output}
		if providerErr = budget.Check(request); providerErr != nil {
			break
		}
		roundTextStart := text.Len()
		var pending []ai.ToolCall
		var roundThinking []json.RawMessage
		var roundProviderContent, roundProviderReasoning strings.Builder
		roundUsage := map[string]int{}
		providerErr = s.Gateway.Stream(ctx, request, func(event ai.GatewayEvent) {
			switch event.Type {
			case "text-delta":
				text.WriteString(event.Delta)
				emit(StreamFrame{Type: "text-delta", Data: map[string]any{"blockId": textBlockID, "delta": event.Delta}})
			case "reasoning-summary-delta":
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
		for k, v := range roundUsage {
			usage[k] += v
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
			if !offered {
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
			systemPrompt += "\n【继续教学】直接回答最后问题。本次不使用工具，不展示自我规划。"
			continue
		}
		assistant := ai.GatewayMessage{Role: "assistant", Content: text.String()[roundTextStart:], ThinkingBlocks: roundThinking, ToolCalls: pending, ProviderContent: roundProviderContent.String(), ReasoningContent: roundProviderReasoning.String()}
		roundMessages = append(roundMessages, assistant)
		limited := toolRounds >= 6 || requestNumber >= 6
		// Reserve a bounded result for every pending ID before any body is loaded.
		resultRoom := budget.Available(ai.GatewayRequest{System: systemPrompt, Messages: roundMessages, Tools: tools}) - 256*len(pending) - 512
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
			} else if isSourceTool(call.ToolName) {
				sourceCalls++
				allocation := min(4000, max(0, resultRoom), reading.limit-reading.used)
				if sourceCalls > 12 {
					code = "tool_limit_reached"
					forceFinal = true
				} else if allocation < 128 {
					code = "budget_exhausted"
					forceFinal = true
				} else {
					operation := strings.TrimSuffix(strings.TrimPrefix(call.ToolName, "get_source_"), "_source")
					if call.ToolName == "search_source" {
						operation = "search"
					}
					if call.ToolName == "read_source" {
						operation = "read"
					}
					sourceID, _ := call.Arguments["sourceId"].(string)
					revisionID, _ := call.Arguments["revisionId"].(string)
					progress := map[string]any{"projectSlug": slug, "responseId": responseID, "operationId": call.CallID, "phase": "started", "operation": operation, "sourceId": sourceID, "revisionId": revisionID}
					s.sourceReading(progress, emit)
					result := reading.execute(ctx, call.ToolName, call.Arguments, allocation)
					encoded, _ := json.Marshal(result)
					resultText = string(encoded)
					done := map[string]any{"projectSlug": slug, "responseId": responseID, "operationId": call.CallID, "phase": "completed", "operation": operation, "sourceId": sourceID, "revisionId": revisionID}
					if data, ok := result.Data.(sourcestore.SourceReadData); ok {
						done["displayRange"] = data.Citation.DisplayRange
					}
					if result.Error != nil {
						done["phase"] = "failed"
						done["reason"] = result.Error.Code
						code = result.Error.Code
						resultStatus = "failed"
						if code == "budget_exhausted" {
							forceFinal = true
						}
					}
					s.sourceReading(done, emit)
				}
			} else if call.ToolName == "search_web" {
				webCalls++
				if webCalls > 2 {
					code = "tool_limit_reached"
					forceFinal = true
				} else if resultRoom < 256 {
					code = "budget_exhausted"
					forceFinal = true
				} else {
					query, _ := call.Arguments["query"].(string)
					if len(call.Arguments) != 1 || query == "" || len([]rune(query)) > 256 {
						code = "invalid_arguments"
					} else {
						emit(StreamFrame{Type: "search-started", Data: map[string]any{"query": query}})
						results, e := searcher.Search(ctx, query)
						if e != nil {
							code = "websearch_unavailable"
							emit(StreamFrame{Type: "search-failed", Data: map[string]any{"query": query, "code": code}})
						} else {
							resultText = ai.TruncateTokens(websearch.FormatResults(results), min(2000, resultRoom))
							emit(StreamFrame{Type: "search-completed", Data: map[string]any{"query": query, "count": len(results)}})
						}
					}
				}
			} else {
				code = "invalid_arguments"
			}
			if resultText == "" {
				encoded, _ := json.Marshal(sourceError(code))
				resultText = string(encoded)
			}
			if code != "" {
				resultStatus = "failed"
			}
			_, _ = conversation.Append("tool-result-recorded", map[string]any{"callId": call.CallID, "status": resultStatus, "code": code})
			roundMessages = append(roundMessages, ai.GatewayMessage{Role: "tool", ToolCallID: call.ProviderCallID, Content: resultText})
			resultRoom -= ai.EstimateTextTokens(resultText) + 32
		}
		toolRounds++
		if streamErr != nil {
			providerErr = streamErr
			break
		}
		if webCalls >= 2 {
			forceFinal = true
		}
		if forceFinal || requestNumber >= 6 {
			tools = nil
			systemPrompt += "\n【本轮阅读结束】只依据已返回证据回答，说明尚未读完的范围；不声称完整阅读。"
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
	message := conversationstore.Message{ID: teacherMessageID, Role: "teacher", Status: status, Blocks: blocks, CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC()}
	if status == "completed" {
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
			_ = usagestore.Append(slug, usagestore.Record{ConversationID: meta.ID, UnitID: unit.UnitID, ResponseID: responseID, ProviderID: effectiveProviderID, InputTokens: usage["inputTokens"], OutputTokens: usage["outputTokens"]})
		}
	}
	if providerErr != nil {
		emit(StreamFrame{Type: "message-failed", Data: map[string]any{"messageId": teacherMessageID, "code": providerFailureCode(providerErr), "partialPreserved": len(blocks) > 0}})
		return providerErr
	}
	latestSeq := finalEvent.Seq
	if meta, e := conversation.Meta(); e == nil {
		latestSeq = meta.LatestSeq
	}
	emit(StreamFrame{Type: "message-completed", Data: map[string]any{"messageId": teacherMessageID, "latestSeq": latestSeq, "sourceCitations": message.SourceCitations, "readingCoverage": message.ReadingCoverage}})
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
	if errors.Is(err, ai.ErrContextBudgetExhausted) {
		return "context_budget_exhausted"
	}
	if errors.Is(err, context.Canceled) {
		return "interrupted"
	}
	return "teacher_provider_unavailable"
}
