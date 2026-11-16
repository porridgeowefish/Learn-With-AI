package config

import (
	"net/url"
	"strings"
	"time"
)

// ModelOutputTokens is the fallback for connections without advertised limits.
const ModelOutputTokens = 100000

const ModelGenerationTimeout = 15 * time.Minute

// ProviderCapacity applies only to connections with verified first-party limits.
func ProviderCapacity(baseURL, model string) (window, output int) {
	endpoint, err := url.Parse(baseURL)
	if err != nil {
		return 0, 0
	}
	host := strings.ToLower(endpoint.Hostname())
	model = strings.ToLower(strings.TrimSpace(model))
	switch {
	case host == "api.openai.com":
		switch model {
		case "gpt-6.1-sol", "gpt-6.1-astra", "gpt-6.1-luna", "gpt-5.4", "gpt-5.4-2026-03-05", "gpt-5.5", "gpt-5.5-2026-04-23":
			return 1_050_000, 128_000
		case "gpt-5.2", "gpt-5.2-2025-12-11":
			return 400_000, 128_000
		}
	case (host == "open.bigmodel.cn" || host == "api.z.ai") && model == "glm-5.3":
		return 1_000_000, 128_000
	case host == "api.deepseek.com" && (model == "deepseek-flash" || model == "deepseek-v4.1-flash" || model == "deepseek-v4-pro"):
		return 1_048_576, 393_216
	case host == "tokenhub.tencentmaas.com" && model == "hy4":
		return 1_048_576, 65_536
	case (host == "dashscope.aliyuncs.com" || strings.HasSuffix(host, ".maas.aliyuncs.com")) && model == "kimi-k3":
		return 1_048_576, 1_048_576
	}
	return 0, 0
}

func ProviderOutputTokens(baseURL, model string) int {
	_, output := ProviderCapacity(baseURL, model)
	if output > 0 {
		return output
	}
	return ModelOutputTokens
}

func ModelOutputLimitKey(baseURL, model string) string {
	endpoint, _ := url.Parse(baseURL)
	if endpoint != nil && (endpoint.Hostname() == "api.openai.com" || endpoint.Hostname() == "api.groq.com") {
		return "max_completion_tokens"
	}
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "chat-latest" || strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") || strings.HasPrefix(model, "o4") {
		return "max_completion_tokens"
	}
	return "max_tokens"
}
