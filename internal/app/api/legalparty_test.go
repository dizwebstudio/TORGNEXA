package api

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/legalparty"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type legalPartySearcherStub struct {
	page legalparty.SearchPage
	got  legalparty.SearchQuery
}

func (s *legalPartySearcherStub) Search(_ context.Context, _ legalparty.Scope, q legalparty.SearchQuery) (legalparty.SearchPage, error) {
	s.got = q
	return s.page, nil
}

type legalPartyScopeStub struct {
	scope legalparty.Scope
	err   error
}

func (s legalPartyScopeStub) LegalPartyScope(*http.Request) (legalparty.Scope, error) {
	return s.scope, s.err
}
func TestLegalPartySearchUsesResolvedTenant(t *testing.T) {
	scope, _ := legalparty.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	search := &legalPartySearcherStub{page: legalparty.SearchPage{Items: []legalparty.SearchResult{}}}
	h := newHandlerWithLegalPartySearch(testLogger(), search, legalPartyScopeStub{scope: scope})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, LegalPartySearchPath+"?q=acme&party_type=legal_entity&limit=25", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if search.got.Text != "acme" || search.got.Limit != 25 {
		t.Fatalf("query=%+v", search.got)
	}
	if !strings.Contains(w.Body.String(), `"items"`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}
func TestLegalPartySearchRejectsMissingTenant(t *testing.T) {
	h := newHandlerWithLegalPartySearch(testLogger(), &legalPartySearcherStub{}, legalPartyScopeStub{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, LegalPartySearchPath+"?q=acme", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
}
