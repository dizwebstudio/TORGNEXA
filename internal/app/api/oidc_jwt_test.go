package api

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func TestJWKSTTLBoundsCacheControlWithoutOverflow(t *testing.T) {
	tests := []struct {
		cacheControl string
		want         time.Duration
	}{
		{want: defaultJWKSTTL},
		{cacheControl: "public, max-age=0", want: minimumJWKSTTL},
		{cacheControl: "max-age=30", want: minimumJWKSTTL},
		{cacheControl: "max-age=3600", want: maximumJWKSTTL},
		{cacheControl: "max-age=2147483647", want: maximumJWKSTTL},
		{cacheControl: "max-age=invalid", want: defaultJWKSTTL},
	}
	for _, testCase := range tests {
		if got := jwksTTL(testCase.cacheControl); got != testCase.want {
			t.Fatalf("jwksTTL(%q)=%s want %s", testCase.cacheControl, got, testCase.want)
		}
	}
}

func TestOIDCLocalJWTValidationAndJWKSRotation(t *testing.T) {
	first, err := a02SigningKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	currentKey := atomic.Pointer[rsa.PrivateKey]{}
	currentKey.Store(first)
	var currentKeyID atomic.Value
	currentKeyID.Store("key-1")
	var unavailable atomic.Bool
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=30")
		_ = json.NewEncoder(w).Encode(oidcJWKDocument{Keys: []oidcJWK{rsaJWK(currentKey.Load(), currentKeyID.Load().(string))}})
	}))
	defer server.Close()

	now := time.Now().UTC().Truncate(time.Second)
	metrics := &oidcHotPathRecorder{}
	cache := newIssuerJWKSCache(server.Client(), server.URL, "", metrics)
	cache.now = func() time.Time { return now }
	cfg := config.OIDC{Issuer: server.URL + "/issuer", JWKSURL: server.URL, ClientID: "web-client", Audience: "api-audience"}
	verifier := &oidcJWTVerifier{config: cfg, cache: cache, now: func() time.Time { return now }}
	claims := oidcClaims{Issuer: cfg.Issuer, Subject: "subject-1", Authorized: cfg.ClientID, Audience: oidcAudience{cfg.Audience}, IssuedAt: now.Add(-time.Minute).Unix(), NotBefore: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix()}

	firstToken := signedOIDCToken(t, first, "key-1", claims)
	if _, err := verifier.verify(t.Context(), firstToken); err != nil {
		t.Fatal("first signed token rejected", err)
	}
	if _, err := verifier.verify(t.Context(), firstToken); err != nil || calls.Load() != 1 {
		t.Fatalf("fresh JWKS cache missed: err=%v calls=%d", err, calls.Load())
	}

	// A changed key under the same kid is discovered after signature failure;
	// the one-second refetch floor prevents attacker-controlled fetch storms.
	currentKey.Store(second)
	now = now.Add(2 * time.Second)
	secondToken := signedOIDCToken(t, second, "key-1", claims)
	if _, err := verifier.verify(t.Context(), secondToken); err != nil || calls.Load() != 2 {
		t.Fatalf("same-kid rotation failed: err=%v calls=%d", err, calls.Load())
	}

	// Once cached, issuer keys remain locally usable during a bounded provider
	// outage. An expired key causes one refresh attempt, then stale-if-error use.
	unavailable.Store(true)
	now = now.Add(minimumJWKSTTL + time.Second)
	if _, err := verifier.verify(t.Context(), secondToken); err != nil || calls.Load() != 3 {
		t.Fatalf("bounded stale JWKS use failed: err=%v calls=%d", err, calls.Load())
	}
	snapshot := metrics.snapshot()
	if snapshot.JWKSHTTPCalls != 3 || snapshot.JWKSCacheHits == 0 || snapshot.JWKSStaleUses != 1 {
		t.Fatalf("JWKS metrics=%+v", snapshot)
	}
}

func TestOIDCLocalJWTRejectsInvalidAuthorizationClaimsAndSignature(t *testing.T) {
	key, err := a02SigningKey()
	if err != nil {
		t.Fatal(err)
	}
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(oidcJWKDocument{Keys: []oidcJWK{rsaJWK(key, "key-1")}})
	}))
	defer server.Close()
	now := time.Now().UTC().Truncate(time.Second)
	cfg := config.OIDC{Issuer: server.URL + "/issuer", JWKSURL: server.URL, ClientID: "web-client", Audience: "api-audience"}
	base := oidcClaims{Issuer: cfg.Issuer, Subject: "subject-1", Authorized: cfg.ClientID, Audience: oidcAudience{cfg.Audience}, IssuedAt: now.Add(-time.Minute).Unix(), NotBefore: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(time.Hour).Unix()}

	tests := []struct {
		name   string
		change func(*oidcClaims)
		key    *rsa.PrivateKey
	}{
		{name: "issuer", change: func(claims *oidcClaims) { claims.Issuer = "https://other.example.test" }, key: key},
		{name: "audience", change: func(claims *oidcClaims) { claims.Audience = oidcAudience{"other-api"} }, key: key},
		{name: "authorized_party", change: func(claims *oidcClaims) { claims.Authorized = "other-client" }, key: key},
		{name: "expired", change: func(claims *oidcClaims) { claims.ExpiresAt = now.Add(-time.Minute).Unix() }, key: key},
		{name: "not_before", change: func(claims *oidcClaims) { claims.NotBefore = now.Add(time.Minute).Unix() }, key: key},
		{name: "subject", change: func(claims *oidcClaims) { claims.Subject = "" }, key: key},
		{name: "signature", change: func(*oidcClaims) {}, key: other},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			claims := base
			testCase.change(&claims)
			metrics := &oidcHotPathRecorder{}
			verifier := &oidcJWTVerifier{config: cfg, cache: newIssuerJWKSCache(server.Client(), server.URL, "", metrics), now: func() time.Time { return now }}
			token := signedOIDCToken(t, testCase.key, "key-1", claims)
			if _, err := verifier.verify(t.Context(), token); err != errOIDCJWTInvalid {
				t.Fatalf("invalid token error=%v", err)
			}
		})
	}
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(base)
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
	verifier := &oidcJWTVerifier{config: cfg, cache: newIssuerJWKSCache(server.Client(), server.URL, "", &oidcHotPathRecorder{}), now: func() time.Time { return now }}
	if _, err := verifier.verify(t.Context(), unsigned); err != errOIDCJWTInvalid {
		t.Fatalf("unsigned decoded payload was accepted: %v", err)
	}
}
