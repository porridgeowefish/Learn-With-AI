package httpserver

import (
	"errors"
	"github.com/xmz14/lll/backend-go/internal/httpx"
	"github.com/xmz14/lll/backend-go/internal/modules/assets"
	"github.com/xmz14/lll/backend-go/internal/modules/projects"
	"github.com/xmz14/lll/backend-go/internal/modules/teacher"
	"github.com/xmz14/lll/backend-go/internal/modules/treasures"
	"net/http"
	"net/url"
	"time"
)

func (s *Server) handleTreasures(w http.ResponseWriter, r *http.Request) {
	ledger, err := s.treasures.Snapshot()
	if err != nil {
		httpx.Error(w, 500, "收藏暂时无法读取，请重试；现有记录不会被重置。")
		return
	}
	httpx.WriteJSON(w, 200, map[string]any{"catalog": treasures.Catalog(), "rewards": ledger.Rewards})
}
func (s *Server) handleTreasureSeen(w http.ResponseWriter, r *http.Request) {
	err := s.treasures.Acknowledge(r.PathValue("rewardId"))
	if errors.Is(err, treasures.ErrRewardNotFound) {
		httpx.Error(w, 404, "reward not found")
		return
	}
	if err != nil {
		httpx.Error(w, 500, "无法保存通知状态")
		return
	}
	httpx.WriteJSON(w, 200, map[string]bool{"ok": true})
}
func (s *Server) handleTreasureSource(w http.ResponseWriter, r *http.Request) {
	ledger, err := s.treasures.Snapshot()
	if err != nil {
		httpx.Error(w, 500, "无法读取收藏")
		return
	}
	for _, reward := range ledger.Rewards {
		if reward.ID == r.PathValue("rewardId") {
			link, content := treasureSource(reward)
			httpx.WriteJSON(w, 200, map[string]any{"available": link != "", "url": link, "content": content})
			return
		}
	}
	httpx.Error(w, 404, "reward not found")
}
func treasureSource(reward treasures.Reward) (string, string) {
	project, err := projects.ReadProjectState(reward.ProjectSlug)
	if err != nil || project.ID != reward.ProjectID || project.CreatedAt.Format(time.RFC3339Nano) != reward.ProjectCreatedAt {
		return "", ""
	}
	base := "/project/" + url.PathEscape(reward.ProjectSlug)
	if reward.Kind == "teacher" {
		store, err := teacher.NewConversation(reward.ProjectSlug)
		if err != nil {
			return "", ""
		}
		messages, err := store.AllMessages()
		if err != nil {
			return "", ""
		}
		for _, m := range messages {
			if m.ID == reward.SourceID {
				content := ""
				for _, b := range m.Blocks {
					content += b.Source + "\n"
				}
				return base + "/teacher?message=" + url.QueryEscape(m.ID), content
			}
		}
	} else if reward.Kind == "feynman" {
		store, err := assets.New(reward.ProjectSlug)
		if err != nil {
			return "", ""
		}
		session, err := store.FeynmanSession(reward.SourceID)
		if err != nil {
			return "", ""
		}
		for _, m := range session.Messages {
			if m.ID == reward.MessageID {
				return base + "/assets?view=feynman&session=" + url.QueryEscape(session.SessionID) + "&message=" + url.QueryEscape(m.ID), m.Content
			}
		}
	}
	return "", ""
}
