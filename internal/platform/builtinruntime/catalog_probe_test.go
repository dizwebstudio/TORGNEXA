package builtinruntime

import (
	"encoding/json"
	"testing"
)

func TestProbeValuesUseStructuredCredentialsWithoutLeakingUnknownFields(t *testing.T) {
	values := probeValues([]byte(`{"client_id":"client-1","api_key":"key-1"}`))
	if values["client_id"] != "client-1" || values["api_key"] != "key-1" {
		t.Fatalf("structured credential placeholders were not resolved: %#v", values)
	}
	if values["secret"] == "" || values["access_token"] == "" {
		t.Fatal("raw credential aliases must remain available to connector probe")
	}
}

func TestProbeValuesUseLinesForCompoundCredentials(t *testing.T) {
	values := probeValues([]byte("token-one\nkey-two"))
	if values["line1"] != "token-one" || values["line2"] != "key-two" {
		t.Fatalf("line placeholders were not resolved: %#v", values)
	}
	if values["authorization"] != "token-one" || values["session_id"] != "key-two" {
		t.Fatalf("manifest credential aliases were not resolved: %#v", values)
	}
}

func TestExpandProbeValueFailsClosedOnUnknownPlaceholder(t *testing.T) {
	value, ok := expandProbeValue("Bearer ${missing}", map[string]string{"secret": "safe"})
	if ok || value != "Bearer ${missing}" {
		t.Fatalf("unknown placeholder was accepted: %q, %v", value, ok)
	}
}

func TestProbeStatusOKHonorsExplicitAllowlist(t *testing.T) {
	if !probeStatusOK(204, []int{204}) || probeStatusOK(200, []int{204}) || !probeStatusOK(200, nil) {
		t.Fatal("probe status allowlist is not exact")
	}
}

func TestProbeConfigTemplateRemainsStrictJSON(t *testing.T) {
	var value struct {
		ProbeURL string `json:"probe_url"`
	}
	if decodeStrict(json.RawMessage(`{"probe_url":"https://example.com/health"}`), &value) != nil || value.ProbeURL == "" {
		t.Fatal("valid probe configuration was rejected")
	}
	if decodeStrict(json.RawMessage(`{"probe_url":"https://example.com/health","token":"secret"}`), &value) == nil {
		t.Fatal("unknown probe configuration field was accepted")
	}
}
