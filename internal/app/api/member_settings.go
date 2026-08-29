package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
)

const MembersSettingsPath = "/api/v1/settings/members"

type memberSettingsAPI struct {
	repository *tenancyrepo.Repository
	profiles   userProfileStore
	audit      auditCapturer
}
type memberInviteRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Role        string `json:"role"`
}
type memberUpdateRequest struct {
	Role            string `json:"role"`
	Status          string `json:"status"`
	ExpectedVersion int64  `json:"expected_version"`
}
type memberView struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	OIDCSubject string    `json:"oidc_subject,omitempty"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	Version     int64     `json:"version"`
	InvitedAt   time.Time `json:"invited_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func newMemberSettingsRoutes(repository *tenancyrepo.Repository, auditService auditCapturer, profileStore ...userProfileStore) []ProtectedRoute {
	api := &memberSettingsAPI{repository: repository, audit: auditService}
	if len(profileStore) > 0 {
		api.profiles = profileStore[0]
	}
	routes := []ProtectedRoute{
		{Method: http.MethodGet, Path: MembersSettingsPath, Permission: "settings.members.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodPost, Path: MembersSettingsPath, Permission: "settings.members.write", Handler: http.HandlerFunc(api.invite)},
		{Method: http.MethodPatch, Path: MembersSettingsPath + "/", PathPrefix: true, Permission: "settings.members.write", Handler: http.HandlerFunc(api.update)},
	}
	routes = append(routes, ProtectedRoute{Method: http.MethodGet, Path: MembersSettingsPath + "/", PathPrefix: true, Permission: "settings.members.read", Handler: http.HandlerFunc(api.getProfile)})
	return routes
}

func (a *memberSettingsAPI) list(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a == nil || a.repository == nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		v, e := strconv.Atoi(raw)
		if e != nil || v < 1 || v > 100 {
			writeProblem(w, 400, "Bad Request")
			return
		}
		limit = v
	}
	cursor := r.URL.Query().Get("cursor")
	if len(cursor) > 128 {
		writeProblem(w, 400, "Bad Request")
		return
	}
	items, err := a.repository.ListMembers(r.Context(), scope, cursor, limit+1)
	if err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	next := ""
	if len(items) > limit {
		next = items[limit-1].ID
		items = items[:limit]
	}
	views := make([]memberView, 0, len(items))
	for _, m := range items {
		views = append(views, memberToView(m))
	}
	writeJSON(w, 200, map[string]any{"items": views, "next_cursor": next})
}

func (a *memberSettingsAPI) invite(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !ok || a == nil || a.repository == nil || a.audit == nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	var input memberInviteRequest
	if decodeStrictJSON(r, &input) != nil || key == "" || len(key) > 128 {
		writeProblem(w, 400, "Bad Request")
		return
	}
	member, err := a.repository.InviteMember(r.Context(), scope, tenancyrepo.Member{ID: newApprovalID(), Email: input.Email, DisplayName: input.DisplayName, Role: input.Role, InvitationKey: key})
	if err != nil {
		writeProblem(w, 409, "Conflict")
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	if _, err = a.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: "settings.member.invited", ResourceType: "workspace_member", ResourceID: member.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"role": member.Role, "status": member.Status}}); err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	writeJSON(w, 201, memberToView(member))
}

func (a *memberSettingsAPI) update(w http.ResponseWriter, r *http.Request) {
	if a.profiles != nil && strings.HasSuffix(r.URL.Path, "/profile") {
		a.updateProfile(w, r)
		return
	}
	scope, ok := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	id := strings.TrimPrefix(r.URL.Path, MembersSettingsPath+"/")
	if !ok || a == nil || a.repository == nil || a.audit == nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	if id == "" || strings.Contains(id, "/") || key == "" || len(key) > 128 {
		writeProblem(w, 400, "Bad Request")
		return
	}
	var input memberUpdateRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, 400, "Bad Request")
		return
	}
	member, err := a.repository.UpdateMember(r.Context(), scope, id, input.Role, input.Status, key, input.ExpectedVersion)
	if errors.Is(err, tenancyrepo.ErrLastAdministrator) {
		writeProblem(w, 409, "Last active administrator cannot be disabled")
		return
	}
	if err != nil {
		writeProblem(w, 409, "Conflict")
		return
	}
	principal, _ := PrincipalFromContext(r.Context())
	if _, err = a.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: "settings.member.updated", ResourceType: "workspace_member", ResourceID: member.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"role": member.Role, "status": member.Status, "version": member.Version}}); err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	writeJSON(w, 200, memberToView(member))
}

func memberToView(m tenancyrepo.Member) memberView {
	return memberView{ID: m.ID, Email: m.Email, DisplayName: m.DisplayName, OIDCSubject: m.OIDCSubject, Role: m.Role, Status: m.Status, Version: m.Version, InvitedAt: m.InvitedAt, UpdatedAt: m.UpdatedAt}
}

func (a *memberSettingsAPI) getProfile(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a == nil || a.repository == nil || a.profiles == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	id, valid := memberProfileID(r.URL.Path)
	if !valid {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	member, err := a.repository.GetMember(r.Context(), scope, id)
	if err != nil || member.OIDCSubject == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	profile, err := a.profiles.Get(r.Context(), scope, member.OIDCSubject)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, profileViewFromProfile(profile))
}

func (a *memberSettingsAPI) updateProfile(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := profileRequestContext(r)
	if !ok || a.repository == nil || a.profiles == nil || a.audit == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	id, valid := memberProfileID(r.URL.Path)
	if !valid {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key Required")
		return
	}
	var input profileUpdateInput
	if decodeStrictJSON(r, &input) != nil || input.Version < 1 || input.PictureUploadID != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	member, err := a.repository.GetMember(r.Context(), scope, id)
	if err != nil || member.OIDCSubject == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	current, err := a.profiles.Get(r.Context(), scope, member.OIDCSubject)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	update := mergeProfileUpdate(current, input)
	update.SubjectRef = current.SubjectRef
	update.ExpectedVersion = input.Version
	update.MutationKey = key
	update.MutationHash = profileMutationHash(update)
	updated, err := a.profiles.Update(r.Context(), scope, update)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	changed := changedProfileFields(current, updated)
	if len(changed) > 0 {
		if _, auditErr := a.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api", Action: "settings.member.profile.updated", ResourceType: "user_profile", ResourceID: member.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"changed_fields": changed, "version": updated.Version}}); auditErr != nil {
			writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
			return
		}
	}
	writeJSON(w, http.StatusOK, profileViewFromProfile(updated))
}

func memberProfileID(path string) (string, bool) {
	rest := strings.TrimPrefix(path, MembersSettingsPath+"/")
	if rest == path || !strings.HasSuffix(rest, "/profile") {
		return "", false
	}
	id := strings.TrimSuffix(rest, "/profile")
	return id, id != "" && !strings.Contains(id, "/")
}
