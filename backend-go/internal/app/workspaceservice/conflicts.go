// Package workspaceservice coordinates transfers between learning workspaces.
package workspaceservice

import "strings"

// ProjectEntry contains the visible names from a validated project and its
// current catalog. Historical versions are not separate visible projects.
type ProjectEntry struct {
	Slug   string
	Title  string
	Topics []TopicEntry
}

type TopicEntry struct {
	ID      string
	Title   string
	Chapter string
}

type Location struct {
	Source       string `json:"source"`
	ProjectSlug  string `json:"projectSlug"`
	ProjectTitle string `json:"projectTitle"`
	TopicID      string `json:"topicId,omitempty"`
	Chapter      string `json:"chapter,omitempty"`
	Title        string `json:"title"`
}

type NameConflict struct {
	Kind      string     `json:"kind"`
	Locations []Location `json:"locations"`
}

// ImportNameConflicts returns each collision once, in incoming catalog order.
// Existing duplicates that the import does not touch are left alone.
func ImportNameConflicts(current, incoming []ProjectEntry) []NameConflict {
	type key struct{ kind, name string }
	buckets := make(map[key][]Location)
	var incomingOrder []key
	seen := make(map[key]bool)
	add := func(kind string, loc Location) {
		k := key{kind, strings.TrimSpace(loc.Title)}
		buckets[k] = append(buckets[k], loc)
		if loc.Source == "incoming" && !seen[k] {
			seen[k] = true
			incomingOrder = append(incomingOrder, k)
		}
	}
	index := func(source string, entries []ProjectEntry) {
		for _, project := range entries {
			loc := Location{Source: source, ProjectSlug: project.Slug, ProjectTitle: project.Title, Title: project.Title}
			add("project", loc)
			for _, topic := range project.Topics {
				loc.TopicID, loc.Chapter, loc.Title = topic.ID, topic.Chapter, topic.Title
				add("topic", loc)
			}
		}
	}
	index("current", current)
	index("incoming", incoming)
	conflicts := []NameConflict{}
	for _, k := range incomingOrder {
		if locations := buckets[k]; len(locations) > 1 {
			conflicts = append(conflicts, NameConflict{Kind: k.kind, Locations: locations})
		}
	}
	return conflicts
}
