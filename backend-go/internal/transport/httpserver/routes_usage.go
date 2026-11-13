package httpserver

import (
	"net/http"
	"strconv"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

func (s *Server) handleListTeacherUsage(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	rows, err := teacher.ListTeacherUsage()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	start := len(rows)
	if page-1 <= len(rows)/pageSize {
		start = (page - 1) * pageSize
	}
	end := start + min(pageSize, len(rows)-start)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"page": page, "pageSize": pageSize, "total": len(rows), "conversations": rows[start:end]})
}

func (s *Server) handleListProjectUsage(w http.ResponseWriter, r *http.Request) {
	rows, err := teacher.ListProjectUsage()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	index, err := projects.IndexAll()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	bySlug := map[string]teacher.ProjectUsage{}
	for _, row := range rows {
		bySlug[row.ProjectSlug] = row
	}
	result := []map[string]any{}
	for _, project := range index {
		row := bySlug[project.Slug]
		row.ProjectSlug = project.Slug
		row.Title = project.Title
		usage, err := generation.UsageRecords(project.Slug)
		if err != nil {
			learningWorkspaceError(w, err)
			return
		}
		input, output, unknown := 0, 0, 0
		for _, u := range usage {
			if !u.Known {
				unknown++
			}
			input += u.Tokens["inputTokens"]
			output += u.Tokens["outputTokens"]
		}
		if len(usage) == 0 && row.InputTokens+row.OutputTokens+row.NoteInputTokens+row.NoteOutputTokens == 0 {
			continue
		}
		result = append(result, map[string]any{"projectSlug": row.ProjectSlug, "title": row.Title, "inputTokens": row.InputTokens + input, "outputTokens": row.OutputTokens + output, "noteInputTokens": row.NoteInputTokens, "noteOutputTokens": row.NoteOutputTokens, "generationInputTokens": input, "generationOutputTokens": output, "generationRequests": len(usage), "unknownUsageRequests": unknown})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": result})
}
