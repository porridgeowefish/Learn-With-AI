package teacher

import (
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/continuity"
)

type SummaryService = continuity.Service
type TeachingSummary = continuity.Summary

func NewSummaryService(g ai.Gateway) *SummaryService { return continuity.New(g) }

var TeachingSummaries = continuity.List
var RevokeTeachingSummary = continuity.Revoke
