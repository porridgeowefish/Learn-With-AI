package teacherservice

import ai "github.com/xmz14/lll/backend-go/internal/modules/ai"

func promptText(name string) string {
	text, err := ai.RenderPrompt(name, nil)
	if err != nil {
		panic(err)
	}
	return text
}
