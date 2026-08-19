package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
)

const WorkspaceSettingsPath = "/api/v1/settings/workspace"

type workspaceSettingsAPI struct {
	repository *tenancyrepo.Repository
	audit      auditCapturer
}

type workspaceSettingsUpdate struct {
	OrganizationName    string `json:"organization_name"`
	WorkspaceName       string `json:"workspace_name"`
	OrganizationVersion int64  `json:"organization_version"`
	WorkspaceVersion    int64  `json:"workspace_version"`
}

type workspaceSettingsView struct {
	OrganizationName    string `json:"organization_name"`
	WorkspaceName       string `json:"workspace_name"`
	OrganizationStatus  string `json:"organization_status"`
	WorkspaceStatus     string `json:"workspace_status"`
	OrganizationVersion int64  `json:"organization_version"`
	WorkspaceVersion    int64  `json:"workspace_version"`
	UpdatedAt           string `json:"updated_at"`
}

func newWorkspaceSettingsRoutes(repository *tenancyrepo.Repository, auditService auditCapturer) []ProtectedRoute {
	api := &workspaceSettingsAPI{repository: repository, audit: auditService}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: WorkspaceSettingsPath, Permission: "settings.workspace.read", Handler: http.HandlerFunc(api.get)},
		{Method: http.MethodPut, Path: WorkspaceSettingsPath, Permission: "settings.workspace.write", Handler: http.HandlerFunc(api.update)},
	}
}

func (api *workspaceSettingsAPI) get(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	organization, err := api.repository.Organization(request.Context(), scope)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	workspace, err := api.repository.Workspace(request.Context(), scope)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	writeJSON(w, http.StatusOK, workspaceView(organization, workspace))
}

func (api *workspaceSettingsAPI) update(w http.ResponseWriter, request *http.Request) {
	scope, ok := ScopeFromContext(request.Context())
	if !ok || api == nil || api.repository == nil || api.audit == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	var input workspaceSettingsUpdate
	if err := decodeStrictJSON(request, &input); err != nil || strings.TrimSpace(request.Header.Get("Idempotency-Key")) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	organization, workspace, err := api.repository.UpdateProfile(request.Context(), scope, tenancy.ProfileUpdate{OrganizationName: input.OrganizationName, WorkspaceName: input.WorkspaceName, OrganizationVersion: input.OrganizationVersion, WorkspaceVersion: input.WorkspaceVersion})
	if err != nil {
		if errors.Is(err, tenancy.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	principal, _ := PrincipalFromContext(request.Context())
	_, err = api.audit.Capture(request.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: "settings.workspace.updated", ResourceType: "workspace", ResourceID: workspace.ID.String(), CorrelationID: request.Header.Get("Idempotency-Key"), Risk: audit.RiskWriteSafe, Summary: audit.Summary{"organization_version": organization.Version, "workspace_version": workspace.Version}})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, workspaceView(organization, workspace))
}

func workspaceView(organization tenancy.Organization, workspace tenancy.Workspace) workspaceSettingsView {
	updated := workspace.UpdatedAt
	if organization.UpdatedAt.After(updated) {
		updated = organization.UpdatedAt
	}
	return workspaceSettingsView{OrganizationName: organization.Name, WorkspaceName: workspace.Name, OrganizationStatus: string(organization.Status), WorkspaceStatus: string(workspace.Status), OrganizationVersion: organization.Version, WorkspaceVersion: workspace.Version, UpdatedAt: updated.Format("2006-01-02T15:04:05Z07:00")}
}
