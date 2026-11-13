package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

func (s *Server) comparisonSubject(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if bearer(r) != "" {
		account, ok := s.authenticated(w, r)
		return "account:" + account.ID, "registered", ok
	}
	subject := s.guestComparisonSubject(r)
	if subject == "" {
		fail(w, http.StatusBadRequest, "guest_identity_unavailable")
		return "", "", false
	}
	return subject, "guest", true
}

func (s *Server) guestComparisonSubject(r *http.Request) string {
	ip, err := netip.ParseAddr(s.clientIP(r))
	if err != nil || len(s.comparisonKey) == 0 {
		return ""
	}
	digest := hmac.New(sha256.New, s.comparisonKey)
	digest.Write([]byte("answer-comparison-guest\x00" + ip.Unmap().String()))
	return "guest:" + hex.EncodeToString(digest.Sum(nil))
}

func (s *Server) servePrompt(w http.ResponseWriter, r *http.Request) {
	subject, _, ok := s.comparisonSubject(w, r)
	if !ok {
		return
	}
	scene := r.URL.Query().Get("scene")
	catalog := s.prompts.Load().active
	pack, found := catalog.Stable(scene)
	if !found {
		fail(w, http.StatusNotFound, "prompt_scene_unavailable")
		return
	}
	if scene == "teacher/general" {
		raw, err := s.db.SelectedComparisonPrompt(r.Context(), subject)
		if err == nil && len(raw) > 0 {
			var selected promptpack.Pack
			if err = json.Unmarshal(raw, &selected); err == nil {
				pack, err = catalog.SelectedTeacher(selected)
			}
		}
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "prompt_scene_unavailable")
			return
		}
	}
	w.Header().Set("ETag", pack.ETag())
	if r.Header.Get("If-None-Match") == pack.ETag() {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	respond(w, http.StatusOK, pack)
}

func (s *Server) startComparison(w http.ResponseWriter, r *http.Request) {
	subject, audience, ok := s.comparisonSubject(w, r)
	if !ok {
		return
	}
	var input struct{ Operation string }
	if !decode(w, r, &input, 1024) {
		return
	}
	if !identifier.MatchString(input.Operation) {
		fail(w, http.StatusBadRequest, "invalid_comparison_operation")
		return
	}
	if !s.allowAuth(w, r, "comparison-start", subject, 30) {
		return
	}
	catalog := s.prompts.Load().active
	next := store.Comparison{Audience: audience, Decision: "disabled"}
	if config := catalog.Comparison; config != nil && config.Percent > 0 {
		next.Experiment, next.Model, next.Decision = config.ID, config.Model, "not_sampled"
		hash := sha256.Sum256([]byte(config.ID + "\x00" + subject))
		if binary.BigEndian.Uint64(hash[:8])%100 < uint64(config.Percent) {
			next.Decision = "sampled"
			next.PackA, _ = json.Marshal(catalog.A)
			next.PackB, _ = json.Marshal(catalog.B)
		}
	}
	result, err := s.db.StartComparison(r.Context(), subject, input.Operation, next, time.Now())
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
		return
	}
	if result.Decision != "sampled" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	respond(w, http.StatusOK, map[string]string{"id": result.ID})
}

func (s *Server) streamComparison(w http.ResponseWriter, r *http.Request) {
	subject, _, ok := s.comparisonSubject(w, r)
	if !ok {
		return
	}
	var input struct{ ID, Arm, Question string }
	if !decode(w, r, &input, 256<<10) {
		return
	}
	if !identifier.MatchString(input.ID) || (input.Arm != "a" && input.Arm != "b") || strings.TrimSpace(input.Question) == "" || len(input.Question) > 64<<10 {
		fail(w, http.StatusBadRequest, "invalid_comparison_request")
		return
	}
	if !s.allowAuth(w, r, "comparison-stream", subject, 12) {
		return
	}
	cmp, err := s.db.ComparisonForSubject(r.Context(), subject, input.ID)
	if err != nil || cmp.Decision != "sampled" {
		fail(w, http.StatusNotFound, "comparison_unavailable")
		return
	}
	s.modelMu.RLock()
	defer s.modelMu.RUnlock()
	model, known := s.models[cmp.Model]
	if !known {
		fail(w, http.StatusServiceUnavailable, "comparison_model_unavailable")
		return
	}
	raw := cmp.PackA
	if input.Arm == "b" {
		raw = cmp.PackB
	}
	var pack promptpack.Pack
	if json.Unmarshal(raw, &pack) != nil || pack.Content.Resources["teaching/teacher"] == "" {
		fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
		return
	}
	system := pack.Content.Resources["teaching/teacher"]
	estimated := ai.EstimateTextTokens(system) + ai.EstimateTextTokens(input.Question) + 128
	if estimated > min(model.MaxInput, 64_000) {
		fail(w, http.StatusRequestEntityTooLarge, "model_input_limit")
		return
	}
	maxOutput := min(model.MaxOutput, 4096)
	hold := store.BudgetHold{Currency: s.budget.Currency, Limit: s.budget.MonthlyNanos}
	if hold.Limit > 0 {
		estimator, has := s.gateway.(interface {
			EstimateCost(string, string, int, int, bool) (int64, bool)
		})
		if has {
			hold.Estimated, has = estimator.EstimateCost(model.ID, hold.Currency, estimated, maxOutput, false)
		}
		if !has {
			fail(w, http.StatusServiceUnavailable, "model_cost_unconfigured")
			return
		}
	}
	start := time.Now()
	questionDigest := sha256.Sum256([]byte(input.Question))
	if err = s.db.ReserveComparisonArm(r.Context(), cmp.ID, input.Arm, hex.EncodeToString(questionDigest[:]), start, hold); err != nil {
		if errors.Is(err, store.ErrConflict) {
			fail(w, http.StatusConflict, "comparison_arm_already_started")
		} else {
			fail(w, http.StatusServiceUnavailable, "comparison_budget_unavailable")
		}
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	emit := func(value any) {
		data, err := json.Marshal(value)
		if err != nil {
			cancel()
			return
		}
		controller := http.NewResponseController(w)
		if controller.SetWriteDeadline(time.Now().Add(10*time.Second)) == nil {
			defer controller.SetWriteDeadline(time.Time{})
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", data); err != nil || controller.Flush() != nil {
			cancel()
		}
	}
	var hasText bool
	hidden := false
	attempts, streamErr := s.gateway.Stream(ctx, model.ID, estimated+maxOutput, "", ai.GatewayRequest{
		System: system, Messages: []ai.GatewayMessage{{Role: "user", Content: input.Question}}, MaxOutputTokens: maxOutput, ShowReasoning: &hidden,
	}, func(event ai.GatewayEvent) {
		if event.Type == "text-delta" {
			hasText = hasText || strings.TrimSpace(event.Delta) != ""
			emit(event)
		}
	})
	var spent int64
	knownCost := true
	for _, attempt := range attempts {
		if attempt.Status == "rate_limited" || attempt.Status == "auth_rejected" {
			continue
		}
		if attempt.CostNanos == nil || attempt.Currency != hold.Currency {
			knownCost = false
			continue
		}
		spent += *attempt.CostNanos
	}
	var cost *int64
	if knownCost {
		cost = &spent
	}
	completed := streamErr == nil && ctx.Err() == nil && hasText
	saveCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	if err = s.db.FinishComparisonArm(saveCtx, cmp.ID, input.Arm, completed, cost, time.Now()); err != nil || !completed {
		emit(map[string]string{"type": "error", "code": "comparison_arm_failed"})
		return
	}
	emit(map[string]string{"type": "done"})
}

func (s *Server) voteComparison(w http.ResponseWriter, r *http.Request) {
	subject, _, ok := s.comparisonSubject(w, r)
	if !ok {
		return
	}
	var input struct{ ID, Vote string }
	if !decode(w, r, &input, 1024) {
		return
	}
	if !identifier.MatchString(input.ID) || (input.Vote != "a" && input.Vote != "b" && input.Vote != "tie") {
		fail(w, http.StatusBadRequest, "invalid_comparison_vote")
		return
	}
	err := s.db.VoteComparison(r.Context(), subject, input.ID, input.Vote, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			fail(w, http.StatusNotFound, "comparison_unavailable")
		} else if errors.Is(err, store.ErrConflict) {
			fail(w, http.StatusConflict, "comparison_vote_unavailable")
		} else {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) registerComparisonAdmin(allow func(http.ResponseWriter, *http.Request, bool) (store.Account, bool)) {
	s.mux.HandleFunc("GET /v1/admin/answer-comparisons", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		rows, err := s.db.ComparisonMetrics(r.Context())
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
			return
		}
		respond(w, http.StatusOK, rows)
	})
	s.mux.HandleFunc("GET /v1/admin/answer-comparisons/audit", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		audience := r.URL.Query().Get("audience")
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if (audience != "" && audience != "guest" && audience != "registered") || offset < 0 || offset > 1_000_000 {
			fail(w, http.StatusBadRequest, "invalid_comparison_audit")
			return
		}
		rows, err := s.db.ComparisonAudit(r.Context(), audience, offset)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
			return
		}
		respond(w, http.StatusOK, rows)
	})
}
