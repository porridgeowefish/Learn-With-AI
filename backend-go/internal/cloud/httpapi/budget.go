package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"time"
)

type Budget struct {
	Currency     string `json:"currency"`
	MonthlyNanos int64  `json:"monthlyNanos"`
}

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// EnableBudget configures the monthly supplier cost limit before serving traffic.
func (s *Server) EnableBudget(budget Budget) error {
	if budget.MonthlyNanos < 0 || (budget.MonthlyNanos > 0 && !currencyCode.MatchString(budget.Currency)) {
		return errors.New("invalid cloud cost budget")
	}
	s.budget = budget
	if s.adminAuthorize == nil {
		return nil
	}
	s.mux.HandleFunc("GET /v1/admin/budget", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, false); !ok {
			return
		}
		usage, err := s.db.BudgetUsage(r.Context(), budget.Currency, time.Now())
		if err != nil {
			fail(w, http.StatusInternalServerError, "budget_unavailable")
			return
		}
		respond(w, http.StatusOK, map[string]any{"limit": budget.MonthlyNanos, "usage": usage})
	})
	return nil
}
