package audit

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	testOrganization = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWorkspace    = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	testAuditID      = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
)

func TestSanitizeSummaryRedactsCredentialsWithoutMutatingInput(t *testing.T) {
	t.Parallel()
	input := Summary{
		"oauth_token_value": "must-not-survive",
		"aws_access_key_id": "AKIA_SYNTHETIC",
		"material_blob":     "-----BEGIN ENCRYPTED PRIVATE KEY-----\nraw\n-----END ENCRYPTED PRIVATE KEY-----",
		"before": map[string]any{
			"status":        "draft",
			"Authorization": "Bearer raw-secret-token",
			"client_secret": "should-never-leak",
		},
		"after": []any{
			map[string]any{"status": "published", "api-key": "secret"},
			"Bearer another-secret",
		},
		"private_material": "-----BEGIN PRIVATE KEY-----\nraw\n-----END PRIVATE KEY-----",
	}

	sanitized, err := SanitizeSummary(input)
	if err != nil {
		t.Fatalf("SanitizeSummary() error = %v", err)
	}
	before := sanitized["before"].(Summary)
	if before["Authorization"] != redactedValue || before["client_secret"] != redactedValue || before["status"] != "draft" {
		t.Fatalf("sanitized before = %#v", before)
	}
	after := sanitized["after"].([]any)
	if after[0].(Summary)["api-key"] != redactedValue || after[1] != redactedValue {
		t.Fatalf("sanitized after = %#v", after)
	}
	if sanitized["private_material"] != redactedValue {
		t.Fatalf("private key value = %#v", sanitized["private_material"])
	}
	if sanitized["oauth_token_value"] != redactedValue || sanitized["aws_access_key_id"] != redactedValue || sanitized["material_blob"] != redactedValue {
		t.Fatalf("embedded secret markers were not redacted: %#v", sanitized)
	}
	originalBefore := input["before"].(map[string]any)
	if originalBefore["Authorization"] != "Bearer raw-secret-token" {
		t.Fatal("SanitizeSummary mutated caller input")
	}

	encoded, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"raw-secret-token", "should-never-leak", "another-secret", "AKIA_SYNTHETIC", "BEGIN PRIVATE KEY", "BEGIN ENCRYPTED PRIVATE KEY"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("sanitized JSON still contains %q: %s", forbidden, encoded)
		}
	}
}

func TestSanitizeSummaryRedactsPII(t *testing.T) {
	summary, err := SanitizeSummary(Summary{
		"customer_email": "synthetic.person@example.invalid",
		"full_name":      "Synthetic Person",
		"client_ip":      "203.0.113.42",
		"nested":         map[string]any{"contact": "other.person@example.invalid", "status": "ok"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"synthetic.person@example.invalid", "Synthetic Person", "203.0.113.42", "other.person@example.invalid"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("PII leaked in audit summary: %s", text)
		}
	}
	if !strings.Contains(text, "[REDACTED_PII]") || !strings.Contains(text, "ok") {
		t.Fatalf("unexpected sanitized summary: %s", text)
	}
}

func TestSanitizeSummaryRejectsUnsupportedOrUnboundedData(t *testing.T) {
	t.Parallel()
	if _, err := SanitizeSummary(Summary{"unsafe": time.Now()}); !errors.Is(err, ErrInvalidSummary) {
		t.Fatalf("unsupported value error = %v", err)
	}
	if _, err := SanitizeSummary(Summary{"oversized": strings.Repeat("x", maxSummaryBytes+1)}); !errors.Is(err, ErrInvalidSummary) {
		t.Fatalf("oversized value error = %v", err)
	}
	deep := Summary{"level": map[string]any{}}
	cursor := deep["level"].(map[string]any)
	for index := 0; index < maxSummaryDepth+2; index++ {
		next := map[string]any{}
		cursor["level"] = next
		cursor = next
	}
	if _, err := SanitizeSummary(deep); !errors.Is(err, ErrInvalidSummary) {
		t.Fatalf("deep summary error = %v", err)
	}
}

func TestSanitizeSummaryAcceptsStringSlices(t *testing.T) {
	summary, err := SanitizeSummary(Summary{"changed_fields": []string{"given_name", "job_title"}})
	if err != nil {
		t.Fatalf("SanitizeSummary() error = %v", err)
	}
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"changed_fields":["given_name","job_title"]}` {
		t.Fatalf("unexpected sanitized string slice: %s", encoded)
	}
}

func TestServiceCaptureBuildsSafeImmutableRecord(t *testing.T) {
	t.Parallel()
	scope := mustScope(t)
	repository := &captureRepository{}
	instant := time.Date(2026, 8, 9, 8, 0, 0, 123, time.FixedZone("synthetic", 3*60*60))
	service, err := newService(repository, fixedIDGenerator{id: testAuditID}, fixedClock{instant: instant})
	if err != nil {
		t.Fatal(err)
	}

	record, err := service.Capture(context.Background(), scope, Entry{
		ActorID:       "oidc:user-42",
		Source:        "api",
		Action:        "catalog.product.update",
		ResourceType:  "product",
		ResourceID:    "product-42",
		CorrelationID: "request-42",
		Risk:          RiskWriteSensitive,
		Summary: Summary{
			"changed_fields": []any{"title", "price"},
			"authorization":  "Bearer forbidden",
		},
	})
	if err != nil {
		t.Fatalf("Capture() error = %v", err)
	}
	if repository.calls != 1 || repository.record.ID != testAuditID {
		t.Fatalf("repository calls=%d record=%#v", repository.calls, repository.record)
	}
	if record.OrganizationID != scope.OrganizationID() || record.WorkspaceID != scope.WorkspaceID() || record.Risk != RiskWriteSensitive {
		t.Fatalf("record scope/risk = %#v", record)
	}
	if !record.CreatedAt.Equal(instant.UTC()) || record.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v", record.CreatedAt)
	}
	if record.Summary["authorization"] != redactedValue || repository.record.Summary["authorization"] != redactedValue {
		t.Fatalf("unsafe summary reached repository: %#v", repository.record.Summary)
	}
	if err := ValidateRecord(record); err != nil {
		t.Fatalf("ValidateRecord() error = %v", err)
	}
}

func TestServiceFailsClosedBeforeRepository(t *testing.T) {
	t.Parallel()
	repository := &captureRepository{}
	service, err := newService(repository, fixedIDGenerator{id: testAuditID}, fixedClock{instant: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	scope := mustScope(t)
	entry := Entry{
		ActorID:       "system",
		Source:        "worker",
		Action:        "sync.run",
		ResourceType:  "connector_account",
		ResourceID:    "connector-1",
		CorrelationID: "job-1",
		Risk:          RiskWriteSafe,
	}
	entry.Risk = "unknown"
	if _, err := service.Capture(context.Background(), scope, entry); !errors.Is(err, ErrInvalidEntry) {
		t.Fatalf("invalid risk error = %v", err)
	}
	if repository.calls != 0 {
		t.Fatalf("invalid entry reached repository %d times", repository.calls)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	entry.Risk = RiskWriteSafe
	if _, err := service.Capture(canceled, scope, entry); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Capture() error = %v", err)
	}
	if _, err := service.Capture(context.Background(), tenancy.Scope{}, entry); !errors.Is(err, tenancy.ErrInvalidScope) {
		t.Fatalf("invalid scope error = %v", err)
	}
}

func TestValidateRecordRejectsUnsanitizedSummary(t *testing.T) {
	t.Parallel()
	scope := mustScope(t)
	record := Record{
		ID:             testAuditID,
		OrganizationID: scope.OrganizationID(),
		WorkspaceID:    scope.WorkspaceID(),
		ActorID:        "system",
		Source:         "api",
		Action:         "resource.read",
		ResourceType:   "resource",
		ResourceID:     "resource-1",
		CorrelationID:  "request-1",
		Risk:           RiskRead,
		Summary:        Summary{"authorization": "Bearer raw"},
		CreatedAt:      time.Now().UTC(),
	}
	if err := ValidateRecord(record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("ValidateRecord() error = %v", err)
	}
}

type captureRepository struct {
	record Record
	calls  int
	err    error
}

func (repository *captureRepository) Append(_ context.Context, _ tenancy.Scope, record Record) error {
	repository.calls++
	repository.record = record
	return repository.err
}

type fixedIDGenerator struct{ id string }

func (generator fixedIDGenerator) NewID() (string, error) { return generator.id, nil }

type fixedClock struct{ instant time.Time }

func (clock fixedClock) Now() time.Time { return clock.instant }

func mustScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(testOrganization, testWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}
