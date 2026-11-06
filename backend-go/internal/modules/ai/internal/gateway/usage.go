package teachergateway

// Optional counts preserve the difference between a reported zero and an
// unsupported/missing metric. All input totals include cached input exactly once.
type openAIUsage struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens"`
	PromptDetails    *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	CacheHitTokens  *int `json:"prompt_cache_hit_tokens"`
	CacheMissTokens *int `json:"prompt_cache_miss_tokens"`
}

func putUsage(out map[string]int, key string, value *int) {
	if value != nil && *value >= 0 {
		out[key] = *value
	}
}

func (u openAIUsage) metrics() map[string]int {
	out := map[string]int{}
	putUsage(out, "inputTokens", u.PromptTokens)
	putUsage(out, "outputTokens", u.CompletionTokens)
	if u.PromptDetails != nil {
		putUsage(out, "cacheReadTokens", u.PromptDetails.CachedTokens)
	}
	putUsage(out, "cacheReadTokens", u.CacheHitTokens)
	putUsage(out, "cacheMissTokens", u.CacheMissTokens)
	if u.PromptTokens == nil && u.CacheHitTokens != nil && u.CacheMissTokens != nil && *u.CacheHitTokens >= 0 && *u.CacheMissTokens >= 0 {
		out["inputTokens"] = *u.CacheHitTokens + *u.CacheMissTokens
	}
	return out
}

type anthropicUsage struct {
	InputTokens      *int `json:"input_tokens"`
	OutputTokens     *int `json:"output_tokens"`
	CacheReadTokens  *int `json:"cache_read_input_tokens"`
	CacheWriteTokens *int `json:"cache_creation_input_tokens"`
}

func (u *anthropicUsage) merge(next *anthropicUsage) {
	if next == nil {
		return
	}
	if next.InputTokens != nil {
		u.InputTokens = next.InputTokens
	}
	if next.OutputTokens != nil {
		u.OutputTokens = next.OutputTokens
	}
	if next.CacheReadTokens != nil {
		u.CacheReadTokens = next.CacheReadTokens
	}
	if next.CacheWriteTokens != nil {
		u.CacheWriteTokens = next.CacheWriteTokens
	}
}

func (u anthropicUsage) metrics() map[string]int {
	out := map[string]int{}
	putUsage(out, "inputTokens", u.InputTokens)
	putUsage(out, "outputTokens", u.OutputTokens)
	putUsage(out, "cacheReadTokens", u.CacheReadTokens)
	putUsage(out, "cacheWriteTokens", u.CacheWriteTokens)
	if _, known := out["inputTokens"]; known {
		out["inputTokens"] += out["cacheReadTokens"] + out["cacheWriteTokens"]
	}
	return out
}

type responsesUsage struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
	InputDetails *struct {
		CachedTokens *int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
	OutputDetails *struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
}

func (u responsesUsage) metrics() map[string]int {
	out := map[string]int{}
	putUsage(out, "inputTokens", u.InputTokens)
	putUsage(out, "outputTokens", u.OutputTokens)
	if u.InputDetails != nil {
		putUsage(out, "cacheReadTokens", u.InputDetails.CachedTokens)
	}
	if u.OutputDetails != nil {
		putUsage(out, "reasoningTokens", u.OutputDetails.ReasoningTokens)
	}
	return out
}
