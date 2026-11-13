package assets

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"assets/review-html/annotations.jsonl",
		"assets/notes/state.json",
		"assets/notes/jobs/*.json",
		"assets/notes/entries/*.json",
		"assets/feynman/sessions/*.json",
		"assets/practice/results/*.json",
		"assets/review-html/asset.json",
		"assets/review-html/current.html",
		"assets/review-html/versions/*/version.json",
		"assets/review-html/versions/*/content.html",
		"assets/practice/asset.json",
		"assets/practice/current.md",
		"assets/practice/versions/*/version.json",
		"assets/practice/versions/*/content.md",
	}
}
