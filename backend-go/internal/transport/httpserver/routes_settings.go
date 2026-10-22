package httpserver

import (
	"net/http"

	"github.com/xmz14/lll/backend-go/internal/httpx"

	"github.com/xmz14/lll/backend-go/internal/uiconfig"
)

func (s *Server) handleGetAppearance(w http.ResponseWriter, r *http.Request) {
	cfg, err := uiconfig.Load()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "load appearance: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cfg)
}

func (s *Server) handlePutAppearance(w http.ResponseWriter, r *http.Request) {
	var cfg uiconfig.Config
	if err := httpx.ReadJSON(r, &cfg); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if !uiconfig.ValidTheme(cfg.Theme) {
		httpx.Error(w, http.StatusBadRequest, "unknown theme")
		return
	}
	if err := uiconfig.Save(cfg); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "save appearance: "+err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cfg)
}
