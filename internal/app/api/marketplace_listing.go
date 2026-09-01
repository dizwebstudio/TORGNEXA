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

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacelistingrepo"
)

const (
	MarketplaceListingsPath              = "/api/v1/marketplace-listings"
	MarketplaceListingTaxonomyPath       = MarketplaceListingsPath + "/taxonomy"
	MarketplaceListingBatchPreviewPath   = MarketplaceListingsPath + "/batch/preview"
	MarketplaceListingBatchApplyPath     = MarketplaceListingsPath + "/batch/apply"
	MarketplaceListingBatchesPath        = MarketplaceListingsPath + "/batches/"
	MarketplaceListingReadAfterWritePath = MarketplaceListingsPath + "/read-after-write"
)

type marketplaceListingStore interface {
	SaveTaxonomy(context.Context, tenancy.Scope, marketplacelisting.Taxonomy) error
	Taxonomy(context.Context, tenancy.Scope, string) (marketplacelisting.Taxonomy, error)
	SaveBatch(context.Context, tenancy.Scope, marketplacelisting.BatchRun) (marketplacelisting.BatchRun, error)
	Batch(context.Context, tenancy.Scope, string) (marketplacelisting.BatchRun, error)
}

type marketplaceListingAPI struct {
	store     marketplaceListingStore
	approvals marketplacePublicationApproval
	now       func() time.Time
}

func newMarketplaceListingRoutes(store *marketplacelistingrepo.Repository, approvals marketplacePublicationApproval) []ProtectedRoute {
	api := marketplaceListingAPI{store: store, approvals: approvals, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: MarketplaceListingTaxonomyPath, Permission: "products.read", Handler: http.HandlerFunc(api.taxonomy)},
		{Method: http.MethodPost, Path: MarketplaceListingBatchPreviewPath, Permission: "products.read", Handler: http.HandlerFunc(api.preview)},
		{Method: http.MethodPost, Path: MarketplaceListingBatchApplyPath, Permission: "products.write", Handler: http.HandlerFunc(api.apply)},
		{Method: http.MethodGet, Path: MarketplaceListingBatchesPath, PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.batch)},
		{Method: http.MethodPost, Path: MarketplaceListingReadAfterWritePath, Permission: "products.read", Handler: http.HandlerFunc(api.readAfterWrite)},
	}
}

type marketplaceListingTaxonomyQuery struct {
	TaxonomyID   string
	ConnectorID  string
	Locale       string
	Jurisdiction string
}

func (api marketplaceListingAPI) taxonomy(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace listing workspace is unavailable")
		return
	}
	query := marketplaceListingTaxonomyQuery{TaxonomyID: strings.TrimSpace(r.URL.Query().Get("taxonomy_id")), ConnectorID: strings.TrimSpace(r.URL.Query().Get("connector_id")), Locale: strings.TrimSpace(r.URL.Query().Get("locale")), Jurisdiction: strings.TrimSpace(r.URL.Query().Get("jurisdiction"))}
	if query.Locale == "" {
		query.Locale = "ru-RU"
	}
	if query.Jurisdiction == "" {
		query.Jurisdiction = "RU"
	}
	var taxonomy marketplacelisting.Taxonomy
	var err error
	if query.ConnectorID == "demo" && query.TaxonomyID == "" {
		taxonomy = marketplacelisting.DemoTaxonomy(query.ConnectorID, query.Locale, query.Jurisdiction, api.now())
	} else if safePublicationID(query.TaxonomyID) {
		taxonomy, err = api.store.Taxonomy(r.Context(), scope, query.TaxonomyID)
	} else {
		writeProblem(w, http.StatusBadRequest, "taxonomy_id or demo connector is required")
		return
	}
	if err != nil {
		writeMarketplaceListingError(w, err)
		return
	}
	fingerprint, fingerprintErr := taxonomy.ComputeFingerprint()
	if fingerprintErr != nil {
		writeMarketplaceListingError(w, marketplacelisting.ErrInvalid)
		return
	}
	taxonomy.Fingerprint = fingerprint
	writeJSON(w, http.StatusOK, taxonomy)
}

type marketplaceListingPreviewRequest struct {
	ConnectorAccountID string                              `json:"connector_account_id"`
	ConnectorID        string                              `json:"connector_id"`
	Taxonomy           marketplacelisting.Taxonomy         `json:"taxonomy"`
	Items              []marketplacelisting.BatchItem      `json:"items"`
	Operations         []marketplacelisting.BatchOperation `json:"operations,omitempty"`
}

func (api marketplaceListingAPI) preview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace listing workspace is unavailable")
		return
	}
	var request marketplaceListingPreviewRequest
	if decodeStrictJSON(r, &request) != nil || !safePublicationID(request.ConnectorAccountID) || !safePublicationID(request.ConnectorID) || request.Taxonomy.Validate() != nil || request.Taxonomy.ConnectorID != request.ConnectorID || len(request.Items) == 0 || len(request.Items) > marketplacelisting.MaxBatchItems {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	for _, item := range request.Items {
		if item.Before.OrganizationID != scope.OrganizationID().String() || item.Before.WorkspaceID != scope.WorkspaceID().String() || item.Before.SKU != item.SKU {
			writeProblem(w, http.StatusBadRequest, "Listing draft is outside the current workspace")
			return
		}
	}
	input, err := json.Marshal(request)
	if err != nil {
		writeMarketplaceListingError(w, marketplacelisting.ErrInvalid)
		return
	}
	inputSum := sha256.Sum256(input)
	previewID := stableID("mlp_", 32, scope, hex.EncodeToString(inputSum[:]))
	now := api.now()
	preview, err := marketplacelisting.BuildBatchPreview(previewID, scope.OrganizationID().String(), scope.WorkspaceID().String(), request.ConnectorAccountID, request.ConnectorID, request.Taxonomy, request.Items, request.Operations, now)
	if err != nil {
		writeMarketplaceListingError(w, err)
		return
	}
	if request.Taxonomy.Source != "synthetic.demo" {
		if err := api.store.SaveTaxonomy(r.Context(), scope, request.Taxonomy); err != nil {
			writeMarketplaceListingError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, preview)
}

type marketplaceListingApplyRequest struct {
	Preview marketplacelisting.BatchPreview `json:"preview"`
}

func (api marketplaceListingAPI) apply(w http.ResponseWriter, r *http.Request) {
	scope, principalOK := ScopeFromContext(r.Context())
	_, hasPrincipal := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !principalOK || !hasPrincipal || api.store == nil || api.approvals == nil || !validIdempotencyKey(key) || !safePublicationID(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and approved request are required")
		return
	}
	var request marketplaceListingApplyRequest
	if decodeStrictJSON(r, &request) != nil || request.Preview.Validate() != nil || request.Preview.OrganizationID != scope.OrganizationID().String() || request.Preview.WorkspaceID != scope.WorkspaceID().String() || request.Preview.EligibleCount == 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	approved, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || approved.State != approval.StateApproved || approved.Action != "marketplace.listings.batch.apply" || approved.ResourceType != "marketplace_listing_batch" || approved.ResourceID != request.Preview.ID {
		writeProblem(w, http.StatusConflict, "Approved matching batch request is required")
		return
	}
	now := api.now()
	run := marketplacelisting.BatchRun{ID: stableID("mlb_", 32, scope, key), PreviewID: request.Preview.ID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), IdempotencyKey: key, ApprovalRef: approvalID, State: marketplacelisting.BatchQueued, InputDigest: request.Preview.InputDigest, Rows: request.Preview.Rows, CreatedAt: now, UpdatedAt: now}
	created, err := api.store.SaveBatch(r.Context(), scope, run)
	if err != nil {
		writeMarketplaceListingError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (api marketplaceListingAPI) batch(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace listing workspace is unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, MarketplaceListingBatchesPath)
	if !safePublicationID(id) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	result, err := api.store.Batch(r.Context(), scope, id)
	if err != nil {
		writeMarketplaceListingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type marketplaceListingReadAfterWriteRequest struct {
	Expected       marketplacelisting.ListingDraft      `json:"expected"`
	ExpectedDigest string                               `json:"expected_digest"`
	Observation    marketplacelisting.RemoteObservation `json:"observation"`
}

func (api marketplaceListingAPI) readAfterWrite(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	var request marketplaceListingReadAfterWriteRequest
	if decodeStrictJSON(r, &request) != nil || request.Expected.OrganizationID != scope.OrganizationID().String() || request.Expected.WorkspaceID != scope.WorkspaceID().String() || request.Expected.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	digest, err := marketplacelisting.DraftDigest(request.Expected)
	if err != nil || (request.ExpectedDigest != "" && request.ExpectedDigest != digest) {
		writeProblem(w, http.StatusBadRequest, "Expected digest does not match the draft")
		return
	}
	drifts, err := marketplacelisting.Reconcile(request.Expected, digest, request.Observation)
	if err != nil {
		writeMarketplaceListingError(w, err)
		return
	}
	decision := "reconciled"
	if len(drifts) > 0 {
		decision = "needs_attention"
	}
	writeJSON(w, http.StatusOK, map[string]any{"expected_digest": digest, "decision": decision, "drifts": drifts})
}

func writeMarketplaceListingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplacelisting.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, marketplacelisting.ErrBatchTooLarge):
		writeProblem(w, http.StatusRequestEntityTooLarge, "Batch exceeds the 1000 SKU limit")
	case errors.Is(err, marketplacelisting.ErrStaleTaxonomy):
		writeProblem(w, http.StatusConflict, "Taxonomy is stale")
	case errors.Is(err, marketplacelisting.ErrConflict), errors.Is(err, marketplacelistingrepo.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	case errors.Is(err, marketplacelistingrepo.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}
