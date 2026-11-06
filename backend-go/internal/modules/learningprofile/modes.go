package learningprofile

type LearningGuideline struct {
	LearningMode string `json:"learningMode"`
	Version      int    `json:"version"`
	Guideline    string `json:"guideline"`
}

// NewLearningGuideline is called once at project creation. The prose is stored
// with the project, so later app updates cannot silently change its guideline.
func NewLearningGuideline(mode string) LearningGuideline {
	mode = NormalizeLearningMode(mode)
	const boundary = "\n\n【学习方式参考】这是本项目创建时选择的方法论引导，不是固定流程或必须逐项完成的模板。请根据当前问题自然取舍；学习者中途明确要求换一种讲法、顺序或角度时，优先遵从，继续利用已有上下文。"
	var guidance string
	switch mode {
	case "history":
		guidance = "历史脉络：可以用有证据的故事串起时间线，结合时代背景、参与者、关键转折与因果关系解释变化。区分事件先后与因果解释；技术概念也可从出现和发展的背景理解。日期、人物言论与动机缺乏依据时明确不确定，不虚构对话或把传说当史实。"
	case "book":
		guidance = "书籍导读：适合先建立阅读地图，再深入文本。可结合书的类型、作者或编者、成书背景、题名、真实目录与篇章组织，帮助理解论述或叙事顺序和可选阅读路线。优先从已上传资料的封面、序言、目录核对版本。没有看到或查证目录时明确说明，不编造章节标题、目录、引文与页码；可给出标明初步性质的阅读建议。区分作者观点、文本证据和教师解释，按书的类型灵活选择阅读方法。"
	case "concept":
		guidance = "概念探究：可以从它解决的问题、直观定义与原理、具体例子与反例、适用条件和边界、相近概念的异同等视角建立联系。按当前困惑挑选最有帮助的角度，必要时用对照、因果图或小实验澄清混淆，不必每轮覆盖所有角度。"
	default:
		guidance = "自由学习：根据当前问题、学习者基础和反馈灵活选择讲解方式，在故事、概念、例子、练习与图示之间自然切换。"
	}
	return LearningGuideline{LearningMode: mode, Version: 1, Guideline: boundary + guidance}
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
