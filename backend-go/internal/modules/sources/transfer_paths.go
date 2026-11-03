package sources

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"sources/*/source.json",
		"sources/*/revisions/*/revision.json",
		"sources/*/revisions/*/derived/content.md",
		"sources/*/revisions/*/derived/structure.json",
		"sources/*/revisions/*/derived/current.json",
		"sources/*/revisions/*/derived/versions/*/manifest.json",
		"sources/*/revisions/*/derived/versions/*/structure.json",
		"sources/*/revisions/*/derived/versions/*/content.md",
		"sources/*/revisions/*/structure/jobs/*.json",
		"sources/*/revisions/*/structure/automatic-budget.json",
	}
}
