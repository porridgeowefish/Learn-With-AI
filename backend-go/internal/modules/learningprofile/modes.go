package learningprofile

type LearningGuideline struct {
	LearningMode string `json:"learningMode"`
	Version      int    `json:"version"`
	Guideline    string `json:"guideline"`
}

// NewLearningGuideline stores a builtin resource reference for new projects.
// Existing version-1 snapshots remain unchanged.
func NewLearningGuideline(mode string) LearningGuideline {
	mode = NormalizeLearningMode(mode)
	return LearningGuideline{LearningMode: mode, Version: 2, Guideline: "prompt:modes/" + mode}
}

// NormalizeLearningMode keeps requests and projects created before mode selection
// compatible with the adaptive teacher.
func NormalizeLearningMode(mode string) string {
	if mode == "" {
		return "general"
	}
	return mode
}

func ValidLearningMode(mode string) bool {
	switch NormalizeLearningMode(mode) {
	case "general", "history", "book", "concept":
		return true
	default:
		return false
	}
}
