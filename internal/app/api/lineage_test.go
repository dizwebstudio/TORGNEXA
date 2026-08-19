package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/lineage"
)

type timelineReaderStub struct {
	page lineage.TimelinePage
	err  error
	got  lineage.TimelineQuery
}

func (s *timelineReaderStub) Timeline(_ context.Context, _ lineage.Scope, q lineage.TimelineQuery) (lineage.TimelinePage, error) {
	s.got = q
	return s.page, s.err
}

type scopeResolverStub struct {
	scope lineage.Scope
	err   error
}

func (s scopeResolverStub) LineageScope(*http.Request) (lineage.Scope, error) { return s.scope, s.err }

func TestLineageTimelineRequiresResolvedTenantAndReturnsPage(t *testing.T) {
	scope, _ := lineage.NewScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	reader := &timelineReaderStub{page: lineage.TimelinePage{Items: []lineage.Record{}}}
	h := newHandlerWithLineage(testLogger(), reader, scopeResolverStub{scope: scope})
	r := httptest.NewRequest(http.MethodGet, LineageTimelinePath+"?system=torgnexa&entity_type=price&entity_id=p1&field=amount&limit=25", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if reader.got.Limit != 25 || reader.got.Field != "amount" {
		t.Fatalf("query=%+v", reader.got)
	}
	if !strings.Contains(w.Body.String(), `"items"`) {
		t.Fatalf("body=%s", w.Body.String())
	}
}
func TestLineageTimelineRejectsUnresolvedScopeAndNonUTCCursor(t *testing.T) {
	h := newHandlerWithLineage(testLogger(), &timelineReaderStub{}, scopeResolverStub{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, LineageTimelinePath+"?system=torgnexa&entity_type=price&entity_id=p1", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", w.Code)
	}
	scope, _ := lineage.NewScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	h = newHandlerWithLineage(testLogger(), &timelineReaderStub{}, scopeResolverStub{scope: scope})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, LineageTimelinePath+"?system=torgnexa&entity_type=price&entity_id=p1&before_at=2026-08-10T03:00:00%2B03:00&before_id=x", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", w.Code)
	}
}
func TestLineageTimelineHeadHasNoBody(t *testing.T) {
	scope, _ := lineage.NewScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	reader := &timelineReaderStub{page: lineage.TimelinePage{}}
	h := newHandlerWithLineage(testLogger(), reader, scopeResolverStub{scope: scope})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, LineageTimelinePath+"?system=torgnexa&entity_type=price&entity_id=p1", nil))
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
	}
	_ = time.UTC
}
