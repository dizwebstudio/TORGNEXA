package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalogbulk"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogbulkrepo"
)

const (
	CatalogBulkPath       = "/api/v1/catalog/bulk"
	CatalogBulkSummaryPath = CatalogBulkPath + "/summary"
	CatalogBulkPreviewPath = CatalogBulkPath + "/previews"
	CatalogBulkApplyPath   = CatalogBulkPath + "/apply"
	CatalogBulkRunPath     = CatalogBulkPath + "/runs"
)

type catalogBulkStore interface {
	SavePreview(context.Context, tenancy.Scope, catalogbulk.Preview) (catalogbulk.Preview, error)
	Preview(context.Context, tenancy.Scope, string) (catalogbulk.Preview, error)
	SaveRun(context.Context, tenancy.Scope, catalogbulk.Run) (catalogbulk.Run, error)
	Run(context.Context, tenancy.Scope, string) (catalogbulk.Run, error)
}

type catalogBulkAPI struct {
	store     catalogBulkStore
	approvals marketplacePublicationApproval
	now       func() time.Time
}

func newCatalogBulkRoutes(store *catalogbulkrepo.Repository, approvals marketplacePublicationApproval) []ProtectedRoute {
	api := catalogBulkAPI{store: store, approvals: approvals, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: CatalogBulkSummaryPath, Permission: "products.read", Handler: http.HandlerFunc(api.summary)},
		{Method: http.MethodPost, Path: CatalogBulkPreviewPath, Permission: "products.read", Handler: http.HandlerFunc(api.preview)},
		{Method: http.MethodGet, Path: CatalogBulkPreviewPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.previewItem)},
		{Method: http.MethodPost, Path: CatalogBulkApplyPath, Permission: "products.write", Handler: http.HandlerFunc(api.apply)},
		{Method: http.MethodGet, Path: CatalogBulkRunPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.runItem)},
		{Method: http.MethodPost, Path: CatalogBulkRunPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.reconcile)},
	}
}

type catalogBulkPreviewRequest struct {
	Selection    catalogbulk.SelectionSnapshot `json:"selection"`
	Projections  []catalogbulk.Projection      `json:"projections"`
	Changes      []catalogbulk.Change          `json:"changes"`
}

func (api catalogBulkAPI) summary(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	now := api.now()
	writeJSON(w, http.StatusOK, map[string]any{"items": []map[string]any{
		{"channel_id": "demo", "label": "Demo channel", "state": catalogbulk.CapabilityQualified, "capabilities": []string{"products.write", "products.media.write", "products.variants.write", "prices.write", "inventory.write"}, "observed_at": now, "fresh_until": now.Add(24 * time.Hour)},
		{"channel_id": "wb", "label": "Wildberries", "state": catalogbulk.CapabilityQualificationNeeded, "capabilities": []string{"products.read", "prices.read", "inventory.read"}, "observed_at": now, "fresh_until": now.Add(time.Hour)},
		{"channel_id": "ozon", "label": "Ozon", "state": catalogbulk.CapabilityQualificationNeeded, "capabilities": []string{"products.read", "prices.read", "inventory.read"}, "observed_at": now, "fresh_until": now.Add(time.Hour)},
	}})
}

func (api catalogBulkAPI) preview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Catalog bulk workspace is unavailable")
		return
	}
	var request catalogBulkPreviewRequest
	if decodeStrictJSON(r, &request) != nil || len(request.Projections) == 0 || len(request.Projections) > catalogbulk.MaxRows {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	for _, projection := range request.Projections {
		if projection.Draft.OrganizationID != scope.OrganizationID().String() || projection.Draft.WorkspaceID != scope.WorkspaceID().String() || projection.Validate() != nil {
			writeProblem(w, http.StatusBadRequest, "Projection is outside the current workspace")
			return
		}
	}
	// A caller may describe a channel, but cannot self-attest production
	// qualification. Only the deterministic synthetic demo target is enabled
	// here; real qualification is admitted by the connector control plane.
	for index := range request.Selection.Targets {
		if request.Selection.Targets[index].ChannelID != "demo" {
			request.Selection.Targets[index].State = catalogbulk.CapabilityQualificationNeeded
		}
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		writeCatalogBulkError(w, catalogbulk.ErrInvalid)
		return
	}
	digest := sha256.Sum256(encoded)
	previewID := stableID("cbp_", 32, scope, hex.EncodeToString(digest[:]))
	preview, err := catalogbulk.BuildPreview(previewID, scope.OrganizationID().String(), scope.WorkspaceID().String(), request.Selection, request.Projections, request.Changes, api.now())
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	created, err := api.store.SavePreview(r.Context(), scope, preview)
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, created)
}

func (api catalogBulkAPI) previewItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, CatalogBulkPreviewPath+"/")
	if !ok || api.store == nil || !safePublicationID(id) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	preview, err := api.store.Preview(r.Context(), scope, id)
	if err == nil && preview.Validate(api.now()) != nil {
		err = catalogbulk.ErrStale
	}
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

type catalogBulkApplyRequest struct {
	PreviewID string `json:"preview_id"`
}

func (api catalogBulkAPI) apply(w http.ResponseWriter, r *http.Request) {
	scope, principalOK := ScopeFromContext(r.Context())
	_, hasPrincipal := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !principalOK || !hasPrincipal || api.store == nil || api.approvals == nil || !validIdempotencyKey(key) || !safePublicationID(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and approved request are required")
		return
	}
	var request catalogBulkApplyRequest
	if decodeStrictJSON(r, &request) != nil || !safePublicationID(request.PreviewID) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	preview, err := api.store.Preview(r.Context(), scope, request.PreviewID)
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	if preview.Validate(api.now()) != nil || preview.EligibleRows == 0 {
		writeCatalogBulkError(w, catalogbulk.ErrStale)
		return
	}
	approved, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || approved.State != approval.StateApproved || approved.Action != "catalog.bulk.apply" || approved.ResourceType != "catalog_bulk_preview" || approved.ResourceID != preview.ID {
		writeCatalogBulkError(w, catalogbulk.ErrApproval)
		return
	}
	run, err := catalogbulk.NewRun(stableID("cbr_", 32, scope, key), key, approvalID, preview, api.now())
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	created, err := api.store.SaveRun(r.Context(), scope, run)
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (api catalogBulkAPI) runItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, CatalogBulkRunPath+"/"), "/"), "/")
	if !ok || api.store == nil || len(parts) != 1 || !safePublicationID(parts[0]) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	run, err := api.store.Run(r.Context(), scope, parts[0])
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

type catalogBulkReconcileRequest struct {
	RowID       string                         `json:"row_id"`
	Observation catalogbulk.RemoteObservation `json:"observation"`
}

func (api catalogBulkAPI) reconcile(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, CatalogBulkRunPath+"/"), "/"), "/")
	if !ok || api.store == nil || len(parts) != 2 || parts[1] != "reconcile" || !safePublicationID(parts[0]) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var request catalogBulkReconcileRequest
	if decodeStrictJSON(r, &request) != nil || request.RowID != request.Observation.RowID {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	run, err := api.store.Run(r.Context(), scope, parts[0])
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	preview, err := api.store.Preview(r.Context(), scope, run.PreviewID)
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	var row catalogbulk.Row
	for _, candidate := range preview.Rows {
		if candidate.ID == request.RowID { row = candidate; break }
	}
	if row.ID == "" {
		writeCatalogBulkError(w, catalogbulkrepo.ErrNotFound)
		return
	}
	result, err := catalogbulk.Reconcile(row, request.Observation)
	if err != nil {
		writeCatalogBulkError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeCatalogBulkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, catalogbulk.ErrInvalid): writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, catalogbulk.ErrTooLarge): writeProblem(w, http.StatusRequestEntityTooLarge, "Catalog bulk selection exceeds the bounded limit")
	case errors.Is(err, catalogbulk.ErrStale): writeProblem(w, http.StatusConflict, "Preview is stale and must be rebuilt")
	case errors.Is(err, catalogbulk.ErrApproval): writeProblem(w, http.StatusConflict, "Matching approval is required")
	case errors.Is(err, catalogbulk.ErrConflict), errors.Is(err, catalogbulkrepo.ErrConflict): writeProblem(w, http.StatusConflict, "Conflict")
	case errors.Is(err, catalogbulkrepo.ErrNotFound): writeProblem(w, http.StatusNotFound, "Not Found")
	default: writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
