package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/search"
)

const (
	searchOrgID       = "018f0000-0000-7000-8000-000000000001"
	searchWorkspaceID = "018f0000-0000-7000-8000-000000000002"
	otherOrgID        = "018f0000-0000-7000-8000-000000000003"
	otherWorkspaceID  = "018f0000-0000-7000-8000-000000000004"
)

type fakeSearchScopeResolver struct {
	scope tenancy.Scope
	err   error
}

func (r fakeSearchScopeResolver) SearchScope(*http.Request) (tenancy.Scope, error) {
	return r.scope, r.err
}

type fakeSearchProvider struct {
	productCalls int
	orderCalls   int
	scope        tenancy.Scope
	productQuery search.ProductQuery
	orderQuery   search.OrderQuery
	productPage  search.ProductPage
	orderPage    search.OrderPage
	err          error
}

type fakeDemoProvider struct {
	fakeSearchProvider
	created int
	deleted int
}

func (p *fakeDemoProvider) SeedDemoOrders(context.Context, tenancy.Scope, string) (int, error) {
	return p.created, p.err
}

func (p *fakeDemoProvider) DeleteDemoOrders(context.Context, tenancy.Scope) (int, error) {
	return p.deleted, p.err
}

type fakeAuditCapturer struct {
	entry audit.Entry
	err   error
}

func (c *fakeAuditCapturer) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	c.entry = entry
	return audit.Record{}, c.err
}

func (p *fakeSearchProvider) SearchProducts(_ context.Context, scope tenancy.Scope, query search.ProductQuery) (search.ProductPage, error) {
	p.productCalls++
	p.scope = scope
	p.productQuery = query
	return p.productPage, p.err
}
func (p *fakeSearchProvider) SearchOrders(_ context.Context, scope tenancy.Scope, query search.OrderQuery) (search.OrderPage, error) {
	p.orderCalls++
	p.scope = scope
	p.orderQuery = query
	return p.orderPage, p.err
}

func searchScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(searchOrgID, searchWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestProductSearchUsesAuthenticatedScopeAndIgnoresClientTenantOverride(t *testing.T) {
	scope := searchScope(t)
	updatedAt := time.Date(2026, 8, 10, 8, 0, 0, 0, time.UTC)
	engine := &fakeSearchProvider{productPage: search.ProductPage{Items: []search.ProductHit{{ID: "018f0000-0000-7000-8000-000000000101", Code: "SKU-42", Title: "Cordless drill", Status: "active", UpdatedAt: updatedAt}}}}
	handler := newHandlerWithSearch(slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), engine, fakeSearchScopeResolver{scope: scope})

	request := httptest.NewRequest(http.MethodGet, ProductSearchPath+"?q=drill&status=active&limit=25&organization_id="+otherOrgID+"&workspace_id="+otherWorkspaceID, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if engine.productCalls != 1 || engine.scope.OrganizationID().String() != searchOrgID || engine.scope.WorkspaceID().String() != searchWorkspaceID {
		t.Fatalf("provider did not receive authenticated scope: %#v", engine.scope)
	}
	if engine.productQuery.Text != "drill" || engine.productQuery.Status != "active" || engine.productQuery.Limit != 25 {
		t.Fatalf("unexpected query: %#v", engine.productQuery)
	}
	if strings.Contains(response.Body.String(), searchOrgID) || strings.Contains(response.Body.String(), searchWorkspaceID) {
		t.Fatal("tenant identifiers leaked into search result payload")
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("search response is cacheable")
	}
}

func TestSearchFailsClosedWhenAuthenticatedScopeIsMissing(t *testing.T) {
	engine := &fakeSearchProvider{}
	handler := newHandlerWithSearch(nil, engine, fakeSearchScopeResolver{err: errors.New("missing auth")})
	request := httptest.NewRequest(http.MethodGet, ProductSearchPath+"?q=drill", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if engine.productCalls != 0 || engine.orderCalls != 0 {
		t.Fatal("provider called without authenticated tenant scope")
	}
}

func TestOrderSearchParsesUTCWindowAndHeadHasNoBody(t *testing.T) {
	scope := searchScope(t)
	engine := &fakeSearchProvider{orderPage: search.OrderPage{}}
	handler := newHandlerWithSearch(nil, engine, fakeSearchScopeResolver{scope: scope})
	request := httptest.NewRequest(http.MethodHead, OrderSearchPath+"?q=ORD-42&status=confirmed&placed_from=2026-08-01T00:00:00Z&placed_to=2026-08-11T00:00:00Z&limit=10", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if response.Body.Len() != 0 {
		t.Fatal("HEAD search returned a body")
	}
	if engine.orderCalls != 1 || engine.orderQuery.PlacedFrom == nil || engine.orderQuery.PlacedTo == nil {
		t.Fatalf("order query not forwarded: %#v", engine.orderQuery)
	}
	if engine.orderQuery.PlacedFrom.Location() != time.UTC || engine.orderQuery.PlacedTo.Location() != time.UTC {
		t.Fatal("order window is not UTC")
	}
}

func TestSearchRejectsInvalidQueryBeforeProvider(t *testing.T) {
	engine := &fakeSearchProvider{}
	handler := newHandlerWithSearch(nil, engine, fakeSearchScopeResolver{scope: searchScope(t)})
	cases := []string{
		ProductSearchPath + "?limit=101",
		ProductSearchPath + "?cursor=bad!cursor",
		OrderSearchPath + "?status=unknown",
		OrderSearchPath + "?placed_from=2026-08-10T12:00:00%2B03:00",
	}
	for _, target := range cases {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d", target, response.Code)
		}
	}
	if engine.productCalls != 0 || engine.orderCalls != 0 {
		t.Fatal("provider called for invalid query")
	}
}

func TestSearchMapsProviderValidationErrorWithoutLeakingDetail(t *testing.T) {
	engine := &fakeSearchProvider{err: search.ErrInvalid}
	handler := newHandlerWithSearch(nil, engine, fakeSearchScopeResolver{scope: searchScope(t)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, ProductSearchPath+"?q=drill", nil))
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "search:") {
		t.Fatalf("unexpected response: %d %s", response.Code, response.Body.String())
	}
}

func TestCreateDemoDatasetAppendsSafeAuditEvidence(t *testing.T) {
	engine := &fakeDemoProvider{created: 5}
	capturer := &fakeAuditCapturer{}
	routes := newSearchRoutes(engine, capturer)
	request := httptest.NewRequest(http.MethodPost, DemoOrdersPath, nil)
	request.Header.Set("Idempotency-Key", "demo-create-1")
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, searchScope(t)))
	request = request.WithContext(context.WithValue(request.Context(), requestIdentityKey{}, Principal{Issuer: "test", Subject: "admin-1"}))
	response := httptest.NewRecorder()
	for _, route := range routes {
		if route.Method == http.MethodPost && route.Path == DemoOrdersPath {
			route.Handler.ServeHTTP(response, request)
			break
		}
	}
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if capturer.entry.Action != "demo.dataset.created" || capturer.entry.ActorID != "admin-1" || capturer.entry.CorrelationID != "demo-create-1" || capturer.entry.Risk != audit.RiskWriteSafe {
		t.Fatalf("unexpected audit entry: %#v", capturer.entry)
	}
	if capturer.entry.Summary["orders_created"] != 5 || capturer.entry.Summary["synthetic"] != true {
		t.Fatalf("unexpected audit summary: %#v", capturer.entry.Summary)
	}
}

func TestDeleteDemoDatasetRequiresApprovalExecutionGrant(t *testing.T) {
	engine := &fakeDemoProvider{deleted: 5}
	capturer := &fakeAuditCapturer{}
	routes := newSearchRoutes(engine, capturer)
	var handler http.Handler
	for _, route := range routes {
		if route.Method == http.MethodDelete && route.Path == DemoOrdersPath {
			handler = route.Handler
			break
		}
	}
	request := httptest.NewRequest(http.MethodDelete, DemoOrdersPath, nil)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, searchScope(t)))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing principal status=%d", response.Code)
	}

	request = httptest.NewRequest(http.MethodDelete, DemoOrdersPath, nil)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, searchScope(t)))
	request = request.WithContext(context.WithValue(request.Context(), requestIdentityKey{}, Principal{Issuer: "test", Subject: "admin-1"}))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Approval required") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if capturer.entry.Action != "" {
		t.Fatalf("sensitive operation bypassed approval: audit=%#v", capturer.entry)
	}
}
