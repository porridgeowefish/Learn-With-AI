package teacherservice

import (
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"strings"
)

// The semantic router proposes an end; local checks bind its quoted evidence
// to the current learner message, never to injected context or a plan.
func confirmedTeachingEnd(plan toolIntent, learner string, history []ai.GatewayMessage) bool {
	if plan.EndDecision != "confirm_end" || strings.TrimSpace(plan.Evidence) == "" || !strings.Contains(learner, plan.Evidence) {
		return false
	}
	if explicitTeachingEnd(learner) {
		return true
	}
	text := strings.ToLower(strings.TrimSpace(learner))
	for _, negative := range []string{"不", "没", "别", "暂时", "可能", "也许", "如果", "是否", "要不要", "not", "never", "maybe", "不结束", "不要结束", "别结束", "还没结束", "继续", "不是结束", "don't end", "do not end", "not finished", "?", "？", "“", "”", "\"", "「", "」"} {
		if strings.Contains(text, negative) {
			return false
		}
	}
	explicit := false
	for _, term := range []string{"结束本次", "本次结束", "结束教学", "今天就到这里", "今天到这里", "结束这次", "到此结束", "end this lesson", "finish this lesson"} {
		if strings.Contains(text, term) {
			explicit = true
		}
	}
	if explicit {
		return true
	}
	for _, done := range []string{"我学完了", "学完了", "本节学完了", "这一节学完了", "我已经学完了", "本章学完了", "这一章学完了"} {
		if strings.Trim(text, " 。！!\n\r") == done {
			return true
		}
	}
	// Short assent is only meaningful in response to an actual assistant ending
	// question. Context messages and earlier unrelated questions cannot authorize.
	last := ""
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			last = history[i].Content
			break
		}
	}
	asks := (strings.Contains(last, "结束") && (strings.Contains(last, "？") || strings.Contains(last, "?")))
	if !asks {
		return false
	}
	for _, yes := range []string{"好", "好的", "可以", "同意", "结束", "确认结束", "是", "是的", "嗯", "ok", "yes"} {
		if strings.Trim(text, " 。！!\n\r") == yes {
			return true
		}
	}
	return false
}

func explicitTeachingEnd(text string) bool {
	text = strings.ToLower(strings.Trim(text, " 。！!\n\r\t"))
	switch text {
	case "我结束了", "结束了", "结束", "确认结束", "我学完了", "学完了", "本节学完了", "这一节学完了", "我已经学完了", "本章学完了", "这一章学完了", "本次结束", "结束本次教学", "结束这次教学", "今天就到这里", "今天到这里", "到此结束":
		return true
	}
	return false
}
