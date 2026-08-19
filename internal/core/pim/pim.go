// Package pim defines provider-neutral product master-data primitives.
package pim

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid       = errors.New("pim: invalid value")
	ErrNotFound      = errors.New("pim: not found")
	ErrConflict      = errors.New("pim: optimistic conflict")
	ErrMergeConflict = errors.New("pim: merge preview contains unresolved conflicts")
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
var fieldPattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
var sourcePattern = regexp.MustCompile(`^[a-z][a-z0-9._:/-]{0,127}$`)

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
func (id ID) String() string { return string(id) }
func (id ID) Valid() bool    { return validSortableID(string(id)) }

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

type EntityType string

const (
	EntityBrand     EntityType = "brand"
	EntityCategory  EntityType = "category"
	EntityAttribute EntityType = "attribute"
)

func (e EntityType) Valid() bool {
	return e == EntityBrand || e == EntityCategory || e == EntityAttribute
}

type Brand struct {
	ID                                      ID
	OrganizationID, WorkspaceID, Code, Name string
	Status                                  Status
	Version                                 int64
	CreatedAt, UpdatedAt                    time.Time
}

func (v Brand) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !validName(v.Name) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type Category struct {
	ID                                      ID
	OrganizationID, WorkspaceID, Code, Name string
	ParentID                                ID
	Status                                  Status
	Version                                 int64
	CreatedAt, UpdatedAt                    time.Time
}

func (v Category) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !validName(v.Name) || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	if v.ParentID != "" && (!v.ParentID.Valid() || v.ParentID == v.ID) {
		return ErrInvalid
	}
	return nil
}

type ValueType string

const (
	ValueText      ValueType = "text"
	ValueInteger   ValueType = "integer"
	ValueDecimal   ValueType = "decimal"
	ValueBoolean   ValueType = "boolean"
	ValueDate      ValueType = "date"
	ValueDateTime  ValueType = "datetime"
	ValueReference ValueType = "reference"
)

func (v ValueType) Valid() bool {
	switch v {
	case ValueText, ValueInteger, ValueDecimal, ValueBoolean, ValueDate, ValueDateTime, ValueReference:
		return true
	}
	return false
}

type Attribute struct {
	ID                                      ID
	OrganizationID, WorkspaceID, Code, Name string
	ValueType                               ValueType
	MultiValue                              bool
	Status                                  Status
	Version                                 int64
	CreatedAt, UpdatedAt                    time.Time
}

func (v Attribute) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !validCode(v.Code) || !validName(v.Name) || !v.ValueType.Valid() || !v.Status.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type ProductBrand struct {
	OrganizationID string
	WorkspaceID    string
	ProductID      ID
	BrandID        ID
	Source         string
	Version        int64
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (v ProductBrand) Validate() error {
	if !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.ProductID.Valid() || !v.BrandID.Valid() || !sourcePattern.MatchString(v.Source) || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type ProductCategory struct {
	OrganizationID string
	WorkspaceID    string
	ProductID      ID
	CategoryID     ID
	IsPrimary      bool
	Source         string
	Version        int64
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (v ProductCategory) Validate() error {
	if !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.ProductID.Valid() || !v.CategoryID.Valid() || !sourcePattern.MatchString(v.Source) || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

// AttributeValue uses a canonical JSON scalar. Decimal is encoded as a JSON string to avoid float drift.
type AttributeValue struct {
	OrganizationID string
	WorkspaceID    string
	ProductID      ID
	AttributeID    ID
	Ordinal        int
	Value          json.RawMessage
	Source         string
	Version        int64
	Active         bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (v AttributeValue) Validate(kind ValueType, multi bool) error {
	if !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.ProductID.Valid() || !v.AttributeID.Valid() || v.Ordinal < 0 || v.Ordinal > 255 || (!multi && v.Ordinal != 0) || !sourcePattern.MatchString(v.Source) || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) || len(v.Value) == 0 || len(v.Value) > 8192 {
		return ErrInvalid
	}
	return validateTypedJSON(v.Value, kind)
}

// FieldAuthority selects the source allowed to win a field-level merge. Larger priority wins.
type FieldAuthority struct {
	ID                          ID
	OrganizationID, WorkspaceID string
	EntityType                  EntityType
	FieldPath, Source           string
	Priority                    int
	Version                     int64
	Active                      bool
	CreatedAt, UpdatedAt        time.Time
}

func (v FieldAuthority) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.EntityType.Valid() || !fieldPattern.MatchString(v.FieldPath) || !sourcePattern.MatchString(v.Source) || v.Priority < 0 || v.Priority > 10000 || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	return nil
}

type DuplicateState string

const (
	DuplicateOpen         DuplicateState = "open"
	DuplicateConfirmed    DuplicateState = "confirmed"
	DuplicateNotDuplicate DuplicateState = "not_duplicate"
	DuplicateMerged       DuplicateState = "merged"
)

func (s DuplicateState) Valid() bool {
	return s == DuplicateOpen || s == DuplicateConfirmed || s == DuplicateNotDuplicate || s == DuplicateMerged
}

type DuplicateSignal struct {
	Kind, Explanation string
	WeightBPS         int
}

func (s DuplicateSignal) Validate() error {
	if !fieldPattern.MatchString(s.Kind) || !validText(s.Explanation, 1, 300) || s.WeightBPS < 0 || s.WeightBPS > 10000 {
		return ErrInvalid
	}
	return nil
}

type DuplicateCandidate struct {
	ID                          ID
	OrganizationID, WorkspaceID string
	EntityType                  EntityType
	LeftID, RightID             ID
	ScoreBPS                    int
	Signals                     []DuplicateSignal
	State                       DuplicateState
	Version                     int64
	CreatedAt, UpdatedAt        time.Time
}

func (v DuplicateCandidate) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.EntityType.Valid() || !v.LeftID.Valid() || !v.RightID.Valid() || v.LeftID == v.RightID || v.LeftID.String() >= v.RightID.String() || v.ScoreBPS < 0 || v.ScoreBPS > 10000 || len(v.Signals) < 1 || len(v.Signals) > 16 || !v.State.Valid() || !validMeta(v.Version, v.CreatedAt, v.UpdatedAt) {
		return ErrInvalid
	}
	for _, s := range v.Signals {
		if s.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// MasterSnapshot is a bounded normalized view used only to compute a preview; it is not persistence.
type MasterSnapshot struct {
	OrganizationID string
	WorkspaceID    string
	EntityType     EntityType
	EntityID       ID
	Source         string
	Version        string
	Fields         map[string]string
}

func (s MasterSnapshot) Validate() error {
	if !validSortableID(s.OrganizationID) || !validSortableID(s.WorkspaceID) || !s.EntityType.Valid() || !s.EntityID.Valid() || !sourcePattern.MatchString(s.Source) || len(s.Version) < 1 || len(s.Version) > 128 || len(s.Fields) > 128 {
		return ErrInvalid
	}
	for k, v := range s.Fields {
		if !fieldPattern.MatchString(k) || !validText(v, 0, 8192) {
			return ErrInvalid
		}
	}
	return nil
}

type MergeDecision string

const (
	MergeKeepTarget MergeDecision = "keep_target"
	MergeTakeSource MergeDecision = "take_source"
	MergeConflict   MergeDecision = "conflict"
	MergeEqual      MergeDecision = "equal"
)

type MergeField struct {
	FieldPath, TargetValue, SourceValue, WinnerSource, Reason string
	Decision                                                  MergeDecision
}
type MergePreview struct {
	ID                           string
	OrganizationID, WorkspaceID  string
	EntityType                   EntityType
	TargetID, SourceID           ID
	TargetVersion, SourceVersion string
	Fields                       []MergeField
	HasConflicts                 bool
	FingerprintSHA256            string
}

func (p MergePreview) Validate() error {
	if !regexp.MustCompile(`^merge\.[0-9a-f]{64}$`).MatchString(p.ID) || !validSortableID(p.OrganizationID) || !validSortableID(p.WorkspaceID) || !p.EntityType.Valid() || !p.TargetID.Valid() || !p.SourceID.Valid() || p.TargetID == p.SourceID || len(p.TargetVersion) < 1 || len(p.TargetVersion) > 128 || len(p.SourceVersion) < 1 || len(p.SourceVersion) > 128 || len(p.Fields) < 1 || len(p.Fields) > 128 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(p.FingerprintSHA256) {
		return ErrInvalid
	}
	conflicts := false
	for _, f := range p.Fields {
		if !fieldPattern.MatchString(f.FieldPath) || !validText(f.TargetValue, 0, 8192) || !validText(f.SourceValue, 0, 8192) || (f.WinnerSource != "" && !sourcePattern.MatchString(f.WinnerSource)) {
			return ErrInvalid
		}
		switch f.Decision {
		case MergeKeepTarget:
			if f.WinnerSource == "" || (f.Reason != "source_missing" && f.Reason != "target_authority") {
				return ErrInvalid
			}
		case MergeTakeSource:
			if f.WinnerSource == "" || (f.Reason != "target_missing" && f.Reason != "source_authority") {
				return ErrInvalid
			}
		case MergeEqual:
			if f.WinnerSource == "" || f.Reason != "equal_values" {
				return ErrInvalid
			}
		case MergeConflict:
			if f.WinnerSource != "" || f.Reason != "equal_authority" {
				return ErrInvalid
			}
			conflicts = true
		default:
			return ErrInvalid
		}
	}
	if conflicts != p.HasConflicts {
		return ErrInvalid
	}
	return nil
}

// BuildMergePreview is deterministic: exact field authorities win; ties with differing values are conflicts; otherwise target is retained.
func BuildMergePreview(target, source MasterSnapshot, authorities []FieldAuthority) (MergePreview, error) {
	if target.Validate() != nil || source.Validate() != nil || target.OrganizationID != source.OrganizationID || target.WorkspaceID != source.WorkspaceID || target.EntityType != source.EntityType || target.EntityID == source.EntityID {
		return MergePreview{}, ErrInvalid
	}
	priorities := map[string]map[string]int{}
	for _, a := range authorities {
		if a.Validate() != nil || a.OrganizationID != target.OrganizationID || a.WorkspaceID != target.WorkspaceID || a.EntityType != target.EntityType || !a.Active {
			return MergePreview{}, ErrInvalid
		}
		if priorities[a.FieldPath] == nil {
			priorities[a.FieldPath] = map[string]int{}
		}
		priorities[a.FieldPath][a.Source] = a.Priority
	}
	keyset := map[string]struct{}{}
	for k := range target.Fields {
		keyset[k] = struct{}{}
	}
	for k := range source.Fields {
		keyset[k] = struct{}{}
	}
	keys := make([]string, 0, len(keyset))
	for k := range keyset {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	p := MergePreview{OrganizationID: target.OrganizationID, WorkspaceID: target.WorkspaceID, EntityType: target.EntityType, TargetID: target.EntityID, SourceID: source.EntityID, TargetVersion: target.Version, SourceVersion: source.Version}
	for _, k := range keys {
		tv, tok := target.Fields[k]
		sv, sok := source.Fields[k]
		d := MergeField{FieldPath: k, TargetValue: tv, SourceValue: sv}
		switch {
		case tok && sok && tv == sv:
			d.Decision = MergeEqual
			d.WinnerSource = target.Source
			d.Reason = "equal_values"
		case !tok && sok:
			d.Decision = MergeTakeSource
			d.WinnerSource = source.Source
			d.Reason = "target_missing"
		case tok && !sok:
			d.Decision = MergeKeepTarget
			d.WinnerSource = target.Source
			d.Reason = "source_missing"
		default:
			tp := priorities[k][target.Source]
			sp := priorities[k][source.Source]
			if sp > tp {
				d.Decision = MergeTakeSource
				d.WinnerSource = source.Source
				d.Reason = "source_authority"
			} else if tp > sp {
				d.Decision = MergeKeepTarget
				d.WinnerSource = target.Source
				d.Reason = "target_authority"
			} else {
				d.Decision = MergeConflict
				d.Reason = "equal_authority"
				p.HasConflicts = true
			}
		}
		p.Fields = append(p.Fields, d)
	}
	canonical := struct {
		OrganizationID string       `json:"organization_id"`
		WorkspaceID    string       `json:"workspace_id"`
		EntityType     EntityType   `json:"entity_type"`
		TargetID       ID           `json:"target_id"`
		SourceID       ID           `json:"source_id"`
		TargetVersion  string       `json:"target_version"`
		SourceVersion  string       `json:"source_version"`
		Fields         []MergeField `json:"fields"`
	}{p.OrganizationID, p.WorkspaceID, p.EntityType, p.TargetID, p.SourceID, p.TargetVersion, p.SourceVersion, p.Fields}

	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	p.FingerprintSHA256 = hex.EncodeToString(sum[:])
	p.ID = "merge." + p.FingerprintSHA256
	if p.Validate() != nil {
		return MergePreview{}, ErrInvalid
	}
	return p, nil
}

type Mutation struct {
	EventID       string
	AuditID       string
	Source        string
	ActorID       string
	CorrelationID string
	CausationID   string
	TraceID       string
	OccurredAt    time.Time
}

func (m Mutation) Validate() error {
	if !validToken(m.EventID, 1, 128) || !validToken(m.AuditID, 1, 128) || !sourcePattern.MatchString(m.Source) || !validOptional(m.ActorID) || !validOptional(m.CorrelationID) || !validOptional(m.CausationID) || !validOptional(m.TraceID) || !isUTC(m.OccurredAt) {
		return ErrInvalid
	}
	return nil
}

type Repository interface {
	Brand(context.Context, Scope, ID) (Brand, error)
	Category(context.Context, Scope, ID) (Category, error)
	Attribute(context.Context, Scope, ID) (Attribute, error)
	CreateBrand(context.Context, Scope, Brand, Mutation) (Brand, error)
	UpdateBrand(context.Context, Scope, Brand, Mutation) (Brand, error)
	CreateCategory(context.Context, Scope, Category, Mutation) (Category, error)
	UpdateCategory(context.Context, Scope, Category, Mutation) (Category, error)
	CreateAttribute(context.Context, Scope, Attribute, Mutation) (Attribute, error)
	UpdateAttribute(context.Context, Scope, Attribute, Mutation) (Attribute, error)
	SetProductBrand(context.Context, Scope, ProductBrand, Mutation) (ProductBrand, error)
	SetProductCategory(context.Context, Scope, ProductCategory, Mutation) (ProductCategory, error)
	SetProductAttributeValue(context.Context, Scope, AttributeValue, Mutation) (AttributeValue, error)
	SetFieldAuthority(context.Context, Scope, FieldAuthority, Mutation) (FieldAuthority, error)
	FlagDuplicate(context.Context, Scope, DuplicateCandidate, Mutation) (DuplicateCandidate, error)
	StoreMergePreview(context.Context, Scope, MergePreview, Mutation) error
}

func validCode(v string) bool { return codePattern.MatchString(v) }
func validName(v string) bool { return validText(v, 1, 300) }
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
func validOptional(v string) bool            { return v == "" || validToken(v, 1, 128) }
func validToken(v string, min, max int) bool {
	return len(v) >= min && len(v) <= max && codePattern.MatchString(v)
}
func validateTypedJSON(raw json.RawMessage, kind ValueType) error {
	var x any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if d.Decode(&x) != nil {
		return ErrInvalid
	}
	switch kind {
	case ValueText, ValueDate, ValueDateTime, ValueReference:
		if _, ok := x.(string); !ok {
			return ErrInvalid
		}
	case ValueInteger:
		n, ok := x.(json.Number)
		if !ok || strings.ContainsAny(n.String(), ".eE") {
			return ErrInvalid
		}
	case ValueDecimal:
		s, ok := x.(string)
		if !ok || !regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,9})?$`).MatchString(s) {
			return ErrInvalid
		}
	case ValueBoolean:
		if _, ok := x.(bool); !ok {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func validSortableID(value string) bool { return validUUIDv7(value) || validULID(value) }
func validUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '7' {
		return false
	}
	if !strings.ContainsRune("89ab", rune(value[19])) {
		return false
	}
	for i, c := range []byte(value) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
func validULID(value string) bool {
	if len(value) != 26 || value[0] < '0' || value[0] > '7' {
		return false
	}
	for _, c := range []byte(value) {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'H') || (c >= 'J' && c <= 'K') || (c >= 'M' && c <= 'N') || (c >= 'P' && c <= 'T') || (c >= 'V' && c <= 'Z') {
			continue
		}
		return false
	}
	return true
}
