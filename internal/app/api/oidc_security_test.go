package api

import (
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func oidcConfigForEgressTest(environment config.Environment, issuer, userinfo string) config.Config {
	return config.Config{Environment: environment, OIDC: config.OIDC{
		Issuer: issuer, JWKSURL: strings.Replace(userinfo, "/userinfo", "/jwks", 1), UserInfoURL: userinfo, ClientID: "client-id", Audience: "api-audience", RequestTimeout: time.Second,
	}}
}

func TestValidateOIDCConfigRequiresIssuerBoundIdentityEndpoints(t *testing.T) {
	if _, err := validateOIDCConfig(oidcConfigForEgressTest(config.EnvironmentProduction, "https://id.example.test/realm", "https://id.example.test/realm/userinfo")); err != nil {
		t.Fatalf("same-host OIDC config rejected: %v", err)
	}
	if _, err := validateOIDCConfig(oidcConfigForEgressTest(config.EnvironmentProduction, "https://id.example.test/realm", "https://169.254.169.254/latest")); err == nil {
		t.Fatal("production OIDC config accepted an unbound userinfo host")
	}
	if _, err := validateOIDCConfig(oidcConfigForEgressTest(config.EnvironmentDevelopment, "http://127.0.0.1:8081/realm", "http://keycloak:8080/realm/userinfo")); err != nil {
		t.Fatalf("development backchannel rejected: %v", err)
	}
	if _, err := validateOIDCConfig(oidcConfigForEgressTest(config.EnvironmentDevelopment, "http://127.0.0.1:8081/realm", "http://attacker.test/realm/userinfo")); err == nil {
		t.Fatal("development OIDC config accepted an unbound userinfo host")
	}
	if _, err := validateOIDCConfig(oidcConfigForEgressTest(config.EnvironmentDevelopment, "ftp://127.0.0.1:8081/realm", "ftp://127.0.0.1:8081/realm/userinfo")); err == nil {
		t.Fatal("development OIDC config accepted a non-HTTP scheme")
	}
	maliciousJWKS := oidcConfigForEgressTest(config.EnvironmentProduction, "https://id.example.test/realm", "https://id.example.test/realm/userinfo")
	maliciousJWKS.OIDC.JWKSURL = "https://169.254.169.254/latest"
	if _, err := validateOIDCConfig(maliciousJWKS); err == nil {
		t.Fatal("production OIDC config accepted an unbound JWKS host")
	}
	maliciousJWKS.OIDC.JWKSURL = "https://id.example.test:4444/realm/jwks"
	if _, err := validateOIDCConfig(maliciousJWKS); err == nil {
		t.Fatal("production OIDC config accepted an unbound JWKS port")
	}
}
