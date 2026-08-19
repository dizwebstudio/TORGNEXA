package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"
)

func TestNewJSONLoggerRedactsSensitiveAttributes(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, Options{Level: "info", Format: "json"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	logger.With("client_secret", "hidden").Info("started\nforged",
		"access-token", "hidden-too",
		"token_count", 3,
		"token", "hidden-token",
		"clientCredentials", "hidden-credentials",
		"authorization_header", "hidden-authorization",
		"password_hash", "hidden-password",
		"data_matrix_verification_code", "hidden-code",
		"signingMaterial", "hidden-signing-material",
		slog.Group("request", "Authorization", "Bearer hidden", "method", "GET"),
		slog.Any("payload", map[string]any{"secret": "hidden-map"}),
		slog.Any("headers", http.Header{"Authorization": []string{"hidden-header"}}),
		slog.Any("error", errors.New("hidden-error")),
	)
	logger.WithGroup("credentials").Info("group", "value", "hidden-group-value")

	var entry map[string]any
	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("log records = %d, want 2", len(lines))
	}
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatalf("decode log: %v; output = %q", err, output.String())
	}
	if entry["client_secret"] != redactedValue || entry["access-token"] != redactedValue {
		t.Fatalf("top-level secrets were not redacted: %#v", entry)
	}
	if entry["token_count"] != float64(3) {
		t.Fatalf("non-sensitive metric was redacted: %#v", entry["token_count"])
	}
	request, ok := entry["request"].(map[string]any)
	if !ok || request["Authorization"] != redactedValue || request["method"] != "GET" {
		t.Fatalf("group redaction failed: %#v", entry["request"])
	}
	if strings.Contains(output.String(), "hidden") {
		t.Fatalf("secret value leaked: %q", output.String())
	}
	if entry["payload"] != redactedComplexValue || entry["headers"] != redactedComplexValue {
		t.Fatalf("complex values were not redacted: %#v", entry)
	}
	var groupEntry map[string]any
	if err := json.Unmarshal(lines[1], &groupEntry); err != nil {
		t.Fatalf("decode grouped log: %v", err)
	}
	credentials, ok := groupEntry["credentials"].(map[string]any)
	if !ok || credentials["value"] != redactedValue {
		t.Fatalf("sensitive WithGroup value leaked: %#v", groupEntry)
	}
	timestamp, ok := entry["time"].(string)
	if !ok || !strings.HasSuffix(timestamp, "Z") {
		t.Fatalf("timestamp is not UTC: %#v", entry["time"])
	}
}

func TestNewJSONLoggerRedactsPIIByFieldAndValue(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, Options{Level: "info", Format: "json"})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("privacy",
		"customer_email", "synthetic.person@example.invalid",
		"full_name", "Synthetic Person",
		"client_ip", "203.0.113.42",
		"unknown_email_value", "other.person@example.invalid",
		"order_id", "order-42",
	)
	text := output.String()
	for _, forbidden := range []string{"synthetic.person@example.invalid", "Synthetic Person", "203.0.113.42", "other.person@example.invalid"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("PII leaked in log: %q", text)
		}
	}
	if !strings.Contains(text, "[REDACTED_PII]") || !strings.Contains(text, "order-42") {
		t.Fatalf("unexpected privacy log output: %q", text)
	}
}

func TestNewHonorsMinimumLevel(t *testing.T) {
	var output bytes.Buffer
	logger, err := New(&output, Options{Level: "warn", Format: "text"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger.Info("ignored")
	logger.Warn("included")
	if strings.Contains(output.String(), "ignored") || !strings.Contains(output.String(), "included") {
		t.Fatalf("unexpected output: %q", output.String())
	}
}

func TestNewPreservesNamedScalars(t *testing.T) {
	type serviceName string
	type attemptCount int
	var output bytes.Buffer
	logger, err := New(&output, Options{Level: "info", Format: "json"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	logger.Info("typed", "service", serviceName("api"), "attempt", attemptCount(2))
	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode log: %v", err)
	}
	if entry["service"] != "api" || entry["attempt"] != float64(2) {
		t.Fatalf("named scalar fields were not preserved: %#v", entry)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name   string
		output *bytes.Buffer
		opts   Options
	}{
		{name: "output", opts: Options{Level: "info", Format: "json"}},
		{name: "level", output: &bytes.Buffer{}, opts: Options{Level: "trace", Format: "json"}},
		{name: "format", output: &bytes.Buffer{}, opts: Options{Level: "info", Format: "binary"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := New(tt.output, tt.opts); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
}
