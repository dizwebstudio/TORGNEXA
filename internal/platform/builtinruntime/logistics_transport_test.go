package builtinruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCDEKCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/oauth/token":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
				t.Fatalf("unexpected token request: method=%s content-type=%q", r.Method, r.Header.Get("Content-Type"))
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("client_id") != "client-1" || r.Form.Get("client_secret") != "secret-1" || r.Form.Get("grant_type") != "client_credentials" {
				t.Fatalf("unexpected token form: %v", r.Form)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "temporary-token"})
		case "/v2/location/cities":
			if r.Method != http.MethodGet || r.URL.Query().Get("size") != "1" || r.Header.Get("Authorization") != "Bearer temporary-token" {
				t.Fatalf("unexpected cities request: method=%s query=%s authorization=%q", r.Method, r.URL.RawQuery, r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total": 0, "items": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := cdekHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","client_secret":"secret-1"}`)); err != nil {
		t.Fatalf("CDEK probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1"}`)); err == nil {
		t.Fatal("malformed CDEK credentials accepted")
	}
}

func TestDellinCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v4/auth/login.json" || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected Деловые Линии request: method=%s path=%s content-type=%q", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var request struct {
			AppKey string `json:"appkey"`
			PAT    string `json:"pat"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.AppKey != "app-1" || request.PAT != "pat-1" {
			t.Fatalf("unexpected Деловые Линии body")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]any{"status": 200}, "data": map[string]any{"sessionID": "session-only"}})
	}))
	defer server.Close()
	transport := dellinHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"appkey":"app-1","pat":"pat-1"}`)); err != nil {
		t.Fatalf("Деловые Линии probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"appkey":"app-1"}`)); err == nil {
		t.Fatal("malformed Деловые Линии credentials accepted")
	}
}

func TestOzonDeliveryCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/warehouse/list" || r.Method != http.MethodPost || r.Header.Get("Client-Id") != "client-1" || r.Header.Get("Api-Key") != "key-1" {
			t.Fatalf("unexpected Ozon Delivery request: method=%s path=%s client=%q api-key=%q", r.Method, r.URL.Path, r.Header.Get("Client-Id"), r.Header.Get("Api-Key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"warehouses": []any{}, "cursor": ""})
	}))
	defer server.Close()
	transport := ozonDeliveryHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1"}`)); err != nil {
		t.Fatalf("Ozon Delivery probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1"}`)); err == nil {
		t.Fatal("malformed Ozon Delivery credentials accepted")
	}
}

func TestRussianPostCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/1.0/settings" || r.Method != http.MethodGet || r.Header.Get("Authorization") != "AccessToken token-1" || r.Header.Get("X-User-Authorization") != "Basic dXNlcjpwYXNz" {
			t.Fatalf("unexpected Почта России request: method=%s path=%s authorization=%q user-authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-User-Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"user": "synthetic"})
	}))
	defer server.Close()
	transport := pochtarussiaHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz"}`)); err != nil {
		t.Fatalf("Почта России probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"token":"token-1"}`)); err == nil {
		t.Fatal("malformed Почта России credentials accepted")
	}
	if err := transport.Ping(context.Background(), []byte(`{"token":"token-1","key":"dXNlcjpwYXNz","unexpected":"value"}`)); err == nil {
		t.Fatal("unknown Почта России credential field accepted")
	}
}

func TestOzonPayCredentialProbe(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/product/list" || r.Method != http.MethodPost || r.Header.Get("Client-Id") != "client-1" || r.Header.Get("Api-Key") != "key-1" {
			t.Fatalf("unexpected Ozon Pay request: method=%s path=%s client=%q api-key=%q", r.Method, r.URL.Path, r.Header.Get("Client-Id"), r.Header.Get("Api-Key"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"items": []any{}, "total": 0, "last_id": ""}})
	}))
	defer server.Close()
	transport := ozonPayHTTP{h: testTLSTransport(t, server)}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1"}`)); err != nil {
		t.Fatalf("Ozon Pay probe failed: %v", err)
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1","unexpected":"value"}`)); err == nil {
		t.Fatal("unknown Ozon Pay credential field accepted")
	}
	if err := transport.Ping(context.Background(), []byte(`{"client_id":"client-1","api_key":"key-1"}{}`)); err == nil {
		t.Fatal("trailing Ozon Pay credential JSON accepted")
	}
}
