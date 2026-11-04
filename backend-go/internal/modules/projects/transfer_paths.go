package projects

// TransferPaths lists this module's durable, portable project records.
func TransferPaths() []string {
	return []string{
		"state.json",
		"unit.json",
		"project.md", // Preserve legacy files during transfer; no runtime consumer.
		"learning-scope.json",
		"learning-plan.json",
		"overview.md",
		"discipline-topics.json",
		"map/current.json",
		"map/versions/*/overview.md",
		"map/versions/*/topics.json",
		"map/versions/*/manifest.json",
	}
}
