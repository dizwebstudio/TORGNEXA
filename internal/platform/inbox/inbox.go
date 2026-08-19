// Package inbox defines broker-neutral consumer idempotency primitives.
package inbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

var (
	// ErrInvalidRecord means consumer identity or event data violates inbox invariants.
	ErrInvalidRecord = errors.New("inbox: invalid record")
	// ErrCollision means the same tenant/consumer/event ID was previously processed
	// with different immutable event content.
	ErrCollision = errors.New("inbox: event id collision")
)

var consumerPattern = regexp.MustCompile(`^[a-z][a-z0-9._:-]{0,127}$`)

// Result describes whether a delivery executed business code or was an already
// completed duplicate. Duplicate is a successful idempotent outcome.
type Result uint8

const (
	ResultProcessed Result = iota + 1
	ResultDuplicate
)

func (result Result) Valid() bool { return result == ResultProcessed || result == ResultDuplicate }

// ValidateConsumer validates a stable logical consumer identity. Instance IDs,
// pod names and ephemeral consumer-group member IDs must not be used here.
func ValidateConsumer(consumer string) error {
	if !consumerPattern.MatchString(consumer) {
		return ErrInvalidRecord
	}
	return nil
}

// Fingerprint returns a deterministic SHA-256 digest of the canonical event
// envelope. Delivery attempt/backoff metadata is intentionally excluded so a
// retry of the same immutable event remains the same idempotency record.
func Fingerprint(event eventbus.Event) (string, error) {
	if err := event.Validate(); err != nil {
		return "", ErrInvalidRecord
	}
	canonical, err := json.Marshal(event)
	if err != nil {
		return "", ErrInvalidRecord
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
