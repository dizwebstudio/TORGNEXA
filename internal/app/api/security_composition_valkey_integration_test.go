package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

func TestRateLimitReplicasShareBudget(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TORGNEXA_TEST_VALKEY_ADDR"))
	if address == "" {
		t.Skip("TORGNEXA_TEST_VALKEY_ADDR is not set")
	}
	valkeyConfig := securityedge.ValkeyConfig{
		Address: address, Namespace: fmt.Sprintf("torgnexa:test:api:%d", time.Now().UnixNano()),
		ConnectTimeout: time.Second, RequestTimeout: time.Second, MaxConnections: 4, MaxKeys: 100,
	}
	ctx := context.Background()
	firstLimiter, err := securityedge.NewValkeyLimiter(ctx, valkeyConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstLimiter.Close() })
	secondLimiter, err := securityedge.NewValkeyLimiter(ctx, valkeyConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondLimiter.Close() })

	edge := edgeTestConfig()
	edge.RatePerMinute = 2
	scope := validTestScope(t)
	principal := Principal{Issuer: "issuer", Subject: "principal"}
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "private.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	first, err := NewProductionHandler(testSecurityLogger(), edge, firstLimiter, authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewProductionHandler(testSecurityLogger(), edge, secondLimiter, authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	send := func(handler http.Handler) int {
		request := httptest.NewRequest(http.MethodGet, "https://api.example.test/api/v1/private", nil)
		request.RemoteAddr = "198.51.100.8:443"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response.Code
	}
	if status := send(first); status != http.StatusNoContent {
		t.Fatalf("first replica initial status=%d", status)
	}
	if status := send(second); status != http.StatusNoContent {
		t.Fatalf("second replica initial status=%d", status)
	}
	if status := send(first); status != http.StatusTooManyRequests {
		t.Fatalf("replica multiplication bypassed authenticated budget: status=%d", status)
	}
}
