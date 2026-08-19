package connectorauth

import (
	"net/url"
	"strings"
	"testing"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func TestCallbackPolicyIsExactAndDefaultsToDeny(t *testing.T) {
	policy, err := NewCallbackPolicy([]string{"https://console.example.test", "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	for _, allowed := range []string{"https://console.example.test" + CallbackPath, "http://127.0.0.1:5173" + CallbackPath} {
		if err = policy.Validate(allowed); err != nil {
			t.Fatalf("Validate(%q): %v", allowed, err)
		}
	}
	for _, denied := range []string{"https://console.example.test/oidc/callback", "https://console.example.test" + CallbackPath + "?next=/", "http://console.example.test" + CallbackPath} {
		if err = policy.Validate(denied); err == nil {
			t.Fatalf("Validate(%q) unexpectedly succeeded", denied)
		}
	}
	empty, _ := NewCallbackPolicy(nil)
	if empty.Validate("https://console.example.test"+CallbackPath) == nil {
		t.Fatal("empty policy must deny")
	}
}

func TestPKCEAuthorizationURLUsesS256AndOpaqueState(t *testing.T) {
	pending, challenge, err := NewPKCE()
	if err != nil {
		t.Fatal(err)
	}
	if pending.State == pending.CodeVerifier || len(pending.State) < 32 || len(pending.CodeVerifier) < 43 || len(challenge) != 43 {
		t.Fatalf("unexpected PKCE material lengths: state=%d verifier=%d challenge=%d", len(pending.State), len(pending.CodeVerifier), len(challenge))
	}
	configuration := sdk.OAuth2Configuration{GrantType: "authorization_code", AuthorizationURL: "https://id.example.test/authorize", TokenURL: "https://id.example.test/token", Scopes: []string{"read", "write"}, ClientAuthMethod: "client_secret_post"}
	raw, err := AuthorizationURL(configuration, "client-id", "https://console.example.test"+CallbackPath, pending.State, challenge)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(raw)
	query := parsed.Query()
	if query.Get("state") != pending.State || query.Get("code_challenge") != challenge || query.Get("code_challenge_method") != "S256" || query.Get("redirect_uri") != "https://console.example.test"+CallbackPath || query.Get("scope") != "read write" {
		t.Fatalf("authorization query = %v", query)
	}
	digest, err := StateDigest(pending.State)
	if err != nil || len(digest) != 64 || strings.Contains(digest, pending.State) {
		t.Fatalf("state digest = %q err=%v", digest, err)
	}
}

func TestCredentialTemplatesDoNotPermitSecretInURL(t *testing.T) {
	manifest := sdk.Manifest{ID: "example", Name: "Example", Family: sdk.FamilyMarketplace, Version: "1.0.0", SDKVersion: 1, Capabilities: []sdk.Capability{"products.read"}, Auth: []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "example.token", Required: true}}, RateLimit: sdk.RateLimitPolicy{MaxConcurrency: 1, RequestTimeoutMS: 1000, Retry: sdk.RetryPolicy{MaxAttempts: 1, BaseBackoffMS: 1, MaxBackoffMS: 1}}, ConnectionTest: &sdk.ConnectionTest{URL: "https://api.example.test/ping?token=${secret}", Method: "GET"}}
	if manifest.Validate() == nil {
		t.Fatal("secret-bearing URL must be rejected")
	}
	fields, err := credentialFields([]byte(`{"access_token":"opaque","username":"alice","password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	value, err := expand("Bearer ${access_token}", fields)
	if err != nil || value != "Bearer opaque" || fields["basic"] == "" {
		t.Fatalf("expanded=%q basic=%q err=%v", value, fields["basic"], err)
	}
	if _, err = expand("Bearer ${missing}", fields); err == nil {
		t.Fatal("missing credential field must fail closed")
	}
	manifest.ConnectionTest = &sdk.ConnectionTest{URL: "https://api.example.test/ping", Method: "GET", Headers: map[string]string{"Connection": "keep-alive"}}
	if manifest.Validate() == nil {
		t.Fatal("hop-by-hop probe header must be rejected")
	}
}

func TestOAuthClientParserRejectsUnknownAndUnsafeFields(t *testing.T) {
	client, err := ParseOAuthClient([]byte(`{"client_id":"id","client_secret":"secret"}`))
	if err != nil || client.ClientID != "id" {
		t.Fatalf("client=%+v err=%v", client, err)
	}
	for _, raw := range []string{`{"client_id":"id"}`, `{"client_id":"id","client_secret":"secret","token":"leak"}`, "{\"client_id\":\"id\",\"client_secret\":\"line\\nfeed\"}"} {
		if _, err = ParseOAuthClient([]byte(raw)); err == nil {
			t.Fatalf("ParseOAuthClient(%s) unexpectedly succeeded", raw)
		}
	}
}
