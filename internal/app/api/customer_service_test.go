package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/torgnexa/torgnexa/internal/core/customerservice"
)

func TestCustomerServiceFilterUsesSafeDefaultsAndRejectsUnsafeValues(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, CustomerServiceInboxPath+"?unresolved=true&limit=25&type=review", nil)
	filter, err := customerServiceFilter(request, core.Filter{})
	if err != nil || filter.Limit != 25 || filter.Type != "review" || !filter.Unresolved {
		t.Fatalf("filter=%+v err=%v", filter, err)
	}
	unsafe := httptest.NewRequest(http.MethodGet, CustomerServiceInboxPath+"?search="+strings.Repeat("x", 161), nil)
	if _, err := customerServiceFilter(unsafe, core.Filter{}); err == nil {
		t.Fatal("unbounded search accepted")
	}
}

func TestCustomerServiceRoutesFailClosedWithoutTenant(t *testing.T) {
	routes := newCustomerServiceRoutes(nil, nil)
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, httptest.NewRequest(routes[0].Method, routes[0].Path, nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("summary returned %d without tenant", response.Code)
	}
}
