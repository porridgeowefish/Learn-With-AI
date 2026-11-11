package httpserver

import (
	"errors"
	"net/http"
	"time"

	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
)

type cloudFlow struct {
	flow    llmcloud.GitHubFlow
	expires time.Time
}

func cloudClient(w http.ResponseWriter, r *http.Request) *llmcloud.Client {
	if !allowSecretSettings(w, r) {
		return nil
	}
	w.Header().Set("Cache-Control", "no-store")
	client, err := llmcloud.Default()
	if err != nil {
		httpx.Error(w, http.StatusServiceUnavailable, "cloud_unavailable")
		return nil
	}
	return client
}

func cloudError(w http.ResponseWriter, err error) {
	var remote *llmcloud.RemoteError
	switch {
	case errors.As(err, &remote) && remote.Status >= 400 && remote.Status <= 599:
		httpx.Error(w, remote.Status, remote.Code)
	case errors.Is(err, llmcloud.ErrLoginRequired):
		httpx.Error(w, http.StatusUnauthorized, "login_required")
	default:
		httpx.Error(w, http.StatusBadGateway, "cloud_unavailable")
	}
}

func (s *Server) handleCloudAccount(w http.ResponseWriter, r *http.Request) {
	client := cloudClient(w, r)
	if client == nil {
		return
	}
	if _, active := client.Current(); !active {
		httpx.WriteJSON(w, http.StatusOK, map[string]bool{"signedIn": false})
		return
	}
	account, wallet, claimed, err := client.Wallet(r.Context())
	if err != nil {
		if errors.Is(err, llmcloud.ErrLoginRequired) {
			if _, active := client.Current(); !active {
				httpx.WriteJSON(w, http.StatusOK, map[string]bool{"signedIn": false})
				return
			}
		}
		cloudError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"signedIn": true, "account": account, "wallet": wallet, "selectedModel": client.Selected().ID, "dailyClaimed": claimed,
	})
}

func (s *Server) handleCloudModels(w http.ResponseWriter, r *http.Request) {
	client := cloudClient(w, r)
	if client == nil {
		return
	}
	models, err := client.Models(r.Context())
	if err != nil {
		cloudError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleCloudGuestModels(w http.ResponseWriter, r *http.Request) {
	client := cloudClient(w, r)
	if client == nil {
		return
	}
	models, err := client.GuestModels(r.Context())
	if err != nil {
		cloudError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"models": models})
}

func (s *Server) handleCloudDailyHistory(w http.ResponseWriter, r *http.Request) {
	client := cloudClient(w, r)
	if client == nil {
		return
	}
	history, err := client.DailyClaims(r.Context())
	if err != nil {
		cloudError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, history)
}

func (s *Server) handleCloudAction(w http.ResponseWriter, r *http.Request) {
	if !siwcMutationAllowed(w, r) {
		return
	}
	client := cloudClient(w, r)
	if client == nil {
		return
	}
	var in struct {
		Email, Purpose, Code, Username, Password, ConfirmPassword, Model, FlowID string
		Link                                                                     bool
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := httpx.ReadJSON(r, &in); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_account_input")
		return
	}
	var out any = map[string]bool{"ok": true}
	var err error
	switch r.PathValue("action") {
	case "send-code":
		err = client.SendEmail(r.Context(), in.Email, in.Purpose)
	case "verify-email":
		out, err = client.VerifyEmail(r.Context(), in.Email, in.Purpose, in.Code, in.Username, in.Password)
	case "send-binding-email":
		err = client.SendBindingEmail(r.Context(), in.Email)
	case "bind-email":
		out, err = client.BindEmail(r.Context(), in.Email, in.Code, in.Password)
	case "unbind-email":
		out, err = client.UnlinkIdentity(r.Context(), "email", in.Password)
	case "unbind-github":
		out, err = client.UnlinkIdentity(r.Context(), "github", in.Password)
	case "password":
		out, err = client.LoginPassword(r.Context(), in.Username, in.Password)
	case "register":
		out, err = client.RegisterPassword(r.Context(), in.Username, in.Password, in.ConfirmPassword)
	case "claim":
		out, err = client.ClaimDaily(r.Context())
	case "select-model":
		err = client.SelectModel(r.Context(), in.Model)
	case "logout":
		err = client.Logout(r.Context())
	case "google-start":
		s.startGoogleLogin(w, r, client)
		return
	case "google-finish":
		s.finishGoogleLogin(w, r, client, in.FlowID)
		return
	case "github-start":
		flow, startErr := client.StartGitHub(r.Context(), in.Link)
		err = startErr
		if err == nil {
			now := time.Now()
			s.cloudLoginFlows.Range(func(key, value any) bool {
				if !now.Before(value.(cloudFlow).expires) {
					s.cloudLoginFlows.Delete(key)
				}
				return true
			})
			s.cloudLoginFlows.Store(flow.ID, cloudFlow{flow: flow, expires: now.Add(10 * time.Minute)})
			out = map[string]string{"flowId": flow.ID, "authorizationUrl": flow.AuthorizationURL}
		}
	case "github-finish", "github-send-code":
		raw, exists := s.cloudLoginFlows.Load(in.FlowID)
		if !exists || !time.Now().Before(raw.(cloudFlow).expires) {
			httpx.Error(w, http.StatusBadRequest, "github_flow_expired")
			return
		}
		flow := raw.(cloudFlow).flow
		if r.PathValue("action") == "github-send-code" {
			err = client.SendGitHubConfirmation(r.Context(), flow)
			break
		}
		account, complete, finishErr := client.FinishGitHub(r.Context(), flow, in.Code)
		err = finishErr
		if err == nil && !complete {
			httpx.WriteJSON(w, http.StatusAccepted, map[string]bool{"pending": true})
			return
		}
		if complete || errors.Is(finishErr, llmcloud.ErrLoginRequired) {
			s.cloudLoginFlows.Delete(in.FlowID)
		}
		out = account
	default:
		httpx.Error(w, http.StatusNotFound, "unknown_cloud_action")
		return
	}
	if err != nil {
		cloudError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
