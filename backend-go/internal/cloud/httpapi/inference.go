package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/xmz14/lll/backend-go/internal/cloud/inference"
	"github.com/xmz14/lll/backend-go/internal/cloud/promptcatalog"
	"github.com/xmz14/lll/backend-go/internal/cloud/routing"
	"github.com/xmz14/lll/backend-go/internal/cloud/store"
	"github.com/xmz14/lll/backend-go/internal/modules/ai"
	"github.com/xmz14/lll/backend-go/internal/platform/promptpack"
)

// Rate uses legacy thousandths per token unless TokensPerPoint sets the input exchange rate.
type Rate struct {
	Input, Cached, Write, Output int64
	TokensPerPoint               int64 `json:",omitempty"`
}

type Model struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	MaxInput  int    `json:"maxInput"`
	MaxOutput int    `json:"maxOutput"`
	Rate      Rate   `json:"rate"`
}

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func (m Model) valid() bool {
	const maxTokens = 4_000_000
	const maxRate = 10_000
	r := m.Rate
	return identifier.MatchString(m.ID) && m.Name != "" && m.MaxInput > 0 && m.MaxInput <= maxTokens &&
		m.MaxOutput > 0 && m.MaxOutput <= maxTokens && r.Input > 0 && r.Input <= maxRate &&
		r.Cached >= 0 && r.Cached <= maxRate && r.Write >= 0 && r.Write <= maxRate && r.Output > 0 && r.Output <= maxRate &&
		r.TokensPerPoint >= 0 && r.TokensPerPoint <= maxTokens
}

func (rate Rate) charge(usage map[string]int) (int64, bool) {
	weighted, known := inference.ChargeTokens(usage, rate.Input, rate.Cached, rate.Write, rate.Output)
	return rate.creditUnits(weighted), known
}

func (rate Rate) creditUnits(weighted int64) int64 {
	if rate.TokensPerPoint == 0 {
		return weighted
	}
	// The input weight defines the baseline. Round up to a thousandth of a point.
	denominator := rate.Input * rate.TokensPerPoint
	return (weighted*1000 + denominator - 1) / denominator
}

type inferenceInput struct {
	Prompt           *promptpack.Reference `json:"prompt"`
	PromptExposureID string                `json:"promptExposureId"`
	Model            string                `json:"model"`
	RequestID        string                `json:"requestId"`
	System           string                `json:"system"`
	TurnInstructions string                `json:"turnInstructions"`
	Messages         []ai.GatewayMessage   `json:"messages"`
	Tools            []ai.Tool             `json:"tools"`
	MaxOutputTokens  int                   `json:"maxOutputTokens"`
	ReasoningEffort  string                `json:"reasoningEffort"`
	ShowReasoning    *bool                 `json:"showReasoning"`
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	account, ok := s.authenticated(w, r)
	if !ok {
		return
	}
	var in inferenceInput
	if !decode(w, r, &in, 2<<20) {
		return
	}
	s.modelMu.RLock()
	defer s.modelMu.RUnlock()
	model, known := s.models[in.Model]
	if !known || !identifier.MatchString(in.RequestID) || len(in.Messages) == 0 || len(in.Tools) > 32 ||
		in.MaxOutputTokens < 0 || in.MaxOutputTokens > model.MaxOutput {
		fail(w, http.StatusBadRequest, "invalid_inference_request")
		return
	}
	if in.MaxOutputTokens == 0 {
		in.MaxOutputTokens = model.MaxOutput
	}
	needsTools := inference.RequiresTools(in.Tools, in.Messages)
	if gateway, ok := s.gateway.(*inference.Gateway); needsTools && ok && !gateway.SupportsTools(model.ID) {
		fail(w, http.StatusUnprocessableEntity, "model_tools_unsupported")
		return
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_inference_request")
		return
	}
	estimated := ai.EstimateTextTokens(string(encoded))
	if estimated > model.MaxInput {
		fail(w, http.StatusRequestEntityTooLarge, "model_input_limit")
		return
	}
	digest := sha256.Sum256(encoded)
	weighted := int64(estimated)*max(model.Rate.Input, model.Rate.Cached, model.Rate.Write) + int64(in.MaxOutputTokens)*model.Rate.Output
	units := model.Rate.creditUnits(weighted)
	budget := store.BudgetHold{Currency: s.budget.Currency, Limit: s.budget.MonthlyNanos}
	if budget.Limit > 0 {
		estimator, ok := s.gateway.(interface {
			EstimateCost(string, string, int, int, bool) (int64, bool)
		})
		if ok {
			budget.Estimated, ok = estimator.EstimateCost(model.ID, budget.Currency, model.MaxInput, in.MaxOutputTokens, needsTools)
		}
		if !ok {
			fail(w, http.StatusServiceUnavailable, "model_cost_unconfigured")
			return
		}
	}
	hold, err := s.db.ReserveBudgeted(r.Context(), account.ID, in.RequestID, hex.EncodeToString(digest[:]), units, time.Now(), budget)
	switch {
	case errors.Is(err, store.ErrBudget):
		fail(w, http.StatusServiceUnavailable, "cloud_budget_exhausted")
		return
	case errors.Is(err, store.ErrBalance):
		fail(w, http.StatusPaymentRequired, "insufficient_credits")
		return
	case errors.Is(err, store.ErrDailyCreditLimit):
		fail(w, http.StatusTooManyRequests, "daily_credit_limit_reached")
		return
	case errors.Is(err, store.ErrConflict), err == nil && !hold.Created:
		fail(w, http.StatusConflict, "request_already_recorded")
		return
	case err != nil:
		fail(w, http.StatusInternalServerError, "credits_unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	startedAt := time.Now()
	var exposure promptcatalog.Exposure
	if in.Prompt != nil && identifier.MatchString(in.PromptExposureID) {
		exposure, _ = s.promptExposure(account.ID, *in.Prompt, startedAt)
	}
	started := false
	emit := func(value any) {
		if !started {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("X-Accel-Buffering", "no")
			started = true
		}
		data, err := json.Marshal(value)
		if err != nil {
			cancel()
			return
		}
		controller := http.NewResponseController(w)
		if controller.SetWriteDeadline(time.Now().Add(10*time.Second)) == nil {
			defer controller.SetWriteDeadline(time.Time{})
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			cancel()
			return
		}
		if err := controller.Flush(); err != nil {
			cancel()
		}
	}
	key := routeKey(account.ID, in)
	preferred := s.routeAffinity.preferred(key, startedAt)
	attempts, streamErr := s.gateway.Stream(ctx, model.ID, estimated+in.MaxOutputTokens, preferred, ai.GatewayRequest{
		System: in.System, TurnInstructions: in.TurnInstructions, Messages: in.Messages, Tools: in.Tools,
		MaxOutputTokens: in.MaxOutputTokens, ReasoningEffort: in.ReasoningEffort, ShowReasoning: in.ShowReasoning,
	}, func(event ai.GatewayEvent) { emit(event) })
	if n := len(attempts); streamErr == nil && n > 0 && attempts[n-1].Status == "completed" {
		s.routeAffinity.remember(key, attempts[n-1].ConnectionID, time.Now())
	}
	accounting, accountingCancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer accountingCancel()
	var experiment *store.ExperimentUse
	if exposure.Experiment != "" {
		experiment = &store.ExperimentUse{
			ExposureID: in.PromptExposureID, Experiment: exposure.Experiment, Variant: exposure.Variant,
			Version: exposure.Version, Completed: streamErr == nil, Teaching: strings.HasPrefix(in.Prompt.Scene, "teacher/"),
			Elapsed: time.Since(startedAt),
		}
	}
	receipt, err := s.finalize(accounting, account.ID, in.RequestID, model, startedAt, attempts, experiment)
	if err != nil {
		_ = s.db.MarkPending(accounting, account.ID, in.RequestID, time.Now())
		receipt = ai.CreditSettlement{Pending: true}
		if started {
			emit(ai.GatewayEvent{Type: "credit-settlement", CreditSettlement: &receipt})
			emit(map[string]string{"type": "error", "code": "settlement_pending"})
		} else {
			failWithSettlement(w, http.StatusInternalServerError, "settlement_pending", receipt)
		}
		return
	}
	if streamErr == nil {
		emit(ai.GatewayEvent{Type: "credit-settlement", CreditSettlement: &receipt})
		emit(map[string]string{"type": "done"})
	} else if started {
		emit(ai.GatewayEvent{Type: "credit-settlement", CreditSettlement: &receipt})
		emit(map[string]string{"type": "error", "code": "inference_interrupted"})
	} else if errors.Is(streamErr, routing.ErrCapacity) {
		w.Header().Set("Retry-After", "5")
		failWithSettlement(w, http.StatusTooManyRequests, "model_busy", receipt)
	} else {
		failWithSettlement(w, http.StatusBadGateway, "inference_unavailable", receipt)
	}
}

func failWithSettlement(w http.ResponseWriter, status int, code string, receipt ai.CreditSettlement) {
	respond(w, status, map[string]any{"error": code, "creditSettlement": receipt})
}

func (s *Server) finalize(ctx context.Context, user, request string, model Model, at time.Time, attempts []inference.Attempt, experiment *store.ExperimentUse) (ai.CreditSettlement, error) {
	metrics := make([]store.AttemptMetric, 0, len(attempts))
	for _, attempt := range attempts {
		metrics = append(metrics, store.AttemptMetric{
			ConnectionID: attempt.ConnectionID, Status: attempt.Status, Elapsed: attempt.Elapsed,
			Input: metricTokens(attempt.Usage, "inputTokens"), Output: metricTokens(attempt.Usage, "outputTokens"),
			Read: metricTokens(attempt.Usage, "cacheReadTokens"), Written: metricTokens(attempt.Usage, "cacheWriteTokens"),
			Currency: attempt.Currency, CostNanos: attempt.CostNanos,
		})
	}
	if err := s.db.RecordAttempts(ctx, user, request, model.ID, s.routeRevision, at, metrics, experiment); err != nil {
		return ai.CreditSettlement{}, err
	}
	return s.settle(ctx, user, request, model.Rate, attempts)
}

func metricTokens(usage map[string]int, field string) *int {
	if n, present := usage[field]; present && n >= 0 {
		return &n
	}
	return nil
}

func (s *Server) settle(ctx context.Context, user, request string, rate Rate, attempts []inference.Attempt) (ai.CreditSettlement, error) {
	var units, spend int64
	knownSpend := true
	for _, attempt := range attempts {
		if attempt.Status == "rate_limited" || attempt.Status == "auth_rejected" {
			continue
		}
		charge, known := rate.charge(attempt.Usage)
		if !known {
			return ai.CreditSettlement{Pending: true}, s.db.MarkPending(ctx, user, request, time.Now())
		}
		units += charge
		if attempt.CostNanos == nil || attempt.Currency != s.budget.Currency {
			knownSpend = false
		} else {
			spend += *attempt.CostNanos
		}
	}
	var cost *int64
	if knownSpend {
		cost = &spend
	}
	return ai.CreditSettlement{Units: units}, s.db.SettleBudgeted(ctx, user, request, units, time.Now(), cost)
}
