package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/integrationcenter"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

type centerReaderStub struct {
	result integrationCenterReadResult
	calls  int
}

func (s *centerReaderStub) Read(_ context.Context, _ tenancy.Scope, request integrationCenterReadRequest) (integrationCenterReadResult, error) {
	s.calls++
	if request.Limit < 1 {
		panic("invalid limit")
	}
	return s.result, nil
}

func TestIntegrationCenterListIsTenantScopedAndFiltered(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	d := integrationcenterTestDimensions(now)
	row, err := integrationcenter.Reduce(integrationcenter.Input{AccountID: "account-1", ConnectorID: "woocommerce", Family: "storefront", Surface: "integrations", Version: 1, Dimensions: d, SourceWatermarks: []string{"accounts:1"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	reader := &centerReaderStub{result: integrationCenterReadResult{Rows: []integrationcenter.Snapshot{row}, GeneratedAt: now, SourceWatermarks: []string{"accounts:1"}}}
	req := httptest.NewRequest(http.MethodGet, IntegrationCenterPath+"?overall=healthy", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestScopeKey{}, validTestScope(t)))
	res := httptest.NewRecorder()
	integrationCenterList(res, req, reader)
	if res.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
	}
	var body integrationCenterResponse
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].AccountID != "account-1" || body.Summary.Healthy != 1 {
		t.Fatalf("body=%+v", body)
	}
	if reader.calls != 1 {
		t.Fatalf("reader calls=%d", reader.calls)
	}
}

func TestIntegrationCenterRejectsTenantSelectorsAndBadFilters(t *testing.T) {
	for _, query := range []string{"?organization_id=other", "?limit=101", "?stale=maybe", "?cursor=" + strings.Repeat("a", 257)} {
		req := httptest.NewRequest(http.MethodGet, IntegrationCenterPath+query, nil)
		req = req.WithContext(context.WithValue(req.Context(), requestScopeKey{}, validTestScope(t)))
		res := httptest.NewRecorder()
		integrationCenterList(res, req, &centerReaderStub{})
		if res.Code != http.StatusBadRequest {
			t.Errorf("query %q status=%d", query, res.Code)
		}
	}
}

func TestIntegrationCenterListSupportsNoStoreAndConditionalRead(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	row, err := integrationcenter.Reduce(integrationcenter.Input{AccountID: "account-1", ConnectorID: "woocommerce", Family: "storefront", Surface: "integrations", Version: 1, Dimensions: integrationcenterTestDimensions(now), SourceWatermarks: []string{"accounts:1"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	reader := &centerReaderStub{result: integrationCenterReadResult{Rows: []integrationcenter.Snapshot{row}, GeneratedAt: now, SourceWatermarks: []string{"accounts:1"}}}
	first := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, IntegrationCenterPath, nil).WithContext(context.WithValue(context.Background(), requestScopeKey{}, validTestScope(t)))
	integrationCenterList(first, request, reader)
	if first.Code != http.StatusOK || first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers/status=%d cache=%q content=%q", first.Code, first.Header().Get("Cache-Control"), first.Header().Get("X-Content-Type-Options"))
	}
	second := httptest.NewRecorder()
	conditional := httptest.NewRequest(http.MethodGet, IntegrationCenterPath, nil).WithContext(context.WithValue(context.Background(), requestScopeKey{}, validTestScope(t)))
	conditional.Header.Set("If-None-Match", first.Header().Get("ETag"))
	integrationCenterList(second, conditional, reader)
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("conditional status/body=%d/%q", second.Code, second.Body.String())
	}
}

func integrationcenterTestDimensions(now time.Time) integrationcenter.Dimensions {
	e := integrationcenter.EvidenceRef{ObservedAt: now.Add(-time.Minute), CheckedAt: now.Add(-time.Minute), SourceKind: "test", SourceRef: "source:1", Visibility: integrationcenter.VisibilityFull, StaleAfterSeconds: 3600, AgeSeconds: 60}
	return integrationcenter.Dimensions{Runtime: integrationcenter.Dimension{Status: "ready", Evidence: e}, Account: integrationcenter.Dimension{Status: "active", Evidence: e}, Credential: integrationcenter.Dimension{Status: "present", Evidence: e}, Configuration: integrationcenter.Dimension{Status: "valid", Evidence: e}, Health: integrationcenter.Dimension{Status: "healthy", Evidence: e}, Capability: integrationcenter.Dimension{Status: "enabled", Evidence: e}, Sync: integrationcenter.Dimension{Status: "idle", Evidence: e}, Reconciliation: integrationcenter.Dimension{Status: "healthy", Evidence: e}, Webhook: integrationcenter.Dimension{Status: "receiving", Evidence: e}, RateLimit: integrationcenter.Dimension{Status: "available", Evidence: e}}
}
