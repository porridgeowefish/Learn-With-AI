package workspace

// ValidateTransferState checks a project independently of the process's active root.
func ValidateTransferState(root string, state *ProjectState) error {
	state.ProjectType = NormalizeProjectType(state.ProjectType)
	return checkLayout(root, state)
}
