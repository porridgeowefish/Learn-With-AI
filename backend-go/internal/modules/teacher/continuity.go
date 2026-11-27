package teacher

import (
	"regexp"
	"strings"

	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher/internal/continuity"
)

type SummaryService = continuity.Service
type TeachingSummary = continuity.Summary

func NewSummaryService(g ai.Gateway) *SummaryService { return continuity.New(g) }

var TeachingSummaries = continuity.List
var RevokeTeachingSummary = continuity.Revoke

var courseFinish = regexp.MustCompile(`^(?:好的?[，,、 ]*)?(?:(?:这节|这课|本节|本单元|这个单元|今天这节)(?:课)?(?:我)?学(?:习)?完了|(?:我)?学完了|(?:请)?结束(?:本次|这次|这节|本节)(?:教学|课程|课|学习)|(?:这节|本节|今天)就到这里)[。！! ]*$`)

// ObserveCourseCompletion seals a lesson after the learner closes it in the classroom.

func ObserveCourseCompletion(s *SummaryService, slug string, info ResponseInfo, learner Message) error {
	var text strings.Builder
	for _, block := range learner.Blocks {
		if block.Type == "markdown" {
			text.WriteString(block.Source)
		}
	}
	if !courseFinish.MatchString(strings.TrimSpace(text.String())) {
		return nil
	}
	source, err := projects.CourseSource(slug)
	if err != nil || source.Type != projects.SourceDisciplineMap || source.MapID == "" || source.BindingStatus == "unresolved" {
		return nil
	}
	conversation, err := NewConversation(slug)
	if err != nil {
		return err
	}
	messages, err := conversation.SequencedMessages()
	if err != nil {
		return err
	}
	hasLearning, hasTeaching := false, false
	for _, item := range messages {
		if item.Message.ID == learner.ID {
			if !hasLearning || !hasTeaching {
				return nil
			}
			if _, err := continuity.Confirm(slug, learner.ID, info.TeacherMessageID, item.Seq); err != nil {
				return err
			}
			s.Notify(slug)
			return nil
		}
		if item.Message.OperationID == "teacher-welcome-v1" || item.Message.Status != "completed" {
			continue
		}
		if item.Message.Role == "learner" {
			hasLearning = true
		}
		if hasLearning && item.Message.Role == "teacher" {
			hasTeaching = true
		}
	}
	return nil
}
