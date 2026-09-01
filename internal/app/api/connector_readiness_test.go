package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectorReadinessListExposesCatalogWithStableCursor(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, ConnectorReadinessPath+"?limit=2", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestScopeKey{}, validTestScope(t)))
	response := httptest.NewRecorder()
	connectorReadinessList(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page connectorReadinessResponse
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Summary.Total != 61 || page.NextCursor == "" || page.Consistency != "repository_snapshot" {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.Items[0].ConnectorID >= page.Items[1].ConnectorID {
		t.Fatalf("items are not sorted: %q, %q", page.Items[0].ConnectorID, page.Items[1].ConnectorID)
	}

	nextRequest := httptest.NewRequest(http.MethodGet, ConnectorReadinessPath+"?limit=2&cursor="+page.NextCursor, nil)
	nextRequest = nextRequest.WithContext(context.WithValue(nextRequest.Context(), requestScopeKey{}, validTestScope(t)))
	nextResponse := httptest.NewRecorder()
	connectorReadinessList(nextResponse, nextRequest)
	if nextResponse.Code != http.StatusOK {
		t.Fatalf("next status=%d body=%s", nextResponse.Code, nextResponse.Body.String())
	}
	var nextPage connectorReadinessResponse
	if err := json.Unmarshal(nextResponse.Body.Bytes(), &nextPage); err != nil {
		t.Fatal(err)
	}
	if len(nextPage.Items) != 2 || nextPage.Items[0].ConnectorID <= page.Items[1].ConnectorID {
		t.Fatalf("cursor did not advance: %+v", nextPage)
	}
}

func TestConnectorReadinessDetailAndFiltersFailClosed(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestScopeKey{}, validTestScope(t))
	detail := httptest.NewRequest(http.MethodGet, ConnectorReadinessDetailPath+"ozon", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	connectorReadinessDetail(response, detail)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"connector_id":"ozon"`) {
		t.Fatalf("detail status=%d body=%s", response.Code, response.Body.String())
	}

	for _, query := range []string{"?status=bogus", "?limit=101", "?cursor=v1.bm90LWEtY29ubmVjdG9y"} {
		request := httptest.NewRequest(http.MethodGet, ConnectorReadinessPath+query, nil).WithContext(ctx)
		response := httptest.NewRecorder()
		connectorReadinessList(response, request)
		if response.Code != http.StatusBadRequest {
			t.Errorf("query=%q status=%d body=%s", query, response.Code, response.Body.String())
		}
	}

	withoutScope := httptest.NewRecorder()
	connectorReadinessList(withoutScope, httptest.NewRequest(http.MethodGet, ConnectorReadinessPath, nil))
	if withoutScope.Code != http.StatusForbidden {
		t.Fatalf("without scope status=%d", withoutScope.Code)
	}
}
