package securitysettings

import (
	"context"
	"net"
	"testing"
	"time"
)

type providerResolverStub map[string][]net.IP

func (r providerResolverStub) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	values := r[host]
	items := make([]net.IPAddr, 0, len(values))
	for _, value := range values {
		items = append(items, net.IPAddr{IP: value})
	}
	return items, nil
}

func TestProviderURLPolicyIsAllowlistedAndSSRFClosed(t *testing.T) {
	policy, err := NewProviderURLPolicy([]string{"id.example.test", "private.example.test"}, []string{"https://console.example.test"}, providerResolverStub{
		"id.example.test":      {net.ParseIP("8.8.8.8")},
		"private.example.test": {net.ParseIP("10.0.0.7")},
	})
	if err != nil {
		t.Fatal(err)
	}
	draft := ProviderDraft{ID: "corporate", Protocol: "oidc", DisplayName: "Corporate ID", IssuerURL: "https://id.example.test", ClientID: "client", CallbackURL: "https://console.example.test/oidc/callback", CorrelationID: "request-1", CreatedAt: time.Now().UTC()}
	if err := policy.ValidateDraft(context.Background(), draft); err != nil {
		t.Fatalf("valid draft rejected: %v", err)
	}
	for name, mutate := range map[string]func(*ProviderDraft){
		"unlisted issuer":   func(value *ProviderDraft) { value.IssuerURL = "https://other.example.test" },
		"private address":   func(value *ProviderDraft) { value.IssuerURL = "https://private.example.test" },
		"unlisted callback": func(value *ProviderDraft) { value.CallbackURL = "https://evil.example.test/oidc/callback" },
		"issuer query":      func(value *ProviderDraft) { value.IssuerURL += "?redirect=private" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := draft
			mutate(&candidate)
			if err := policy.ValidateDraft(context.Background(), candidate); err == nil {
				t.Fatal("unsafe draft accepted")
			}
		})
	}
}

func TestProviderDraftRemainsProviderNeutral(t *testing.T) {
	policy, _ := NewProviderURLPolicy([]string{"id.example.test"}, []string{"https://console.example.test"}, providerResolverStub{"id.example.test": {net.ParseIP("8.8.8.8")}})
	for _, idpID := range []string{"external-login", "corporate", "partner-login"} {
		draft := ProviderDraft{ID: idpID, Protocol: "oidc", DisplayName: idpID, IssuerURL: "https://id.example.test", ClientID: "client", CallbackURL: "https://console.example.test/oidc/callback", CorrelationID: idpID, CreatedAt: time.Now().UTC()}
		if err := policy.ValidateDraft(context.Background(), draft); err != nil {
			t.Fatalf("identity source %s rejected: %v", idpID, err)
		}
	}
}

func TestProviderURLPolicyRejectsPlainHTTPRemoteCallback(t *testing.T) {
	if _, err := NewProviderURLPolicy([]string{"id.example.test"}, []string{"http://console.example.test"}, providerResolverStub{}); err == nil {
		t.Fatal("remote callback origin accepted without HTTPS")
	}
	if _, err := NewProviderURLPolicy([]string{"id.example.test"}, []string{"http://localhost:5173"}, providerResolverStub{}); err != nil {
		t.Fatalf("loopback development callback rejected: %v", err)
	}
}
