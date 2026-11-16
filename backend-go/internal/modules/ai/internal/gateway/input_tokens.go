package teachergateway

import "encoding/json"

// EstimateTextTokens is a conservative estimate for model admission.
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

func EstimateInputTokens(req Request) int {
	n := EstimateTextTokens(req.System) + EstimateTextTokens(req.TurnInstructions) + 64
	for _, message := range req.Messages {
		raw, _ := json.Marshal(message)
		n += EstimateTextTokens(string(raw)) + 24
	}
	for _, tool := range req.Tools {
		raw, _ := json.Marshal(tool)
		n += EstimateTextTokens(string(raw)) + 32
	}
	return n
}
