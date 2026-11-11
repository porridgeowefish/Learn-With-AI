package httpserver

import (
	"context"
	"maps"
	"regexp"
	"slices"
	"time"

	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

var managedReceiptID = regexp.MustCompile(`^infer_[0-9A-HJKMNP-TV-Z]{26}$`)

func refreshConversationCredits(ctx context.Context, messages []teacher.Message) {
	ids := pendingCreditIDs(messages)
	if len(ids) == 0 {
		return
	}
	client, err := llmcloud.Default()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	settled := make(map[string]int64)
	for batch := range slices.Chunk(ids, 128) {
		result, err := client.SettledCredits(ctx, batch)
		if err != nil {
			break
		}
		maps.Copy(settled, result)
	}
	if len(settled) != 0 {
		projectSettledCredits(messages, settled)
	}
}

func pendingCreditIDs(messages []teacher.Message) []string {
	const lookupLimit = 512
	var ids []string
	seen := make(map[string]bool)
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != "teacher" || message.CreditUsage == nil || !message.CreditUsage.Pending {
			continue
		}
		for _, id := range message.CreditUsage.PendingRequests {
			if seen[id] || !managedReceiptID.MatchString(id) {
				continue
			}
			seen[id] = true
			ids = append(ids, id)
			if len(ids) == lookupLimit {
				return ids
			}
		}
	}
	return ids
}

func projectSettledCredits(messages []teacher.Message, settled map[string]int64) {
	for i := range messages {
		message := &messages[i]
		if message.Role != "teacher" || message.CreditUsage == nil || !message.CreditUsage.Pending || len(message.CreditUsage.PendingRequests) == 0 {
			continue
		}
		usage := *message.CreditUsage
		usage.PendingRequests = nil
		seen := make(map[string]bool)
		for _, id := range message.CreditUsage.PendingRequests {
			if seen[id] {
				continue
			}
			seen[id] = true
			if units, ok := settled[id]; ok && units >= 0 {
				usage.Units += units
			} else {
				usage.PendingRequests = append(usage.PendingRequests, id)
			}
		}
		usage.Pending = len(usage.PendingRequests) != 0
		message.CreditUsage = &usage
	}
}
