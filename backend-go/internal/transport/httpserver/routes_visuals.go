package httpserver

import (
	"encoding/json"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"net/http"
)

func (s *Server) handleLearningVisualMetadata(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if len(q) != 2 || len(q["provider"]) != 1 || len(q["asset"]) != 1 {
		http.Error(w, "invalid figure identifier", http.StatusBadRequest)
		return
	}
	figure, err := teacher.VisualMetadata(r.Context(), q.Get("provider"), q.Get("asset"))
	if err != nil {
		http.Error(w, "无法核实图示来源与许可", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(figure)
}

func (s *Server) handleLearningVisual(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if len(q) != 2 || len(q["provider"]) != 1 || len(q["asset"]) != 1 {
		http.Error(w, "invalid figure identifier", http.StatusBadRequest)
		return
	}
	data, mime, err := teacher.VisualImage(r.Context(), q.Get("provider"), q.Get("asset"))
	if err != nil {
		http.Error(w, "图示暂时无法加载，请打开来源查看。", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Disposition", `inline; filename="teaching-figure"`)
	_, _ = w.Write(data)
}
