package httpserver

import (
	"net/http"
	"strings"

	"github.com/xmz14/lll/backend-go/internal/app/workspaceservice"
	paths "github.com/xmz14/lll/backend-go/internal/platform/filesystem"
	"github.com/xmz14/lll/backend-go/internal/platform/workspacegate"
)

func withStarterLanding(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" && r.URL.RawQuery == "" &&
			isLoopbackRemoteAddr(r.RemoteAddr) && strings.Contains(r.Header.Get("Accept"), "text/html") {
			release, err := workspacegate.Enter()
			if err == nil {
				target, err := workspaceservice.TakeStarterLanding(paths.WORKSPACE)
				release()
				if err != nil {
					println("starter landing:", err.Error())
				} else if target != "" {
					w.Header().Set("Cache-Control", "no-store")
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
