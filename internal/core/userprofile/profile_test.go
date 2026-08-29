package userprofile

import (
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

func TestIdentityAndUpdateValidateBoundedProfileValues(t *testing.T) {
	identity := Identity{
		SubjectRef:  strings.Repeat("a", 64),
		Username:    "demo",
		Email:       "demo@example.test",
		GivenName:   "Демо",
		FamilyName:  "Оператор",
		PictureURL:  "https://id.example.test/avatar.png",
		Birthdate:   "1988-04-17",
		JobTitle:    "Оператор",
		Department:  "Операции",
		PhoneNumber: "+7 900 000-00-00",
	}
	if !identity.Valid() {
		t.Fatal("valid identity was rejected")
	}
	if invalid := (Identity{SubjectRef: identity.SubjectRef, Email: "demo@example.test", Birthdate: "1988-02-31"}); invalid.Valid() {
		t.Fatal("invalid birthdate was accepted")
	}
	if invalid := (Identity{SubjectRef: identity.SubjectRef, Email: "demo@example.test", PictureURL: "javascript:alert(1)"}); invalid.Valid() {
		t.Fatal("unsafe picture URL was accepted")
	}

	update := Update{SubjectRef: identity.SubjectRef, GivenName: "Демо", MutationKey: "profile-1", MutationHash: strings.Repeat("b", 64), ExpectedVersion: 1}
	if !update.Valid() {
		t.Fatal("valid update was rejected")
	}
	update.MutationHash = "not-a-sha256"
	if update.Valid() {
		t.Fatal("invalid mutation hash was accepted")
	}
}

func TestProfileValidRequiresTenantAndUTCVersionedTimestamps(t *testing.T) {
	scope, err := tenancy.ParseScope("018f1c8a-7b3c-7def-8000-000000000001", "018f1c8a-7b3c-7def-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	profile := Profile{OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), SubjectRef: strings.Repeat("a", 64), Email: "demo@example.test", Version: 1, CreatedAt: now, UpdatedAt: now}
	if !profile.Valid() {
		t.Fatal("valid profile was rejected")
	}
	profile.UpdatedAt = now.Add(-time.Second)
	if profile.Valid() {
		t.Fatal("profile with non-monotonic timestamp was accepted")
	}
	profile.UpdatedAt = now.In(time.FixedZone("MSK", 3*60*60))
	if profile.Valid() {
		t.Fatal("profile with non-UTC timestamp was accepted")
	}
}
