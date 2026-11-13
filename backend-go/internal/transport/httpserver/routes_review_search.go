package httpserver

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	workspace "github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
)

var resolveReviewSearch = teacher.ConfiguredYuanbaoSearch

func (s *Server) handleReviewSearch(w http.ResponseWriter, r *http.Request) {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host != r.Host {
		httpx.Error(w, http.StatusForbidden, "请从应用页面使用元宝搜索。")
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpx.Error(w, http.StatusUnsupportedMediaType, "搜索请求必须使用 JSON。")
		return
	}
	if !workspace.ValidateSlug(r.PathValue("id")) {
		http.NotFound(w, r)
		return
	}
	project, err := workspace.ReadProjectState(r.PathValue("id"))
	if err != nil || project.ProjectType != workspace.ProjectTypeSystemLearning {
		http.NotFound(w, r)
		return
	}
	var input struct {
		Query string `json:"query"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	err = httpx.ReadJSON(r, &input)
	query := strings.TrimSpace(input.Query)
	if err != nil || query == "" || utf8.RuneCountInString(query) > 256 {
		httpx.Error(w, http.StatusBadRequest, "请选择 1 至 256 个字符进行搜索。")
		return
	}
	searcher := resolveReviewSearch()
	if searcher == nil {
		httpx.Error(w, http.StatusConflict, "请在设置中启用元宝搜索并配置服务 API Key。")
		return
	}
	results, err := searcher.Search(r.Context(), query)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "元宝搜索暂时不可用。请检查搜索配置或稍后重试。")
		return
	}
	if len(results) > 5 {
		results = results[:5]
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": results})
}
