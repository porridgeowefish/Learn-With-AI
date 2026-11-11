package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"reflect"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

type promptState struct {
	active  *promptcatalog.Catalog
	index   promptcatalog.ExposureIndex
	retired []retiredExposure
}

type retiredExposure struct {
	index   promptcatalog.ExposureIndex
	expires time.Time
}

// Publication times are stored at second precision. Keep the whole last
// second so a cache filled just before publication can still be attributed.
const promptExposureWindow = promptpack.CacheTTL + time.Second

// EnablePrompts registers the signed prompt endpoint before serving requests.
func (s *Server) EnablePrompts(catalog *promptcatalog.Catalog) {
	s.prompts.Store(&promptState{active: catalog, index: catalog.ExposureIndex()})
	s.mux.HandleFunc("GET /v1/prompts", func(w http.ResponseWriter, r *http.Request) {
		account, ok := s.authenticated(w, r)
		if !ok {
			return
		}
		s.promptPublish.Lock()
		active := s.prompts.Load().active
		s.promptPublish.Unlock()
		pack, found := active.ForAccount(r.URL.Query().Get("scene"), account.ID)
		if !found {
			fail(w, http.StatusNotFound, "prompt_scene_unavailable")
			return
		}
		w.Header().Set("ETag", pack.ETag())
		if r.Header.Get("If-None-Match") == pack.ETag() {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		respond(w, http.StatusOK, pack)
	})
}

// RestorePromptExposureHistory loads durable attribution before serving traffic.
func (s *Server) RestorePromptExposureHistory(ctx context.Context, key ed25519.PrivateKey, now time.Time) error {
	current := s.prompts.Load()
	if current == nil {
		return errors.New("prompt catalog is not configured")
	}
	configs, err := s.db.RetiredPromptConfigs(ctx, now.Add(-promptExposureWindow))
	if err != nil {
		return err
	}
	retired := make([]retiredExposure, 0, len(configs))
	for _, config := range configs {
		expires := config.RetiredAt.Add(promptExposureWindow)
		if !now.Before(expires) {
			continue
		}
		var draft promptcatalog.Draft
		if err := json.Unmarshal(config.Config, &draft); err != nil {
			return err
		}
		if len(draft.Experiments) == 0 {
			continue
		}
		catalog, err := promptcatalog.Build(draft, key)
		if err != nil {
			return err
		}
		retired = append(retired, retiredExposure{catalog.ExposureIndex(), expires})
	}
	s.prompts.Store(&promptState{active: current.active, index: current.index, retired: retired})
	return nil
}

func (s *Server) replacePromptCatalog(catalog *promptcatalog.Catalog, now time.Time) {
	next := &promptState{active: catalog, index: catalog.ExposureIndex()}
	if current := s.prompts.Load(); current != nil {
		if len(current.index) > 0 {
			next.retired = append(next.retired, retiredExposure{current.index, now.Truncate(time.Second).Add(promptExposureWindow)})
		}
		for _, old := range current.retired {
			if now.Before(old.expires) {
				next.retired = append(next.retired, old)
			}
		}
	}
	s.prompts.Store(next)
}

func (s *Server) promptExposure(account string, ref promptpack.Reference, now time.Time) (promptcatalog.Exposure, bool) {
	state := s.prompts.Load()
	if state == nil {
		return promptcatalog.Exposure{}, false
	}
	if exposure, matches := state.index.Match(account, ref); matches {
		return exposure, true
	}
	for _, old := range state.retired {
		if now.Before(old.expires) {
			if exposure, matches := old.index.Match(account, ref); matches {
				return exposure, true
			}
		}
	}
	return promptcatalog.Exposure{}, false
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
			if len(in.Config.Experiments) > 0 {
				ids := make([]string, 0, len(in.Config.Experiments))
				for _, test := range in.Config.Experiments {
					ids = append(ids, test.ID)
				}
				history, err := s.db.PromptConfigsForExperiments(r.Context(), ids)
				if err != nil {
					fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
					return
				}
				for _, saved := range history {
					var previous promptcatalog.Draft
					if err := json.Unmarshal(saved, &previous); err != nil {
						fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
						return
					}
					if id := changedExperimentID(previous, *in.Config); id != "" {
						respond(w, http.StatusBadRequest, map[string]string{"error": "prompt_experiment_id_reused", "experiment": id})
						return
					}
				}
			}
		}
		publishedAt := time.Now()
		version, err := s.db.PublishPromptConfig(r.Context(), in.BaseVersion, admin.ID, body, publishedAt)
		if errors.Is(err, store.ErrConflict) {
			fail(w, http.StatusConflict, "prompt_configuration_changed")
			return
		}
		if err != nil {
			fail(w, http.StatusInternalServerError, "prompt_configuration_unavailable")
			return
		}
		s.replacePromptCatalog(catalog, publishedAt)
		respond(w, http.StatusOK, map[string]int64{"version": version})
	})
}

func changedExperimentID(previous, next promptcatalog.Draft) string {
	for _, old := range previous.Experiments {
		for _, current := range next.Experiments {
			if old.ID == current.ID && (!reflect.DeepEqual(old, current) || previous.Version != next.Version || !maps.Equal(previous.Resources, next.Resources)) {
				return old.ID
			}
		}
	}
	return ""
}
