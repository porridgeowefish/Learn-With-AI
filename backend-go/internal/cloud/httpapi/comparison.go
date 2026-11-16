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

	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/platform/llmcloud"
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
	if catalog.Comparison != nil {
		experiment := catalog.Comparison.ID
		raw, err := s.db.SelectedComparisonPrompt(r.Context(), subject, experiment)
		if err == nil && len(raw) > 0 {
			var selected promptcatalog.ComparisonPack
			if err = json.Unmarshal(raw, &selected); err == nil {
				pack, err = catalog.Selected(scene, selected)
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
	if config := catalog.Comparison; config != nil {
		hash := sha256.Sum256([]byte(config.ID + "\x00" + subject + "\x00" + input.Operation))
		models := config.ModelIDs()
		model := models[binary.BigEndian.Uint64(hash[8:16])%uint64(len(models))]
		next.Experiment, next.Model, next.Decision = config.ID, model, "not_sampled"
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
	respond(w, http.StatusOK, llmcloud.ComparisonAssignment{ID: result.ID, Model: result.Model, Protocol: 2})
}

func (s *Server) streamComparison(w http.ResponseWriter, r *http.Request) {
	subject, _, ok := s.comparisonSubject(w, r)
	if !ok {
		return
	}
	var input struct {
		ID, Arm string
		llmcloud.ComparisonInput
	}
	if !decode(w, r, &input, 512<<10) {
		return
	}
	if !identifier.MatchString(input.ID) || (input.Arm != "a" && input.Arm != "b") || strings.TrimSpace(input.Question) == "" || len(input.Question) > 64<<10 || len(input.Context) > 16<<10 || !comparisonMode(input.Mode) {
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
	var pack promptcatalog.ComparisonPack
	if json.Unmarshal(raw, &pack) != nil || pack.Content.Resources["teaching/teacher"] == "" {
		fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
		return
	}
	request, err := comparisonRequest(input.ComparisonInput, pack, s.comparisonRules)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_comparison_request")
		return
	}
	if support, ok := s.gateway.(interface{ SupportsTools(string) bool }); ok && !support.SupportsTools(model.ID) {
		request.Tools = nil
	}
	estimated := (ai.Budget{}).Input(request)
	maxInput := min(model.MaxInput, 64_000)
	if estimated > maxInput {
		fail(w, http.StatusRequestEntityTooLarge, "model_input_limit")
		return
	}
	request.MaxOutputTokens = min(model.MaxOutput, 4096)
	start := time.Now()
	questionDigest := sha256.Sum256([]byte(input.Question))
	if input.Teaching != nil {
		binding, _ := json.Marshal(input.ComparisonInput)
		questionDigest = sha256.Sum256(binding)
	} else if input.Mode != "" || input.Context != "" || input.Opening {
		binding, _ := json.Marshal([]any{input.Question, input.Mode, input.Context, input.Opening})
		questionDigest = sha256.Sum256(binding)
	}
	if err = s.db.StartComparisonArm(r.Context(), cmp.ID, input.Arm, hex.EncodeToString(questionDigest[:]), s.supplierCostCurrency, start); err != nil {
		if errors.Is(err, store.ErrConflict) {
			fail(w, http.StatusConflict, "comparison_arm_already_started")
		} else {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
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
	forwardText := func(event ai.GatewayEvent) {
		if event.Type == "text-delta" {
			hasText = hasText || strings.TrimSpace(event.Delta) != ""
			emit(event)
		}
	}
	attempts, streamErr := s.runComparison(ctx, subject, model.ID, maxInput, request, emit, forwardText)
	var spent int64
	knownCost := true
	for _, attempt := range attempts {
		if attempt.Status == "rate_limited" || attempt.Status == "auth_rejected" {
			continue
		}
		if attempt.CostNanos == nil || attempt.Currency != s.supplierCostCurrency {
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
	if err = s.db.FinishComparisonArm(saveCtx, cmp.ID, input.Arm, completed, cost, spent, time.Now()); err != nil || !completed {
		emit(map[string]string{"type": "error", "code": "comparison_arm_failed"})
		return
	}
	emit(map[string]string{"type": "done"})
}

func comparisonMode(mode string) bool {
	switch mode {
	case "", "general", "history", "book", "concept":
		return true
	default:
		return false
	}
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
	s.mux.HandleFunc("GET /v1/admin/answer-comparisons/experiments", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		rows, err := s.db.PublishedComparisons(r.Context(), "", false)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
			return
		}
		result := make([]comparisonExperiment, len(rows))
		for i, row := range rows {
			result[i] = experimentResponse(row)
		}
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/answer-comparisons/experiments/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, true); !ok {
			return
		}
		id := r.PathValue("id")
		if !identifier.MatchString(id) {
			fail(w, http.StatusBadRequest, "invalid_comparison_experiment")
			return
		}
		rows, err := s.db.PublishedComparisons(r.Context(), id, true)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
			return
		}
		if len(rows) == 0 {
			fail(w, http.StatusNotFound, "comparison_experiment_not_found")
			return
		}
		result := experimentResponse(rows[0])
		if len(rows[0].Config) > 0 {
			var config promptcatalog.ComparisonDraft
			if json.Unmarshal(rows[0].Config, &config) != nil {
				fail(w, http.StatusInternalServerError, "comparison_unavailable")
				return
			}
			result.A, result.B, result.Variants = config.A, config.B, config.Variants
		}
		result.BaseResources = rows[0].BaseResources
		respond(w, http.StatusOK, result)
	})
	s.mux.HandleFunc("GET /v1/admin/answer-comparisons", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		experiment := r.URL.Query().Get("experiment")
		if experiment != "" && !identifier.MatchString(experiment) {
			fail(w, http.StatusBadRequest, "invalid_comparison_experiment")
			return
		}
		rows, err := s.db.ComparisonMetrics(r.Context(), experiment)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
			return
		}
		respond(w, http.StatusOK, rows)
	})
	s.mux.HandleFunc("GET /v1/admin/answer-comparisons/records", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allow(w, r, false); !ok {
			return
		}
		query := r.URL.Query()
		experiment, audience, rawOffset := query.Get("experiment"), query.Get("audience"), query.Get("offset")
		offset, offsetErr := strconv.Atoi(rawOffset)
		if !identifier.MatchString(experiment) || (audience != "" && audience != "guest" && audience != "registered") || (rawOffset != "" && offsetErr != nil) || offset < 0 || offset > 1_000_000 {
			fail(w, http.StatusBadRequest, "invalid_comparison_records")
			return
		}
		rows, err := s.db.ComparisonRecords(r.Context(), experiment, audience, offset)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "comparison_unavailable")
			return
		}
		respond(w, http.StatusOK, rows)
	})
}

type comparisonExperiment struct {
	ID            string                            `json:"id"`
	Name          string                            `json:"name"`
	Model         string                            `json:"model"`
	Models        []string                          `json:"models,omitempty"`
	Percent       int                               `json:"percent"`
	CreatedAt     int64                             `json:"createdAt"`
	PublishedAt   int64                             `json:"publishedAt"`
	EndedAt       int64                             `json:"endedAt,omitempty"`
	BaseVersion   int64                             `json:"baseVersion"`
	Active        bool                              `json:"active"`
	A             string                            `json:"a,omitempty"`
	B             string                            `json:"b,omitempty"`
	Variants      *promptcatalog.ComparisonVariants `json:"variants,omitempty"`
	BaseResources json.RawMessage                   `json:"baseResources,omitempty"`
}

func experimentResponse(row store.PublishedComparison) comparisonExperiment {
	return comparisonExperiment{ID: row.ID, Name: row.Name, Model: row.Model, Models: row.Models, Percent: row.Percent,
		CreatedAt: row.CreatedAt, PublishedAt: row.PublishedAt, EndedAt: row.EndedAt, BaseVersion: row.BaseVersion, Active: row.Active}
}
