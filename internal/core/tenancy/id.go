// Package tenancy defines the organization, workspace, and store tenancy core.
package tenancy

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"time"
)

// ErrInvalidID means a tenant identifier is not a canonical UUIDv7 or ULID.
var ErrInvalidID = errors.New("tenancy: invalid sortable identifier")

// OrganizationID uniquely identifies an organization.
type OrganizationID string

// WorkspaceID uniquely identifies a workspace.
type WorkspaceID string

// StoreID uniquely identifies a store or business unit.
type StoreID string

// NewOrganizationID returns a cryptographically random UUIDv7 organization ID.
func NewOrganizationID() (OrganizationID, error) {
	value, err := newUUIDv7(time.Now(), rand.Reader)
	return OrganizationID(value), err
}

// NewWorkspaceID returns a cryptographically random UUIDv7 workspace ID.
func NewWorkspaceID() (WorkspaceID, error) {
	value, err := newUUIDv7(time.Now(), rand.Reader)
	return WorkspaceID(value), err
}

// NewStoreID returns a cryptographically random UUIDv7 store ID.
func NewStoreID() (StoreID, error) {
	value, err := newUUIDv7(time.Now(), rand.Reader)
	return StoreID(value), err
}

// ParseOrganizationID validates and returns an organization ID.
func ParseOrganizationID(value string) (OrganizationID, error) {
	if !validSortableID(value) {
		return "", ErrInvalidID
	}
	return OrganizationID(value), nil
}

// ParseWorkspaceID validates and returns a workspace ID.
func ParseWorkspaceID(value string) (WorkspaceID, error) {
	if !validSortableID(value) {
		return "", ErrInvalidID
	}
	return WorkspaceID(value), nil
}

// ParseStoreID validates and returns a store ID.
func ParseStoreID(value string) (StoreID, error) {
	if !validSortableID(value) {
		return "", ErrInvalidID
	}
	return StoreID(value), nil
}

// String returns the canonical identifier text.
func (id OrganizationID) String() string { return string(id) }

// Valid reports whether the organization ID is canonical.
func (id OrganizationID) Valid() bool { return validSortableID(string(id)) }

// String returns the canonical identifier text.
func (id WorkspaceID) String() string { return string(id) }

// Valid reports whether the workspace ID is canonical.
func (id WorkspaceID) Valid() bool { return validSortableID(string(id)) }

// String returns the canonical identifier text.
func (id StoreID) String() string { return string(id) }

// Valid reports whether the store ID is canonical.
func (id StoreID) Valid() bool { return validSortableID(string(id)) }

func newUUIDv7(now time.Time, random io.Reader) (string, error) {
	milliseconds := now.UTC().UnixMilli()
	if milliseconds < 0 || milliseconds >= 1<<48 || random == nil {
		return "", ErrInvalidID
	}
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[6:]); err != nil {
		return "", errors.Join(ErrInvalidID, err)
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
	return validUUIDv7(value) || validULID(value)
}

func validUUIDv7(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value[14] != '7' {
		return false
	}
	if value[19] != '8' && value[19] != '9' && value[19] != 'a' && value[19] != 'b' {
		return false
	}
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

func validULID(value string) bool {
	if len(value) != 26 || value[0] < '0' || value[0] > '7' {
		return false
	}
	for _, character := range []byte(value) {
		if (character >= '0' && character <= '9') ||
			(character >= 'A' && character <= 'H') ||
			(character >= 'J' && character <= 'K') ||
			(character >= 'M' && character <= 'N') ||
			(character >= 'P' && character <= 'T') ||
			(character >= 'V' && character <= 'Z') {
			continue
		}
		return false
	}
	return true
}
