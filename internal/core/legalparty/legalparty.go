// Package legalparty defines provider-neutral legal-party and counterparty master data.
package legalparty

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid  = errors.New("legalparty: invalid value")
	ErrNotFound = errors.New("legalparty: not found")
	ErrConflict = errors.New("legalparty: optimistic conflict")
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
var countryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var bicPattern = regexp.MustCompile(`^[0-9]{9}$`)
var accountPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9 -]{5,63}$`)

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, ws string) (Scope, error) {
	if !validSortableID(org) || !validSortableID(ws) {
		return Scope{}, ErrInvalid
	}
	return Scope{org, ws}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return validSortableID(s.organizationID) && validSortableID(s.workspaceID)
}

type ID string

func ParseID(v string) (ID, error) {
	if !validSortableID(v) {
		return "", ErrInvalid
	}
	return ID(v), nil
}
func (v ID) String() string { return string(v) }
func (v ID) Valid() bool    { return validSortableID(string(v)) }

type Status string

const (
	StatusDraft    Status = "draft"
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

func (s Status) Valid() bool { return s == StatusDraft || s == StatusActive || s == StatusArchived }
func ValidateTransition(from, to Status) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalid
	}
	if from == StatusDraft && (to == StatusActive || to == StatusArchived) {
		return nil
	}
	if from == StatusActive && to == StatusArchived {
		return nil
	}
	return ErrInvalid
}

type PartyType string

const (
	PartyLegalEntity            PartyType = "legal_entity"
	PartyIndividualEntrepreneur PartyType = "individual_entrepreneur"
	PartyBranch                 PartyType = "branch"
)

func (p PartyType) Valid() bool {
	return p == PartyLegalEntity || p == PartyIndividualEntrepreneur || p == PartyBranch
}

type CounterpartyRole string

const (
	RoleCustomer CounterpartyRole = "customer"
	RoleSupplier CounterpartyRole = "supplier"
	RolePartner  CounterpartyRole = "partner"
	RoleOther    CounterpartyRole = "other"
)

func (r CounterpartyRole) Valid() bool {
	return r == RoleCustomer || r == RoleSupplier || r == RolePartner || r == RoleOther
}

type AddressKind string

const (
	AddressLegal  AddressKind = "legal"
	AddressActual AddressKind = "actual"
	AddressPostal AddressKind = "postal"
)

func (k AddressKind) Valid() bool {
	return k == AddressLegal || k == AddressActual || k == AddressPostal
}

type ContractStatus string

const (
	ContractDraft      ContractStatus = "draft"
	ContractActive     ContractStatus = "active"
	ContractTerminated ContractStatus = "terminated"
	ContractExpired    ContractStatus = "expired"
)

func (s ContractStatus) Valid() bool {
	return s == ContractDraft || s == ContractActive || s == ContractTerminated || s == ContractExpired
}

type AuthorityType string

const (
	AuthorityCharter         AuthorityType = "charter"
	AuthorityPowerOfAttorney AuthorityType = "power_of_attorney"
	AuthorityMChD            AuthorityType = "mchd"
	AuthorityOrder           AuthorityType = "order"
	AuthorityOther           AuthorityType = "other"
)

func (a AuthorityType) Valid() bool {
	return a == AuthorityCharter || a == AuthorityPowerOfAttorney || a == AuthorityMChD || a == AuthorityOrder || a == AuthorityOther
}

type LegalEntity struct {
	ID                                      ID
	OrganizationID, WorkspaceID             string
	Code, LegalName, ShortName, CountryCode string
	INN, KPP, OGRN                          string
	Status                                  Status
	Version                                 int64
	CreatedAt, UpdatedAt                    time.Time
}

func (v LegalEntity) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !validText(v.LegalName, 1, 500) || !validText(v.ShortName, 0, 300) || !countryPattern.MatchString(v.CountryCode) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.CountryCode == "RU" {
		if ValidateINNLegal(v.INN) != nil || ValidateKPP(v.KPP) != nil || ValidateOGRN(v.OGRN) != nil {
			return ErrInvalid
		}
	}
	if !validRegistration(v.INN, 12) || !validRegistration(v.KPP, 16) || !validRegistration(v.OGRN, 32) {
		return ErrInvalid
	}
	return nil
}

type IndividualEntrepreneur struct {
	ID                          ID
	OrganizationID, WorkspaceID string
	Code, FullName, CountryCode string
	INN, OGRNIP                 string
	Status                      Status
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (v IndividualEntrepreneur) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !validText(v.FullName, 1, 500) || !countryPattern.MatchString(v.CountryCode) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.CountryCode == "RU" {
		if ValidateINNIndividual(v.INN) != nil || ValidateOGRNIP(v.OGRNIP) != nil {
			return ErrInvalid
		}
	}
	if !validRegistration(v.INN, 12) || !validRegistration(v.OGRNIP, 32) {
		return ErrInvalid
	}
	return nil
}

type Branch struct {
	ID                           ID
	OrganizationID, WorkspaceID  string
	LegalEntityID                ID
	Code, Name, CountryCode, KPP string
	Status                       Status
	Version                      int64
	CreatedAt, UpdatedAt         time.Time
}

func (v Branch) Validate() error {
	if !v.ID.Valid() || !v.LegalEntityID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !validText(v.Name, 1, 500) || !countryPattern.MatchString(v.CountryCode) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.CountryCode == "RU" && ValidateKPP(v.KPP) != nil {
		return ErrInvalid
	}
	if !validRegistration(v.KPP, 16) {
		return ErrInvalid
	}
	return nil
}

type Counterparty struct {
	ID                          ID
	OrganizationID, WorkspaceID string
	Code                        string
	PartyType                   PartyType
	PartyID                     ID
	Role                        CounterpartyRole
	Status                      Status
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (v Counterparty) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !v.PartyType.Valid() || !v.PartyID.Valid() || !v.Role.Valid() || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type Address struct {
	ID                                                  ID
	OrganizationID, WorkspaceID                         string
	PartyType                                           PartyType
	PartyID                                             ID
	Kind                                                AddressKind
	CountryCode, PostalCode, Region, City, Line1, Line2 string
	IsPrimary                                           bool
	Version                                             int64
	Active                                              bool
	CreatedAt, UpdatedAt                                time.Time
}

func (v Address) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.PartyType.Valid() || !v.PartyID.Valid() || !v.Kind.Valid() || !countryPattern.MatchString(v.CountryCode) || !validText(v.PostalCode, 0, 32) || !validText(v.Region, 0, 200) || !validText(v.City, 0, 200) || !validText(v.Line1, 1, 500) || !validText(v.Line2, 0, 500) || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type BankAccount struct {
	ID                                                                            ID
	OrganizationID, WorkspaceID                                                   string
	CounterpartyID                                                                ID
	Currency, AccountNumber, BankName, BankCountryCode, BIC, CorrespondentAccount string
	IsPrimary                                                                     bool
	Status                                                                        Status
	Version                                                                       int64
	CreatedAt, UpdatedAt                                                          time.Time
}

func (v BankAccount) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.CounterpartyID.Valid() || !currencyPattern.MatchString(v.Currency) || !accountPattern.MatchString(v.AccountNumber) || !validText(v.BankName, 1, 300) || !countryPattern.MatchString(v.BankCountryCode) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.BankCountryCode == "RU" {
		if !bicPattern.MatchString(v.BIC) || (v.CorrespondentAccount != "" && !regexp.MustCompile(`^[0-9]{20}$`).MatchString(v.CorrespondentAccount)) {
			return ErrInvalid
		}
	}
	if !validText(v.BIC, 0, 32) || !validText(v.CorrespondentAccount, 0, 64) {
		return ErrInvalid
	}
	return nil
}

type Contract struct {
	ID                          ID
	OrganizationID, WorkspaceID string
	CounterpartyID              ID
	Number, ContractType        string
	SignedOn                    *time.Time
	ValidFrom                   time.Time
	ValidUntil                  *time.Time
	Status                      ContractStatus
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (v Contract) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.CounterpartyID.Valid() || !validText(v.Number, 1, 128) || !validCode(v.ContractType) || !v.Status.Valid() || !isUTC(v.ValidFrom) || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.SignedOn != nil && !isUTC(*v.SignedOn) {
		return ErrInvalid
	}
	if v.ValidUntil != nil && (!isUTC(*v.ValidUntil) || v.ValidUntil.Before(v.ValidFrom)) {
		return ErrInvalid
	}
	return nil
}

type AuthorityReference struct {
	ID                          ID
	OrganizationID, WorkspaceID string
	CounterpartyID              ID
	Type                        AuthorityType
	ReferenceNumber, Issuer     string
	IssuedAt                    time.Time
	ExpiresAt                   *time.Time
	Status                      Status
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (v AuthorityReference) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.CounterpartyID.Valid() || !v.Type.Valid() || !validText(v.ReferenceNumber, 1, 256) || !validText(v.Issuer, 0, 300) || !isUTC(v.IssuedAt) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.ExpiresAt != nil && (!isUTC(*v.ExpiresAt) || v.ExpiresAt.Before(v.IssuedAt)) {
		return ErrInvalid
	}
	return nil
}

type SearchQuery struct {
	Text, INN, RegistrationID string
	PartyType                 PartyType
	Limit                     int
}

func (q SearchQuery) Validate() error {
	if q.Text != "" && !validText(q.Text, 1, 200) {
		return ErrInvalid
	}
	if q.INN != "" && !regexp.MustCompile(`^[0-9]{10,12}$`).MatchString(q.INN) {
		return ErrInvalid
	}
	if q.RegistrationID != "" && !regexp.MustCompile(`^[0-9A-Za-z.-]{5,32}$`).MatchString(q.RegistrationID) {
		return ErrInvalid
	}
	if q.PartyType != "" && !q.PartyType.Valid() {
		return ErrInvalid
	}
	if q.Limit < 1 || q.Limit > 100 {
		return ErrInvalid
	}
	return nil
}

type SearchResult struct {
	PartyType      PartyType `json:"party_type"`
	PartyID        string    `json:"party_id"`
	Code           string    `json:"code"`
	DisplayName    string    `json:"display_name"`
	INN            string    `json:"inn,omitempty"`
	RegistrationID string    `json:"registration_id,omitempty"`
	Status         Status    `json:"status"`
}
type SearchPage struct {
	Items []SearchResult `json:"items"`
}

type DuplicateSignal struct {
	Kind        string `json:"kind"`
	Explanation string `json:"explanation"`
	WeightBPS   int    `json:"weight_bps"`
}

func (v DuplicateSignal) Validate() error {
	if !validCode(v.Kind) || !validText(v.Explanation, 1, 300) || v.WeightBPS < 0 || v.WeightBPS > 10000 {
		return ErrInvalid
	}
	return nil
}

type DuplicateCandidate struct {
	PartyType PartyType         `json:"party_type"`
	LeftID    ID                `json:"left_id"`
	RightID   ID                `json:"right_id"`
	ScoreBPS  int               `json:"score_bps"`
	Signals   []DuplicateSignal `json:"signals"`
}

func (v DuplicateCandidate) Validate() error {
	if !v.PartyType.Valid() || !v.LeftID.Valid() || !v.RightID.Valid() || v.LeftID == v.RightID || v.LeftID.String() >= v.RightID.String() || v.ScoreBPS < 0 || v.ScoreBPS > 10000 || len(v.Signals) < 1 || len(v.Signals) > 16 {
		return ErrInvalid
	}
	for _, s := range v.Signals {
		if s.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// DetectDuplicates is deterministic and explainable. Exact government identifiers dominate name similarity.
func DetectDuplicates(kind PartyType, leftID, rightID ID, leftName, rightName, leftINN, rightINN, leftReg, rightReg string) (DuplicateCandidate, error) {
	if !kind.Valid() || !leftID.Valid() || !rightID.Valid() || leftID == rightID {
		return DuplicateCandidate{}, ErrInvalid
	}
	if leftID.String() > rightID.String() {
		leftID, rightID = rightID, leftID
	}
	var signals []DuplicateSignal
	score := 0
	if leftINN != "" && leftINN == rightINN {
		signals = append(signals, DuplicateSignal{"inn_exact", "same tax identifier", 7000})
		score += 7000
	}
	if leftReg != "" && leftReg == rightReg {
		signals = append(signals, DuplicateSignal{"registration_exact", "same state registration identifier", 7000})
		score += 7000
	}
	if normalizeName(leftName) == normalizeName(rightName) && normalizeName(leftName) != "" {
		signals = append(signals, DuplicateSignal{"normalized_name", "same normalized legal name", 3000})
		score += 3000
	}
	if score > 10000 {
		score = 10000
	}
	if len(signals) == 0 {
		return DuplicateCandidate{}, ErrNotFound
	}
	out := DuplicateCandidate{kind, leftID, rightID, score, signals}
	return out, out.Validate()
}

func normalizeName(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	fields := strings.FieldsFunc(v, func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`.,\"'()«»-_`, r) })
	return strings.Join(fields, "")
}

type Mutation struct {
	EventID, AuditID, Source, ActorID, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

func (m Mutation) Validate() error {
	if !validToken(m.EventID) || !validToken(m.AuditID) || !validCode(m.Source) || !validOptional(m.ActorID) || !validOptional(m.CorrelationID) || !validOptional(m.CausationID) || !validOptional(m.TraceID) || !isUTC(m.OccurredAt) {
		return ErrInvalid
	}
	return nil
}

type Repository interface {
	LegalEntity(context.Context, Scope, ID) (LegalEntity, error)
	IndividualEntrepreneur(context.Context, Scope, ID) (IndividualEntrepreneur, error)
	Branch(context.Context, Scope, ID) (Branch, error)
	Counterparty(context.Context, Scope, ID) (Counterparty, error)
	Search(context.Context, Scope, SearchQuery) (SearchPage, error)
	CreateLegalEntity(context.Context, Scope, LegalEntity, Mutation) (LegalEntity, error)
	UpdateLegalEntity(context.Context, Scope, LegalEntity, Mutation) (LegalEntity, error)
	CreateIndividualEntrepreneur(context.Context, Scope, IndividualEntrepreneur, Mutation) (IndividualEntrepreneur, error)
	UpdateIndividualEntrepreneur(context.Context, Scope, IndividualEntrepreneur, Mutation) (IndividualEntrepreneur, error)
	CreateBranch(context.Context, Scope, Branch, Mutation) (Branch, error)
	CreateCounterparty(context.Context, Scope, Counterparty, Mutation) (Counterparty, error)
	CreateAddress(context.Context, Scope, Address, Mutation) (Address, error)
	CreateBankAccount(context.Context, Scope, BankAccount, Mutation) (BankAccount, error)
	CreateContract(context.Context, Scope, Contract, Mutation) (Contract, error)
	CreateAuthorityReference(context.Context, Scope, AuthorityReference, Mutation) (AuthorityReference, error)
	StoreMergePreview(context.Context, Scope, MergePreview, Mutation) error
}

// DeterministicMergeID gives a stable review/evidence id without applying a destructive merge.
func DeterministicMergeID(kind PartyType, target, source ID, targetVersion, sourceVersion int64) (string, error) {
	if !kind.Valid() || !target.Valid() || !source.Valid() || target == source || targetVersion < 1 || sourceVersion < 1 {
		return "", ErrInvalid
	}
	parts := []string{string(kind), target.String(), source.String(), itoa(targetVersion), itoa(sourceVersion)}
	sort.Strings(parts[1:3])
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "party-merge." + hex.EncodeToString(sum[:]), nil
}

func ValidateINNLegal(v string) error {
	if !regexp.MustCompile(`^[0-9]{10}$`).MatchString(v) {
		return ErrInvalid
	}
	if checksumDigit(v[:9], []int{2, 4, 10, 3, 5, 9, 4, 6, 8}) != int(v[9]-'0') {
		return ErrInvalid
	}
	return nil
}
func ValidateINNIndividual(v string) error {
	if !regexp.MustCompile(`^[0-9]{12}$`).MatchString(v) {
		return ErrInvalid
	}
	if checksumDigit(v[:10], []int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) != int(v[10]-'0') {
		return ErrInvalid
	}
	if checksumDigit(v[:11], []int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) != int(v[11]-'0') {
		return ErrInvalid
	}
	return nil
}
func ValidateKPP(v string) error {
	if !regexp.MustCompile(`^[0-9]{4}[0-9A-Z]{2}[0-9]{3}$`).MatchString(v) {
		return ErrInvalid
	}
	return nil
}
func ValidateOGRN(v string) error {
	if !regexp.MustCompile(`^[0-9]{13}$`).MatchString(v) {
		return ErrInvalid
	}
	if modDecimal(v[:12], 11)%10 != int(v[12]-'0') {
		return ErrInvalid
	}
	return nil
}
func ValidateOGRNIP(v string) error {
	if !regexp.MustCompile(`^[0-9]{15}$`).MatchString(v) {
		return ErrInvalid
	}
	if modDecimal(v[:14], 13)%10 != int(v[14]-'0') {
		return ErrInvalid
	}
	return nil
}
func checksumDigit(v string, w []int) int {
	s := 0
	for i := range v {
		s += int(v[i]-'0') * w[i]
	}
	return (s % 11) % 10
}
func modDecimal(v string, m int) int {
	r := 0
	for i := range v {
		r = (r*10 + int(v[i]-'0')) % m
	}
	return r
}

func validRegistration(v string, max int) bool {
	return v == "" || (len(v) <= max && regexp.MustCompile(`^[0-9A-Za-z.-]+$`).MatchString(v))
}
func validCode(v string) bool { return codePattern.MatchString(v) }
func validText(v string, min, max int) bool {
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	n := utf8.RuneCountInString(v)
	if n < min || n > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validMeta(v int64, c, u time.Time) bool { return v >= 1 && isUTC(c) && isUTC(u) && !u.Before(c) }
func isUTC(v time.Time) bool                 { return !v.IsZero() && v.Location() == time.UTC }
func validOptional(v string) bool            { return v == "" || validToken(v) }
func validToken(v string) bool               { return len(v) >= 1 && len(v) <= 128 && codePattern.MatchString(v) }
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	b := [32]byte{}
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
func validSortableID(v string) bool { return validUUIDv7(v) || validULID(v) }
func validUUIDv7(v string) bool {
	if len(v) != 36 || v[8] != '-' || v[13] != '-' || v[18] != '-' || v[23] != '-' || v[14] != '7' {
		return false
	}
	if !strings.ContainsRune("89ab", rune(v[19])) {
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

type MergeDecision string

const (
	MergeKeepTarget MergeDecision = "keep_target"
	MergeTakeSource MergeDecision = "take_source"
	MergeConflict   MergeDecision = "conflict"
	MergeEqual      MergeDecision = "equal"
)

type MergeField struct {
	FieldPath   string        `json:"field_path"`
	TargetValue string        `json:"target_value"`
	SourceValue string        `json:"source_value"`
	Reason      string        `json:"reason"`
	Decision    MergeDecision `json:"decision"`
}
type MergePreview struct {
	ID                string       `json:"id"`
	OrganizationID    string       `json:"organization_id"`
	WorkspaceID       string       `json:"workspace_id"`
	PartyType         PartyType    `json:"party_type"`
	TargetID          ID           `json:"target_id"`
	SourceID          ID           `json:"source_id"`
	TargetVersion     int64        `json:"target_version"`
	SourceVersion     int64        `json:"source_version"`
	Fields            []MergeField `json:"fields"`
	HasConflicts      bool         `json:"has_conflicts"`
	FingerprintSHA256 string       `json:"fingerprint_sha256"`
	CreatedAt         time.Time    `json:"created_at"`
}

func BuildMergePreview(org, ws string, kind PartyType, target, source ID, targetVersion, sourceVersion int64, targetFields, sourceFields map[string]string, at time.Time) (MergePreview, error) {
	if !validSortableID(org) || !validSortableID(ws) || !kind.Valid() || !target.Valid() || !source.Valid() || target == source || targetVersion < 1 || sourceVersion < 1 || !isUTC(at) || len(targetFields) > 64 || len(sourceFields) > 64 {
		return MergePreview{}, ErrInvalid
	}
	keys := map[string]struct{}{}
	for k := range targetFields {
		if !validCode(k) {
			return MergePreview{}, ErrInvalid
		}
		keys[k] = struct{}{}
	}
	for k := range sourceFields {
		if !validCode(k) {
			return MergePreview{}, ErrInvalid
		}
		keys[k] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	p := MergePreview{OrganizationID: org, WorkspaceID: ws, PartyType: kind, TargetID: target, SourceID: source, TargetVersion: targetVersion, SourceVersion: sourceVersion, CreatedAt: at}
	for _, k := range ordered {
		tv, tok := targetFields[k]
		sv, sok := sourceFields[k]
		f := MergeField{FieldPath: k, TargetValue: tv, SourceValue: sv}
		switch {
		case tok && sok && tv == sv:
			f.Decision = MergeEqual
			f.Reason = "equal_values"
		case tok && !sok:
			f.Decision = MergeKeepTarget
			f.Reason = "source_missing"
		case !tok && sok:
			f.Decision = MergeTakeSource
			f.Reason = "target_missing"
		default:
			f.Decision = MergeConflict
			f.Reason = "manual_resolution_required"
			p.HasConflicts = true
		}
		p.Fields = append(p.Fields, f)
	}
	raw := org + "|" + ws + "|" + string(kind) + "|" + target.String() + "|" + source.String() + "|" + itoa(targetVersion) + "|" + itoa(sourceVersion)
	for _, f := range p.Fields {
		raw += "|" + f.FieldPath + "|" + f.TargetValue + "|" + f.SourceValue + "|" + string(f.Decision) + "|" + f.Reason
	}
	sum := sha256.Sum256([]byte(raw))
	p.FingerprintSHA256 = hex.EncodeToString(sum[:])
	p.ID = "party-merge." + p.FingerprintSHA256
	return p, nil
}

// RussianIdentifierValidator is the typed country-specific validation adapter.
// Generic legal-party aggregates remain provider/country-neutral; composition selects
// this adapter when country_code=RU.
type RussianIdentifierValidator struct{}

func (RussianIdentifierValidator) LegalEntity(inn, kpp, ogrn string) error {
	if ValidateINNLegal(inn) != nil || ValidateKPP(kpp) != nil || ValidateOGRN(ogrn) != nil {
		return ErrInvalid
	}
	return nil
}
func (RussianIdentifierValidator) IndividualEntrepreneur(inn, ogrnip string) error {
	if ValidateINNIndividual(inn) != nil || ValidateOGRNIP(ogrnip) != nil {
		return ErrInvalid
	}
	return nil
}
func (RussianIdentifierValidator) Branch(kpp string) error { return ValidateKPP(kpp) }
