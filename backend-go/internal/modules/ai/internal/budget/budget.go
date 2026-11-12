package budget

import (
	"encoding/json"
	"errors"
	aiconfig "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/aiconfig"
	gateway "github.com/xmz14/lll/backend-go/internal/modules/ai/internal/gateway"
	platformconfig "github.com/xmz14/lll/backend-go/internal/platform/config"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Estimates are deliberately conservative; a configured window is a capacity
// constraint, not a claim that the local estimator is a provider tokenizer.
func EstimateTextTokens(s string) int {
	ascii, other := 0, 0
	for _, r := range s {
		if r < 128 {
			ascii++
		} else {
			other++
		}
	}
	return (ascii+2)/3 + (other*3+1)/2
}

type Budget struct{ Window, Output, Thinking, Margin int }

var ErrContextBudgetExhausted = errors.New("teacher context budget exhausted; current learner message retained")

type TeacherBudget struct{ Window, InputLimit, Output, Margin int }

func ForProvider(id string) TeacherBudget {
	b := TeacherBudget{Output: platformconfig.ModelOutputTokens}
	if cfg, err := aiconfig.Load(); err == nil && cfg != nil {
		p := cfg.Resolve("teacher")
		if id != "" {
			p = cfg.Find(id)
		}
		if p != nil {
			b.Window = configuredWindow(*p)
			b.InputLimit = p.ManagedMaxInput
			b.Output = GenerationOutputTokens(*p)
			b.Margin = min(1024, b.Window/16)
		}
	}
	return b
}

func (b TeacherBudget) capacity() int {
	if b.InputLimit > 0 && (b.Window == 0 || b.InputLimit < b.Window) {
		return b.InputLimit
	}
	return b.Window
}

func (b TeacherBudget) ShouldCompact(req gateway.Request) bool {
	const standardWindow = 256000
	capacity := b.capacity()
	if capacity == 0 || capacity > standardWindow {
		capacity = standardWindow
	}
	return (Budget{}).Input(req) >= capacity*80/100
}

func (b TeacherBudget) Available(req gateway.Request) int {
	capacity := b.capacity()
	if capacity == 0 {
		return math.MaxInt
	}
	return capacity - b.Margin - 1 - (Budget{}).Input(req)
}

func (b TeacherBudget) Fit(req *gateway.Request) error {
	if b.Available(*req) < 0 {
		return ErrContextBudgetExhausted
	}
	if b.Window > 0 {
		room := b.Window - b.Margin - (Budget{}).Input(*req)
		req.MaxOutputTokens = min(req.MaxOutputTokens, room)
	}
	return nil
}

func (b TeacherBudget) SourceLimit(req gateway.Request) int {
	limit := 12000
	if capacity := b.capacity(); capacity > 0 {
		limit = min(limit, capacity/4)
	}
	return min(limit, max(0, b.Available(req)-1024))
}
func (b Budget) Input(req gateway.Request) int {
	n := EstimateTextTokens(req.System) + EstimateTextTokens(req.TurnInstructions) + 64
	for _, m := range req.Messages {
		raw, _ := json.Marshal(m)
		n += EstimateTextTokens(string(raw)) + 24
	}
	for _, t := range req.Tools {
		raw, _ := json.Marshal(t)
		n += EstimateTextTokens(string(raw)) + 32
	}
	return n
}
func (b Budget) Available(req gateway.Request) int {
	// Anthropic thinking and OpenAI reasoning both consume the same 100k ceiling.
	return b.Window - b.Output - b.Margin - b.Input(req)
}
func (b Budget) Check(req gateway.Request) error {
	if b.Available(req) < 0 {
		return ErrContextBudgetExhausted
	}
	return nil
}

// CheckGenerationRequest uses the same complete serialized-input estimator as
// teaching turns, without dropping any mandatory generation instructions.
func CheckGenerationRequest(req gateway.Request, p aiconfig.Provider) (int, error) {
	b := generationBudget(p)
	b.Output = GenerationOutputTokens(p)
	if p.Kind == "anthropic" && p.Thinking && req.MaxThinkingTokens > 0 {
		b.Thinking = req.MaxThinkingTokens
	}
	return b.Available(req), b.Check(req)
}

// Every model uses the same learner-authorized single-request output ceiling.
func GenerationOutputTokens(p aiconfig.Provider) int {
	if p.Kind == aiconfig.ManagedKind && p.ManagedMaxOutput > 0 {
		return p.ManagedMaxOutput
	}
	return platformconfig.ModelOutputTokens
}

// Generation-only thinking allowance; normal teaching requests retain 2048.
func GenerationThinkingTokens(p aiconfig.Provider) int {
	if p.Kind != "anthropic" || !p.Thinking {
		return 0
	}
	b := generationBudget(p)
	thinking := 2048
	if endpoint, err := url.Parse(p.BaseURL); err == nil && p.Model == "glm-5.3" && (endpoint.Hostname() == "open.bigmodel.cn" || endpoint.Hostname() == "api.z.ai") {
		thinking = 20480
	}
	return max(1, min(thinking, b.Window/16))
}

func generationBudget(p aiconfig.Provider) Budget {
	// Unknown connections retain the previous ~23k input allowance plus the explicit output
	// reservation. This is a local budgeting fallback, not a model capability.
	b := Budget{Window: 124576, Output: platformconfig.ModelOutputTokens, Margin: 1024}
	if window := configuredWindow(p); window > 0 {
		b.Window = window
	}
	if p.Kind == "anthropic" && p.Thinking {
		b.Thinking = 2048
	}
	b.Margin = min(b.Margin, b.Window/16)
	return b
}

func configuredWindow(p aiconfig.Provider) int {
	if p.ContextWindowTokens > 0 {
		return p.ContextWindowTokens
	}
	if strings.TrimSpace(p.Model) == "glm-5.3" {
		// Verified GLM-5.3 model capacity (2026-10-11). Legacy connections
		// omit this field; do not incorrectly constrain the first-party model
		// to the generic fallback. Unknown relays retain the safe fallback.
		if endpoint, err := url.Parse(p.BaseURL); err == nil && (endpoint.Hostname() == "open.bigmodel.cn" || endpoint.Hostname() == "api.z.ai") {
			return 1000000
		}
	}
	if p.ContextWindowTokens == 0 {
		if endpoint, err := url.Parse(p.BaseURL); err == nil && endpoint.Hostname() == "tokenhub.tencentmaas.com" {
			model := strings.TrimSpace(p.Model)
			switch model {
			case "hy4-preview", "deepseek/deepseek-flash", "deepseek-v4.1-flash", "deepseek-v4-flash-202605", "deepseek-v4-pro-202606",
				"deepseek/deepseek-v4-flash-0731", "deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-pro-0813", "deepseek/deepseek-v4-pro",
				"deepseek/deepseek-v4-flash-vision-exp", "deepseek-v4-flash-0731", "deepseek-v4-pro-0813", "deepseek-v4-flash", "deepseek-v4-pro",
				"glm-5.3-flash", "glm-5.3-flashx", "glm-5.3", "glm-5.2", "kimi-k3", "kimi-k2.8-preview", "minimax-m3",
				"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.5-pro", "step-5-preview":
				return 1000000
			case "hy3", "kimi-k2.7-code-highspeed", "kimi-k2.7-code", "kimi-k2.6":
				return 256000
			case "glm-5v-turbo", "minimax-m2.7":
				return 200000
			case "hunyuan-role-latest", "hy-role":
				return 32000
			}
		}
	}
	return 0
}
func TruncateTokens(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if EstimateTextTokens(s) <= limit {
		return s
	}
	end := 0
	n := 0
	for offset, r := range s {
		cost := 1
		if r >= 128 {
			cost = 2
		}
		if n+cost > limit {
			break
		}
		n += cost
		end = offset + utf8.RuneLen(r)
	}
	return s[:end]
}
