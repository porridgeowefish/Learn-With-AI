package teacher

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"conversation/events.jsonl",
		"conversation/conversation.json",
		"conversation/system-prompt.txt",
		"conversation/learning-mode.json",
		"conversation/usage.jsonl",
		"conversation/note-usage.jsonl",
	}
}
