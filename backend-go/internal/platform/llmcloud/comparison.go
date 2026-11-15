package llmcloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type ComparisonAssignment struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Protocol int    `json:"protocol"`
}

func (c *Client) StartComparison(ctx context.Context, operation string) (ComparisonAssignment, error) {
	var result ComparisonAssignment
	if c.prompts == nil {
		return result, nil
	}
	token, _ := c.promptSubject()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	status, err := c.json(ctx, http.MethodPost, "v1/answer-comparison/start", token, map[string]string{"operation": operation}, &result)
	if err != nil || status == http.StatusNoContent {
		return ComparisonAssignment{}, err
	}
	if result.ID == "" {
		return ComparisonAssignment{}, errors.New("invalid answer comparison")
	}
	return result, nil
}

type ComparisonInput struct {
	Question string              `json:"question"`
	Context  string              `json:"context,omitempty"`
	Mode     string              `json:"mode,omitempty"`
	Opening  bool                `json:"opening,omitempty"`
	Teaching *ComparisonTeaching `json:"teaching,omitempty"`
}

// ComparisonTeaching carries one provider-neutral teaching request for both arms.
// Indices refer to the positions of the replaceable parts of that request.
type ComparisonTeaching struct {
	Messages        json.RawMessage `json:"messages"`
	Tools           json.RawMessage `json:"tools,omitempty"`
	TeacherIndex    int             `json:"teacherIndex"`
	ModeIndex       *int            `json:"modeIndex,omitempty"`
	ModeText        string          `json:"modeText,omitempty"`
	OpeningIndex    *int            `json:"openingIndex,omitempty"`
	ReasoningEffort string          `json:"reasoningEffort,omitempty"`
}

func (c *Client) SubmitComparisonToolResult(ctx context.Context, id, text string, final bool) error {
	token, _ := c.promptSubject()
	_, err := c.json(ctx, http.MethodPost, "v1/answer-comparison/tool-result", token, struct {
		ID, Text string
		Final    bool
	}{id, text, final}, nil)
	return err
}

func (c *Client) StreamComparison(ctx context.Context, id, arm string, input ComparisonInput, emit func(json.RawMessage) error) error {
	token, _ := c.promptSubject()
	request := struct {
		ID  string `json:"id"`
		Arm string `json:"arm"`
		ComparisonInput
	}{id, arm, input}
	return c.streamAt(ctx, "v1/answer-comparison/stream", token, request, emit)
}

func (c *Client) VoteComparison(ctx context.Context, id, vote string) error {
	token, _ := c.promptSubject()
	_, err := c.json(ctx, http.MethodPost, "v1/answer-comparison/vote", token, map[string]string{"id": id, "vote": vote}, nil)
	return err
}

func (c *Client) SubmitReplyFeedback(ctx context.Context, replyID, model, value string, revision uint64) error {
	input := struct {
		ReplyID  string `json:"replyId"`
		Model    string `json:"model"`
		Value    string `json:"value"`
		Revision uint64 `json:"revision"`
	}{replyID, model, value, revision}
	_, err := c.json(ctx, http.MethodPost, "v1/reply-feedback", "", input, nil)
	return err
}
