package teacherservice

import (
	"context"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"strings"
)

// FeynmanFinishRequest is the exact learner text sent by the "我讲完了" button.
const FeynmanFinishRequest = "我讲完了，请对照复习稿点评：哪些讲清楚了、哪些讲漏了、哪些讲错了。"

// FeynmanOpening is the unpersisted instruction that lets the novice open a new round.
var FeynmanOpening = promptText("feynman/context/opening")

// FeynmanReviewLimit bounds the review excerpt sent as the hidden reference.
const FeynmanReviewLimit = 30000

// FeynmanSystemPrompt builds the novice-listener prompt around one review text.
func FeynmanSystemPrompt(review string) string {
	composed, err := BuildFeynmanComposition(context.Background(), review)
	if err != nil {
		panic(err)
	}
	return composed.Text
}
func BuildFeynmanComposition(ctx context.Context, review string) (ai.ComposedPrompt, error) {
	review = strings.TrimSpace(review)
	runes := []rune(review)
	truncated := "false"
	if len(runes) > FeynmanReviewLimit {
		review = string(runes[:FeynmanReviewLimit])
		truncated = "true"
	}
	return ai.ComposePrompt(ctx, "teacher/feynman", map[string]string{"reviewTruncated": truncated}, map[string]ai.PromptSource{"review": {Role: "context", Read: func(context.Context) (string, error) { return review, nil }}})
}
