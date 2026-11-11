package httpserver

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

type googleLoginFlow struct {
	flow    llmcloud.GoogleFlow
	expires time.Time
	code    string
	denied  bool
}

func (s *Server) handleGoogleConfig(w http.ResponseWriter, r *http.Request) {
	client := cloudClient(w, r)
	if client != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"available": client.GoogleAvailable(r.Context())})
	}
}

func (s *Server) startGoogleLogin(w http.ResponseWriter, r *http.Request, client *llmcloud.Client) {
	_, port, err := net.SplitHostPort(r.Host)
	portNumber, portErr := strconv.ParseUint(port, 10, 16)
	if err != nil || portErr != nil || portNumber == 0 {
		httpx.Error(w, http.StatusBadRequest, "google_local_callback_unavailable")
		return
	}
	flow, err := client.StartGoogle(r.Context(), "http://127.0.0.1:"+port+"/api/cloud/google/callback")
	if err != nil {
		cloudError(w, err)
		return
	}
	now := time.Now()
	s.googleLoginFlows.Range(func(key, value any) bool {
		if !now.Before(value.(googleLoginFlow).expires) {
			s.googleLoginFlows.Delete(key)
		}
		return true
	})
	s.googleLoginFlows.Store(flow.State, googleLoginFlow{flow: flow, expires: now.Add(10 * time.Minute)})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"flowId": flow.State, "authorizationUrl": flow.AuthorizationURL})
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if !allowSecretSettings(w, r) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	state, code := r.URL.Query().Get("state"), r.URL.Query().Get("code")
	raw, ok := s.googleLoginFlows.Load(state)
	if ok {
		flow := raw.(googleLoginFlow)
		if time.Now().Before(flow.expires) && flow.code == "" && !flow.denied {
			flow.denied = code == "" || len(code) > 4096 || r.URL.Query().Has("error")
			if !flow.denied {
				flow.code = code
			}
			if s.googleLoginFlows.CompareAndSwap(state, raw, flow) && !flow.denied {
				_, _ = io.WriteString(w, "Google 授权已完成。请回到 Learn-With-AI。")
				return
			}
		}
	}
	w.WriteHeader(http.StatusBadRequest)
	_, _ = io.WriteString(w, "Google 授权未完成。请回到 Learn-With-AI。")
}

func (s *Server) finishGoogleLogin(w http.ResponseWriter, r *http.Request, client *llmcloud.Client, id string) {
	raw, exists := s.googleLoginFlows.Load(id)
	if !exists || !time.Now().Before(raw.(googleLoginFlow).expires) {
		httpx.Error(w, http.StatusBadRequest, "google_flow_expired")
		return
	}
	flow := raw.(googleLoginFlow)
	if flow.denied {
		s.googleLoginFlows.CompareAndDelete(id, raw)
		httpx.Error(w, http.StatusUnauthorized, "google_authorization_failed")
		return
	}
	if flow.code == "" {
		httpx.WriteJSON(w, http.StatusAccepted, map[string]bool{"pending": true})
		return
	}
	if !s.googleLoginFlows.CompareAndDelete(id, raw) {
		httpx.Error(w, http.StatusBadRequest, "google_flow_expired")
		return
	}
	account, err := client.FinishGoogle(r.Context(), flow.flow, flow.code)
	if err != nil {
		cloudError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, account)
}
