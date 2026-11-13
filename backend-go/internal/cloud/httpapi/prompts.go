package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

func promptExperimentBase(ctx context.Context, db *store.Store, draft promptcatalog.Draft) (map[string]string, error) {
	if draft.Comparison == nil {
		return draft.Resources, nil
	}
	initial, err := db.InitialExperimentConfig(ctx, draft.Comparison.ID)
	if errors.Is(err, store.ErrNotFound) {
		return draft.Resources, nil
	}
	if err != nil {
		return nil, err
	}
	var old promptcatalog.Draft
	if err := json.Unmarshal(initial.Config, &old); err != nil {
		return nil, err
	}
	if old.Resources == nil {
		return nil, errors.New("experiment template baseline missing")
	}
	if !promptcatalog.HasReviewHTMLContract(old.Resources["review"]) {
		old.Resources["review"] = draft.Resources["review"]
	}
	return old.Resources, nil
}

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
	s.mux.HandleFunc("GET /v1/admin/prompts/history", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, true); !ok {
			return
		}
		offset, ok := promptHistoryOffset(w, r)
		if !ok {
			return
		}
		entries, err := s.db.PromptHistory(r.Context(), offset)
		if err != nil {
			fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
			return
		}
		respond(w, http.StatusOK, entries)
	})
	s.mux.HandleFunc("GET /v1/admin/prompts/history/{version}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, true); !ok {
			return
		}
		version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
		if err != nil || version <= 0 {
			fail(w, http.StatusBadRequest, "invalid_prompt_configuration")
			return
		}
		entry, err := s.db.PromptConfig(r.Context(), version)
		if err != nil {
			promptVersionError(w, err)
			return
		}
		respond(w, http.StatusOK, entry)
	})
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
		if in.RollbackTo < 0 || (in.RollbackTo > 0) == (in.Config != nil) {
			fail(w, http.StatusBadRequest, "invalid_prompt_configuration")
			return
		}
		s.promptPublish.Lock()
		defer s.promptPublish.Unlock()
		active, err := s.db.ActivePromptConfig(r.Context())
		if err != nil {
			promptVersionError(w, err)
			return
		}
		if active.Version != in.BaseVersion {
			fail(w, http.StatusConflict, "prompt_configuration_changed")
			return
		}
		var current promptcatalog.Draft
		if json.Unmarshal(active.Config, &current) != nil {
			fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
			return
		}
		if in.RollbackTo > 0 {
			old, err := s.db.PromptConfig(r.Context(), in.RollbackTo)
			if err != nil {
				promptVersionError(w, err)
				return
			}
			if json.Unmarshal(old.Config, &in.Config) != nil || in.Config == nil || in.Config.Resources == nil {
				fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
				return
			}
			in.Config.Comparison = current.Comparison
			if !promptcatalog.HasReviewHTMLContract(in.Config.Resources["review"]) {
				in.Config.Resources["review"] = current.Resources["review"]
			}
		}
		if current.Resources["review"] != in.Config.Resources["review"] && !promptcatalog.HasReviewHTMLContract(in.Config.Resources["review"]) {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_prompt_configuration", "detail": "review: retain <article> and </article> in the HTML output instructions"})
			return
		}
		base, err := promptExperimentBase(r.Context(), s.db, *in.Config)
		if err != nil {
			promptVersionError(w, err)
			return
		}
		catalog, err := promptcatalog.BuildWithExperimentBase(*in.Config, base, key)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_prompt_configuration", "detail": err.Error()})
			return
		}
		s.modelMu.RLock()
		defer s.modelMu.RUnlock()
		if candidate := catalog.Comparison; candidate != nil && candidate.Percent > 0 {
			for _, model := range candidate.ModelIDs() {
				if _, exists := s.models[model]; !exists {
					respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_prompt_configuration", "detail": "comparison models must be active"})
					return
				}
			}
		}
		body, err := json.Marshal(in.Config)
		if err != nil {
			fail(w, http.StatusBadRequest, "invalid_prompt_configuration")
			return
		}
		publishedAt := time.Now()
		version, err := s.db.PublishPromptConfig(r.Context(), in.BaseVersion, admin.ID, body, publishedAt)
		if errors.Is(err, store.ErrComparisonExperimentChanged) {
			respond(w, http.StatusConflict, map[string]string{"error": "comparison_experiment_immutable", "detail": "Create a new experiment to change the models or prompt variants."})
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

func promptVersionError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		fail(w, http.StatusNotFound, "prompt_version_not_found")
		return
	}
	fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
}

func promptHistoryOffset(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("offset")
	if raw == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 || offset > 1_000_000 {
		fail(w, http.StatusBadRequest, "invalid_prompt_history_offset")
		return 0, false
	}
	return offset, true
}
