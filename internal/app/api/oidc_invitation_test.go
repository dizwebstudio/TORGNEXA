package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

const a02Subject = "synthetic-invitee"
const a02InvitedEmail = "invited-admin@example.test"

type a02EmailCase struct {
	name, email, verification, subject string
	wantVerified, wantProfile          string
	discardedUserInfo                  bool
}

func a02EmailCases() []a02EmailCase {
	return []a02EmailCase{
		{name: "verified", email: a02InvitedEmail, verification: "true", wantVerified: a02InvitedEmail, wantProfile: a02InvitedEmail},
		{name: "verified_normalized", email: "  INVITED-ADMIN@EXAMPLE.TEST  ", verification: "true", wantVerified: a02InvitedEmail, wantProfile: a02InvitedEmail},
		{name: "unverified", email: a02InvitedEmail, verification: "false", wantProfile: a02InvitedEmail},
		{name: "missing_verification", email: a02InvitedEmail, wantProfile: a02InvitedEmail},
		{name: "null_verification", email: a02InvitedEmail, verification: "null", wantProfile: a02InvitedEmail},
		{name: "token_email_only", verification: "true", wantProfile: a02InvitedEmail},
		{name: "different_verified_address", email: "own-address@example.test", verification: "true", wantVerified: "own-address@example.test", wantProfile: "own-address@example.test"},
		{name: "malformed_address", email: "not-an-email", verification: "true", wantProfile: a02InvitedEmail},
		{name: "display_address", email: "Somebody <invited-admin@example.test>", verification: "true", wantProfile: "somebody <invited-admin@example.test>"},
		{name: "string_verification", email: a02InvitedEmail, verification: `"true"`, wantProfile: a02InvitedEmail, discardedUserInfo: true},
		{name: "numeric_verification", email: a02InvitedEmail, verification: "1", wantProfile: a02InvitedEmail, discardedUserInfo: true},
		{name: "different_userinfo_subject", email: a02InvitedEmail, verification: "true", subject: "another-subject", wantProfile: a02InvitedEmail, discardedUserInfo: true},
	}
}

type a02OIDCFixture struct {
	authenticator *oidcAuthenticator
	token         string
	info          atomic.Value
	jwksCalls     atomic.Int64
	userinfoCalls atomic.Int64
	providerDown  atomic.Bool
}

var (
	a02KeyOnce sync.Once
	a02Key     *rsa.PrivateKey
	a02KeyErr  error
)

func a02SigningKey() (*rsa.PrivateKey, error) {
	a02KeyOnce.Do(func() { a02Key, a02KeyErr = rsa.GenerateKey(rand.Reader, 2048) })
	return a02Key, a02KeyErr
}

func signedOIDCToken(t *testing.T, key *rsa.PrivateKey, keyID string, claims oidcClaims) string {
	t.Helper()
	header, err := json.Marshal(oidcJWTHeader{Algorithm: "RS256", KeyID: keyID, Type: "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(struct {
		oidcClaims
		EmailVerified bool `json:"email_verified"`
	}{claims, true})
	if err != nil {
		t.Fatal(err)
	}
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return input + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func rsaJWK(key *rsa.PrivateKey, keyID string) oidcJWK {
	return oidcJWK{KeyType: "RSA", KeyID: keyID, Use: "sig", Algorithm: "RS256", Modulus: base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()), Exponent: "AQAB"}
}

// A local TLS issuer exposes bounded JWKS and UserInfo fixtures. The access
// token is genuinely RS256-signed and always claims email_verified=true to
// catch unsafe token fallback for invitation ownership.
func newA02OIDCFixture(t *testing.T, scope tenancy.Scope, sessions securitysettings.Store, emailCase a02EmailCase) *a02OIDCFixture {
	t.Helper()
	fixture := &a02OIDCFixture{}
	fixture.setInfo(t, emailCase)
	var acceptedToken atomic.Value
	key, err := a02SigningKey()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fixture.providerDown.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/jwks" {
			fixture.jwksCalls.Add(1)
			w.Header().Set("Cache-Control", "public, max-age=300")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(oidcJWKDocument{Keys: []oidcJWK{rsaJWK(key, "synthetic-key")}})
			return
		}
		if r.URL.Path != "/userinfo" || r.Header.Get("Authorization") != acceptedToken.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fixture.userinfoCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture.info.Load().([]byte))
	}))
	t.Cleanup(server.Close)
	claims := oidcClaims{
		Issuer: server.URL, Subject: a02Subject, Authorized: "synthetic-client",
		Audience:  oidcAudience{"synthetic-api"},
		ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix(),
		SessionID: "synthetic-session", Email: a02InvitedEmail,
		OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(),
	}
	claims.RealmAccess.Roles = []string{"admin"}
	fixture.token = signedOIDCToken(t, key, "synthetic-key", claims)
	acceptedToken.Store("Bearer " + fixture.token)
	cfg := config.OIDC{Issuer: server.URL, JWKSURL: server.URL + "/jwks", UserInfoURL: server.URL + "/userinfo", ClientID: "synthetic-client", Audience: "synthetic-api", RequestTimeout: 2 * time.Second}
	boundary, err := validateOIDCConfig(config.Config{Environment: config.EnvironmentProduction, OIDC: cfg})
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client() // Trust only the fixture's TLS certificate.
	client.Timeout = cfg.RequestTimeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	metrics := &oidcHotPathRecorder{}
	fixture.authenticator = &oidcAuthenticator{cfg: cfg, userinfoHost: boundary.issuerHost, environment: config.EnvironmentProduction, client: client, sessions: sessions, metrics: metrics}
	fixture.authenticator.verifier = &oidcJWTVerifier{config: cfg, cache: newIssuerJWKSCache(client, cfg.JWKSURL, boundary.issuerHost, metrics), now: time.Now}
	fixture.authenticator.profiles = newOIDCProfileCache(metrics)
	return fixture
}

func (fixture *a02OIDCFixture) setInfo(t *testing.T, emailCase a02EmailCase) {
	t.Helper()
	subject := emailCase.subject
	if subject == "" {
		subject = a02Subject
	}
	raw, err := json.Marshal(struct {
		Subject       string          `json:"sub"`
		Email         string          `json:"email,omitempty"`
		EmailVerified json.RawMessage `json:"email_verified,omitempty"`
	}{subject, emailCase.email, json.RawMessage(emailCase.verification)})
	if err != nil {
		t.Fatal(err)
	}
	fixture.info.Store(raw)
	if fixture.authenticator != nil {
		// Test-only explicit hydration boundary: a real runtime refreshes after
		// the bounded profile TTL or a new process instance.
		fixture.authenticator.profiles = newOIDCProfileCache(fixture.authenticator.metrics)
	}
}

func (fixture *a02OIDCFixture) request() *http.Request {
	request := httptest.NewRequest(http.MethodGet, "https://api.example.test/api/v1/private", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.token)
	request.RemoteAddr = "127.0.0.1:4321"
	return request
}

func TestA02OIDCInvitationEmailProof(t *testing.T) {
	for _, emailCase := range a02EmailCases() {
		t.Run(emailCase.name, func(t *testing.T) {
			fixture := newA02OIDCFixture(t, validTestScope(t), &settingsSecurityStoreStub{}, emailCase)
			principal, err := fixture.authenticator.Authenticate(t.Context(), fixture.request())
			if err != nil {
				t.Fatal(err)
			}
			if principal.VerifiedEmail != emailCase.wantVerified || principal.Profile.Email != emailCase.wantProfile || principal.Email != emailCase.wantProfile {
				t.Fatal("profile email and invitation ownership proof were not kept separate")
			}
			if emailCase.discardedUserInfo && principal.VerifiedEmail != "" {
				t.Fatal("discarded UserInfo produced invitation evidence")
			}
			projection, err := json.Marshal(principal)
			if err != nil || strings.Contains(string(projection), "VerifiedEmail") || strings.Contains(string(projection), fixture.token) {
				t.Fatal("runtime invitation proof/token escaped into JSON")
			}
		})
	}
}

func TestA02InvitationContractRequiresUserInfoOwnership(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/openapi/torgnexa-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, operation, ok := strings.Cut(string(raw), "operationId: inviteWorkspaceMember\n")
	if !ok {
		t.Fatal("invitation operation missing")
	}
	operation, _, _ = strings.Cut(operation, "  /settings/members/{member_id}:")
	for _, required := range []string{"email_verified=true", "UserInfo response for the same subject", "invitation remains unchanged", "'403':"} {
		if !strings.Contains(operation, required) {
			t.Errorf("invitation contract missing %q", required)
		}
	}
}
