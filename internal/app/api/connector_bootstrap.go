package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const (
	ConnectorBootstrapStatePath   = "/api/v1/connector-accounts:bootstrap"
	ConnectorBootstrapPreviewPath = "/api/v1/connector-accounts:bootstrap-preview"
	ConnectorSchedulePath         = "/api/v1/connector-accounts:schedule"
	bootstrapPreviewTTL           = 30 * time.Minute
)

type connectorBootstrapAccountStore interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
}

type connectorBootstrapStore interface {
	syncPolicyReader
	CreateBootstrapPreview(context.Context, tenancy.Scope, syncengine.BootstrapPreview) (syncengine.BootstrapPreview, error)
	HasCurrentBootstrapPreview(context.Context, tenancy.Scope, string, time.Time) (bool, error)
	CreateInitialJob(context.Context, tenancy.Scope, string, string, time.Time) (syncengine.SyncJob, error)
	PutAccountSchedule(context.Context, tenancy.Scope, syncengine.AccountSchedule, int64) (syncengine.AccountSchedule, error)
	ListBootstrapPreviews(context.Context, tenancy.Scope, int) ([]syncengine.BootstrapPreview, error)
	ListAccountSchedules(context.Context, tenancy.Scope, int) ([]syncengine.AccountSchedule, error)
	ListSyncJobs(context.Context, tenancy.Scope, int) ([]syncengine.SyncJob, error)
}

type connectorBootstrapAPI struct {
	accounts connectorBootstrapAccountStore
	store    connectorBootstrapStore
	guard    syncPolicyCapabilityGuard
	audit    auditCapturer
	now      func() time.Time
}

type bootstrapPreviewInput struct {
	AccountID       string `json:"account_id"`
	ExpectedVersion int64  `json:"expected_version"`
}

type bootstrapStartInput struct {
	PreviewID string `json:"preview_id"`
}

type schedulePutInput struct {
	AccountID       string                  `json:"account_id"`
	AccountVersion  int64                   `json:"account_version"`
	Mode            syncengine.ScheduleMode `json:"mode"`
	IntervalMinutes int                     `json:"interval_minutes"`
	Enabled         bool                    `json:"enabled"`
	ExpectedVersion int64                   `json:"expected_version"`
}

type bootstrapPreviewView struct {
	ID             string     `json:"id"`
	AccountID      string     `json:"account_id"`
	AccountVersion int64      `json:"account_version"`
	PolicyCount    int        `json:"policy_count"`
	ReadCount      int        `json:"read_count"`
	WriteCount     int        `json:"write_count"`
	CreatedAt      time.Time  `json:"created_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	ConsumedAt     *time.Time `json:"consumed_at,omitempty"`
}

type scheduleView struct {
	AccountID       string                  `json:"account_id"`
	Mode            syncengine.ScheduleMode `json:"mode"`
	IntervalMinutes int                     `json:"interval_minutes"`
	Enabled         bool                    `json:"enabled"`
	NextRunAt       *time.Time              `json:"next_run_at,omitempty"`
	LastEnqueuedAt  *time.Time              `json:"last_enqueued_at,omitempty"`
	LastJobID       string                  `json:"last_job_id,omitempty"`
	Version         int64                   `json:"version"`
	UpdatedAt       time.Time               `json:"updated_at"`
}

type syncJobView struct {
	ID                 string                   `json:"id"`
	AccountID          string                   `json:"account_id"`
	Kind               syncengine.SyncJobKind   `json:"kind"`
	Mode               syncengine.ScheduleMode  `json:"mode"`
	Status             syncengine.SyncJobStatus `json:"status"`
	CheckpointPolicyID string                   `json:"checkpoint_policy_id,omitempty"`
	StartedRuns        int                      `json:"started_runs"`
	AttemptCount       int                      `json:"attempt_count"`
	LastErrorCode      string                   `json:"last_error_code,omitempty"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

func newConnectorBootstrapRoutes(accounts connectorBootstrapAccountStore, store connectorBootstrapStore, guard syncPolicyCapabilityGuard, auditService auditCapturer) []ProtectedRoute {
	api := connectorBootstrapAPI{accounts: accounts, store: store, guard: guard, audit: auditService, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ConnectorBootstrapStatePath, Permission: "sync.read", Handler: http.HandlerFunc(api.state)},
		{Method: http.MethodPost, Path: ConnectorBootstrapPreviewPath, Permission: "sync.write", Handler: http.HandlerFunc(api.preview)},
		{Method: http.MethodPost, Path: ConnectorBootstrapStatePath, Permission: "sync.write", Handler: http.HandlerFunc(api.start)},
		{Method: http.MethodPut, Path: ConnectorSchedulePath, Permission: "sync.write", Handler: http.HandlerFunc(api.putSchedule)},
	}
}

func (api connectorBootstrapAPI) preview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input bootstrapPreviewInput
	if !ok || api.store == nil || api.accounts == nil || api.audit == nil || key == "" || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.ExpectedVersion {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if account.Status != sdk.AccountActive || account.Health.Status != sdk.HealthHealthy {
		writeProblem(w, http.StatusUnprocessableEntity, "Active healthy account required")
		return
	}
	policies, err := api.store.ListPolicies(r.Context(), scope, 200)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	policyCount, reads, writes := 0, 0, 0
	for _, policy := range policies {
		accountMatches := sameSyncIdentity(policy.ConnectorAccountID, account.ID)
		if !policy.Enabled || !accountMatches {
			continue
		}
		if authorizeSyncPolicy(r.Context(), api.guard, scope, account.ID, policy.EntityType, policy.Direction) != nil {
			writeProblem(w, http.StatusUnprocessableEntity, "Connector account capability required")
			return
		}
		policyCount++
		if policy.Direction.AllowsInbound() {
			reads++
		}
		if policy.Direction.AllowsOutbound() {
			writes++
		}
	}
	if policyCount == 0 {
		writeProblem(w, http.StatusUnprocessableEntity, "Enabled sync policy required")
		return
	}
	now := api.now().UTC()
	preview, err := api.store.CreateBootstrapPreview(r.Context(), scope, syncengine.BootstrapPreview{ID: key, AccountID: account.ID, AccountVersion: account.Version, PolicyCount: policyCount, ReadCount: reads, WriteCount: writes, CreatedAt: now, ExpiresAt: now.Add(bootstrapPreviewTTL)})
	if err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err = api.capture(r, scope, "connector.account.bootstrap_previewed", account.ID, audit.RiskWriteSafe, audit.Summary{"account_version": account.Version, "policy_count": policyCount, "read_count": reads, "write_count": writes}); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, previewView(preview))
}

func (api connectorBootstrapAPI) start(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input bootstrapStartInput
	if !ok || api.store == nil || api.audit == nil || key == "" || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	job, err := api.store.CreateInitialJob(r.Context(), scope, input.PreviewID, key, api.now().UTC())
	if errors.Is(err, syncengine.ErrPreviewUnavailable) {
		writeProblem(w, http.StatusUnprocessableEntity, "Current unconsumed preview required")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err = api.capture(r, scope, "connector.account.initial_import_queued", job.AccountID, audit.RiskWriteSafe, audit.Summary{"job_id": job.ID, "preview_id": job.PreviewID}); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusAccepted, jobView(job))
}

func (api connectorBootstrapAPI) putSchedule(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input schedulePutInput
	if !ok || api.store == nil || api.accounts == nil || api.audit == nil || key == "" || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.AccountID)
	if err != nil || account.Version != input.AccountVersion {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if input.Enabled && (account.Status != sdk.AccountActive || account.Health.Status != sdk.HealthHealthy) {
		writeProblem(w, http.StatusUnprocessableEntity, "Active healthy account required")
		return
	}
	now := api.now().UTC()
	if input.Enabled {
		hasPreview, err := api.store.HasCurrentBootstrapPreview(r.Context(), scope, account.ID, now)
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		if !hasPreview {
			writeProblem(w, http.StatusUnprocessableEntity, "Current bootstrap preview required")
			return
		}
	}
	var next *time.Time
	if input.Enabled {
		value := now.Add(time.Duration(input.IntervalMinutes) * time.Minute)
		next = &value
	}
	schedule, err := api.store.PutAccountSchedule(r.Context(), scope, syncengine.AccountSchedule{AccountID: account.ID, Mode: input.Mode, IntervalMinutes: input.IntervalMinutes, Enabled: input.Enabled, NextRunAt: next, Version: 1, CreatedAt: now, UpdatedAt: now}, input.ExpectedVersion)
	if errors.Is(err, syncengine.ErrScheduleConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if err = api.capture(r, scope, "connector.account.schedule_updated", account.ID, audit.RiskWriteSensitive, audit.Summary{"enabled": schedule.Enabled, "mode": string(schedule.Mode), "interval_minutes": schedule.IntervalMinutes, "schedule_version": schedule.Version}); err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, accountScheduleView(schedule))
}

func (api connectorBootstrapAPI) state(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	previews, err := api.store.ListBootstrapPreviews(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	schedules, err := api.store.ListAccountSchedules(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	jobs, err := api.store.ListSyncJobs(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, 500, "Internal Server Error")
		return
	}
	pv := make([]bootstrapPreviewView, 0, len(previews))
	for _, v := range previews {
		pv = append(pv, previewView(v))
	}
	sv := make([]scheduleView, 0, len(schedules))
	for _, v := range schedules {
		sv = append(sv, accountScheduleView(v))
	}
	jv := make([]syncJobView, 0, len(jobs))
	for _, v := range jobs {
		jv = append(jv, jobView(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"previews": pv, "schedules": sv, "jobs": jv})
}

func (api connectorBootstrapAPI) capture(r *http.Request, scope tenancy.Scope, action, resourceID string, risk audit.Risk, summary audit.Summary) error {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return ErrUnauthenticated
	}
	_, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: principal.Subject, Source: "api", Action: action, ResourceType: "connector_account", ResourceID: resourceID, CorrelationID: r.Header.Get("Idempotency-Key"), Risk: risk, Summary: summary})
	return err
}

func previewView(v syncengine.BootstrapPreview) bootstrapPreviewView {
	return bootstrapPreviewView{v.ID, v.AccountID, v.AccountVersion, v.PolicyCount, v.ReadCount, v.WriteCount, v.CreatedAt, v.ExpiresAt, v.ConsumedAt}
}
func accountScheduleView(v syncengine.AccountSchedule) scheduleView {
	return scheduleView{v.AccountID, v.Mode, v.IntervalMinutes, v.Enabled, v.NextRunAt, v.LastEnqueuedAt, v.LastJobID, v.Version, v.UpdatedAt}
}
func jobView(v syncengine.SyncJob) syncJobView {
	return syncJobView{v.ID, v.AccountID, v.Kind, v.Mode, v.Status, v.CheckpointPolicyID, v.StartedRuns, v.AttemptCount, v.LastErrorCode, v.CreatedAt, v.UpdatedAt}
}
