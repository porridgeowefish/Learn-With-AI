// Package activity owns durable learning activity and its projections.
package activity

import (
	"github.com/xmz14/lll/backend-go/internal/modules/activity/internal/progressstore"
	"time"
)

const PolicyVersion = progressstore.PolicyVersion
const LearningPolicyVersion = progressstore.LearningPolicyVersion

type ProgressEvent = progressstore.Event
type ProgressSummary = progressstore.Summary
type ProgressStore = progressstore.Store
type ActivityEvent = progressstore.ActivityEvent
type ActivityDay = progressstore.ActivityDay
type ActivitySummary = progressstore.ActivitySummary

var NewProgressStore = progressstore.New

func RecordActivity(slug string, event ProgressEvent) (ProgressEvent, bool, error) {
	if event.PolicyVersion == "" {
		event.PolicyVersion = LearningPolicyVersion
	}
	store, err := progressstore.New(slug)
	if err != nil {
		return ProgressEvent{}, false, err
	}
	awarded, added, _, err := store.Award(event)
	return awarded, added, err
}
func Aggregate(slug string, weeks int, now time.Time) (ActivitySummary, error) {
	return progressstore.Aggregate(slug, weeks, now)
}
