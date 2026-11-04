package httpserver

import (
	"github.com/xmz14/lll/backend-go/internal/httpx"
	ai "github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/modules/learningprofile"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"net/http"
)

func (s *Server) learningProfile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	st, e := learningprofile.Read()
	if e != nil {
		learningWorkspaceError(w, e)
		return
	}
	if r.Method == "PUT" {
		var cfg learningprofile.Settings
		if e = httpx.ReadJSON(r, &cfg); e != nil {
			httpx.Error(w, 400, "配置格式无效")
			return
		}
		if cfg.Search.APIKey == "" {
			cfg.Search.APIKey = st.Settings.Search.APIKey
		}
		if cfg.Search.Enabled && (!st.Settings.Search.Enabled || cfg.Search != st.Settings.Search) {
			search := teacher.SearchFromSettings(cfg.Search)
			if search == nil {
				cfg.Search.Enabled = false
				_ = learningprofile.SaveSettings(cfg)
				httpx.Error(w, 400, "请完成搜索配置后再开启")
				return
			}
			if _, e = search.Search(r.Context(), "OpenAI official website"); e != nil {
				cfg.Search.Enabled = false
				_ = learningprofile.SaveSettings(cfg)
				httpx.Error(w, 400, "搜索验证失败，已关闭联网搜索："+e.Error())
				return
			}
		}
		if e = learningprofile.SaveSettings(cfg); e != nil {
			httpx.Error(w, 400, e.Error())
			return
		}
		st, e = learningprofile.Read()
		if e != nil {
			learningWorkspaceError(w, e)
			return
		}
	}
	st.Settings.Search.HasKey = st.Settings.Search.APIKey != ""
	st.Settings.Search.APIKey = ""
	capabilities := map[string][]string{}
	if cfg, e := ai.LoadConfig(); e == nil && cfg != nil {
		for _, p := range cfg.Providers {
			capabilities[p.ID] = ai.ReasoningCapabilities(p)
		}
	}
	httpx.WriteJSON(w, 200, map[string]any{"settings": st.Settings, "capabilities": capabilities})
}
func (s *Server) learningMemories(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var e error
	if r.Method == "DELETE" {
		e = learningprofile.DeleteMemory(r.PathValue("memoryId"))
	}
	if r.Method == "PUT" {
		var in struct {
			Text string `json:"text"`
		}
		e = httpx.ReadJSON(r, &in)
		if e == nil {
			e = learningprofile.EditMemory(r.PathValue("memoryId"), in.Text)
		}
	}
	if e != nil {
		httpx.Error(w, 400, e.Error())
		return
	}
	st, e := learningprofile.Read()
	if e != nil {
		learningWorkspaceError(w, e)
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"memories": st.Memories, "lastError": st.LastError})
}
