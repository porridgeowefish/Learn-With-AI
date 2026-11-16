package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"time"
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// EnableCostReporting exposes recorded supplier costs to existing Backstage clients.
func (s *Server) EnableCostReporting(currency string) error {
	if currency == "" {
		currency = "CNY"
	}
	if !currencyCode.MatchString(currency) {
		return errors.New("invalid supplier cost currency")
	}
	s.supplierCostCurrency = currency
	if s.adminAuthorize == nil {
		return nil
	}
	s.mux.HandleFunc("GET /v1/admin/budget", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.adminAuthorize(w, r, false); !ok {
			return
		}
		usage, err := s.db.MonthlySupplierCost(r.Context(), currency, time.Now())
		if err != nil {
			fail(w, http.StatusInternalServerError, "budget_unavailable")
			return
		}
		respond(w, http.StatusOK, map[string]any{"limit": 0, "usage": usage})
	})
	return nil
}
