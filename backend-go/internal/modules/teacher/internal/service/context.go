package teacherservice

import (
	"context"
	"encoding/json"
	"fmt"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/learningprofile"
	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/continuity"

	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	preferencestore "github.com/xmz14/lll/backend-go/internal/modules/preferences"

	conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"
)

func buildTeacherContext(ctx context.Context, slug string, store *conversationstore.Store, providerID ...string) (string, []ai.GatewayMessage, error) {
	return buildTeacherContextWithFacts(ctx, slug, store, nil, providerID...)
}
func buildTeacherContextWithFacts(ctx context.Context, slug string, store *conversationstore.Store, turnFacts map[string]string, providerID ...string) (string, []ai.GatewayMessage, error) {
	composed, err := buildTeacherComposition(ctx, slug, store, turnFacts, providerID...)
	if err != nil {
		return "", nil, err
	}
	return "", composed.Messages, nil
}
func buildTeacherComposition(ctx context.Context, slug string, store *conversationstore.Store, turnFacts map[string]string, providerID ...string) (ai.ComposedPrompt, error) {
	guideline, err := store.LearningGuideline()
	if err != nil {
		return ai.ComposedPrompt{}, err
	}
	sequenced, err := store.SequencedMessages()
	if err != nil {
		return ai.ComposedPrompt{}, err
	}
	profile, err := learningprofile.Read()
	if err != nil {
		return ai.ComposedPrompt{}, err
	}
	facts := map[string]string{"length": profile.Settings.Length, "webSearchEnabled": "false", "sourcesEnabled": "true", "learningMode": guideline.LearningMode, "isOpening": "false", "hasCourse": "false", "sectionID": ""}
	for key, value := range turnFacts {
		if key != "webSearchEnabled" && key != "sourcesEnabled" {
			return ai.ComposedPrompt{}, fmt.Errorf("unknown teacher turn fact %s", key)
		}
		facts[key] = value
	}
	if len(sequenced) == 0 {
		facts["isOpening"] = "true"
	}
	if root, e := projects.ProjectRootForSlug(slug); e == nil {
		if b, e := os.ReadFile(filepath.Join(root, "learning-scope.json")); e == nil {
			var scope projects.Scope
			if json.Unmarshal(b, &scope) == nil && scope.SchemaVersion == projects.SchemaVersion {
				if scope.Source.MapSlug != "" {
					facts["hasCourse"] = "true"
				}
				facts["sectionID"] = scope.Source.TopicID
			}
		}
	}
	sources := map[string]ai.PromptSource{
		"history": {Messages: func(context.Context) ([]ai.GatewayMessage, error) {
			return toProviderMessages(slug, sequenced), nil
		}},
		"course_framework": {Role: "context", Read: func(context.Context) (string, error) { return continuity.CourseFramework(slug) }},
		"prior_knowledge":  {Role: "context", Read: func(context.Context) (string, error) { return continuity.PriorKnowledge(slug) }},
		"preferences":      {Role: "context", Read: func(context.Context) (string, error) { return preferenceAppendix(), nil }},
		"scope":            {Role: "context", Read: func(context.Context) (string, error) { return softScopeAppendix(slug), nil }},
		"instructions":     {Role: "context", Read: func(context.Context) (string, error) { return store.SystemPrompt() }},
		"guideline":        {Role: "context", Read: func(context.Context) (string, error) { return learningGuidelinePrompt(store) }},
	}
	composed, err := ai.ComposePrompt(ctx, "teacher/general", facts, sources)
	if err != nil {
		return ai.ComposedPrompt{}, err
	}
	return composed, nil
}
func preferenceAppendix() string {
	snapshot, err := preferencestore.Read()
	if err != nil || strings.TrimSpace(snapshot.Content) == "" {
		return ""
	}
	return snapshot.Content
}

func softScopeAppendix(slug string) string {
	root, err := projects.ProjectRootForSlug(slug)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, "learning-scope.json"))
	if err != nil || len(data) > 256<<10 {
		return ""
	}
	var scope projects.Scope
	if json.Unmarshal(data, &scope) != nil || scope.SchemaVersion != projects.SchemaVersion {
		return ""
	}
	view := struct {
		Title           string   `json:"title,omitempty"`
		Chapter         string   `json:"chapter,omitempty"`
		Goal            string   `json:"goal,omitempty"`
		TeachingOutline string   `json:"teachingOutline,omitempty"`
		InScope         []string `json:"inScope,omitempty"`
		Prerequisites   []string `json:"prerequisites,omitempty"`
		Concepts        []string `json:"concepts,omitempty"`
	}{scope.Title, scope.ChapterTitle, scope.Goal, scope.TeachingOutline, scope.InScope, scope.Prerequisites, scope.OwnedConcepts}
	encoded, _ := json.Marshal(view)
	return string(encoded)
}

func toProviderMessages(slug string, messages []conversationstore.SequencedMessage) []ai.GatewayMessage {
	out := make([]ai.GatewayMessage, 0, len(messages))
	for _, item := range messages {
		if item.Message.Role == "teacher" && item.Message.Status == "failed" {
			continue
		}
		role := "assistant"
		if item.Message.Role == "learner" {
			role = "user"
		}
		var content strings.Builder
		for _, block := range item.Message.Blocks {
			if block.Type == "reasoning-summary" {
				continue
			}
			if block.Type == "chat-image" || block.Type == "chat-audio" {
				content.WriteString(promptText("teaching/attachment-text"))
			}
			if block.Source != "" {
				content.WriteString(stripLegacyMessageIDPrefix(block.Source))
				content.WriteByte('\n')
			}
			if block.Type == "attachment" && block.ArtifactRef != "" {
				// Attachment history is immutable. OCR progress and current names
				// belong in this turn's catalog, not in every historical message.
				content.WriteString("[学习者选择了资料：sourceId=" + block.ArtifactRef + "。当前资料信息以本轮目录为准。]\n")
			}
		}
		visible := strings.TrimSpace(content.String())
		if role == "assistant" && visible == "" {
			continue
		}
		out = append(out, ai.GatewayMessage{Role: role, Content: visible})
	}
	return out
}

func stripLegacyMessageIDPrefix(source string) string {
	trimmed := strings.TrimLeft(source, " \t\r\n")
	if !strings.HasPrefix(trimmed, "[messageId=") {
		return source
	}
	end := strings.IndexByte(trimmed, ']')
	if end < 0 {
		return source
	}
	return strings.TrimLeft(trimmed[end+1:], " \t\r\n")
}

func estimateMessageTokens(message conversationstore.Message) int {
	tokens := 12
	for _, block := range message.Blocks {
		for _, r := range block.Source {
			if r <= 127 {
				tokens++
			} else {
				tokens += 3
			}
		}
	}
	// Rough UTF-8-aware conversion: English averages near four chars/token;
	// CJK is conservatively close to one token per rune.
	return tokens/4 + utf8.RuneCountInString(joinBlocks(message.Blocks))/2
}

func joinBlocks(blocks []conversationstore.Block) string {
	var out strings.Builder
	for _, block := range blocks {
		out.WriteString(block.Source)
	}
	return out.String()
}
