// Package compliance defines provider-neutral product compliance evidence and policy evaluation.
package compliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("compliance: invalid value")
	ErrNotFound = errors.New("compliance: not found")
	ErrConflict = errors.New("compliance: optimistic conflict")
	ErrBlocked  = errors.New("compliance: operation blocked")
)

var tokenPattern = regexp.MustCompile(`^[a-z][a-z0-9._:/-]{0,127}$`)
var docNumberPattern = regexp.MustCompile(`^[A-Za-z0-9А-Яа-яЁё][A-Za-z0-9А-Яа-яЁё ._:/()№+\-]{0,255}$`)

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, ws string) (Scope, error) {
	if !validID(org) || !validID(ws) {
		return Scope{}, ErrInvalid
	}
	return Scope{org, ws}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool            { return validID(s.organizationID) && validID(s.workspaceID) }

type ID string

func ParseID(v string) (ID, error) {
	if !validID(v) {
		return "", ErrInvalid
	}
	return ID(v), nil
}
func (id ID) String() string { return string(id) }
func (id ID) Valid() bool    { return validID(string(id)) }

type DocumentType string

const (
	DocumentDeclaration       DocumentType = "declaration"
	DocumentCertificate       DocumentType = "certificate"
	DocumentEACEvidence       DocumentType = "eac_evidence"
	DocumentStateRegistration DocumentType = "state_registration"
	DocumentVeterinary        DocumentType = "veterinary"
	DocumentSanitary          DocumentType = "sanitary"
	DocumentRefusalLetter     DocumentType = "refusal_letter"
	DocumentInformationLetter DocumentType = "information_letter"
	DocumentOther             DocumentType = "other"
)

func (v DocumentType) Valid() bool {
	switch v {
	case DocumentDeclaration, DocumentCertificate, DocumentEACEvidence, DocumentStateRegistration, DocumentVeterinary, DocumentSanitary, DocumentRefusalLetter, DocumentInformationLetter, DocumentOther:
		return true
	}
	return false
}

type DocumentStatus string

const (
	StatusDraft              DocumentStatus = "draft"
	StatusValid              DocumentStatus = "valid"
	StatusSuspended          DocumentStatus = "suspended"
	StatusRevoked            DocumentStatus = "revoked"
	StatusExpired            DocumentStatus = "expired"
	StatusVerificationFailed DocumentStatus = "verification_failed"
)

func (s DocumentStatus) Valid() bool {
	switch s {
	case StatusDraft, StatusValid, StatusSuspended, StatusRevoked, StatusExpired, StatusVerificationFailed:
		return true
	}
	return false
}

type ComplianceDocument struct {
	ID                 ID             `json:"id"`
	OrganizationID     string         `json:"organization_id"`
	WorkspaceID        string         `json:"workspace_id"`
	Type               DocumentType   `json:"document_type"`
	Number             string         `json:"number"`
	Jurisdiction       string         `json:"jurisdiction"`
	Issuer             string         `json:"issuer"`
	RegistrySource     string         `json:"registry_source"`
	RegistryReference  string         `json:"registry_reference,omitempty"`
	Status             DocumentStatus `json:"status"`
	IssuedAt           time.Time      `json:"issued_at"`
	ExpiresAt          time.Time      `json:"expires_at,omitempty"`
	HolderPartyType    string         `json:"holder_party_type,omitempty"`
	HolderPartyID      string         `json:"holder_party_id,omitempty"`
	EvidenceObjectID   string         `json:"evidence_object_id,omitempty"`
	VerificationSource string         `json:"verification_source,omitempty"`
	VerifiedAt         time.Time      `json:"verified_at,omitempty"`
	Version            int64          `json:"version"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
}

func (d ComplianceDocument) Validate() error {
	if !d.ID.Valid() || !validID(d.OrganizationID) || !validID(d.WorkspaceID) || !d.Type.Valid() || !docNumberPattern.MatchString(d.Number) || !country(d.Jurisdiction) || !text(d.Issuer, 1, 300) || !token(d.RegistrySource) || !d.Status.Valid() || !utc(d.IssuedAt) || d.Version < 1 || !utc(d.CreatedAt) || !utc(d.UpdatedAt) || d.UpdatedAt.Before(d.CreatedAt) {
		return ErrInvalid
	}
	if !d.ExpiresAt.IsZero() && (!utc(d.ExpiresAt) || d.ExpiresAt.Before(d.IssuedAt)) {
		return ErrInvalid
	}
	if d.RegistryReference != "" && !safeText(d.RegistryReference, 256) {
		return ErrInvalid
	}
	if d.HolderPartyType != "" && (!token(d.HolderPartyType) || !validID(d.HolderPartyID)) {
		return ErrInvalid
	}
	if d.EvidenceObjectID != "" && !token(d.EvidenceObjectID) {
		return ErrInvalid
	}
	if d.VerificationSource != "" && !token(d.VerificationSource) {
		return ErrInvalid
	}
	if !d.VerifiedAt.IsZero() && !utc(d.VerifiedAt) {
		return ErrInvalid
	}
	return nil
}
func (d ComplianceDocument) Effective(at time.Time) bool {
	return d.Status == StatusValid && utc(at) && !at.Before(d.IssuedAt) && (d.ExpiresAt.IsZero() || at.Before(d.ExpiresAt))
}

type SubjectType string

const (
	SubjectProduct  SubjectType = "product"
	SubjectOffer    SubjectType = "offer"
	SubjectCategory SubjectType = "category"
	SubjectGTIN     SubjectType = "gtin"
	SubjectSKU      SubjectType = "sku"
)

func (s SubjectType) Valid() bool {
	return s == SubjectProduct || s == SubjectOffer || s == SubjectCategory || s == SubjectGTIN || s == SubjectSKU
}

type Binding struct {
	ID                          ID `json:"id"`
	OrganizationID, WorkspaceID string
	DocumentID                  ID
	SubjectType                 SubjectType
	SubjectID                   string
	Active                      bool
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (b Binding) Validate() error {
	if !b.ID.Valid() || !validID(b.OrganizationID) || !validID(b.WorkspaceID) || !b.DocumentID.Valid() || !b.SubjectType.Valid() || b.Version < 1 || !utc(b.CreatedAt) || !utc(b.UpdatedAt) || b.UpdatedAt.Before(b.CreatedAt) {
		return ErrInvalid
	}
	if b.SubjectType == SubjectGTIN {
		if !validGTIN(b.SubjectID) {
			return ErrInvalid
		}
	} else if b.SubjectType == SubjectSKU {
		if strings.TrimSpace(b.SubjectID) == "" || len(b.SubjectID) > 128 {
			return ErrInvalid
		}
	} else if !validID(b.SubjectID) {
		return ErrInvalid
	}
	return nil
}

type Operation string

const (
	OperationPublication Operation = "publication"
	OperationSale        Operation = "sale"
	OperationAdvertising Operation = "advertising"
	OperationShipping    Operation = "shipping"
)

func (o Operation) Valid() bool {
	return o == OperationPublication || o == OperationSale || o == OperationAdvertising || o == OperationShipping
}

type Outcome string

const (
	OutcomeAllow    Outcome = "allow"
	OutcomeWarn     Outcome = "warn"
	OutcomeApproval Outcome = "approval_required"
	OutcomeBlock    Outcome = "block"
)

func (o Outcome) Valid() bool {
	return o == OutcomeAllow || o == OutcomeWarn || o == OutcomeApproval || o == OutcomeBlock
}
func severity(o Outcome) int {
	switch o {
	case OutcomeBlock:
		return 4
	case OutcomeApproval:
		return 3
	case OutcomeWarn:
		return 2
	default:
		return 1
	}
}

type Requirement struct {
	DocumentType         DocumentType `json:"document_type"`
	FailureOutcome       Outcome      `json:"failure_outcome"`
	VerificationRequired bool         `json:"verification_required"`
	MinValidityHours     int          `json:"min_validity_hours"`
}

func (r Requirement) Validate() error {
	if !r.DocumentType.Valid() || !r.FailureOutcome.Valid() || r.FailureOutcome == OutcomeAllow || r.MinValidityHours < 0 || r.MinValidityHours > 24*365*10 {
		return ErrInvalid
	}
	return nil
}

type Policy struct {
	ID                                              ID `json:"id"`
	OrganizationID, WorkspaceID, Code, Jurisdiction string
	Operation                                       Operation
	ChannelFamily                                   string
	SellerRole                                      string
	CategoryID                                      ID
	Requirements                                    []Requirement
	EffectiveFrom                                   time.Time
	EffectiveUntil                                  time.Time
	Active                                          bool
	Version                                         int64
	CreatedAt, UpdatedAt                            time.Time
}

func (p Policy) Validate() error {
	if !p.ID.Valid() || !validID(p.OrganizationID) || !validID(p.WorkspaceID) || !token(p.Code) || !country(p.Jurisdiction) || !p.Operation.Valid() || p.Version < 1 || !utc(p.EffectiveFrom) || !utc(p.CreatedAt) || !utc(p.UpdatedAt) || p.UpdatedAt.Before(p.CreatedAt) || len(p.Requirements) == 0 || len(p.Requirements) > 32 {
		return ErrInvalid
	}
	if !p.EffectiveUntil.IsZero() && (!utc(p.EffectiveUntil) || !p.EffectiveUntil.After(p.EffectiveFrom)) {
		return ErrInvalid
	}
	if p.ChannelFamily != "" && !token(p.ChannelFamily) {
		return ErrInvalid
	}
	if p.SellerRole != "" && !token(p.SellerRole) {
		return ErrInvalid
	}
	if p.CategoryID != "" && !p.CategoryID.Valid() {
		return ErrInvalid
	}
	seen := map[DocumentType]bool{}
	for _, r := range p.Requirements {
		if r.Validate() != nil || seen[r.DocumentType] {
			return ErrInvalid
		}
		seen[r.DocumentType] = true
	}
	return nil
}
func (p Policy) Applies(c EvaluationContext) bool {
	if !p.Active || p.Operation != c.Operation || p.Jurisdiction != c.Jurisdiction || c.At.Before(p.EffectiveFrom) || (!p.EffectiveUntil.IsZero() && !c.At.Before(p.EffectiveUntil)) {
		return false
	}
	if p.ChannelFamily != "" && p.ChannelFamily != c.ChannelFamily {
		return false
	}
	if p.SellerRole != "" && p.SellerRole != c.SellerRole {
		return false
	}
	if p.CategoryID != "" && p.CategoryID != c.CategoryID {
		return false
	}
	return true
}

type EvaluationContext struct {
	Operation     Operation `json:"operation"`
	Jurisdiction  string    `json:"jurisdiction"`
	ProductID     ID        `json:"product_id"`
	OfferID       ID        `json:"offer_id,omitempty"`
	CategoryID    ID        `json:"category_id,omitempty"`
	GTIN          string    `json:"gtin,omitempty"`
	SKU           string    `json:"sku,omitempty"`
	SellerPartyID string    `json:"seller_party_id,omitempty"`
	SellerRole    string    `json:"seller_role,omitempty"`
	ChannelID     string    `json:"connector_id,omitempty"`
	ChannelFamily string    `json:"connector_family,omitempty"`
	At            time.Time `json:"at"`
}

func (c EvaluationContext) Validate() error {
	if !c.Operation.Valid() || !country(c.Jurisdiction) || !c.ProductID.Valid() || !utc(c.At) {
		return ErrInvalid
	}
	if c.OfferID != "" && !c.OfferID.Valid() {
		return ErrInvalid
	}
	if c.CategoryID != "" && !c.CategoryID.Valid() {
		return ErrInvalid
	}
	if c.GTIN != "" && !validGTIN(c.GTIN) {
		return ErrInvalid
	}
	if c.SKU != "" && !safeText(c.SKU, 128) {
		return ErrInvalid
	}
	if c.SellerPartyID != "" && !validID(c.SellerPartyID) {
		return ErrInvalid
	}
	if c.SellerRole != "" && !token(c.SellerRole) {
		return ErrInvalid
	}
	if c.ChannelID != "" && !token(c.ChannelID) {
		return ErrInvalid
	}
	if c.ChannelFamily != "" && !token(c.ChannelFamily) {
		return ErrInvalid
	}
	return nil
}

type Reason struct {
	Code          string       `json:"code"`
	PolicyID      string       `json:"policy_id"`
	PolicyVersion int64        `json:"policy_version"`
	DocumentType  DocumentType `json:"document_type"`
	EvidenceIDs   []string     `json:"evidence_ids,omitempty"`
}
type Evaluation struct {
	Outcome           Outcome   `json:"outcome"`
	Reasons           []Reason  `json:"reasons"`
	EvaluatedAt       time.Time `json:"evaluated_at"`
	FingerprintSHA256 string    `json:"fingerprint_sha256"`
}

func Evaluate(c EvaluationContext, policies []Policy, documents []ComplianceDocument, bindings []Binding) (Evaluation, error) {
	if c.Validate() != nil {
		return Evaluation{}, ErrInvalid
	}
	applicable := make([]Policy, 0)
	for _, p := range policies {
		if p.Validate() != nil {
			return Evaluation{}, ErrInvalid
		}
		if p.Applies(c) {
			applicable = append(applicable, p)
		}
	}
	sort.Slice(applicable, func(i, j int) bool {
		if applicable[i].Code == applicable[j].Code {
			return applicable[i].Version < applicable[j].Version
		}
		return applicable[i].Code < applicable[j].Code
	})
	out := Evaluation{Outcome: OutcomeAllow, EvaluatedAt: c.At}
	for _, p := range applicable {
		for _, req := range p.Requirements {
			evidence, reason := coverRequirement(c, req, documents, bindings)
			if reason != "" {
				r := Reason{Code: reason, PolicyID: p.ID.String(), PolicyVersion: p.Version, DocumentType: req.DocumentType, EvidenceIDs: evidence}
				out.Reasons = append(out.Reasons, r)
				if severity(req.FailureOutcome) > severity(out.Outcome) {
					out.Outcome = req.FailureOutcome
				}
			}
		}
	}
	digest := sha256.Sum256([]byte(canonicalEvaluation(c, out)))
	out.FingerprintSHA256 = hex.EncodeToString(digest[:])
	return out, nil
}
func coverRequirement(c EvaluationContext, req Requirement, docs []ComplianceDocument, bindings []Binding) ([]string, string) {
	matching := []string{}
	expired := false
	unverified := false
	for _, d := range docs {
		if d.Validate() != nil || d.Type != req.DocumentType || d.Jurisdiction != c.Jurisdiction {
			continue
		}
		if !boundTo(d.ID, c, bindings) {
			continue
		}
		matching = append(matching, d.ID.String())
		if !d.Effective(c.At) {
			if !d.ExpiresAt.IsZero() && !c.At.Before(d.ExpiresAt) {
				expired = true
			}
			continue
		}
		if req.MinValidityHours > 0 && !d.ExpiresAt.IsZero() && d.ExpiresAt.Sub(c.At) < time.Duration(req.MinValidityHours)*time.Hour {
			expired = true
			continue
		}
		if req.VerificationRequired && (d.VerificationSource == "" || d.VerifiedAt.IsZero()) {
			unverified = true
			continue
		}
		return matching, ""
	}
	sort.Strings(matching)
	if len(matching) == 0 {
		return nil, "missing_evidence"
	}
	if expired {
		return matching, "expired_evidence"
	}
	if unverified {
		return matching, "unverified_evidence"
	}
	return matching, "invalid_evidence"
}
func boundTo(doc ID, c EvaluationContext, bs []Binding) bool {
	for _, b := range bs {
		if !b.Active || b.DocumentID != doc {
			continue
		}
		switch b.SubjectType {
		case SubjectProduct:
			if b.SubjectID == c.ProductID.String() {
				return true
			}
		case SubjectOffer:
			if c.OfferID != "" && b.SubjectID == c.OfferID.String() {
				return true
			}
		case SubjectCategory:
			if c.CategoryID != "" && b.SubjectID == c.CategoryID.String() {
				return true
			}
		case SubjectGTIN:
			if c.GTIN != "" && b.SubjectID == c.GTIN {
				return true
			}
		case SubjectSKU:
			if c.SKU != "" && b.SubjectID == c.SKU {
				return true
			}
		}
	}
	return false
}
func canonicalEvaluation(c EvaluationContext, e Evaluation) string {
	var b strings.Builder
	b.WriteString(string(c.Operation))
	b.WriteByte('|')
	b.WriteString(c.Jurisdiction)
	b.WriteByte('|')
	b.WriteString(c.ProductID.String())
	b.WriteByte('|')
	b.WriteString(c.OfferID.String())
	b.WriteByte('|')
	b.WriteString(c.CategoryID.String())
	b.WriteByte('|')
	b.WriteString(c.GTIN)
	b.WriteByte('|')
	b.WriteString(c.SKU)
	b.WriteByte('|')
	b.WriteString(c.SellerPartyID)
	b.WriteByte('|')
	b.WriteString(c.SellerRole)
	b.WriteByte('|')
	b.WriteString(c.ChannelID)
	b.WriteByte('|')
	b.WriteString(c.ChannelFamily)
	b.WriteByte('|')
	b.WriteString(c.At.Format(time.RFC3339Nano))
	b.WriteByte('|')
	b.WriteString(string(e.Outcome))
	for _, r := range e.Reasons {
		b.WriteByte('|')
		b.WriteString(r.Code)
		b.WriteByte(':')
		b.WriteString(r.PolicyID)
		b.WriteByte(':')
		b.WriteString(string(r.DocumentType))
		for _, id := range r.EvidenceIDs {
			b.WriteByte(':')
			b.WriteString(id)
		}
	}
	return b.String()
}

type VerificationRequest struct {
	Document ComplianceDocument
	At       time.Time
}
type VerificationResult struct {
	Status                    DocumentStatus
	Source, RegistryReference string
	VerifiedAt                time.Time
}
type RegistryVerifier interface {
	Verify(context.Context, VerificationRequest) (VerificationResult, error)
}

type ExpiryAlert struct {
	ID, DocumentID   string
	ExpiresAt, DueAt time.Time
	LeadHours        int
}
type ExpiryNotifier interface {
	Notify(context.Context, Scope, ExpiryAlert) error
}

func NewExpiryAlert(document ComplianceDocument, leadHours int, at time.Time) (ExpiryAlert, error) {
	if document.Validate() != nil || leadHours < 1 || leadHours > 24*365 || document.ExpiresAt.IsZero() || !utc(at) {
		return ExpiryAlert{}, ErrInvalid
	}
	due := document.ExpiresAt.Add(-time.Duration(leadHours) * time.Hour)
	sum := sha256.Sum256([]byte(document.ID.String() + "|" + document.ExpiresAt.Format(time.RFC3339Nano) + "|" + itoa(int64(leadHours))))
	return ExpiryAlert{"expiry." + hex.EncodeToString(sum[:]), document.ID.String(), document.ExpiresAt, due, leadHours}, nil
}

type Mutation struct {
	EventID, AuditID, Source, ActorID, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

func (m Mutation) Validate() error {
	if !token(m.EventID) || !token(m.AuditID) || !token(m.Source) || !optionalToken(m.ActorID) || !optionalToken(m.CorrelationID) || !optionalToken(m.CausationID) || !optionalToken(m.TraceID) || !utc(m.OccurredAt) {
		return ErrInvalid
	}
	return nil
}

type Evaluator interface {
	Evaluate(context.Context, Scope, EvaluationContext) (Evaluation, error)
}
type Repository interface {
	Evaluator
	CreateDocument(context.Context, Scope, ComplianceDocument, Mutation) (ComplianceDocument, error)
	UpdateDocument(context.Context, Scope, ComplianceDocument, Mutation) (ComplianceDocument, error)
	CreateBinding(context.Context, Scope, Binding, Mutation) (Binding, error)
	CreatePolicy(context.Context, Scope, Policy, Mutation) (Policy, error)
	Verify(context.Context, Scope, ID, RegistryVerifier, Mutation) (ComplianceDocument, error)
	ExpiryDue(context.Context, Scope, time.Time, int, int) ([]ComplianceDocument, error)
}

func validGTIN(v string) bool {
	if len(v) != 8 && len(v) != 12 && len(v) != 13 && len(v) != 14 {
		return false
	}
	sum := 0
	for i := len(v) - 2; i >= 0; i-- {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
		w := 1
		if (len(v)-1-i)%2 == 1 {
			w = 3
		}
		sum += int(v[i]-'0') * w
	}
	if v[len(v)-1] < '0' || v[len(v)-1] > '9' {
		return false
	}
	return (10-sum%10)%10 == int(v[len(v)-1]-'0')
}
func country(v string) bool {
	return len(v) == 2 && v[0] >= 'A' && v[0] <= 'Z' && v[1] >= 'A' && v[1] <= 'Z'
}
func token(v string) bool         { return tokenPattern.MatchString(v) }
func optionalToken(v string) bool { return v == "" || token(v) }
func text(v string, min, max int) bool {
	return len(strings.TrimSpace(v)) >= min && len(v) <= max && !strings.ContainsAny(v, "\x00\r\n")
}
func safeText(v string, max int) bool { return len(v) <= max && !strings.ContainsAny(v, "\x00\r\n") }
func utc(v time.Time) bool            { return !v.IsZero() && v.Location() == time.UTC }
func validID(v string) bool           { return validUUIDv7(v) || validULID(v) }
func validUUIDv7(v string) bool {
	if len(v) != 36 || v[8] != '-' || v[13] != '-' || v[18] != '-' || v[23] != '-' || v[14] != '7' || !strings.ContainsRune("89ab", rune(v[19])) {
		return false
	}
	for i, c := range []byte(v) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func validULID(v string) bool {
	if len(v) != 26 || v[0] < '0' || v[0] > '7' {
		return false
	}
	for _, c := range []byte(v) {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'H') || (c >= 'J' && c <= 'K') || (c >= 'M' && c <= 'N') || (c >= 'P' && c <= 'T') || (c >= 'V' && c <= 'Z') {
			continue
		}
		return false
	}
	return true
}
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	b := [32]byte{}
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
