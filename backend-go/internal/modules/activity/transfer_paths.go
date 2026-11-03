package activity

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"progress/events.jsonl",
		"progress/summary.json",
	}
}
