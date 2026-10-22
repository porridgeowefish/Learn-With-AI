package httpserver

import (
	"errors"
	"github.com/xmz14/lll/backend-go/internal/app/projectservice"
	projects "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"

	"net/http"

	"regexp"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/generation"
)

// handleDeleteProject permanently removes one learning project and its local
// artifacts. The frontend owns the single user-confirmation step; the backend
// owns validation, active-run protection, and associated reference cleanup.
func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	mediaLock := s.chatMediaLock(slug)
	mediaLock.Lock()
	defer mediaLock.Unlock()
	if !projects.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}

	if err := s.projectOperations().Delete(slug); err != nil {
		if errors.Is(err, teacher.ErrResponseActive) || errors.Is(err, teacher.ErrNotesProjectBusy) {
			httpx.Error(w, 409, err.Error())
			return
		}
		learningWorkspaceError(w, err)
		return
	}

	s.cache.Invalidate(slug)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"deleted": true, "projectId": slug})
}

// validEditableFilename limits learner-owned zone writes to safe filenames.
var validEditableFilename = regexp.MustCompile(`^[A-Za-z0-9._\-]+$`)

// cache is the package-global project index. Populated on startup and on writes.

// createProjectRequest is the body of POST /api/projects.
// Why/Current/Target/Standard seed only system-learning project.md files.
// Discipline-map creation ignores them at the workspace boundary.
type createProjectRequest struct {
	SystemPrompt string                      `json:"systemPrompt,omitempty"`
	Title        string                      `json:"title"`
	Slug         string                      `json:"slug,omitempty"`
	ProjectType  projects.ProjectType        `json:"projectType,omitempty"`
	Why          string                      `json:"why,omitempty"`
	Current      string                      `json:"current,omitempty"`
	Target       string                      `json:"target,omitempty"`
	Standard     string                      `json:"standard,omitempty"`
	ScopeSource  *learningScopeSourceRequest `json:"scopeSource,omitempty"`
}

type learningScopeSourceRequest struct {
	MapVersionID string `json:"mapVersionId,omitempty"`
	Type         string `json:"type"`
	MapSlug      string `json:"mapSlug"`
	TopicID      string `json:"topicId"`
}

// handleListProjects returns the indexed project tree.
func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if err := s.cache.Rebuild(); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "rebuild: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"projects": s.cache.All()})
}

// handleCreateProject creates a flat, independent project.
func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		httpx.Error(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.SystemPrompt) > 32*1024 {
		httpx.Error(w, http.StatusBadRequest, "conversation instructions exceed 32768 UTF-8 bytes")
		return
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = projects.Slugify(title)
	}
	if !projects.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug: "+slug)
		return
	}
	projectType := projects.NormalizeProjectType(req.ProjectType)
	if !projects.ValidateProjectType(projectType) {
		httpx.Error(w, http.StatusBadRequest, "invalid_project_type")
		return
	}
	var scope *projects.Scope
	if req.ScopeSource != nil {
		if projectType != projects.ProjectTypeSystemLearning {
			httpx.Error(w, http.StatusBadRequest, "scope_source_requires_system_learning")
			return
		}
		resolved, err := resolveMapLearningScope(*req.ScopeSource)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		scope = &resolved
	}
	state, err := s.projectOperations().Create(slug, title, projects.ProjectInput{
		SystemPrompt:  req.SystemPrompt,
		ProjectType:   projectType,
		Why:           req.Why,
		Current:       req.Current,
		Target:        req.Target,
		Standard:      req.Standard,
		LearningScope: scope,
	})
	if err != nil {
		if projects.IsSlugConflict(err) {
			httpx.Error(w, http.StatusConflict, "project already exists: "+slug)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.cache.Invalidate(slug)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"project": state})
}

func resolveMapLearningScope(source learningScopeSourceRequest) (projects.Scope, error) {
	source.Type = strings.TrimSpace(source.Type)
	source.MapSlug = strings.TrimSpace(source.MapSlug)
	source.TopicID = strings.TrimSpace(source.TopicID)
	if source.Type != projects.SourceDisciplineMap ||
		!projects.ValidateSlug(source.MapSlug) || source.TopicID == "" {
		return projects.Scope{}, errors.New("invalid_scope_source")
	}
	state, err := projects.ReadProjectState(source.MapSlug)
	if err != nil || state.ProjectType != projects.ProjectTypeDisciplineMap {
		return projects.Scope{}, errors.New("invalid_scope_source")
	}
	projection, err := generation.ReadMap(source.MapSlug, source.MapVersionID)
	if err != nil {
		return projects.Scope{}, errors.New("discipline_topics_unavailable")
	}
	topic, ok := projects.FindTopic(projection.Topics, source.TopicID)
	if !ok {
		return projects.Scope{}, errors.New("discipline_topic_not_found")
	}
	scope := projects.SnapshotFromTopic(source.MapSlug, topic, time.Now())
	scope.Source.MapVersionID = projection.MapVersionID
	return scope, nil
}

// handleGetProject returns one project's full state.
func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("id")
	if !projects.ValidateSlug(slug) {
		httpx.Error(w, http.StatusBadRequest, "invalid slug")
		return
	}
	state, err := projects.ReadProjectState(slug)
	if err != nil {
		if errors.Is(err, errors.New("not found")) || strings.Contains(err.Error(), "not exist") {
			httpx.Error(w, http.StatusNotFound, "project not found")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"project": struct {
			*projects.ProjectState
		}{
			ProjectState: state,
		},
	})
}

// ensure workspace is imported even if no direct usage

func (s *Server) projectOperations() *projectservice.Service {
	operations := &projectservice.Service{Teaching: s.teachingLifecycle(), Tools: s.learningTools(), Notes: s.notes}
	if s.generation != nil {
		operations.Generation = s.generation
	}
	return operations
}
