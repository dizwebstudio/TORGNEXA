package builtinruntime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestLocalAIHTTPPinsLoopbackAndPreservesBasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer local" {
			t.Fatalf("unexpected request: method=%s path=%s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()
	endpoint, err := url.Parse(server.URL + "/v1")
	if err != nil {
		t.Fatal(err)
	}
	status, body, err := newLocalAIHTTP().do(context.Background(), endpoint.String(), "/chat/completions", []byte(`{}`), http.Header{"Authorization": []string{"Bearer local"}})
	if err != nil || status != http.StatusOK || string(body) == "" {
		t.Fatalf("status=%d body=%q err=%v", status, body, err)
	}
}

func TestLocalAIHTTPRejectsNonLocalEndpoint(t *testing.T) {
	if _, _, err := newLocalAIHTTP().do(context.Background(), "http://example.com:11434/v1", "/chat/completions", nil, nil); err == nil {
		t.Fatal("expected non-local endpoint rejection")
	}
}
