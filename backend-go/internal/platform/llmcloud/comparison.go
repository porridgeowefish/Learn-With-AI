package llmcloud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

func (c *Client) StartComparison(ctx context.Context, operation string) (string, error) {
	if c.prompts == nil {
		return "", nil
	}
	var result struct{ ID string }
	token, _ := c.promptSubject()
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	status, err := c.json(ctx, http.MethodPost, "v1/answer-comparison/start", token, map[string]string{"operation": operation}, &result)
	if err != nil || status == http.StatusNoContent {
		return "", err
	}
	if result.ID == "" {
		return "", errors.New("invalid answer comparison")
	}
	return result.ID, nil
}

type ComparisonInput struct {
	Question string `json:"question"`
	Context  string `json:"context,omitempty"`
	Mode     string `json:"mode,omitempty"`
	Opening  bool   `json:"opening,omitempty"`
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
