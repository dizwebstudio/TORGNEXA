// Package userprofile defines the bounded current-user profile contract.
package userprofile

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	// ErrInvalid means a profile or identity value is outside the public contract.
	ErrInvalid = errors.New("userprofile: invalid value")
	// ErrNotFound means the requested profile does not exist in the current tenant.
	ErrNotFound = errors.New("userprofile: profile not found")
	// ErrConflict means the optimistic version or idempotency precondition failed.
	ErrConflict = errors.New("userprofile: concurrent update")
)

var (
	subjectRefPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	uploadIDPattern   = regexp.MustCompile(`^upl_[0-9a-f]{32}$`)
	birthdatePattern  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	hashPattern       = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Identity is the bounded identity-provider projection used to initialize a
// local profile. SubjectRef is a one-way issuer+subject reference, never a raw
// OIDC identifier.
type Identity struct {
	SubjectRef  string
	Username    string
	Email       string
	GivenName   string
	FamilyName  string
	PictureURL  string
	Birthdate   string
	JobTitle    string
	Department  string
	PhoneNumber string
}

// Valid reports whether the identity can be safely used as profile seed data.
func (identity Identity) Valid() bool {
	return subjectRefPattern.MatchString(identity.SubjectRef) &&
		validText(identity.Username, 128) && validEmail(identity.Email) &&
		validText(identity.GivenName, 160) && validText(identity.FamilyName, 160) &&
		validPictureURL(identity.PictureURL) && validBirthdate(identity.Birthdate) &&
		validText(identity.JobTitle, 160) && validText(identity.Department, 160) &&
		validText(identity.PhoneNumber, 64)
}

// Profile is the tenant-scoped, editable projection of the current user.
// Email and username are synchronized from the identity provider and are not
// editable through the profile API.
type Profile struct {
	OrganizationID  tenancy.OrganizationID
	WorkspaceID     tenancy.WorkspaceID
	SubjectRef      string
	Username        string
	Email           string
	GivenName       string
	FamilyName      string
	Birthdate       string
	JobTitle        string
	Department      string
	PhoneNumber     string
	PictureUploadID string
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Valid reports whether the persisted profile satisfies its invariants.
func (profile Profile) Valid() bool {
	return profile.OrganizationID.Valid() && profile.WorkspaceID.Valid() &&
		subjectRefPattern.MatchString(profile.SubjectRef) &&
		validText(profile.Username, 128) && validEmail(profile.Email) &&
		validText(profile.GivenName, 160) && validText(profile.FamilyName, 160) &&
		validBirthdate(profile.Birthdate) && validText(profile.JobTitle, 160) &&
		validText(profile.Department, 160) && validText(profile.PhoneNumber, 64) &&
		(profile.PictureUploadID == "" || uploadIDPattern.MatchString(profile.PictureUploadID)) &&
		profile.Version >= 1 && isUTC(profile.CreatedAt) && isUTC(profile.UpdatedAt) &&
		!profile.UpdatedAt.Before(profile.CreatedAt)
}

// Update is the complete normalized mutation after PATCH semantics have been
// resolved by the HTTP adapter. Empty strings intentionally clear editable
// text fields; provider-owned username and email are excluded.
type Update struct {
	SubjectRef      string
	GivenName       string
	FamilyName      string
	Birthdate       string
	JobTitle        string
	Department      string
	PhoneNumber     string
	PictureUploadID string
	ExpectedVersion int64
	MutationKey     string
	MutationHash    string
}

// Valid reports whether an update is bounded and retry-safe.
func (update Update) Valid() bool {
	return subjectRefPattern.MatchString(update.SubjectRef) &&
		validText(update.GivenName, 160) && validText(update.FamilyName, 160) &&
		validBirthdate(update.Birthdate) && validText(update.JobTitle, 160) &&
		validText(update.Department, 160) && validText(update.PhoneNumber, 64) &&
		(update.PictureUploadID == "" || uploadIDPattern.MatchString(update.PictureUploadID)) &&
		update.ExpectedVersion >= 1 && validMutationValue(update.MutationKey, 128) &&
		hashPattern.MatchString(update.MutationHash)
}

func validMutationValue(value string, maximum int) bool {
	if value == "" || len(value) > maximum || value != strings.TrimSpace(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validText(value string, maximum int) bool {
	if len(value) > maximum || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func validEmail(value string) bool {
	return value == "" || (validText(value, 254) && strings.Contains(value, "@"))
}

func validPictureURL(value string) bool {
	if value == "" {
		return true
	}
	return validText(value, 2048) && (strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "/"))
}

func validBirthdate(value string) bool {
	if value == "" {
		return true
	}
	if !birthdatePattern.MatchString(value) {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func isUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }
