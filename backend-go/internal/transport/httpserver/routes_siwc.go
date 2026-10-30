package httpserver

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/platform/siwc"
)

func siwcMutationAllowed(w http.ResponseWriter, r *http.Request) bool {
	if !allowSecretSettings(w, r) {
		return false
	}
	if r.Header.Get("Origin") != "http://"+r.Host && r.Header.Get("Origin") != "https://"+r.Host {
		httpx.Error(w, 403, "same origin required")
		return false
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		httpx.Error(w, 415, "JSON request required")
		return false
	}
	return true
}
func (s *Server) handleSIWCStatus(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	info, err := siwc.Status()
	if err != nil {
		httpx.Error(w, 500, "ChatGPT connection unavailable")
		return
	}
	httpx.WriteJSON(w, 200, info)
}
func (s *Server) handleSIWCStart(w http.ResponseWriter, r *http.Request) {
	if !siwcMutationAllowed(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	url, err := siwc.Start()
	if err != nil {
		httpx.Error(w, 409, err.Error())
		return
	}
	httpx.WriteJSON(w, 200, map[string]string{"authorizationUrl": url})
}
func (s *Server) handleSIWCDisconnect(w http.ResponseWriter, r *http.Request) {
	if !siwcMutationAllowed(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := siwc.Disconnect(ctx); err != nil {
		httpx.Error(w, 500, "could not disconnect ChatGPT")
		return
	}
	httpx.WriteJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleSIWCModels(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	models, err := siwc.CurrentModelCatalog(ctx)
	if err != nil {
		httpx.Error(w, 502, err.Error())
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"models": models})
}
