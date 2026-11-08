package teacherservice

var SystemPrompt = teacherBasePrompt + teacherReasoningPrompt + diagramPrompt + mathFormattingPrompt + mathTeachingPrompt

var mathFormattingPrompt = promptText("teaching/mathFormattingPrompt")

var diagramPrompt = promptText("teaching/diagramPrompt")

var mathTeachingPrompt = promptText("teaching/mathTeachingPrompt")
