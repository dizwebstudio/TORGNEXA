package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

type profileStoreStub struct {
	profile   userprofile.Profile
	ensureErr error
	updateErr error
	updates   []userprofile.Update
}

func (store *profileStoreStub) Ensure(context.Context, tenancy.Scope, userprofile.Identity) (userprofile.Profile, error) {
	if store.ensureErr != nil {
		return userprofile.Profile{}, store.ensureErr
	}
	return store.profile, nil
}

func (store *profileStoreStub) Update(_ context.Context, _ tenancy.Scope, update userprofile.Update) (userprofile.Profile, error) {
	if store.updateErr != nil {
		return userprofile.Profile{}, store.updateErr
	}
	store.updates = append(store.updates, update)
	store.profile.GivenName = update.GivenName
	store.profile.FamilyName = update.FamilyName
	store.profile.Birthdate = update.Birthdate
	store.profile.JobTitle = update.JobTitle
	store.profile.Department = update.Department
	store.profile.PhoneNumber = update.PhoneNumber
	store.profile.PictureUploadID = update.PictureUploadID
	store.profile.Version++
	store.profile.UpdatedAt = store.profile.UpdatedAt.Add(time.Second)
	return store.profile, nil
}

type auditStub struct{ entries []audit.Entry }

func (stub *auditStub) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	stub.entries = append(stub.entries, entry)
	return audit.Record{}, nil
}

type uploadReceiverStub struct{ called bool }

func (stub *uploadReceiverStub) ReceiveWithID(context.Context, tenancy.Scope, uploads.ID, uploads.Metadata, io.Reader, uploads.Mutation) (uploads.Record, error) {
	stub.called = true
	return uploads.Record{ID: "upl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: uploads.StateQuarantined}, nil
}

func profileRequest(t *testing.T, method, target string, body io.Reader, principal Principal, scope tenancy.Scope) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, target, body)
	request = request.WithContext(context.WithValue(context.WithValue(request.Context(), requestIdentityKey{}, principal), requestScopeKey{}, scope))
	return request
}

func profileFixture(t *testing.T) (tenancy.Scope, Principal, userprofile.Profile) {
	t.Helper()
	scope := validTestScope(t)
	subjectRef := strings.Repeat("a", 64)
	identity := userprofile.Identity{SubjectRef: subjectRef, Username: "demo", Email: "demo@example.test", GivenName: "Демо", FamilyName: "Оператор", JobTitle: "Оператор"}
	now := time.Date(2026, time.August, 29, 10, 0, 0, 0, time.UTC)
	profile := userprofile.Profile{OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), SubjectRef: subjectRef, Username: identity.Username, Email: identity.Email, GivenName: identity.GivenName, FamilyName: identity.FamilyName, JobTitle: identity.JobTitle, Version: 1, CreatedAt: now, UpdatedAt: now}
	principal := Principal{Issuer: "https://id.example.test", Subject: "subject-1", SubjectRef: subjectRef, Email: identity.Email, Profile: identity}
	if !principal.Profile.Valid() || !profile.Valid() {
		t.Fatal("invalid profile fixture")
	}
	return scope, principal, profile
}

func TestUserProfileGetReturnsTenantScopedProfileWithoutRawSubject(t *testing.T) {
	scope, principal, profile := profileFixture(t)
	store := &profileStoreStub{profile: profile}
	api := profileAPI{profiles: store}
	request := profileRequest(t, http.MethodGet, CurrentUserProfilePath, nil, principal, scope)
	recorder := httptest.NewRecorder()
	api.get(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["username"] != "demo" || body["email"] != "demo@example.test" || body["subject_ref"] != nil {
		t.Fatalf("unexpected profile response: %#v", body)
	}
}

func TestUserProfileUpdateRequiresIdempotencyAndAuditsChangedFields(t *testing.T) {
	scope, principal, profile := profileFixture(t)
	store := &profileStoreStub{profile: profile}
	auditor := &auditStub{}
	api := profileAPI{profiles: store, audit: auditor}

	missingKey := profileRequest(t, http.MethodPatch, CurrentUserProfilePath, strings.NewReader(`{"given_name":"Мария","version":1}`), principal, scope)
	missingKey.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	api.update(recorder, missingKey)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key status=%d", recorder.Code)
	}

	request := profileRequest(t, http.MethodPatch, CurrentUserProfilePath, strings.NewReader(`{"given_name":"Мария","job_title":"Руководитель","version":1}`), principal, scope)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "profile-update-1")
	recorder = httptest.NewRecorder()
	api.update(recorder, request)
	if recorder.Code != http.StatusOK || len(auditor.entries) != 1 {
		t.Fatalf("status=%d audits=%d body=%s", recorder.Code, len(auditor.entries), recorder.Body.String())
	}
	if auditor.entries[0].Action != "settings.profile.updated" || auditor.entries[0].Summary["picture_changed"] != false {
		t.Fatalf("unexpected audit entry: %#v", auditor.entries[0])
	}
	changed, ok := auditor.entries[0].Summary["changed_fields"].([]string)
	if !ok || len(changed) != 2 || changed[0] != "given_name" || changed[1] != "job_title" {
		t.Fatalf("unexpected changed fields: %#v", auditor.entries[0].Summary["changed_fields"])
	}
}

func TestImageOnlyUploadReceiverRejectsNonImageFilenameBeforeStorage(t *testing.T) {
	stub := &uploadReceiverStub{}
	receiver := imageOnlyUploadReceiver{receiver: stub}
	_, err := receiver.ReceiveWithID(context.Background(), validTestScope(t), "upl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", uploads.Metadata{OriginalFilename: "payload.txt", DeclaredMediaType: "text/plain", DeclaredSizeBytes: 1}, strings.NewReader("x"), uploads.Mutation{})
	if !errors.Is(err, uploads.ErrInvalid) || stub.called {
		t.Fatalf("non-image upload was accepted: err=%v called=%v", err, stub.called)
	}
	_, err = receiver.ReceiveWithID(context.Background(), validTestScope(t), "upl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", uploads.Metadata{OriginalFilename: "avatar.png", DeclaredMediaType: "image/png", DeclaredSizeBytes: 1}, strings.NewReader("x"), uploads.Mutation{})
	if err != nil || !stub.called {
		t.Fatalf("image upload was not delegated: err=%v called=%v", err, stub.called)
	}
}
