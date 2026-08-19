package torgnexa

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGeneratedClientBuildsTenantNeutralRequest(t *testing.T) {
	var got *http.Request
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Clone(req.Context())
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"items":[]}`))}, nil
	})}
	client, err := NewClient(Config{BaseURL: "https://api.example.test/api/v1", BearerToken: "token", HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.ListProducts(context.Background(), ListProductsRequest{Q: "bolt", Status: "active", Limit: 25, Cursor: "v1.abc"})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if got == nil {
		t.Fatal("request not captured")
	}
	if got.URL.Path != "/api/v1/products" {
		t.Fatalf("path=%q", got.URL.Path)
	}
	if got.URL.Query().Get("q") != "bolt" || got.URL.Query().Get("limit") != "25" {
		t.Fatalf("query=%q", got.URL.RawQuery)
	}
	if got.Header.Get("Authorization") != "Bearer token" {
		t.Fatalf("authorization=%q", got.Header.Get("Authorization"))
	}
	if got.URL.Query().Get("organization_id") != "" || got.URL.Query().Get("workspace_id") != "" {
		t.Fatal("generated client must not synthesize tenant selectors")
	}
}

func TestGeneratedClientPreservesBinaryResponse(t *testing.T) {
	payload := []byte("%PDF-1.7\n\xff\x00\n%%EOF")
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Query().Get("format") != "pdf" {
			t.Fatalf("format=%q", req.URL.Query().Get("format"))
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/pdf"}}, Body: io.NopCloser(bytes.NewReader(payload))}, nil
	})}
	client, err := NewClient(Config{BaseURL: "https://api.example.test/api/v1", HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.GetReportData(context.Background(), GetReportDataRequest{ReportId: "sales_daily", Format: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response.Body, payload) {
		t.Fatalf("binary response changed: %q", []byte(response.Body))
	}
}

func TestGeneratedClientEscapesPathAndReturnsAPIError(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.EscapedPath() != "/api/v1/notifications/n%2F1/read" {
			t.Fatalf("escaped path=%q", req.URL.EscapedPath())
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"missing"}`))}, nil
	})}
	client, err := NewClient(Config{BaseURL: "https://api.example.test/api/v1", HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.MarkNotificationRead(context.Background(), MarkNotificationReadRequest{NotificationId: "n/1"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error=%T %v", err, err)
	}
	if response == nil || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("response=%#v error=%#v", response, apiErr)
	}
}

func TestGeneratedClientRequiresPathAndRequiredQueryParameters(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://api.example.test/api/v1", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport must not be called"); return nil, nil })}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetNotificationPreference(context.Background(), GetNotificationPreferenceRequest{}); err == nil {
		t.Fatal("expected missing path parameter error")
	}
	if _, err := client.GetLineageTimeline(context.Background(), GetLineageTimelineRequest{}); err == nil {
		t.Fatal("expected missing required query parameter error")
	}
}
