package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/cloudbilling"
	"github.com/torgnexa/torgnexa/internal/platform/pluginmarketplace"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/cloudbillingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/fxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/retention"
	"github.com/torgnexa/torgnexa/internal/platform/settlements"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

type settlementLister interface {
	List(context.Context, tenancy.Scope, string, int) ([]settlements.Entry, error)
}

type counterpartyLister interface {
	ListCounterparties(context.Context, legalparty.Scope, int) ([]legalparty.Counterparty, error)
}

type reconciliationJobStore interface {
	CreateRun(context.Context, tenancy.Scope, reconciliation.Run) (reconciliation.Run, error)
	Run(context.Context, tenancy.Scope, string) (reconciliation.Run, error)
}

type privacyWorkflow interface {
	CreateSubjectRequest(context.Context, tenancy.Scope, retention.SubjectRequestSpec) (retention.Job, error)
}

type privacyWorkflowAdapter struct {
	service    *retention.Service
	repository retention.Repository
}

func (adapter privacyWorkflowAdapter) CreateSubjectRequest(ctx context.Context, scope tenancy.Scope, spec retention.SubjectRequestSpec) (retention.Job, error) {
	job, err := adapter.service.CreateSubjectRequest(ctx, scope, spec)
	if err == nil || adapter.repository == nil || !errors.Is(err, retention.ErrConflict) {
		return job, err
	}
	existingJob, jobErr := adapter.repository.Job(ctx, scope, spec.JobID)
	existingRequest, requestErr := adapter.repository.Request(ctx, scope, spec.RequestID)
	if jobErr == nil && requestErr == nil && existingJob.RequestID == spec.RequestID && existingRequest.Type == spec.Type && existingRequest.Subject == spec.Subject && existingRequest.CorrectionArtifactRef == spec.CorrectionArtifactRef {
		return existingJob, nil
	}
	return retention.Job{}, err
}

type cloudSubscriptionReader interface {
	CurrentSubscription(context.Context, tenancy.Scope) (cloudbilling.Subscription, error)
}

type pluginListingReader interface {
	ListVisible(context.Context, tenancy.Scope, int) ([]pluginmarketplace.ListingView, error)
}

type uploadReceiver interface {
	ReceiveWithID(context.Context, tenancy.Scope, uploads.ID, uploads.Metadata, io.Reader, uploads.Mutation) (uploads.Record, error)
}

var apiIdempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func newReservedContractRoutes(deps productionRouteDependencies, guard ...syncPolicyCapabilityGuard) []ProtectedRoute {
	var capabilityGuard syncPolicyCapabilityGuard
	if len(guard) > 0 {
		capabilityGuard = guard[0]
	}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: "/api/v1/connectors", Permission: "connectors.accounts.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listConnectors(w, r, deps.accounts) })},
		{Method: http.MethodPost, Path: "/api/v1/reconciliation/jobs", Permission: "sync.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			createReconciliationJob(w, r, deps.syncPolicies, deps.reconciliations, capabilityGuard)
		})},
		{Method: http.MethodGet, Path: "/api/v1/settlements", Permission: "settlements.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listSettlements(w, r, deps.settlements) })},
		{Method: http.MethodPost, Path: "/api/v1/privacy/requests", Permission: "privacy.requests.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { createPrivacyRequest(w, r, deps.privacy) })},
		{Method: http.MethodGet, Path: "/api/v1/counterparties", Permission: "counterparties.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listCounterparties(w, r, deps.counterparties) })},
		{Method: http.MethodGet, Path: "/api/v1/fx/rates", Permission: "fx.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listFXRates(w, r, deps.fxRates) })},
		{Method: http.MethodGet, Path: "/api/v1/cloud/subscription", Permission: "cloud.subscription.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { getCloudSubscription(w, r, deps.cloudSubscription) })},
		{Method: http.MethodPost, Path: "/api/v1/uploads", Permission: "uploads.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { createUpload(w, r, deps.uploads) })},
		{Method: http.MethodGet, Path: "/api/v1/plugins", Permission: "plugins.read", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { listPlugins(w, r, deps.plugins) })},
	}
}

func listConnectors(w http.ResponseWriter, r *http.Request, repository *connectorrepo.Repository) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	items, err := repository.ListAccounts(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), "", 100)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]connectorAccountView, 0, len(items))
	for _, item := range items {
		views = append(views, accountView(item))
	}
	writeJSON(w, http.StatusOK, views)
}

type reconciliationJobInput struct {
	PolicyID string              `json:"policy_id"`
	Mode     reconciliation.Mode `json:"mode,omitempty"`
}

func createReconciliationJob(w http.ResponseWriter, r *http.Request, policies syncPolicyRepository, repository reconciliationJobStore, guard syncPolicyCapabilityGuard) {
	scope, scoped := ScopeFromContext(r.Context())
	principal, identified := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input reconciliationJobInput
	if !scoped || !identified || policies == nil || repository == nil || !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil || strings.TrimSpace(input.PolicyID) == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if input.Mode == "" {
		input.Mode = reconciliation.ModeOnDemand
	}
	if input.Mode != reconciliation.ModeOnDemand {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	policy, err := policies.Policy(r.Context(), scope, input.PolicyID)
	if err != nil || !policy.Enabled {
		writeProblem(w, http.StatusUnprocessableEntity, "Enabled policy required")
		return
	}
	authorizationErr := authorizeSyncPolicy(r.Context(), guard, scope, policy.ConnectorAccountID, policy.EntityType, policy.Direction)
	if authorizationErr != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Connector account capability required")
		return
	}
	id := stableID("rec_api_", 40, scope, key)
	now := time.Now().UTC()
	created, err := repository.CreateRun(r.Context(), scope, reconciliation.Run{ID: id, PolicyID: input.PolicyID, Mode: input.Mode, TriggerRef: boundedActorRef(principal.Subject), Status: reconciliation.RunRunning, Version: 1, StartedAt: now, UpdatedAt: now})
	if err != nil {
		existing, lookupErr := repository.Run(r.Context(), scope, id)
		if lookupErr != nil || existing.PolicyID != input.PolicyID || existing.Mode != input.Mode {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		created = existing
	}
	writeJSON(w, http.StatusAccepted, syncRunView{created.ID, created.PolicyID, created.Mode, created.Status, created.ScannedCount, created.DriftCount, created.StartedAt, created.UpdatedAt})
}

func listSettlements(w http.ResponseWriter, r *http.Request, repository settlementLister) {
	scope, ok := ScopeFromContext(r.Context())
	limit, valid := boundedLimit(r, 50, 200)
	after, cursorErr := decodeAccountCursor(r.URL.Query().Get("cursor"))
	if !ok || !valid || cursorErr != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	items, err := repository.List(r.Context(), scope, after, limit+1)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	next := ""
	if len(items) > limit {
		next = encodeAccountCursor(items[limit-1].ID)
		items = items[:limit]
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, map[string]any{"id": item.ID, "source_system": item.SourceSystem, "source_account_id": item.SourceAccountID, "source_entry_ref": item.SourceEntryRef, "order_id": item.OrderID, "adjusts_entry_id": item.AdjustsEntryID, "fee_code": item.FeeCode, "fx_rate_ref": item.FXRateRef, "kind": item.Kind, "amount": map[string]any{"minor_units": item.Amount.MinorUnits(), "currency": item.Amount.Currency().String()}, "occurred_at": item.OccurredAt, "imported_at": item.ImportedAt, "disputed": item.Disputed})
	}
	response := map[string]any{"items": views}
	if next != "" {
		response["next_cursor"] = next
	}
	writeJSON(w, http.StatusOK, response)
}

type privacyRequestInput struct {
	Type                  retention.RequestType `json:"request_type"`
	SubjectKind           string                `json:"subject_kind"`
	SubjectOpaqueID       string                `json:"subject_opaque_id"`
	CorrectionArtifactRef string                `json:"correction_artifact_ref,omitempty"`
}

func createPrivacyRequest(w http.ResponseWriter, r *http.Request, service privacyWorkflow) {
	scope, ok := ScopeFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input privacyRequestInput
	if !ok || service == nil || !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	requestID := stableID("prq_", 48, scope, key)
	jobID := stableID("prj_", 48, scope, key)
	job, err := service.CreateSubjectRequest(r.Context(), scope, retention.SubjectRequestSpec{RequestID: requestID, JobID: jobID, Type: input.Type, Subject: retention.SubjectRef{Kind: input.SubjectKind, OpaqueID: input.SubjectOpaqueID}, CorrectionArtifactRef: input.CorrectionArtifactRef})
	if err != nil {
		if errors.Is(err, retention.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if errors.Is(err, retention.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"request_id": requestID, "job_id": job.ID, "action": job.Action, "status": job.Status, "created_at": job.CreatedAt})
}

func listCounterparties(w http.ResponseWriter, r *http.Request, repository counterpartyLister) {
	if repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	limit, valid := boundedLimit(r, 50, 100)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, err := productionScopeResolver{}.LegalPartyScope(r)
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	items, err := repository.ListCounterparties(r.Context(), scope, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, map[string]any{"id": item.ID, "code": item.Code, "party_type": item.PartyType, "party_id": item.PartyID, "role": item.Role, "status": item.Status, "version": item.Version, "created_at": item.CreatedAt, "updated_at": item.UpdatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func listFXRates(w http.ResponseWriter, r *http.Request, repository *fxrepo.Repository) {
	limit, valid := boundedLimit(r, 50, 200)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	items, err := repository.ListFacts(r.Context(), fxrepo.ListQuery{Base: strings.ToUpper(r.URL.Query().Get("base")), Quote: strings.ToUpper(r.URL.Query().Get("quote")), Limit: limit})
	if err != nil {
		if errors.Is(err, fxrepo.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func getCloudSubscription(w http.ResponseWriter, r *http.Request, repository cloudSubscriptionReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	item, err := repository.CurrentSubscription(r.Context(), scope)
	if errors.Is(err, cloudbillingrepo.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"mode": cloudbilling.ModeCommunity, "subscription": nil})
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mode": cloudbilling.ModeCloud, "subscription": map[string]any{"id": item.ID, "plan_id": item.PlanID, "plan_version": item.PlanVersion, "state": item.State, "current_period_start": item.CurrentPeriodStart, "current_period_end": item.CurrentPeriodEnd, "updated_at": item.UpdatedAt, "version": item.Version}})
}

func createUpload(w http.ResponseWriter, r *http.Request, service uploadReceiver) {
	scope, scoped := ScopeFromContext(r.Context())
	principal, identified := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !scoped || !identified || service == nil || !validIdempotencyKey(key) || mediaErr != nil || r.Body == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	filename := strings.TrimSpace(r.Header.Get("X-Filename"))
	declaredMediaType := mediaType
	declaredSize := r.ContentLength
	var source io.Reader = r.Body
	if mediaType == "application/json" {
		var input struct {
			Filename          string `json:"filename"`
			DeclaredMediaType string `json:"declared_media_type,omitempty"`
			ContentBase64     string `json:"content_base64"`
		}
		if decodeStrictJSON(r, &input) != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		content, err := base64.StdEncoding.Strict().DecodeString(input.ContentBase64)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		filename = strings.TrimSpace(input.Filename)
		declaredMediaType = strings.TrimSpace(input.DeclaredMediaType)
		declaredSize = int64(len(content))
		source = bytes.NewReader(content)
	}
	if filename == "" || declaredSize < 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	id := uploads.ID(stableID("upl_", 32, scope, key))
	eventID := stableID("upload.api.", 40, scope, key)
	actor := boundedActorRef(principal.Subject)
	record, err := service.ReceiveWithID(r.Context(), scope, id, uploads.Metadata{OriginalFilename: filename, DeclaredMediaType: declaredMediaType, DeclaredSizeBytes: declaredSize}, source, uploads.Mutation{EventID: eventID, OccurredAt: time.Now().UTC(), Source: "api", CorrelationID: stableID("idem.", 40, scope, key), ActorID: actor})
	if err != nil {
		switch {
		case errors.Is(err, uploads.ErrInvalid):
			writeProblem(w, http.StatusBadRequest, "Bad Request")
		case errors.Is(err, uploads.ErrConflict):
			writeProblem(w, http.StatusConflict, "Conflict")
		default:
			writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": record.ID, "state": record.State, "content_size_bytes": record.ContentSizeBytes, "content_sha256": record.ContentSHA256, "received_at": record.ReceivedAt, "quarantined_at": record.QuarantinedAt, "version": record.Version})
}

func listPlugins(w http.ResponseWriter, r *http.Request, repository pluginListingReader) {
	scope, ok := ScopeFromContext(r.Context())
	limit, valid := boundedLimit(r, 50, 200)
	if !ok || !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	items, err := repository.ListVisible(r.Context(), scope, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func boundedLimit(r *http.Request, fallback, maximum int) (int, bool) {
	value := fallback
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, false
		}
		value = parsed
	}
	return value, value >= 1 && value <= maximum
}

func validIdempotencyKey(value string) bool {
	return apiIdempotencyKeyPattern.MatchString(value)
}

func stableID(prefix string, hexLength int, scope tenancy.Scope, key string) string {
	digest := sha256.Sum256([]byte(scope.OrganizationID().String() + "\x00" + scope.WorkspaceID().String() + "\x00" + key))
	return prefix + hex.EncodeToString(digest[:])[:hexLength]
}

func boundedActorRef(subject string) string {
	digest := sha256.Sum256([]byte(subject))
	return "actor." + hex.EncodeToString(digest[:16])
}

// privacyQueueStore represents the durable workflow-orchestrator target. API
// admission creates only the pending job; execution remains fail-closed until a
// worker supplies the authoritative per-store processors.
type privacyQueueStore struct{}

func (privacyQueueStore) Name() string                   { return "postgres-authoritative-orchestrator" }
func (privacyQueueStore) Class() retention.StoreClass    { return retention.StoreAuthoritative }
func (privacyQueueStore) Supports(retention.Action) bool { return true }
func (privacyQueueStore) Step(context.Context, tenancy.Scope, retention.Step) (retention.StepResult, error) {
	return retention.StepResult{}, retention.ErrManualReview
}
