package api

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

type authnStub struct {
	principal Principal
	err       error
}

func (a authnStub) Authenticate(context.Context, *http.Request) (Principal, error) {
	return a.principal, a.err
}

type tenantStub struct {
	scope tenancy.Scope
	err   error
}

func (t tenantStub) ResolveTenant(context.Context, Principal, *http.Request) (tenancy.Scope, error) {
	return t.scope, t.err
}

type authzStub struct{ err error }

func (a authzStub) Authorize(context.Context, Principal, tenancy.Scope, string) error { return a.err }

func edgeTestConfig() securityedge.Config {
	return securityedge.Config{
		TrustedProxyCIDRs: []string{"127.0.0.1/32"},
		AdminCIDRs:        []string{"127.0.0.1/32"},
		AllowedOrigins:    []string{"https://console.example.test"},
		MaxRequestBytes:   1 << 20,
		MaxUploadBytes:    1 << 19,
		RatePerMinute:     100,
		HSTSSeconds:       31536000,
	}
}

func validTestScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope("018f1c8a-7b3c-7def-8000-000000000001", "018f1c8a-7b3c-7def-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func testSecurityLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestProductionCompositionRejectsPrivateRouteWithoutSecurityDependencies(t *testing.T) {
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "private.read", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	if _, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, []ProtectedRoute{route}, nil); !errors.Is(err, ErrSecurityCompositionInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestProductionCompositionAuthenticatesResolvesTenantThenAuthorizes(t *testing.T) {
	scope := validTestScope(t)
	principal := Principal{Issuer: "https://id.example.test", Subject: "user-1", Roles: []string{"reader"}}
	called := false
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "private.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPrincipal, ok := PrincipalFromContext(r.Context())
		if !ok || gotPrincipal.Subject != principal.Subject {
			t.Fatalf("principal missing: %+v %v", gotPrincipal, ok)
		}
		gotScope, ok := ScopeFromContext(r.Context())
		if !ok || gotScope.OrganizationID() != scope.OrganizationID() || gotScope.WorkspaceID() != scope.WorkspaceID() {
			t.Fatalf("scope missing")
		}
		if _, ok := ClientIPFromContext(r.Context()); !ok {
			t.Fatalf("client IP missing")
		}
		called = true
		w.WriteHeader(http.StatusNoContent)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.test/api/v1/private", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v body=%s", rr.Code, called, rr.Body.String())
	}
	if rr.Header().Get("Strict-Transport-Security") == "" || rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("security headers missing: %v", rr.Header())
	}
}

func TestProductionCompositionProtectsParameterizedPrefixRoute(t *testing.T) {
	scope := validTestScope(t)
	principal := Principal{Issuer: "https://id.example.test", Subject: "user-1"}
	called := false
	route := ProtectedRoute{
		Method:     http.MethodPost,
		Path:       "/api/v1/notifications/",
		PathPrefix: true,
		Permission: "notifications.read",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/api/v1/notifications/ntf-1/read", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent || !called {
		t.Fatalf("status=%d called=%v body=%s", rr.Code, called, rr.Body.String())
	}
}

func TestProductionCompositionFailsClosedAtEachAuthorizationStage(t *testing.T) {
	scope := validTestScope(t)
	principal := Principal{Issuer: "issuer", Subject: "subject"}
	cases := []struct {
		name   string
		authn  Authenticator
		tenant TenantResolver
		authz  Authorizer
		want   int
	}{
		{"authn", authnStub{err: ErrUnauthenticated}, tenantStub{scope: scope}, authzStub{}, http.StatusUnauthorized},
		{"tenant", authnStub{principal: principal}, tenantStub{err: ErrUnauthorized}, authzStub{}, http.StatusForbidden},
		{"authz", authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{err: ErrUnauthorized}, http.StatusForbidden},
	}
	route := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "private.read", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not run") })}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), tc.authn, tc.tenant, tc.authz, []ProtectedRoute{route}, nil)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "https://api.example.test/api/v1/private", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code != tc.want {
				t.Fatalf("status=%d want=%d", rr.Code, tc.want)
			}
		})
	}
}

func TestProductionCompositionRejectsSpoofedForwardingFromUntrustedPeer(t *testing.T) {
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.test"+HealthPath, nil)
	req.RemoteAddr = "203.0.113.8:1234"
	req.Header.Set("X-Forwarded-For", "198.51.100.1")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestProductionCompositionRejectsDisallowedBrowserOrigin(t *testing.T) {
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "https://api.example.test"+HealthPath, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Origin", "https://evil.example")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestProductionCompositionBoundsRequestBodiesBeforePrivateHandler(t *testing.T) {
	cfg := edgeTestConfig()
	cfg.MaxRequestBytes = 8
	cfg.MaxUploadBytes = 8
	scope := validTestScope(t)
	principal := Principal{Issuer: "issuer", Subject: "subject"}
	route := ProtectedRoute{Method: http.MethodPost, Path: "/api/v1/private", Permission: "private.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err == nil {
			t.Fatal("oversized body read unexpectedly succeeded")
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), cfg, securityedge.NewLimiter(), authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{}, []ProtectedRoute{route}, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test/api/v1/private", strings.NewReader("0123456789"))
	req.RemoteAddr = "127.0.0.1:1234"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPublicWebhookRouteBypassesAuthAndNeverPopulatesPrincipalOrScope(t *testing.T) {
	called := false
	route := PublicWebhookRoute{Method: http.MethodPost, Path: webhookPathPrefix + "payments/yookassa/acct-1", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PrincipalFromContext(r.Context()); ok {
			t.Fatal("PublicWebhookRoute handler must never see a principal")
		}
		if _, ok := ScopeFromContext(r.Context()); ok {
			t.Fatal("PublicWebhookRoute handler must never see a tenant scope")
		}
		called = true
		w.WriteHeader(http.StatusOK)
	})}
	// authn/tenant/authz are all nil: a webhook route must not need them at all.
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, []PublicWebhookRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+webhookPathPrefix+"payments/yookassa/acct-1", strings.NewReader(`{"event":"payment.succeeded"}`))
	req.RemoteAddr = "198.51.100.9:443"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !called {
		t.Fatalf("status=%d called=%v body=%s", rr.Code, called, rr.Body.String())
	}
}

func TestPublicWebhookRoutePathMustLiveUnderWebhookPrefix(t *testing.T) {
	route := PublicWebhookRoute{Method: http.MethodPost, Path: "/api/v1/payments/webhook", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	if _, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, []PublicWebhookRoute{route}); !errors.Is(err, ErrSecurityCompositionInvalid) {
		t.Fatalf("error = %v, want ErrSecurityCompositionInvalid", err)
	}
}

func TestPublicWebhookRouteRejectsDuplicateRegistration(t *testing.T) {
	route := PublicWebhookRoute{Method: http.MethodPost, Path: webhookPathPrefix + "payments/yookassa/acct-1", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	if _, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, []PublicWebhookRoute{route, route}); !errors.Is(err, ErrSecurityCompositionInvalid) {
		t.Fatalf("error = %v, want ErrSecurityCompositionInvalid", err)
	}
}

func TestPublicWebhookRouteSupportsParameterizedPrefixDispatch(t *testing.T) {
	var gotPath string
	route := PublicWebhookRoute{Method: http.MethodPost, Path: webhookPathPrefix + "payments/", PathPrefix: true, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, []PublicWebhookRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+webhookPathPrefix+"payments/sbp/acct-2", nil)
	req.RemoteAddr = "198.51.100.9:443"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || gotPath != webhookPathPrefix+"payments/sbp/acct-2" {
		t.Fatalf("status=%d gotPath=%q", rr.Code, gotPath)
	}
}

func TestPublicWebhookRouteUnmatchedPathReturns404WithoutRunningAnyHandler(t *testing.T) {
	route := PublicWebhookRoute{Method: http.MethodPost, Path: webhookPathPrefix + "payments/yookassa/acct-1", Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler must not run for an unmatched path") })}
	handler, err := NewProductionHandler(testSecurityLogger(), edgeTestConfig(), securityedge.NewLimiter(), nil, nil, nil, nil, []PublicWebhookRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+webhookPathPrefix+"payments/yookassa/other-account", nil)
	req.RemoteAddr = "198.51.100.9:443"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestPublicWebhookRouteBoundsBodyIndependentlyOfTenantConfig(t *testing.T) {
	cfg := edgeTestConfig()
	cfg.MaxRequestBytes = 1 << 30 // deliberately huge, to prove the webhook cap is not derived from this
	cfg.MaxUploadBytes = 1 << 29
	route := PublicWebhookRoute{Method: http.MethodPost, Path: webhookPathPrefix + "payments/yookassa/acct-1", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err == nil {
			t.Fatal("oversized webhook body read unexpectedly succeeded")
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	})}
	handler, err := NewProductionHandler(testSecurityLogger(), cfg, securityedge.NewLimiter(), nil, nil, nil, nil, []PublicWebhookRoute{route})
	if err != nil {
		t.Fatal(err)
	}
	oversized := strings.Repeat("a", webhookMaxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+webhookPathPrefix+"payments/yookassa/acct-1", strings.NewReader(oversized))
	req.RemoteAddr = "198.51.100.9:443"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestPublicWebhookRouteRateLimitBudgetIsIndependentOfTenantBudget(t *testing.T) {
	cfg := edgeTestConfig()
	cfg.RatePerMinute = 1 // tenant traffic gets exactly one request per minute
	scope := validTestScope(t)
	principal := Principal{Issuer: "issuer", Subject: "subject"}
	tenantRoute := ProtectedRoute{Method: http.MethodGet, Path: "/api/v1/private", Permission: "private.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	webhookRoute := PublicWebhookRoute{Method: http.MethodPost, Path: webhookPathPrefix + "payments/yookassa/acct-1", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })}
	handler, err := NewProductionHandler(testSecurityLogger(), cfg, securityedge.NewLimiter(), authnStub{principal: principal}, tenantStub{scope: scope}, authzStub{}, []ProtectedRoute{tenantRoute}, []PublicWebhookRoute{webhookRoute})
	if err != nil {
		t.Fatal(err)
	}

	tenantReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "https://api.example.test/api/v1/private", nil)
		req.RemoteAddr = "127.0.0.1:1"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}
	webhookReq := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "https://api.example.test"+webhookPathPrefix+"payments/yookassa/acct-1", nil)
		req.RemoteAddr = "127.0.0.1:1"
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	if rr := tenantReq(); rr.Code != http.StatusNoContent {
		t.Fatalf("first tenant request status=%d", rr.Code)
	}
	if rr := tenantReq(); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("second tenant request should be rate limited: status=%d", rr.Code)
	}
	// The tenant budget is exhausted, but the webhook path uses a separate
	// budget keyed under a different limiter key and must be unaffected.
	if rr := webhookReq(); rr.Code != http.StatusOK {
		t.Fatalf("webhook request should not share the exhausted tenant budget: status=%d", rr.Code)
	}
}

func TestAPIPackageExposesOnlySecureProductionHandlerFactory(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, name, nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if strings.HasPrefix(fn.Name.Name, "New") && strings.Contains(fn.Name.Name, "Handler") && fn.Name.Name != "NewProductionHandler" {
				t.Fatalf("exported handler factory %s bypasses the production security composition root", fn.Name.Name)
			}
		}
	}
}
