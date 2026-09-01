// Package lineage defines provider-neutral provenance records and timeline reads.
package lineage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalid  = errors.New("lineage: invalid value")
	ErrNotFound = errors.New("lineage: not found")
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)

type Scope struct{ organizationID, workspaceID string }

func NewScope(organizationID, workspaceID string) (Scope, error) {
	if !domain.ValidTenantScope(organizationID, workspaceID) {
		return Scope{}, ErrInvalid
	}
	return Scope{organizationID: organizationID, workspaceID: workspaceID}, nil
}
func (s Scope) Valid() bool            { return domain.ValidTenantScope(s.organizationID, s.workspaceID) }
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }

// Ref identifies one source or destination fact without copying its payload.
type Ref struct {
	System     string     `json:"system"`
	EntityType string     `json:"entity_type"`
	EntityID   string     `json:"entity_id"`
	Version    string     `json:"version,omitempty"`
	Field      string     `json:"field,omitempty"`
	ObservedAt *time.Time `json:"observed_at,omitempty"`
}

func (r Ref) Validate() error {
	if !validToken(r.System) || !validToken(r.EntityType) || !validText(r.EntityID, 512) || !validOptionalText(r.Version, 128) || !validOptionalToken(r.Field) {
		return ErrInvalid
	}
	if r.ObservedAt != nil && (!isUTC(*r.ObservedAt) || r.ObservedAt.IsZero()) {
		return ErrInvalid
	}
	return nil
}

// Input is one explicit dependency of a lineage transformation.
type Input struct {
	Role string `json:"role"`
	Ref  Ref    `json:"ref"`
}

func (i Input) Validate() error {
	if !validToken(i.Role) || i.Ref.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

type Transformation struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	MappingID string `json:"mapping_id,omitempty"`
	RuleID    string `json:"rule_id,omitempty"`
}

func (t Transformation) Validate() error {
	if !validToken(t.Kind) || !validText(t.ID, 256) || !validText(t.Version, 128) || !validOptionalText(t.MappingID, 256) || !validOptionalText(t.RuleID, 256) {
		return ErrInvalid
	}
	return nil
}

type Result string

const (
	ResultApplied  Result = "applied"
	ResultObserved Result = "observed"
	ResultRejected Result = "rejected"
)

func (r Result) Valid() bool { return r == ResultApplied || r == ResultObserved || r == ResultRejected }

// Record is immutable provenance evidence for one output fact/version.
type Record struct {
	ID             string         `json:"id"`
	OrganizationID string         `json:"organization_id"`
	WorkspaceID    string         `json:"workspace_id"`
	Source         string         `json:"source"`
	ActorID        string         `json:"actor_id,omitempty"`
	Operation      string         `json:"operation"`
	Output         Ref            `json:"output"`
	Inputs         []Input        `json:"inputs"`
	Transformation Transformation `json:"transformation"`
	CorrelationID  string         `json:"correlation_id"`
	CausationID    string         `json:"causation_id,omitempty"`
	AuditID        string         `json:"audit_id"`
	EventID        string         `json:"event_id"`
	Result         Result         `json:"result"`
	OccurredAt     time.Time      `json:"occurred_at"`
}

func (r Record) Validate() error {
	if !validToken(r.ID) || !domain.ValidSortableID(r.OrganizationID) || !domain.ValidSortableID(r.WorkspaceID) || !validToken(r.Source) || !validOptionalText(r.ActorID, 256) || !validToken(r.Operation) || r.Output.Validate() != nil || r.Transformation.Validate() != nil || !validText(r.CorrelationID, 256) || !validOptionalText(r.CausationID, 256) || !validText(r.AuditID, 160) || !validText(r.EventID, 160) || !r.Result.Valid() || !isUTC(r.OccurredAt) {
		return ErrInvalid
	}
	if len(r.Inputs) > 32 {
		return ErrInvalid
	}
	seen := map[string]struct{}{}
	for _, input := range r.Inputs {
		if input.Validate() != nil {
			return ErrInvalid
		}
		key := input.Role + "\x00" + input.Ref.System + "\x00" + input.Ref.EntityType + "\x00" + input.Ref.EntityID + "\x00" + input.Ref.Version + "\x00" + input.Ref.Field
		if _, ok := seen[key]; ok {
			return ErrInvalid
		}
		seen[key] = struct{}{}
	}
	return nil
}

// DeterministicID derives a bounded idempotency-safe lineage id from an event id.
func DeterministicID(eventID string) (string, error) {
	if !validText(eventID, 160) {
		return "", ErrInvalid
	}
	sum := sha256.Sum256([]byte("torgnexa.lineage.v1\x00" + eventID))
	return "lin." + hex.EncodeToString(sum[:]), nil
}

// FingerprintSHA256 binds the complete canonical provenance record for safe idempotent retries.
func FingerprintSHA256(record Record) (string, error) {
	if err := record.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func VersionNumber(v int64) string {
	return strconv.FormatInt(v, 10)
}

type TimelineQuery struct {
	System, EntityType, EntityID, Field string
	BeforeAt                            *time.Time
	BeforeID                            string
	Limit                               int
}

func (q TimelineQuery) Validate() error {
	if !validToken(q.System) || !validToken(q.EntityType) || !validText(q.EntityID, 512) || !validOptionalToken(q.Field) || q.Limit < 1 || q.Limit > 200 {
		return ErrInvalid
	}
	if q.BeforeAt != nil {
		if !isUTC(*q.BeforeAt) || !validToken(q.BeforeID) {
			return ErrInvalid
		}
	} else if q.BeforeID != "" {
		return ErrInvalid
	}
	return nil
}

type Cursor struct {
	OccurredAt time.Time `json:"occurred_at"`
	ID         string    `json:"id"`
}
type TimelinePage struct {
	Items      []Record `json:"items"`
	NextCursor *Cursor  `json:"next_cursor,omitempty"`
}

type Reader interface {
	Timeline(context.Context, Scope, TimelineQuery) (TimelinePage, error)
}

type Appender interface {
	Append(context.Context, Scope, Record) error
}

func validToken(v string) bool         { return tokenPattern.MatchString(v) }
func validOptionalToken(v string) bool { return v == "" || validToken(v) }
func validText(v string, max int) bool {
	return strings.TrimSpace(v) == v && len(v) > 0 && len(v) <= max
}
func validOptionalText(v string, max int) bool { return v == "" || validText(v, max) }
func isUTC(v time.Time) bool                   { return !v.IsZero() && v.Location() == time.UTC }

func (r Record) String() string {
	return fmt.Sprintf("%s:%s/%s@%s", r.Output.System, r.Output.EntityType, r.Output.EntityID, r.Output.Version)
}
