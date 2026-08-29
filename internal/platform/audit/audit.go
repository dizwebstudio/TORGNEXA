// Package audit defines TORGNEXA's append-only application audit contract.
package audit

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/privacy"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	maxSummaryBytes = 32 << 10
	maxSummaryDepth = 8
	maxSummaryNodes = 256
	redactedValue   = secrets.RedactedValue
)

var (
	// ErrInvalidEntry means an audit event does not satisfy the canonical contract.
	ErrInvalidEntry = errors.New("audit: invalid entry")
	// ErrInvalidRecord means a persisted audit row violates canonical invariants.
	ErrInvalidRecord = errors.New("audit: invalid persisted record")
	// ErrInvalidSummary means an audit summary is unsafe, malformed, or exceeds bounds.
	ErrInvalidSummary = errors.New("audit: invalid summary")
)

// Risk is the authorization/audit risk class of the operation being recorded.
type Risk string

const (
	RiskRead               Risk = "read"
	RiskWriteSafe          Risk = "write_safe"
	RiskWriteSensitive     Risk = "write_sensitive"
	RiskLegallySignificant Risk = "legally_significant"
)

// Valid reports whether risk is a class callers may emit. Legacy rows migrated
// from the pre-Task-003 schema can contain "unclassified", but new writes cannot.
func (risk Risk) Valid() bool {
	return risk == RiskRead || risk == RiskWriteSafe || risk == RiskWriteSensitive || risk == RiskLegallySignificant
}

// Summary is bounded, JSON-compatible audit metadata. It must contain only a
// compact before/after or decision summary, never full payloads or credentials.
type Summary map[string]any

// Entry is the caller-provided audit event. Service.Capture sanitizes Summary,
// creates the immutable identifier/timestamp, and persists one Record.
type Entry struct {
	ActorID       string
	Source        string
	Action        string
	ResourceType  string
	ResourceID    string
	CorrelationID string
	Risk          Risk
	Summary       Summary
}

// Record is one immutable tenant-scoped audit row.
type Record struct {
	ID             string
	OrganizationID tenancy.OrganizationID
	WorkspaceID    tenancy.WorkspaceID
	ActorID        string
	Source         string
	Action         string
	ResourceType   string
	ResourceID     string
	CorrelationID  string
	Risk           Risk
	Summary        Summary
	CreatedAt      time.Time
}

// Repository is intentionally append-oriented. Mutating or deleting historical
// records is not part of the application port.
type Repository interface {
	Append(context.Context, tenancy.Scope, Record) error
}

// IDGenerator creates canonical time-sortable audit identifiers.
type IDGenerator interface {
	NewID() (string, error)
}

// Clock supplies UTC timestamps and is injectable for deterministic tests.
type Clock interface {
	Now() time.Time
}

// Service sanitizes and validates audit data before persistence.
type Service struct {
	repository Repository
	ids        IDGenerator
	clock      Clock
}

// NewService constructs the production audit service.
func NewService(repository Repository) (*Service, error) {
	return newService(repository, uuidV7Generator{random: rand.Reader}, systemClock{})
}

func newService(repository Repository, ids IDGenerator, clock Clock) (*Service, error) {
	if repository == nil {
		return nil, errors.New("audit service: repository is required")
	}
	if ids == nil {
		return nil, errors.New("audit service: id generator is required")
	}
	if clock == nil {
		return nil, errors.New("audit service: clock is required")
	}
	return &Service{repository: repository, ids: ids, clock: clock}, nil
}

// Capture appends one safe immutable audit record. Secret-like keys and raw
// authorization/private-key values are redacted before repository code sees
// them. Validation errors intentionally do not echo caller summary values.
func (service *Service) Capture(ctx context.Context, scope tenancy.Scope, entry Entry) (Record, error) {
	if ctx == nil {
		return Record{}, errors.New("audit service: context is required")
	}
	if err := ctx.Err(); err != nil {
		return Record{}, fmt.Errorf("audit service: %w", err)
	}
	if !scope.Valid() {
		return Record{}, tenancy.ErrInvalidScope
	}
	if service == nil || service.repository == nil || service.ids == nil || service.clock == nil {
		return Record{}, errors.New("audit service: service is not initialized")
	}
	if !validEntry(entry) {
		return Record{}, ErrInvalidEntry
	}

	summary, err := SanitizeSummary(entry.Summary)
	if err != nil {
		return Record{}, err
	}
	id, err := service.ids.NewID()
	if err != nil || !validSortableID(id) {
		return Record{}, fmt.Errorf("audit service: generate id: %w", ErrInvalidRecord)
	}
	record := Record{
		ID:             id,
		OrganizationID: scope.OrganizationID(),
		WorkspaceID:    scope.WorkspaceID(),
		ActorID:        entry.ActorID,
		Source:         entry.Source,
		Action:         entry.Action,
		ResourceType:   entry.ResourceType,
		ResourceID:     entry.ResourceID,
		CorrelationID:  entry.CorrelationID,
		Risk:           entry.Risk,
		Summary:        summary,
		CreatedAt:      service.clock.Now().UTC(),
	}
	if err := ValidateRecord(record); err != nil {
		return Record{}, err
	}
	if err := service.repository.Append(ctx, scope, record); err != nil {
		return Record{}, fmt.Errorf("audit service: append: %w", err)
	}
	return record, nil
}

// SanitizeSummary deep-copies JSON-compatible summary data and replaces
// credential-bearing fields/values with a fixed marker. It rejects excessive
// depth/size and unsupported values instead of serializing arbitrary objects.
func SanitizeSummary(summary Summary) (Summary, error) {
	if summary == nil {
		return Summary{}, nil
	}
	nodes := 0
	value, err := sanitizeMap(summary, 0, &nodes)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > maxSummaryBytes {
		return nil, ErrInvalidSummary
	}
	return value, nil
}

// ValidateRecord applies the persistence invariants and verifies that Summary
// is already sanitized. Repository adapters call this defensively so unsafe
// records cannot bypass Service.Capture.
func ValidateRecord(record Record) error {
	if !validSortableID(record.ID) || !record.OrganizationID.Valid() || !record.WorkspaceID.Valid() ||
		!validRequiredText(record.ActorID, 256) || !validIdentifierText(record.Source, 128) ||
		!validIdentifierText(record.Action, 160) || !validIdentifierText(record.ResourceType, 128) ||
		!validRequiredText(record.ResourceID, 512) || !validRequiredText(record.CorrelationID, 256) ||
		!record.Risk.Valid() || record.CreatedAt.IsZero() {
		return ErrInvalidRecord
	}
	sanitized, err := SanitizeSummary(record.Summary)
	if err != nil {
		return ErrInvalidRecord
	}
	originalJSON, err := json.Marshal(record.Summary)
	if err != nil {
		return ErrInvalidRecord
	}
	sanitizedJSON, err := json.Marshal(sanitized)
	if err != nil || string(originalJSON) != string(sanitizedJSON) {
		return ErrInvalidRecord
	}
	return nil
}

func validEntry(entry Entry) bool {
	return validRequiredText(entry.ActorID, 256) && validIdentifierText(entry.Source, 128) &&
		validIdentifierText(entry.Action, 160) && validIdentifierText(entry.ResourceType, 128) &&
		validRequiredText(entry.ResourceID, 512) && validRequiredText(entry.CorrelationID, 256) && entry.Risk.Valid()
}

func validRequiredText(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) &&
		utf8.RuneCountInString(value) <= limit && !hasControl(value)
}

func validIdentifierText(value string, limit int) bool {
	if !validRequiredText(value, limit) {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '.' || character == '_' || character == '-' || character == ':' || character == '/' {
			continue
		}
		return false
	}
	return true
}

func hasControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func sanitizeMap(input map[string]any, depth int, nodes *int) (Summary, error) {
	if depth > maxSummaryDepth {
		return nil, ErrInvalidSummary
	}
	output := make(Summary, len(input))
	for key, raw := range input {
		if key == "" || key != strings.TrimSpace(key) || !utf8.ValidString(key) || utf8.RuneCountInString(key) > 128 || hasControl(key) {
			return nil, ErrInvalidSummary
		}
		(*nodes)++
		if *nodes > maxSummaryNodes {
			return nil, ErrInvalidSummary
		}
		if marker, redact := privacy.RedactionForKey(key); redact {
			output[key] = marker
			continue
		}
		value, err := sanitizeValue(raw, depth+1, nodes)
		if err != nil {
			return nil, err
		}
		output[key] = value
	}
	return output, nil
}

func sanitizeValue(value any, depth int, nodes *int) (any, error) {
	if depth > maxSummaryDepth {
		return nil, ErrInvalidSummary
	}
	switch typed := value.(type) {
	case nil, bool:
		return typed, nil
	case string:
		if !utf8.ValidString(typed) || len(typed) > maxSummaryBytes {
			return nil, ErrInvalidSummary
		}
		if redacted := privacy.RedactString("", typed); redacted != typed {
			return redacted, nil
		}
		return typed, nil
	case json.Number:
		if _, err := typed.Float64(); err != nil {
			return nil, ErrInvalidSummary
		}
		return typed, nil
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return nil, ErrInvalidSummary
		}
		return typed, nil
	case float32:
		if math.IsInf(float64(typed), 0) || math.IsNaN(float64(typed)) {
			return nil, ErrInvalidSummary
		}
		return typed, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return typed, nil
	case map[string]any:
		return sanitizeMap(typed, depth, nodes)
	case Summary:
		return sanitizeMap(map[string]any(typed), depth, nodes)
	case []any:
		output := make([]any, len(typed))
		for index, item := range typed {
			(*nodes)++
			if *nodes > maxSummaryNodes {
				return nil, ErrInvalidSummary
			}
			sanitized, err := sanitizeValue(item, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			output[index] = sanitized
		}
		return output, nil
	case []string:
		output := make([]any, len(typed))
		for index, item := range typed {
			(*nodes)++
			if *nodes > maxSummaryNodes {
				return nil, ErrInvalidSummary
			}
			sanitized, err := sanitizeValue(item, depth+1, nodes)
			if err != nil {
				return nil, err
			}
			output[index] = sanitized
		}
		return output, nil
	default:
		return nil, ErrInvalidSummary
	}
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

type uuidV7Generator struct{ random io.Reader }

func (generator uuidV7Generator) NewID() (string, error) {
	return newUUIDv7(time.Now(), generator.random)
}

func newUUIDv7(now time.Time, random io.Reader) (string, error) {
	milliseconds := now.UTC().UnixMilli()
	if milliseconds < 0 || milliseconds >= 1<<48 || random == nil {
		return "", ErrInvalidRecord
	}
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[6:]); err != nil {
		return "", errors.Join(ErrInvalidRecord, err)
	}
	var timestamp [8]byte
	// #nosec G115 -- milliseconds is explicitly constrained to [0, 2^48) above.
	binary.BigEndian.PutUint64(timestamp[:], uint64(milliseconds))
	copy(raw[:6], timestamp[2:])
	raw[6] = raw[6]&0x0f | 0x70
	raw[8] = raw[8]&0x3f | 0x80

	var encoded [36]byte
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded[:]), nil
}

func validSortableID(value string) bool {
	if len(value) == 36 && value[8] == '-' && value[13] == '-' && value[18] == '-' && value[23] == '-' && value[14] == '7' && strings.Contains("89ab", value[19:20]) {
		for index, character := range []byte(value) {
			if index == 8 || index == 13 || index == 18 || index == 23 {
				continue
			}
			if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
				return false
			}
		}
		return true
	}
	if len(value) != 26 || value[0] < '0' || value[0] > '7' {
		return false
	}
	for _, character := range []byte(value) {
		if (character >= '0' && character <= '9') || (character >= 'A' && character <= 'H') ||
			(character >= 'J' && character <= 'K') || (character >= 'M' && character <= 'N') ||
			(character >= 'P' && character <= 'T') || (character >= 'V' && character <= 'Z') {
			continue
		}
		return false
	}
	return true
}
