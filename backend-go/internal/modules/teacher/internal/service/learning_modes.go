package teacherservice

import conversationstore "github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/conversation"

func learningGuidelinePrompt(conversation *conversationstore.Store) (string, error) {
	saved, err := conversation.LearningGuideline()
	if err != nil {
		return "", err
	}
	return saved.Guideline, nil
}
