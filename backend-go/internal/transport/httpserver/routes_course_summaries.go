package httpserver

import (
	"encoding/json"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"net/http"
)

func (s *Server) courseSummaries(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	projection, err := generation.ReadMap(slug, r.URL.Query().Get("versionId"))
	if err != nil {
		generationError(w, err)
		return
	}
	state, err := projects.ReadProjectState(slug)
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	all, err := projects.IndexAll()
	if err != nil {
		learningWorkspaceError(w, err)
		return
	}
	lessons := []map[string]any{}
	for _, p := range all {
		if p.ProjectType != projects.ProjectTypeSystemLearning {
			continue
		}
		source, e := projects.CourseSource(p.Slug)
		if e != nil || source.Type != projects.SourceDisciplineMap || source.MapSlug != slug {
			continue
		}
		if source.BindingStatus != "unresolved" && source.MapID != "" && source.MapID != state.ID {
			continue
		}
		if source.BindingStatus != "unresolved" && source.MapVersionID != "" && source.MapVersionID != projection.MapVersionID {
			continue
		}
		if _, ok := projects.FindTopic(projection.Topics, source.TopicID); !ok {
			continue
		}
		item := map[string]any{"projectSlug": p.Slug, "title": p.Title, "topicId": source.TopicID, "linked": source.BindingStatus != "unresolved" && source.MapID == state.ID && source.MapVersionID == projection.MapVersionID, "needsConfirmation": source.BindingStatus == "unresolved"}
		summaries, e := teacher.TeachingSummaries(p.Slug)
		if e != nil {
			item["error"] = "无法读取学习概括"
		} else {
			item["summaries"] = summaries
		}
		lessons = append(lessons, item)
	}
	httpx.WriteJSON(w, 200, map[string]any{"lessons": lessons})
}

func (s *Server) confirmCourseBinding(w http.ResponseWriter, r *http.Request) {
	var in struct {
		MapSlug      string `json:"mapSlug"`
		MapVersionID string `json:"mapVersionId"`
		TopicID      string `json:"topicId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		httpx.Error(w, 400, "invalid course binding")
		return
	}
	if err := projects.ConfirmCourseBinding(r.PathValue("id"), in.MapSlug, in.MapVersionID, in.TopicID); err != nil {
		httpx.Error(w, 409, err.Error())
		return
	}
	httpx.WriteJSON(w, 200, map[string]bool{"linked": true})
}
