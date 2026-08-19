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
		`[]`, `{}`, `null`,
	} {
		if err := validateConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("unsafe config accepted: %s", raw)
		}
	}
}
