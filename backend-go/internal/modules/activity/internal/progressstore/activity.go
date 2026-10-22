package progressstore

import (
	"fmt"
	"sort"
	"time"

	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
)

type ActivityEvent struct {
	Event
	ProjectSlug  string `json:"projectSlug"`
	ProjectTitle string `json:"projectTitle"`
}

type ActivityDay struct {
	Date     string          `json:"date"`
	Activity int             `json:"activity"`
	Growth   int             `json:"growth"`
	Actions  int             `json:"actions"`
	Events   []ActivityEvent `json:"events"`
}

type ActivitySummary struct {
	RangeStart    string        `json:"rangeStart"`
	RangeEnd      string        `json:"rangeEnd"`
	ActiveDays    int           `json:"activeDays"`
	CurrentStreak int           `json:"currentStreak"`
	LongestStreak int           `json:"longestStreak"`
	TotalActions  int           `json:"totalActions"`
	TotalGrowth   int           `json:"totalGrowth"`
	Days          []ActivityDay `json:"days"`
}

// Aggregate derives a global or one-project rhythm from project-local streams.
// TotalGrowth is all-time; day/activity metrics are limited to the requested range.
func Aggregate(projectSlug string, weeks int, now time.Time) (ActivitySummary, error) {
	if weeks != 26 && weeks != 52 {
		return ActivitySummary{}, fmt.Errorf("weeks must be 26 or 52")
	}
	loc := now.Location()
	end := dayStart(now.In(loc))
	weekdayFromMonday := (int(end.Weekday()) + 6) % 7
	start := end.AddDate(0, 0, -((weeks-1)*7 + weekdayFromMonday))
	dayCount := int(end.Sub(start).Hours()/24) + 1
	out := ActivitySummary{
		RangeStart: start.Format("2006-01-02"),
		RangeEnd:   end.Format("2006-01-02"),
		Days:       make([]ActivityDay, dayCount),
	}
	byDate := make(map[string]*ActivityDay, len(out.Days))
	for i := range out.Days {
		date := start.AddDate(0, 0, i).Format("2006-01-02")
		out.Days[i] = ActivityDay{Date: date, Events: []ActivityEvent{}}
		byDate[date] = &out.Days[i]
	}

	projects, err := workspace.IndexAll()
	if err != nil {
		return ActivitySummary{}, err
	}
	for _, project := range projects {
		if projectSlug != "" && project.Slug != projectSlug {
			continue
		}
		store, err := New(project.Slug)
		if err != nil {
			continue
		}
		events, err := store.ReadEvents()
		if err != nil {
			return ActivitySummary{}, err
		}
		for _, event := range events {
			out.TotalGrowth += event.Delta
			created, err := time.Parse(time.RFC3339, event.CreatedAt)
			if err != nil {
				continue
			}
			date := created.In(loc).Format("2006-01-02")
			day := byDate[date]
			if day == nil {
				continue
			}
			day.Growth += event.Delta
			activity := event.ActivityDelta
			if activity == 0 && event.SourceType == "practice-submit" {
				activity = 1 // backward-compatible activity for pre-iter-08 submissions
			}
			if activity <= 0 {
				if event.Delta != 0 {
					day.Events = append(day.Events, ActivityEvent{Event: event, ProjectSlug: project.Slug, ProjectTitle: project.Title})
				}
				continue
			}
			day.Activity += activity
			day.Actions++
			day.Events = append(day.Events, ActivityEvent{Event: event, ProjectSlug: project.Slug, ProjectTitle: project.Title})
		}
	}

	for i := range out.Days {
		day := &out.Days[i]
		if day.Activity > 0 {
			out.ActiveDays++
		}
		out.TotalActions += day.Actions
		sort.SliceStable(day.Events, func(a, b int) bool {
			return day.Events[a].CreatedAt > day.Events[b].CreatedAt
		})
	}
	out.LongestStreak = longestStreak(out.Days)
	out.CurrentStreak = currentStreak(out.Days)
	return out, nil
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func longestStreak(days []ActivityDay) int {
	best, current := 0, 0
	for _, day := range days {
		if day.Activity > 0 {
			current++
			if current > best {
				best = current
			}
		} else {
			current = 0
		}
	}
	return best
}

func currentStreak(days []ActivityDay) int {
	if len(days) == 0 {
		return 0
	}
	i := len(days) - 1
	// A streak remains current through the following rest-of-day boundary.
	if days[i].Activity == 0 {
		i--
	}
	count := 0
	for ; i >= 0 && days[i].Activity > 0; i-- {
		count++
	}
	return count
}
