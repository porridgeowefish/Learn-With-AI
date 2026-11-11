package teacherservice

import "context"
import "strings"
import ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
import conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"

func learningGuidelinePrompt(conversation *conversationstore.Store) (string, error) {
	return learningGuidelinePromptForContext(context.Background(), conversation)
}

func learningGuidelinePromptForContext(ctx context.Context, conversation *conversationstore.Store) (string, error) {
	saved, err := conversation.LearningGuideline()
	if err != nil {
		return "", err
	}
	if saved.Version == 2 && saved.Guideline == "prompt:modes/"+saved.LearningMode {
		return ai.RenderPromptContext(ctx, strings.TrimPrefix(saved.Guideline, "prompt:"), nil)
	}
	return saved.Guideline, nil
}
