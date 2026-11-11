package httpapi

import (
	"errors"
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/cloud/auth"
)

func (s *Server) EnableGoogle(g *auth.Google) {
	s.mux.HandleFunc("GET /v1/auth/google/config", func(w http.ResponseWriter, _ *http.Request) {
		respond(w, http.StatusOK, map[string]string{"clientId": g.ClientID()})
	})
	s.mux.HandleFunc("POST /v1/auth/google/finish", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Code, Verifier, RedirectURI string }
		if !decode(w, r, &in, 8192) || !s.allowAuth(w, r, "google-finish", "", 20) {
			return
		}
		session, err := g.Finish(r.Context(), in.Code, in.Verifier, in.RedirectURI)
		switch {
		case errors.Is(err, auth.ErrGoogleEmail):
			fail(w, http.StatusBadRequest, "google_verified_email_required")
		case errors.Is(err, auth.ErrGoogle):
			fail(w, http.StatusUnauthorized, "google_authorization_failed")
		default:
			writeSession(w, session, err)
		}
	})
}
