package teachergateway

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

var ErrContextOverflow = errors.New("model context window exceeded")

func IsContextOverflow(err error) bool {
	var remote *llmcloud.RemoteError
	return errors.Is(err, ErrContextOverflow) ||
		(errors.As(err, &remote) && remote.Code == "model_input_limit")
}

type contextLimitDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func contextLimitResponse(raw []byte) bool {
	var envelope struct {
		contextLimitDetail
		Error contextLimitDetail `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	return contextLimitMessage(envelope.Code, envelope.Message) ||
		contextLimitMessage(envelope.Error.Code, envelope.Error.Message)
}

func contextLimitMessage(code, message string) bool {
	switch code {
	case "context_length_exceeded", "context_window_exceeded", "model_input_limit":
		return true
	}
	message = strings.ToLower(message)
	return strings.Contains(message, "maximum context length") ||
		strings.Contains(message, "exceeds the context window") ||
		strings.Contains(message, "prompt is too long")
}
