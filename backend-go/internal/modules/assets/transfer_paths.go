package assets

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"assets/review/annotations.jsonl",
		"assets/notes/state.json",
		"assets/notes/jobs/*.json",
		"assets/notes/entries/*.json",
		"assets/feynman/sessions/*.json",
		"assets/practice/results/*.json",
		"assets/review/asset.json",
		"assets/review/current.md",
		"assets/review/versions/*/version.json",
		"assets/review/versions/*/content.md",
		"assets/practice/asset.json",
		"assets/practice/current.md",
		"assets/practice/versions/*/version.json",
		"assets/practice/versions/*/content.md",
	}
}
