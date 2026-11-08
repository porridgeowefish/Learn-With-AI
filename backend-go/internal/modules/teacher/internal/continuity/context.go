package continuity

import (
	"encoding/json"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"sort"
	"strings"
	"time"
)

func CourseFramework(slug string) (string, error) {
	course, err := projects.CourseForProject(slug)
	if err != nil || course.ID == "" {
		return "", err
	}
	topics, err := json.Marshal(course.Topics)
	if err != nil {
		return "", err
	}
	return course.OverviewMarkdown + "\n\n" + string(topics), nil
}

// PriorKnowledge includes every valid completed lesson of unambiguous course
// members, ordered by completion, without importing their planned topic text.
func PriorKnowledge(slug string) (string, error) {
	members, err := projects.CourseMembers(slug)
	if err != nil {
		return "", err
	}
	type entry struct {
		at   time.Time
		text string
	}
	entries := []entry{}
	for _, member := range members {
		if member.Slug == slug {
			continue
		}
		items, e := List(member.Slug)
		if e != nil {
			return "", e
		}
		for _, v := range items {
			if v.Status != "completed" {
				continue
			}
			at := v.CreatedAt
			if v.CompletedAt != nil {
				at = *v.CompletedAt
			}
			text := member.Title + "：" + strings.Join(v.Concepts, "、")
			if v.Scenario != "" {
				text += "。场景：" + v.Scenario
			}
			entries = append(entries, entry{at, text})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].at.Before(entries[j].at) })
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.text)
	}
	return strings.Join(out, "\n"), nil
}
