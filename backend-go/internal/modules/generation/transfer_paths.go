package generation

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"generation/jobs/*.json",
		"generation/inputs/*/input.json",
		"generation/usage.jsonl",
		"generation/candidates/*/output.txt",
	}
}
