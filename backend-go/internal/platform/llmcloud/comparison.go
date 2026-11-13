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

func (c *Client) StreamComparison(ctx context.Context, id, arm, question string, emit func(json.RawMessage) error) error {
	token, _ := c.promptSubject()
	return c.streamAt(ctx, "v1/answer-comparison/stream", token, map[string]string{"id": id, "arm": arm, "question": question}, emit)
}

func (c *Client) VoteComparison(ctx context.Context, id, vote string) error {
	token, _ := c.promptSubject()
	_, err := c.json(ctx, http.MethodPost, "v1/answer-comparison/vote", token, map[string]string{"id": id, "vote": vote}, nil)
	return err
}
