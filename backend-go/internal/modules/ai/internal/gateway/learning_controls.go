package teachergateway

import (
	"fmt"
	config "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	"regexp"
	"strings"
)

// Conservative protocol capabilities. Unknown/third-party models do not inherit
// OpenAI features merely because they expose a compatible endpoint.
func ReasoningCapabilities(p config.Provider) []string {
	m := strings.ToLower(p.Model)
	if p.Kind == "anthropic" {
		if strings.HasPrefix(m, "claude-3-7-sonnet") || strings.HasPrefix(m, "claude-sonnet-4-2025") || strings.HasPrefix(m, "claude-opus-4-2025") {
			return []string{"low", "medium", "high", "xhigh"}
		}
		return []string{}
	}
	if p.Kind != "siwc" && !strings.HasPrefix(strings.TrimRight(p.BaseURL, "/"), "https://api.openai.com/") {
		return []string{}
	}
	if strings.Contains(m, "chat") || strings.Contains(m, "realtime") {
		return []string{}
	}
	m = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`).ReplaceAllString(m, "")
	switch m {
	case "gpt-5.2", "gpt-5.3-codex", "gpt-5.4", "gpt-5.5", "gpt-5.6", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol", "gpt-6.1-terra":
		return []string{"low", "medium", "high", "xhigh"}
	case "gpt-5", "gpt-5-mini", "gpt-5-nano", "gpt-5.1", "o3", "o3-mini", "o4-mini":
		return []string{"low", "medium", "high"}
	}
	return []string{}
}

func validateEffort(p config.Provider, in Request) error {
	if in.ReasoningEffort == "" {
		return nil
	}
	for _, value := range ReasoningCapabilities(p) {
		if value == in.ReasoningEffort {
			return nil
		}
	}
	return fmt.Errorf("模型 %s 不支持思考强度 %s，请重新选择", p.Model, in.ReasoningEffort)
}
