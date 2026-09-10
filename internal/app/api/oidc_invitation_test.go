package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
	unauthenticated                    bool
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
		{name: "string_verification", email: a02InvitedEmail, verification: `"true"`, unauthenticated: true},
		{name: "numeric_verification", email: a02InvitedEmail, verification: "1", unauthenticated: true},
		{name: "different_userinfo_subject", email: a02InvitedEmail, verification: "true", subject: "another-subject", unauthenticated: true},
	}
}

type a02OIDCFixture struct {
	authenticator *oidcAuthenticator
	token         string
	info          atomic.Value
}

// A local TLS UserInfo fixture accepts exactly one synthetic credential. It
// models the trusted provider response, not JWT signature validation or an IdP.
// The token always claims email_verified=true to catch unsafe token fallback.
func newA02OIDCFixture(t *testing.T, scope tenancy.Scope, sessions securitysettings.Store, emailCase a02EmailCase) *a02OIDCFixture {
	t.Helper()
	fixture := &a02OIDCFixture{}
	fixture.setInfo(t, emailCase)
	var acceptedToken atomic.Value
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/userinfo" || r.Header.Get("Authorization") != acceptedToken.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture.info.Load().([]byte))
	}))
	t.Cleanup(server.Close)
	claims := oidcClaims{
		Issuer: server.URL, Subject: a02Subject, Authorized: "synthetic-client",
		ExpiresAt: time.Now().Add(time.Hour).Unix(), IssuedAt: time.Now().Add(-time.Minute).Unix(),
		SessionID: "synthetic-session", Email: a02InvitedEmail,
		OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(),
	}
	claims.RealmAccess.Roles = []string{"admin"}
	raw, err := json.Marshal(struct {
		oidcClaims
		EmailVerified bool `json:"email_verified"`
	}{claims, true})
	if err != nil {
		t.Fatal(err)
	}
	fixture.token = "synthetic." + base64.RawURLEncoding.EncodeToString(raw) + ".synthetic"
	acceptedToken.Store("Bearer " + fixture.token)
	cfg := config.OIDC{Issuer: server.URL, UserInfoURL: server.URL + "/userinfo", ClientID: "synthetic-client", RequestTimeout: 2 * time.Second}
	boundary, err := validateOIDCConfig(config.Config{Environment: config.EnvironmentProduction, OIDC: cfg})
	if err != nil {
		t.Fatal(err)
	}
	client := server.Client() // Trust only the fixture's TLS certificate.
	client.Timeout = cfg.RequestTimeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	fixture.authenticator = &oidcAuthenticator{cfg: cfg, userinfoHost: boundary.issuerHost, environment: config.EnvironmentProduction, client: client, sessions: sessions}
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
			if emailCase.unauthenticated {
				if !errors.Is(err, ErrUnauthenticated) || principal.VerifiedEmail != "" {
					t.Fatal("invalid UserInfo produced authenticated invitation evidence")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if principal.VerifiedEmail != emailCase.wantVerified || principal.Profile.Email != emailCase.wantProfile || principal.Email != emailCase.wantProfile {
				t.Fatal("profile email and invitation ownership proof were not kept separate")
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
