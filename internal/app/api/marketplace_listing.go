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
	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/marketplacetaxonomy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacelistingrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacepublicationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
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
	remote    marketplaceListingRemoteDependencies
	now       func() time.Time
}

type marketplaceListingRemoteDependencies struct {
	publications *marketplacepublicationrepo.Repository
	quality      *publicationqualityrepo.Repository
}

func newMarketplaceListingRoutes(store *marketplacelistingrepo.Repository, approvals marketplacePublicationApproval, remote ...marketplaceListingRemoteDependencies) []ProtectedRoute {
	var dependencies marketplaceListingRemoteDependencies
	if len(remote) > 0 {
		dependencies = remote[0]
	}
	api := marketplaceListingAPI{store: store, approvals: approvals, remote: dependencies, now: func() time.Time { return time.Now().UTC() }}
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
	ChannelID    string
	Locale       string
	Jurisdiction string
}

func (api marketplaceListingAPI) taxonomy(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace listing workspace is unavailable")
		return
	}
	query := marketplaceListingTaxonomyQuery{TaxonomyID: strings.TrimSpace(r.URL.Query().Get("taxonomy_id")), ChannelID: strings.TrimSpace(r.URL.Query().Get("connector_id")), Locale: strings.TrimSpace(r.URL.Query().Get("locale")), Jurisdiction: strings.TrimSpace(r.URL.Query().Get("jurisdiction"))}
	if query.Locale == "" {
		query.Locale = "ru-RU"
	}
	if query.Jurisdiction == "" {
		query.Jurisdiction = "RU"
	}
	var taxonomy marketplacelisting.Taxonomy
	var err error
	if query.ChannelID == "demo" && query.TaxonomyID == "" {
		taxonomy = marketplacelisting.DemoTaxonomy(query.ChannelID, query.Locale, query.Jurisdiction, api.now())
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
	taxonomy = marketplacetaxonomy.AttachProfile(taxonomy)
	fingerprint, fingerprintErr := taxonomy.ComputeFingerprint()
	if fingerprintErr != nil {
		writeMarketplaceListingError(w, marketplacelisting.ErrInvalid)
		return
	}
	taxonomy.Fingerprint = fingerprint
	writeJSON(w, http.StatusOK, taxonomy)
}

type marketplaceListingPreviewRequest struct {
	ChannelAccountID string                              `json:"connector_account_id"`
	ChannelID        string                              `json:"connector_id"`
	Taxonomy         marketplacelisting.Taxonomy         `json:"taxonomy"`
	Items            []marketplacelisting.BatchItem      `json:"items"`
	Operations       []marketplacelisting.BatchOperation `json:"operations,omitempty"`
}

func (api marketplaceListingAPI) preview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace listing workspace is unavailable")
		return
	}
	var request marketplaceListingPreviewRequest
	if decodeStrictJSON(r, &request) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	request.Taxonomy = marketplacetaxonomy.AttachProfile(request.Taxonomy)
	if !safePublicationID(request.ChannelAccountID) || !safePublicationID(request.ChannelID) || request.Taxonomy.Validate() != nil || request.Taxonomy.ChannelID != request.ChannelID || len(request.Items) == 0 || len(request.Items) > marketplacelisting.MaxBatchItems {
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
	preview, err := marketplacelisting.BuildBatchPreview(previewID, scope.OrganizationID().String(), scope.WorkspaceID().String(), request.ChannelAccountID, request.ChannelID, request.Taxonomy, request.Items, request.Operations, now)
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
	Preview      marketplacelisting.BatchPreview `json:"preview"`
	Publications []marketplaceListingRemoteItem  `json:"publications,omitempty"`
}

// marketplaceListingRemoteItem is the explicit bridge from the approved
// listing projection to the existing immutable publication snapshot. The
// listing API never assembles provider payloads and never stores credentials.
type marketplaceListingRemoteItem struct {
	SKU              string                               `json:"sku"`
	Snapshot         marketplacepublication.Snapshot     `json:"snapshot"`
	Operation        marketplacepublication.OperationKind `json:"operation"`
	RemoteID         string                               `json:"remote_id,omitempty"`
	RemoteOperationID string                              `json:"remote_operation_id,omitempty"`
	QualityReceiptID string                               `json:"quality_receipt_id"`
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
	if len(request.Publications) > request.Preview.EligibleCount {
		writeProblem(w, http.StatusBadRequest, "Remote publications exceed eligible preview rows")
		return
	}
	if len(request.Publications) > 0 && (api.remote.publications == nil || api.remote.quality == nil) {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace publication queue is unavailable")
		return
	}
	rows := make(map[string]marketplacelisting.BatchRow, len(request.Preview.Rows))
	for _, row := range request.Preview.Rows {
		rows[row.SKU] = row
	}
	operationIDs := make([]string, 0, len(request.Publications))
	snapshots := make([]marketplacepublication.Snapshot, 0, len(request.Publications))
	operations := make([]marketplacepublication.Operation, 0, len(request.Publications))
	seenSKUs := make(map[string]struct{}, len(request.Publications))
	now := api.now()
	for _, item := range request.Publications {
		row, exists := rows[item.SKU]
		if !exists || !row.Eligible || item.Snapshot.Validate() != nil || item.Snapshot.Target.OrganizationID != scope.OrganizationID().String() || item.Snapshot.Target.WorkspaceID != scope.WorkspaceID().String() || item.Snapshot.Target.ConnectorAccountID != request.Preview.ChannelAccountID || item.Snapshot.Target.ConnectorID != request.Preview.ChannelID || item.Snapshot.SKU != item.SKU || item.Snapshot.CategoryCode != row.After.CategoryCode || !item.Operation.Valid() || !validRemotePublicationIdentity(item.Operation, item.RemoteID, item.RemoteOperationID) || !safePublicationID(item.QualityReceiptID) {
			writeProblem(w, http.StatusBadRequest, "Remote publication does not match the approved preview")
			return
		}
		if _, exists := seenSKUs[item.SKU]; exists {
			writeProblem(w, http.StatusConflict, "Duplicate SKU in remote publication plan")
			return
		}
		if err := marketplacetaxonomy.RemoteOperationAdmission(request.Preview.ChannelID, item.Operation); err != nil {
			writeMarketplaceListingError(w, err)
			return
		}
		seenSKUs[item.SKU] = struct{}{}
		receipt, receiptErr := api.remote.quality.Receipt(r.Context(), scope, item.QualityReceiptID)
		if receiptErr != nil || !qualityReceiptMatches(receipt, item.Snapshot, now) {
			writeProblem(w, http.StatusUnprocessableEntity, "Publication quality receipt is missing, stale or does not match the snapshot")
			return
		}
		digest, digestErr := item.Snapshot.ComputeDigest()
		if digestErr != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		operationKey := stableID("mlk_", 32, scope, key+"\x00"+item.SKU)
		operation := marketplacepublication.Operation{ID: stableID("mlo_", 32, scope, key+"\x00"+item.SKU), Target: item.Snapshot.Target, SnapshotID: item.Snapshot.ID, SnapshotDigest: digest, Kind: item.Operation, State: marketplacepublication.StateQueued, IdempotencyKey: operationKey, RemoteID: item.RemoteID, RemoteOperationID: item.RemoteOperationID, ApprovalRef: approvalID, QualityReceiptRef: item.QualityReceiptID, Version: 1, CreatedAt: now, UpdatedAt: now}
		snapshots = append(snapshots, item.Snapshot)
		operations = append(operations, operation)
		operationIDs = append(operationIDs, operation.ID)
	}
	run := marketplacelisting.BatchRun{ID: stableID("mlb_", 32, scope, key), PreviewID: request.Preview.ID, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), IdempotencyKey: key, ApprovalRef: approvalID, State: marketplacelisting.BatchQueued, InputDigest: request.Preview.InputDigest, Rows: request.Preview.Rows, RemoteOperationIDs: operationIDs, CreatedAt: now, UpdatedAt: now}
	created, err := api.store.SaveBatch(r.Context(), scope, run)
	if err != nil {
		writeMarketplaceListingError(w, err)
		return
	}
	if len(operations) > 0 {
		if _, err := api.remote.publications.EnqueueBatch(r.Context(), scope, snapshots, operations); err != nil {
			writeMarketplaceListingError(w, err)
			return
		}
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
	case errors.Is(err, marketplacepublication.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Remote publication plan is invalid")
	case errors.Is(err, marketplacetaxonomy.ErrRemoteOperationNotQualified):
		writeProblem(w, http.StatusConflict, "Provider qualification is required before remote listing apply")
	case errors.Is(err, marketplacepublication.ErrConflict), errors.Is(err, marketplacepublicationrepo.ErrConflict):
		writeProblem(w, http.StatusConflict, "Publication operation conflicts with existing evidence")
	case errors.Is(err, marketplacepublicationrepo.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
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

func validPublicationRemoteID(value string) bool {
	return value == "" || safePublicationID(value)
}

func validRemotePublicationIdentity(kind marketplacepublication.OperationKind, remoteID, remoteOperationID string) bool {
	if !validPublicationRemoteID(remoteID) || !validPublicationRemoteID(remoteOperationID) {
		return false
	}
	switch kind {
	case marketplacepublication.OperationCreateProduct:
		return remoteID == "" && remoteOperationID == ""
	case marketplacepublication.OperationStatusRead:
		return remoteID != "" || remoteOperationID != ""
	default:
		// The writer contract requires an existing remote product for update,
		// lifecycle and media operations. An operation ID alone is only a
		// status-reader identity and must not be mistaken for a product ID.
		return remoteID != ""
	}
}
