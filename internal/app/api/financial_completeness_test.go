package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseCompletenessQueryDefaultsAndNormalizesCurrency(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/financial-completeness?currency=rub", nil)
	basis, from, to, currency, err := parseCompletenessQuery(request)
	if err != nil {
		t.Fatal(err)
	}
	if basis != "order_accrual" || currency != "RUB" || !to.After(from) || to.Sub(from) > 31*24*time.Hour {
		t.Fatalf("unexpected defaults: basis=%s from=%s to=%s currency=%s", basis, from, to, currency)
	}
}

func TestParseCompletenessQueryRejectsUnsafeRange(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/financial-completeness?from=2026-01-01T00:00:00Z&to=2027-01-03T00:00:00Z", nil)
	if _, _, _, _, err := parseCompletenessQuery(request); err == nil {
		t.Fatal("range over one year accepted")
	}
}

func TestFinancialCompletenessRoutesFailClosedWithoutTenant(t *testing.T) {
	routes := newFinancialCompletenessRoutes(nil, nil)
	for _, route := range routes {
		request := httptest.NewRequest(route.Method, route.Path, nil)
		response := httptest.NewRecorder()
		route.Handler.ServeHTTP(response, request)
		if response.Code != 403 && response.Code != 400 {
			t.Errorf("%s %s returned %d without tenant/auth context", route.Method, route.Path, response.Code)
		}
	}
}
