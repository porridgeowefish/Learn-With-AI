// Package httpserver adapts the current learning application's HTTP protocols.
package httpserver

import (
	"github.com/xmz14/lll/backend-go/internal/httpx"

	"github.com/xmz14/lll/backend-go/internal/modules/generation"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/sources"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	shutdown        func()
	teacher         *teacher.Service
	generation      *generation.Service
	lifecycle       *teacher.Lifecycle
	tools           *teacher.LearningTools
	notes           *teacher.NoteService
	sourceProcessor *sources.Processor
	broadcaster     *httpx.Broadcaster
	cache           *projects.Index

	chatMediaMu sync.Map
	mu          sync.RWMutex
}
type Dependencies struct {
	Lifecycle       *teacher.Lifecycle
	Tools           *teacher.LearningTools
	Teacher         *teacher.Service
	Generation      *generation.Service
	SourceProcessor *sources.Processor
	Broadcaster     *httpx.Broadcaster
	Cache           *projects.Index
}

func New(deps Dependencies) *Server {
	if deps.Broadcaster == nil {
		deps.Broadcaster = httpx.NewBroadcaster()
	}
	if deps.Cache == nil {
		deps.Cache = projects.NewIndex()
	}
	return &Server{lifecycle: deps.Lifecycle, tools: deps.Tools, teacher: deps.Teacher, generation: deps.Generation, sourceProcessor: deps.SourceProcessor, broadcaster: deps.Broadcaster, cache: deps.Cache}
}
func (s *Server) Broadcaster() *httpx.Broadcaster { return s.broadcaster }
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/settings/teacher", s.handleGetTeacherSettings)
	mux.HandleFunc("PUT /api/settings/teacher", s.handlePutTeacherSettings)
	mux.HandleFunc("GET /api/projects/{id}/notes", s.handleGetNotes)
	mux.HandleFunc("POST /api/projects/{id}/notes/jobs/{jobId}/retry", s.handleRetryNote)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("POST /api/system/shutdown", s.handleShutdown)
	mux.HandleFunc("GET /api/settings/ask-ai", s.handleGetAskAiSettings)
	mux.HandleFunc("PUT /api/settings/ask-ai", s.handlePutAskAiSettings)
	mux.HandleFunc("POST /api/settings/ask-ai/probe", s.handleProbeAskAi)
	mux.HandleFunc("POST /api/model-connections/models", s.handleModelCatalog)
	mux.HandleFunc("GET /api/settings/ai-services", s.handleGetAIServices)
	mux.HandleFunc("PUT /api/settings/ai-services", s.handlePutAIServices)
	mux.HandleFunc("POST /api/settings/ai-services/probe", s.handleProbeAIServices)
	mux.HandleFunc("GET /api/settings/appearance", s.handleGetAppearance)
	mux.HandleFunc("PUT /api/settings/appearance", s.handlePutAppearance)
	mux.HandleFunc("GET /api/activity", s.handleGetActivity)
	mux.HandleFunc("GET /api/usage/projects", s.handleListProjectUsage)
	mux.HandleFunc("GET /api/usage/teacher", s.handleListTeacherUsage)
	mux.HandleFunc("GET /api/projects", s.handleListProjects)
	mux.HandleFunc("POST /api/projects", s.handleCreateProject)
	mux.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	mux.HandleFunc("POST /api/project-type-advice", s.handleProjectTypeAdvice)
	mux.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	mux.HandleFunc("GET /api/projects/{id}/discipline-map", s.handleDisciplineMap)
	mux.HandleFunc("POST /api/projects/{id}/generation-jobs", s.handleCreateGeneration)
	mux.HandleFunc("GET /api/projects/{id}/generation-jobs", s.handleListGeneration)
	mux.HandleFunc("GET /api/projects/{id}/generation-jobs/{jobId}", s.handleGetGeneration)
	mux.HandleFunc("GET /api/projects/{id}/generation-jobs/{jobId}/candidate", s.handleGenerationCandidate)
	mux.HandleFunc("POST /api/projects/{id}/generation-jobs/{jobId}/cancel", s.handleCancelGeneration)
	mux.HandleFunc("POST /api/projects/{id}/generation-jobs/{jobId}/retry", s.handleRetryGeneration)
	mux.HandleFunc("GET /api/projects/{id}/learning-plan", s.handleGetDisciplineLearningPlan)
	mux.HandleFunc("PUT /api/projects/{id}/learning-plan", s.handlePutDisciplineLearningPlan)
	mux.HandleFunc("POST /api/projects/{id}/activity", s.handlePostActivity)
	mux.HandleFunc("GET /api/projects/{id}/conversation", s.handleGetConversation)
	mux.HandleFunc("GET /api/projects/{id}/conversation/instructions", s.handleConversationInstructions)
	mux.HandleFunc("GET /api/system/speech", s.handleSpeechCapability)
	mux.HandleFunc("POST /api/projects/{id}/conversation/attachments", s.handleChatAttachment)
	mux.HandleFunc("GET /api/projects/{id}/conversation/attachments/{attachmentId}/file", s.handleChatAttachmentFile)
	mux.HandleFunc("PUT /api/projects/{id}/conversation/instructions", s.handleConversationInstructions)
	mux.HandleFunc("POST /api/projects/{id}/conversation/turns", s.handleTeacherTurn)
	mux.HandleFunc("POST /api/projects/{id}/conversation/opening", s.handleTeacherOpening)
	mux.HandleFunc("POST /api/projects/{id}/conversation/turns/queue", s.handleQueueTeacherTurn)
	mux.HandleFunc("GET /api/projects/{id}/conversation/responses/active", s.handleActiveTeacherResponse)
	mux.HandleFunc("POST /api/projects/{id}/conversation/responses/{responseId}/stop", s.handleStopTeacherResponse)
	mux.HandleFunc("POST /api/projects/{id}/conversation/responses/{responseId}/regenerate", s.handleRegenerateTeacherResponse)
	mux.HandleFunc("POST /api/projects/{id}/conversation/queue/{queueId}/edit", s.handleEditQueuedTurn)
	mux.HandleFunc("POST /api/projects/{id}/conversation/queue/{queueId}/discard", s.handleDiscardQueuedTurn)
	mux.HandleFunc("POST /api/projects/{id}/conversation/queue/{queueId}/steer", s.handleSteerQueuedTurn)
	mux.HandleFunc("GET /api/projects/{id}/conversation/export.md", s.handleExportConversation)
	mux.HandleFunc("GET /api/projects/{id}/assets", s.handleListLearningAssets)
	mux.HandleFunc("GET /api/projects/{id}/assets/{assetKey}", s.handleGetLearningAsset)
	mux.HandleFunc("PUT /api/projects/{id}/assets/{assetKey}", s.handlePutLearningAsset)
	mux.HandleFunc("GET /api/projects/{id}/assets/{assetKey}/versions", s.handleListLearningAssetVersions)
	mux.HandleFunc("GET /api/projects/{id}/feynman/sessions", s.handleListFeynmanSessions)
	mux.HandleFunc("POST /api/projects/{id}/feynman/sessions", s.handleCreateFeynmanSession)
	mux.HandleFunc("POST /api/projects/{id}/feynman/sessions/{sessionId}/stream", s.handleFeynmanStream)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}/revisions/{revisionId}/files/{fileKey}", s.handleReadSourceFile)
	mux.HandleFunc("GET /api/projects/{id}/assets/practice/rounds", s.handlePracticeRounds)
	mux.HandleFunc("GET /api/projects/{id}/assets/practice/results", s.handleListPracticeResults)
	mux.HandleFunc("PUT /api/projects/{id}/assets/practice/results/{versionId}", s.handlePutPracticeResult)
	mux.HandleFunc("DELETE /api/projects/{id}/assets/practice/results/{versionId}", s.handleDeletePracticeResult)
	mux.HandleFunc("GET /api/projects/{id}/sources", s.handleListSources)
	mux.HandleFunc("POST /api/projects/{id}/sources", s.handleUploadSource)
	mux.HandleFunc("POST /api/projects/{id}/sources/{sourceId}/retry", s.handleRetrySource)
	mux.HandleFunc("POST /api/projects/{id}/sources/{sourceId}/structure-jobs", s.handleCreateStructure)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}/structure-jobs", s.handleListStructure)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}/structure-jobs/{jobId}", s.handleGetStructure)
	mux.HandleFunc("POST /api/projects/{id}/sources/{sourceId}/structure-jobs/{jobId}/cancel", s.handleCancelStructure)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}", s.handleGetSource)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}/evidence/{evidenceId}", s.handleSourceEvidence)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}/retrieval", s.handleSourceRetrievalStatus)
	mux.HandleFunc("DELETE /api/projects/{id}/sources/{sourceId}", s.handleDeleteSource)
	mux.HandleFunc("POST /api/projects/{id}/sources/{sourceId}/permanent-delete", s.handlePermanentDeleteSource)
	mux.HandleFunc("GET /api/projects/{id}/sources/{sourceId}/revisions/{revisionId}/content", s.handleReadSourceContent)
	mux.HandleFunc("GET /api/preferences", s.handleGetPreferences)
	mux.HandleFunc("PUT /api/preferences", s.handlePutPreferences)
	mux.HandleFunc("GET /api/projects/{id}/assets/review/annotations", s.handleListAnnotations)
	mux.HandleFunc("POST /api/projects/{id}/assets/review/annotations", s.handleCreateAnnotation)
	mux.HandleFunc("PATCH /api/projects/{id}/assets/review/annotations/{annotationId}", s.handleUpdateAnnotation)
	mux.HandleFunc("DELETE /api/projects/{id}/assets/review/annotations/{annotationId}", s.handleDeleteAnnotation)
	mux.HandleFunc("POST /api/projects/{id}/assets/review/annotations/{annotationId}/ask-stream", s.handleAnnotationAskAiStream)
	mux.HandleFunc("POST /api/projects/{id}/assets/review/annotations/{annotationId}/ask/summarize", s.handleAnnotationAskAiSummarize)
	mux.HandleFunc("GET /api/folders", s.handleGetFolders)
	mux.HandleFunc("PUT /api/folders", s.handlePutFolders)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, 404, map[string]any{"error": "API route not found"})
	})
	mux.HandleFunc("/files/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	mux.HandleFunc("GET /api/events", s.broadcaster.SSEHandler(map[string]any{}))
	mux.Handle("/", spaHandler(paths.FRONTEND_ROOT, paths.FRONTEND_INDEX))
	return logging(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		if strings.HasPrefix(r.URL.Path, "/api/projects/") && pattern != "/api/" && pattern != "/" {
			parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/projects/"), "/")
			if len(parts) > 0 && parts[0] != "" {
				if !projects.ValidateSlug(parts[0]) {
					httpx.Error(w, 400, "invalid project")
					return
				}
				if _, err := projects.ReadProjectState(parts[0]); err != nil {
					learningWorkspaceError(w, err)
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	}))
}
func (s *Server) SetShutdownFunc(fn func())              { s.mu.Lock(); s.shutdown = fn; s.mu.Unlock() }
func (s *Server) AttachNotes(notes *teacher.NoteService) { s.notes = notes }
func (s *Server) Close() {
	s.teachingLifecycle().Close()
	s.learningTools().Close()
	if s.generation != nil {
		s.generation.Close()
	}
	if s.sourceProcessor != nil {
		s.sourceProcessor.Close()
	}
	if s.notes != nil {
		_ = s.notes.Close()
	}
}
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemoteAddr(r.RemoteAddr) {
		httpx.Error(w, 403, "shutdown is only allowed from localhost")
		return
	}
	s.mu.RLock()
	shutdown := s.shutdown
	s.mu.RUnlock()
	if shutdown == nil {
		httpx.Error(w, 503, "shutdown handler is not configured")
		return
	}
	httpx.WriteJSON(w, 202, map[string]any{"shuttingDown": true})
	go shutdown()
}
func isLoopbackRemoteAddr(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func spaHandler(root, index string) http.Handler {
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(root, filepath.FromSlash(r.URL.Path))
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		if strings.Contains(filepath.Base(r.URL.Path), ".") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	count := 0
	if s.cache != nil {
		if err := s.cache.Rebuild(); err != nil {
			println("health: project index:", err.Error())
		}
		count = len(s.cache.All())
	}
	httpx.WriteJSON(w, 200, map[string]any{"ok": true, "workspace": paths.WORKSPACE, "stats": map[string]any{"projects": count}})
}
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			println(r.Method, r.URL.Path, time.Since(start).String())
		}
	})
}
