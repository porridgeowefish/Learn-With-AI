package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/cloud/auth"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
)

// EnableGitHub registers the OAuth endpoints during server assembly.
func (s *Server) EnableGitHub(g *auth.GitHub) {
	s.mux.HandleFunc("POST /v1/auth/github/start", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Challenge string
			Link      bool
		}
		if !decode(w, r, &in, 4096) || !s.allowAuth(w, r, "github-start", "", 20) {
			return
		}
		var owner, ownerToken string
		if in.Link {
			account, ok := s.authenticated(w, r)
			if !ok {
				return
			}
			owner = account.ID
			ownerToken = bearer(r)
		}
		result, err := g.Start(r.Context(), in.Challenge, owner, ownerToken)
		if err != nil {
			authError(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/auth/github/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		q := r.URL.Query()
		if err := g.Callback(r.Context(), q.Get("state"), q.Get("code")); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "GitHub 授权未完成。请回到 Learn-With-AI 查看详情。")
			return
		}
		_, _ = io.WriteString(w, "GitHub 授权已完成。请回到 Learn-With-AI。")
	})
	s.mux.HandleFunc("POST /v1/auth/github/finish", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ FlowID, Verifier string }
		if !decode(w, r, &in, 4096) {
			return
		}
		session, err := g.Finish(r.Context(), in.FlowID, in.Verifier, bearer(r))
		switch {
		case errors.Is(err, store.ErrWaiting):
			w.WriteHeader(http.StatusAccepted)
		case errors.Is(err, store.ErrConflict):
			fail(w, http.StatusConflict, "github_link_required")
		case errors.Is(err, store.ErrCode):
			fail(w, http.StatusBadRequest, "github_verified_email_required")
		case errors.Is(err, store.ErrGitHub):
			fail(w, http.StatusUnauthorized, "github_authorization_failed")
		case errors.Is(err, store.ErrGitHubSession):
			fail(w, http.StatusUnauthorized, "login_required")
		default:
			writeSession(w, session, err)
		}
	})
}
