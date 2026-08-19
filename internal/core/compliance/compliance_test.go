package compliance

import (
	"testing"
	"time"
)

const org = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
const ws = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
const prod = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
const doc = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004"
const pol = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0005"
const bind = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0006"

func TestEvaluateBlocksMissingAndAllowsValid(t *testing.T) {
	at := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	pid, _ := ParseID(prod)
	policy := Policy{ID: ID(pol), OrganizationID: org, WorkspaceID: ws, Code: "ru.eac", Jurisdiction: "RU", Operation: OperationPublication, Requirements: []Requirement{{DocumentType: DocumentCertificate, FailureOutcome: OutcomeBlock, VerificationRequired: true}}, EffectiveFrom: at.Add(-time.Hour), Active: true, Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)}
	ctx := EvaluationContext{Operation: OperationPublication, Jurisdiction: "RU", ProductID: pid, At: at}
	got, err := Evaluate(ctx, []Policy{policy}, nil, nil)
	if err != nil || got.Outcome != OutcomeBlock || got.Reasons[0].Code != "missing_evidence" {
		t.Fatalf("missing=%+v err=%v", got, err)
	}
	d := ComplianceDocument{ID: ID(doc), OrganizationID: org, WorkspaceID: ws, Type: DocumentCertificate, Number: "ЕАЭС RU C-DE.001", Jurisdiction: "RU", Issuer: "Registry", RegistrySource: "registry", Status: StatusValid, IssuedAt: at.Add(-24 * time.Hour), ExpiresAt: at.Add(365 * 24 * time.Hour), VerificationSource: "registry", VerifiedAt: at.Add(-time.Hour), Version: 1, CreatedAt: at.Add(-24 * time.Hour), UpdatedAt: at.Add(-time.Hour)}
	b := Binding{ID: ID(bind), OrganizationID: org, WorkspaceID: ws, DocumentID: ID(doc), SubjectType: SubjectProduct, SubjectID: prod, Active: true, Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)}
	got, err = Evaluate(ctx, []Policy{policy}, []ComplianceDocument{d}, []Binding{b})
	if err != nil || got.Outcome != OutcomeAllow || len(got.Reasons) != 0 {
		t.Fatalf("valid=%+v err=%v", got, err)
	}
}
func TestExpiredAndUnverifiedReasons(t *testing.T) {
	at := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	ctx := EvaluationContext{Operation: OperationPublication, Jurisdiction: "RU", ProductID: ID(prod), At: at}
	p := Policy{ID: ID(pol), OrganizationID: org, WorkspaceID: ws, Code: "ru.decl", Jurisdiction: "RU", Operation: OperationPublication, Requirements: []Requirement{{DocumentType: DocumentDeclaration, FailureOutcome: OutcomeBlock, VerificationRequired: true}}, EffectiveFrom: at.Add(-time.Hour), Active: true, Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)}
	b := Binding{ID: ID(bind), OrganizationID: org, WorkspaceID: ws, DocumentID: ID(doc), SubjectType: SubjectProduct, SubjectID: prod, Active: true, Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)}
	d := ComplianceDocument{ID: ID(doc), OrganizationID: org, WorkspaceID: ws, Type: DocumentDeclaration, Number: "D-1", Jurisdiction: "RU", Issuer: "Issuer", RegistrySource: "registry", Status: StatusValid, IssuedAt: at.Add(-48 * time.Hour), ExpiresAt: at.Add(-time.Hour), Version: 1, CreatedAt: at.Add(-48 * time.Hour), UpdatedAt: at.Add(-2 * time.Hour)}
	r, _ := Evaluate(ctx, []Policy{p}, []ComplianceDocument{d}, []Binding{b})
	if r.Reasons[0].Code != "expired_evidence" {
		t.Fatalf("reason=%+v", r)
	}
	d.ExpiresAt = at.Add(24 * time.Hour)
	r, _ = Evaluate(ctx, []Policy{p}, []ComplianceDocument{d}, []Binding{b})
	if r.Reasons[0].Code != "unverified_evidence" {
		t.Fatalf("reason=%+v", r)
	}
}
func TestExpiryAlertDeterministic(t *testing.T) {
	at := time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)
	d := ComplianceDocument{ID: ID(doc), OrganizationID: org, WorkspaceID: ws, Type: DocumentCertificate, Number: "C-1", Jurisdiction: "RU", Issuer: "Issuer", RegistrySource: "registry", Status: StatusValid, IssuedAt: at.Add(-time.Hour), ExpiresAt: at.Add(10 * 24 * time.Hour), Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at}
	a, e := NewExpiryAlert(d, 72, at)
	b, _ := NewExpiryAlert(d, 72, at)
	if e != nil || a.ID != b.ID || a.DueAt != d.ExpiresAt.Add(-72*time.Hour) {
		t.Fatalf("alert=%+v err=%v", a, e)
	}
}
func TestGTIN(t *testing.T) {
	if !validGTIN("4601234567893") {
		t.Fatal("known gtin13 rejected")
	}
	if validGTIN("4601234567894") {
		t.Fatal("invalid check digit accepted")
	}
}

func TestEvaluateSKUScopedEvidence(t *testing.T) {
	at := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	productID := ID(prod)
	d := ComplianceDocument{ID: ID(doc), OrganizationID: org, WorkspaceID: ws, Type: DocumentCertificate, Number: "SKU-CERT-1", Jurisdiction: "RU", Issuer: "Registry", RegistrySource: "registry", Status: StatusValid, IssuedAt: at.Add(-24 * time.Hour), ExpiresAt: at.Add(365 * 24 * time.Hour), VerificationSource: "registry", VerifiedAt: at.Add(-time.Hour), Version: 1, CreatedAt: at.Add(-24 * time.Hour), UpdatedAt: at.Add(-time.Hour)}
	b := Binding{ID: ID(bind), OrganizationID: org, WorkspaceID: ws, DocumentID: ID(doc), SubjectType: SubjectSKU, SubjectID: "SKU-001", Active: true, Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)}
	p := Policy{ID: ID(pol), OrganizationID: org, WorkspaceID: ws, Code: "ru.sku.cert", Jurisdiction: "RU", Operation: OperationPublication, Requirements: []Requirement{{DocumentType: DocumentCertificate, FailureOutcome: OutcomeBlock, VerificationRequired: true}}, EffectiveFrom: at.Add(-time.Hour), Active: true, Version: 1, CreatedAt: at.Add(-time.Hour), UpdatedAt: at.Add(-time.Hour)}
	ctx := EvaluationContext{Operation: OperationPublication, Jurisdiction: "RU", ProductID: productID, SKU: "SKU-001", At: at}
	result, err := Evaluate(ctx, []Policy{p}, []ComplianceDocument{d}, []Binding{b})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeAllow {
		t.Fatalf("expected SKU-scoped evidence to allow publication, got %s (%v)", result.Outcome, result.Reasons)
	}
}
