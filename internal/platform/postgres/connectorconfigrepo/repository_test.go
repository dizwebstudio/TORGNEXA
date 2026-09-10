package connectorconfigrepo

import (
	"encoding/json"
	"testing"
)

func TestValidateConfigAcceptsNonSecretProviderSettings(t *testing.T) {
	for _, raw := range []string{
		`{"business_id":123,"campaign_id":456,"warehouses":[{"id":1,"name":"main"}]}`,
		`{"store_host":"shop.example.com","base_path":"/wp-json/wc/v3","store_currency":"RUB"}`,
		`{"host":"erp.example.com","catalog":{"resource":"Catalog_Products"}}`,
		`{"chat_id":-123,"webhook_secret_reference":"sec:v1:0123456789abcdef0123456789abcdef"}`,
	} {
		if err := validateConfig(json.RawMessage(raw)); err != nil {
			t.Fatalf("valid config rejected: %s: %v", raw, err)
		}
	}
}

func TestValidateConfigRejectsSecretsRecursively(t *testing.T) {
	for _, raw := range []string{
		`{"api_key":"x"}`,
		`{"nested":{"access-token":"x"}}`,
		`{"items":[{"private_key":"x"}]}`,
		`{"authorization":"Bearer x"}`,
		`{"webhook_secret_reference":"plaintext-secret"}`,
		`{"webhook_secret_reference":null}`,
		`{"webhook_secret_reference":{"token":"x"}}`,
		`{"nested":{"webhook_secret_reference":"sec:v1:0123456789abcdef0123456789abcdef"}}`,
		`{"webhook_secret_reference":"sec:v1:0123456789abcdef0123456789abcdef","token":"x"}`,
		`[]`, `{}`, `null`,
	} {
		if err := validateConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsafe config accepted: %s", raw)
		}
	}
}
