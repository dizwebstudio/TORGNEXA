package api

import (
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func oidcConfigForEgressTest(environment config.Environment, issuer, userinfo string) config.Config {
	return config.Config{Environment: environment, OIDC: config.OIDC{
		Issuer: issuer, UserInfoURL: userinfo, ClientID: "client-id", RequestTimeout: time.Second,
	}}
}

func TestValidateOIDCConfigRequiresBoundedUserInfoHost(t *testing.T) {
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
}
