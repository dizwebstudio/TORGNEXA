package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/userprofilerepo"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

const CurrentUserProfilePath = "/api/v1/me/profile"

type userProfileStore interface {
	Ensure(context.Context, tenancy.Scope, userprofile.Identity) (userprofile.Profile, error)
	Update(context.Context, tenancy.Scope, userprofile.Update) (userprofile.Profile, error)
}

type uploadEvidenceReader interface {
	ListSecurityEvidence(context.Context, tenancy.Scope, uploads.ID, int) ([]uploads.SecurityEvidence, error)
}

type profileUpdateInput struct {
	GivenName       *string `json:"given_name"`
	FamilyName      *string `json:"family_name"`
	Birthdate       *string `json:"birthdate"`
	JobTitle        *string `json:"job_title"`
	Department      *string `json:"department"`
	PhoneNumber     *string `json:"phone_number"`
	PictureUploadID *string `json:"picture_upload_id"`
	ClearPicture    bool    `json:"clear_picture"`
	Version         int64   `json:"version"`
}

type profileAvatarDeleteInput struct {
	Version int64 `json:"version"`
}

type profileView struct {
	Username        string `json:"username"`
	Email           string `json:"email"`
	GivenName       string `json:"given_name"`
	FamilyName      string `json:"family_name"`
	Birthdate       string `json:"birthdate,omitempty"`
	JobTitle        string `json:"job_title,omitempty"`
	Department      string `json:"department,omitempty"`
	PhoneNumber     string `json:"phone_number,omitempty"`
	PictureURL      string `json:"picture_url,omitempty"`
	PictureUploadID string `json:"picture_upload_id,omitempty"`
	PictureSource   string `json:"picture_source"`
	Version         int64  `json:"version"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

type profileAPI struct {
	profiles       userProfileStore
	audit          auditCapturer
	uploads        uploadReceiver
	uploadStatus   uploadStatusReader
	uploadAccess   uploadReleaseGate
	uploadEvidence uploadEvidenceReader
}

// newUserProfileRoutes registers the current-user profile and avatar surface.
// Profile data is scoped by the authenticated subject and current workspace;
// there is intentionally no route accepting another user's identifier.
func newUserProfileRoutes(api profileAPI) []ProtectedRoute {
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: CurrentUserProfilePath, Permission: "settings.profile.read", Handler: http.HandlerFunc(api.get)},
		{Method: http.MethodPatch, Path: CurrentUserProfilePath, Permission: "settings.profile.write", Handler: http.HandlerFunc(api.update)},
		{Method: http.MethodPost, Path: CurrentUserProfilePath + "/avatar", Permission: "settings.profile.write", Handler: http.HandlerFunc(api.uploadAvatar)},
		{Method: http.MethodDelete, Path: CurrentUserProfilePath + "/avatar", Permission: "settings.profile.write", Handler: http.HandlerFunc(api.deleteAvatar)},
	}
}

func (api profileAPI) get(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := profileRequestContext(r)
	if !ok || api.profiles == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	profile, err := api.profiles.Ensure(r.Context(), scope, principal.Profile)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.view(principal, profile))
}

func (api profileAPI) update(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := profileRequestContext(r)
	if !ok || api.profiles == nil || api.audit == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input profileUpdateInput
	if decodeStrictJSON(r, &input) != nil || input.Version < 1 || (input.ClearPicture && input.PictureUploadID != nil) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	current, err := api.profiles.Ensure(r.Context(), scope, principal.Profile)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	update := mergeProfileUpdate(current, input)
	if update.PictureUploadID != "" {
		if err := api.verifyAvatarUpload(r, scope, update.PictureUploadID); err != nil {
			writeProfileError(w, err)
			return
		}
	}
	update.SubjectRef = principal.SubjectRef
	update.ExpectedVersion = input.Version
	update.MutationKey = key
	update.MutationHash = profileMutationHash(update)
	updated, err := api.profiles.Update(r.Context(), scope, update)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	changed := changedProfileFields(current, updated)
	if len(changed) > 0 {
		if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{
			ActorID: boundedActorRef(principal.Subject), Source: "api", Action: "settings.profile.updated", ResourceType: "user_profile", ResourceID: principal.SubjectRef, CorrelationID: key, Risk: audit.RiskWriteSensitive,
			Summary: audit.Summary{"changed_fields": changed, "picture_changed": current.PictureUploadID != updated.PictureUploadID, "version": updated.Version},
		}); auditErr != nil {
			writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, api.view(principal, updated))
}

func (api profileAPI) uploadAvatar(w http.ResponseWriter, r *http.Request) {
	if api.uploads == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	createUpload(w, r, imageOnlyUploadReceiver{receiver: api.uploads})
}

func (api profileAPI) deleteAvatar(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := profileRequestContext(r)
	if !ok || api.profiles == nil || api.audit == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input profileAvatarDeleteInput
	if decodeStrictJSON(r, &input) != nil || input.Version < 1 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	current, err := api.profiles.Ensure(r.Context(), scope, principal.Profile)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	update := userprofile.Update{SubjectRef: principal.SubjectRef, GivenName: current.GivenName, FamilyName: current.FamilyName, Birthdate: current.Birthdate, JobTitle: current.JobTitle, Department: current.Department, PhoneNumber: current.PhoneNumber, ExpectedVersion: input.Version, MutationKey: key}
	update.MutationHash = profileMutationHash(update)
	updated, err := api.profiles.Update(r.Context(), scope, update)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	if current.PictureUploadID != updated.PictureUploadID {
		if _, auditErr := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api", Action: "settings.profile.avatar_removed", ResourceType: "user_profile", ResourceID: principal.SubjectRef, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"picture_changed": true, "version": updated.Version}}); auditErr != nil {
			writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, api.view(principal, updated))
}

func profileRequestContext(r *http.Request) (tenancy.Scope, Principal, bool) {
	scope, scoped := ScopeFromContext(r.Context())
	principal, identified := PrincipalFromContext(r.Context())
	if !scoped || !identified || !principal.Profile.Valid() || principal.SubjectRef != principal.Profile.SubjectRef {
		return tenancy.Scope{}, Principal{}, false
	}
	return scope, principal, true
}

func mergeProfileUpdate(current userprofile.Profile, input profileUpdateInput) userprofile.Update {
	update := userprofile.Update{GivenName: current.GivenName, FamilyName: current.FamilyName, Birthdate: current.Birthdate, JobTitle: current.JobTitle, Department: current.Department, PhoneNumber: current.PhoneNumber, PictureUploadID: current.PictureUploadID}
	if input.GivenName != nil {
		update.GivenName = strings.TrimSpace(*input.GivenName)
	}
	if input.FamilyName != nil {
		update.FamilyName = strings.TrimSpace(*input.FamilyName)
	}
	if input.Birthdate != nil {
		update.Birthdate = strings.TrimSpace(*input.Birthdate)
	}
	if input.JobTitle != nil {
		update.JobTitle = strings.TrimSpace(*input.JobTitle)
	}
	if input.Department != nil {
		update.Department = strings.TrimSpace(*input.Department)
	}
	if input.PhoneNumber != nil {
		update.PhoneNumber = strings.TrimSpace(*input.PhoneNumber)
	}
	if input.ClearPicture {
		update.PictureUploadID = ""
	} else if input.PictureUploadID != nil {
		update.PictureUploadID = strings.TrimSpace(*input.PictureUploadID)
	}
	return update
}

func (api profileAPI) verifyAvatarUpload(r *http.Request, scope tenancy.Scope, uploadID string) error {
	id := uploads.ID(uploadID)
	if api.uploadAccess == nil || api.uploadEvidence == nil || api.uploadStatus == nil || !id.Valid() {
		return userprofile.ErrInvalid
	}
	if _, err := api.uploadAccess.ResolveReleased(r.Context(), scope, id); err != nil {
		return userprofile.ErrConflict
	}
	record, err := api.uploadStatus.Get(r.Context(), scope, id)
	if err != nil || record.State != uploads.StateReleased {
		return userprofile.ErrConflict
	}
	evidence, err := api.uploadEvidence.ListSecurityEvidence(r.Context(), scope, id, 1)
	if err != nil || len(evidence) != 1 || evidence[0].Decision != uploads.DecisionClean || !strings.HasPrefix(evidence[0].DetectedMediaType, "image/") {
		return userprofile.ErrConflict
	}
	return nil
}

type imageOnlyUploadReceiver struct{ receiver uploadReceiver }

func (receiver imageOnlyUploadReceiver) ReceiveWithID(ctx context.Context, scope tenancy.Scope, id uploads.ID, metadata uploads.Metadata, source io.Reader, mutation uploads.Mutation) (uploads.Record, error) {
	mediaType := strings.ToLower(strings.TrimSpace(metadata.DeclaredMediaType))
	if mediaType != "" {
		base, _, err := mime.ParseMediaType(mediaType)
		if err != nil || !strings.HasPrefix(base, "image/") {
			return uploads.Record{}, uploads.ErrInvalid
		}
	}
	ext := strings.ToLower(path.Ext(metadata.OriginalFilename))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return uploads.Record{}, uploads.ErrInvalid
	}
	return receiver.receiver.ReceiveWithID(ctx, scope, id, metadata, source, mutation)
}

func profileMutationHash(update userprofile.Update) string {
	value := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", update.ExpectedVersion, update.GivenName, update.FamilyName, update.Birthdate, update.JobTitle, update.Department, update.PhoneNumber, update.PictureUploadID)
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func changedProfileFields(before, after userprofile.Profile) []string {
	fields := make([]string, 0, 7)
	if before.GivenName != after.GivenName {
		fields = append(fields, "given_name")
	}
	if before.FamilyName != after.FamilyName {
		fields = append(fields, "family_name")
	}
	if before.Birthdate != after.Birthdate {
		fields = append(fields, "birthdate")
	}
	if before.JobTitle != after.JobTitle {
		fields = append(fields, "job_title")
	}
	if before.Department != after.Department {
		fields = append(fields, "department")
	}
	if before.PhoneNumber != after.PhoneNumber {
		fields = append(fields, "phone_number")
	}
	if before.PictureUploadID != after.PictureUploadID {
		fields = append(fields, "picture")
	}
	return fields
}

func (api profileAPI) view(principal Principal, profile userprofile.Profile) profileView {
	username, email := profile.Username, profile.Email
	if principal.Profile.Username != "" {
		username = principal.Profile.Username
	}
	if principal.Profile.Email != "" {
		email = principal.Profile.Email
	}
	view := profileView{Username: username, Email: email, GivenName: profile.GivenName, FamilyName: profile.FamilyName, Birthdate: profile.Birthdate, JobTitle: profile.JobTitle, Department: profile.Department, PhoneNumber: profile.PhoneNumber, PictureUploadID: profile.PictureUploadID, Version: profile.Version, CreatedAt: profile.CreatedAt.Format(time.RFC3339), UpdatedAt: profile.UpdatedAt.Format(time.RFC3339)}
	if profile.PictureUploadID != "" {
		view.PictureURL = uploads.ContentPath(uploads.ID(profile.PictureUploadID))
		view.PictureSource = "uploaded"
	} else if principal.Profile.PictureURL != "" {
		view.PictureURL = principal.Profile.PictureURL
		view.PictureSource = "identity_provider"
	} else {
		view.PictureSource = "none"
	}
	return view
}

func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, userprofile.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, userprofile.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, userprofile.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	default:
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
	}
}

var _ userProfileStore = (*userprofilerepo.Repository)(nil)
