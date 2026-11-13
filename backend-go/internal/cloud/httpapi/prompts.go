package httpapi

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

type promptState struct{ active *promptcatalog.Catalog }

func (s *Server) EnablePrompts(catalog *promptcatalog.Catalog, identityKey []byte) {
	s.comparisonKey = append([]byte(nil), identityKey...)
	s.prompts.Store(&promptState{active: catalog})
	s.mux.HandleFunc("GET /v1/prompts", s.servePrompt)
	s.mux.HandleFunc("POST /v1/answer-comparison/start", s.startComparison)
	s.mux.HandleFunc("POST /v1/answer-comparison/stream", s.streamComparison)
	s.mux.HandleFunc("POST /v1/answer-comparison/vote", s.voteComparison)
}

func (s *Server) EnablePromptAdmin(key ed25519.PrivateKey) {
	s.mux.HandleFunc("GET /v1/admin/prompts", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, true); !ok {
			return
		}
		active, err := s.db.ActivePromptConfig(r.Context())
		if err != nil {
			fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
			return
		}
		respond(w, http.StatusOK, active)
	})
	s.mux.HandleFunc("POST /v1/admin/prompts/publish", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := s.adminAuthorize(w, r, true)
		if !ok {
			return
		}
		var in struct {
			BaseVersion, RollbackTo int64
			Config                  *promptcatalog.Draft
		}
		if !decode(w, r, &in, 1<<20) {
			return
		}
		if (in.RollbackTo > 0) == (in.Config != nil) {
			fail(w, http.StatusBadRequest, "invalid_prompt_configuration")
			return
		}
		if in.RollbackTo > 0 {
			old, err := s.db.PromptConfig(r.Context(), in.RollbackTo)
			if err != nil || json.Unmarshal(old.Config, &in.Config) != nil || in.Config == nil {
				fail(w, http.StatusNotFound, "prompt_configuration_unavailable")
				return
			}
		}
		catalog, err := promptcatalog.Build(*in.Config, key)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_prompt_configuration", "detail": err.Error()})
			return
		}
		if candidate := catalog.Comparison; candidate != nil && candidate.Percent > 0 {
			s.modelMu.RLock()
			_, exists := s.models[candidate.Model]
			s.modelMu.RUnlock()
			if !exists {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_prompt_configuration", "detail": "comparison model must be active"})
				return
			}
		}
		body, err := json.Marshal(in.Config)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_prompt_configuration")
			return
		}
		s.promptPublish.Lock()
		defer s.promptPublish.Unlock()
		if in.RollbackTo == 0 {
			active, err := s.db.ActivePromptConfig(r.Context())
			if err != nil {
				fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
				return
			}
			if active.Version != in.BaseVersion {
				fail(w, http.StatusConflict, "prompt_configuration_changed")
				return
			}
		}

		publishedAt := time.Now()
		version, err := s.db.PublishPromptConfig(r.Context(), in.BaseVersion, admin.ID, body, publishedAt)
		if errors.Is(err, store.ErrComparisonExperimentChanged) {
			respond(w, http.StatusConflict, map[string]string{"error": "comparison_experiment_immutable", "detail": "Create a new experiment to change the model or prompt variants."})
			return
		}
		if errors.Is(err, store.ErrConflict) {
			fail(w, http.StatusConflict, "prompt_configuration_changed")
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
			return
		}
		s.prompts.Store(&promptState{active: catalog})
		respond(w, http.StatusOK, map[string]int64{"version": version})
	})
}
