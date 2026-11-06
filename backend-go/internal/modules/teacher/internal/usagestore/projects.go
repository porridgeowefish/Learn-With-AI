package usagestore

import (
	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"path/filepath"
	"sort"
)

type ProjectUsage struct {
	ProjectSlug      string `json:"projectSlug"`
	Title            string `json:"title"`
	InputTokens      int    `json:"inputTokens"`
	OutputTokens     int    `json:"outputTokens"`
	NoteInputTokens  int    `json:"noteInputTokens"`
	NoteOutputTokens int    `json:"noteOutputTokens"`
	CacheReadTokens  *int   `json:"cacheReadTokens,omitempty"`
	CacheWriteTokens *int   `json:"cacheWriteTokens,omitempty"`
}

func ListProjects() ([]ProjectUsage, error) {
	index, err := projects.IndexAll()
	if err != nil {
		return nil, err
	}
	rows := []ProjectUsage{}
	for _, project := range index {
		root, err := projects.ProjectRootForSlug(project.Slug)
		if err != nil {
			return nil, err
		}
		row := ProjectUsage{ProjectSlug: project.Slug, Title: project.Title}
		hasUsage := false
		for _, name := range []string{"usage.jsonl", "note-usage.jsonl"} {
			records, err := read(filepath.Join(root, "conversation", name))
			if err != nil {
				return nil, err
			}
			hasUsage = hasUsage || len(records) > 0
			for _, record := range records {
				addOptionalTokens(&row.CacheReadTokens, record.CacheReadTokens)
				addOptionalTokens(&row.CacheWriteTokens, record.CacheWriteTokens)
				if name == "usage.jsonl" {
					row.InputTokens += record.InputTokens
					row.OutputTokens += record.OutputTokens
				} else {
					row.NoteInputTokens += record.InputTokens
					row.NoteOutputTokens += record.OutputTokens
				}
			}
		}
		if hasUsage {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		return a.InputTokens+a.OutputTokens+a.NoteInputTokens+a.NoteOutputTokens > b.InputTokens+b.OutputTokens+b.NoteInputTokens+b.NoteOutputTokens
	})
	return rows, nil
}
