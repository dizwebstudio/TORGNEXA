package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacepublicationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/publicationquality"
)

const (
	MarketplacePublicationsPath = "/api/v1/marketplace-publications"
	MarketplacePreflightPath    = MarketplacePublicationsPath + "/preflight"
)

type marketplacePublicationApproval interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
}

type marketplacePublicationAPI struct {
	repository *marketplacepublicationrepo.Repository
	quality    *publicationqualityrepo.Repository
	accounts   *connectorrepo.Repository
	approvals  marketplacePublicationApproval
	runtime    *builtinruntime.Registry
	now        func() time.Time
}

func newMarketplacePublicationRoutes(repository *marketplacepublicationrepo.Repository, quality *publicationqualityrepo.Repository, accounts *connectorrepo.Repository, approvals marketplacePublicationApproval, runtime *builtinruntime.Registry) []ProtectedRoute {
	api := marketplacePublicationAPI{repository: repository, quality: quality, accounts: accounts, approvals: approvals, runtime: runtime, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodPost, Path: MarketplacePreflightPath, Permission: "products.write", Handler: http.HandlerFunc(api.preflight)},
		{Method: http.MethodPost, Path: MarketplacePublicationsPath, Permission: "products.write", Handler: http.HandlerFunc(api.enqueue)},
		{Method: http.MethodGet, Path: MarketplacePublicationsPath, Permission: "products.read", Handler: http.HandlerFunc(api.list)},
		{Method: http.MethodGet, Path: MarketplacePublicationsPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.item)},
		{Method: http.MethodPost, Path: MarketplacePublicationsPath + "/", PathPrefix: true, Permission: "products.write", Handler: http.HandlerFunc(api.item)},
	}
}

type marketplacePreflightRequest struct {
	Snapshot         marketplacepublication.Snapshot `json:"snapshot"`
	QualityReceiptID string                          `json:"quality_receipt_id"`
}

func (api marketplacePublicationAPI) preflight(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil || api.quality == nil || api.accounts == nil || api.runtime == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace publication is unavailable")
		return
	}
	var input marketplacePreflightRequest
	if decodeStrictJSON(r, &input) != nil || input.Snapshot.Validate() != nil || input.QualityReceiptID == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	digest, err := input.Snapshot.ComputeDigest()
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, accountErr := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), input.Snapshot.Target.ConnectorAccountID)
	accountTarget := marketplacepublication.Target{OrganizationID: account.OrganizationID, WorkspaceID: account.WorkspaceID, ConnectorAccountID: account.ID, ConnectorID: account.ConnectorID}
	supportsProductsWrite := api.runtime.SupportsCapability(account.ConnectorID, "products.write")
	if accountErr != nil || account.Status != sdk.AccountActive || !input.Snapshot.Target.SameAccount(accountTarget) || account.Family != sdk.FamilyMarketplace || !supportsProductsWrite {
		writeProblem(w, http.StatusConflict, "Marketplace product publication is not enabled for this account")
		return
	}
	settings, settingsErr := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if settingsErr != nil || !sdk.CapabilityEnabled(settings, "products.write") {
		writeProblem(w, http.StatusConflict, "products.write is disabled for this account")
		return
	}
	receipt, err := api.quality.Receipt(r.Context(), scope, input.QualityReceiptID)
	if err != nil || !qualityReceiptMatches(receipt, input.Snapshot, api.now()) {
		writeProblem(w, http.StatusUnprocessableEntity, "Publication quality receipt is missing, stale or does not match the snapshot")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshot_id": input.Snapshot.ID, "snapshot_digest": digest, "decision": receipt.Decision, "quality_receipt_id": input.QualityReceiptID, "valid_until": receipt.ValidUntil.UTC()})
}

func (api marketplacePublicationAPI) enqueue(w http.ResponseWriter, r *http.Request) {
	scope, principalOK := ScopeFromContext(r.Context())
	_, hasPrincipal := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !principalOK || !hasPrincipal || api.repository == nil || api.accounts == nil || api.runtime == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated tenant are required")
		return
	}
	var request sdk.ProductPublicationRequest
	if decodeStrictJSON(r, &request) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if request.IdempotencyKey != "" && request.IdempotencyKey != key {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key does not match request")
		return
	}
	request.IdempotencyKey = key
	if request.Validate() != nil || request.Snapshot.Target.OrganizationID != scope.OrganizationID().String() || request.Snapshot.Target.WorkspaceID != scope.WorkspaceID().String() {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), scope.OrganizationID().String(), scope.WorkspaceID().String(), request.Snapshot.Target.ConnectorAccountID)
	accountTarget := marketplacepublication.Target{OrganizationID: account.OrganizationID, WorkspaceID: account.WorkspaceID, ConnectorAccountID: account.ID, ConnectorID: account.ConnectorID}
	supportsProductsWrite := api.runtime.SupportsCapability(account.ConnectorID, "products.write")
	if err != nil || account.Status != sdk.AccountActive || !request.Snapshot.Target.SameAccount(accountTarget) || account.Family != sdk.FamilyMarketplace || !supportsProductsWrite {
		writeProblem(w, http.StatusConflict, "Marketplace product publication is not enabled for this account")
		return
	}
	if !request.DryRun {
		if err := builtinruntime.RemoteOperationAdmission(request.Snapshot.Target.ConnectorID, request.Operation); err != nil {
			writeProblem(w, http.StatusConflict, "Provider qualification is required before remote publication")
			return
		}
	}
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, "products.write") {
		writeProblem(w, http.StatusConflict, "products.write is disabled for this account")
		return
	}
	if api.quality == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Publication quality is unavailable")
		return
	}
	receipt, err := api.quality.Receipt(r.Context(), scope, request.QualityReceiptID)
	if err != nil || !qualityReceiptMatches(receipt, request.Snapshot, api.now()) {
		writeProblem(w, http.StatusUnprocessableEntity, "Publication quality receipt is missing, stale or does not match the snapshot")
		return
	}
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !request.DryRun {
		if api.approvals == nil || approvalID == "" || !safePublicationID(approvalID) {
			writeProblem(w, http.StatusConflict, "Approved publication request is required")
			return
		}
		approved, approvalErr := api.approvals.Request(r.Context(), scope, approvalID)
		if approvalErr != nil || approved.State != approval.StateApproved || approved.ResourceID != request.Snapshot.ID || approved.Action != "marketplace.product.publish" {
			writeProblem(w, http.StatusConflict, "Approved matching publication request is required")
			return
		}
	}
	digest, _ := request.Snapshot.ComputeDigest()
	now := api.now()
	operation := marketplacepublication.Operation{ID: stableID("mpo_", 32, scope, key), Target: request.Snapshot.Target, SnapshotID: request.Snapshot.ID, SnapshotDigest: digest, Kind: request.Operation, State: marketplacepublication.StateQueued, IdempotencyKey: key, RemoteID: request.RemoteID, DryRun: request.DryRun, ApprovalRef: approvalID, QualityReceiptRef: request.QualityReceiptID, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := api.repository.SaveSnapshot(r.Context(), scope, request.Snapshot); err != nil {
		writeMarketplacePublicationError(w, err)
		return
	}
	created, err := api.repository.Enqueue(r.Context(), scope, operation)
	if err != nil {
		writeMarketplacePublicationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (api marketplacePublicationAPI) list(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace publication is unavailable")
		return
	}
	items, err := api.repository.ListOperations(r.Context(), scope, 100)
	if err != nil {
		writeMarketplacePublicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api marketplacePublicationAPI) item(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, MarketplacePublicationsPath+"/")
	if strings.HasSuffix(path, "/drifts") && r.Method == http.MethodGet {
		api.drifts(w, r, strings.TrimSuffix(path, "/drifts"))
		return
	}
	if strings.HasSuffix(path, "/retry") && r.Method == http.MethodPost {
		api.retry(w, r, strings.TrimSuffix(path, "/retry"))
		return
	}
	if r.Method != http.MethodGet || !safePublicationID(path) || api.repository == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	operation, err := api.repository.Operation(r.Context(), scope, path)
	if err != nil {
		writeMarketplacePublicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, operation)
}

func (api marketplacePublicationAPI) drifts(w http.ResponseWriter, r *http.Request, id string) {
	if !safePublicationID(id) || api.repository == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	items, err := api.repository.ListDrifts(r.Context(), scope, id, 100)
	if err != nil {
		writeMarketplacePublicationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api marketplacePublicationAPI) retry(w http.ResponseWriter, r *http.Request, id string) {
	if !safePublicationID(id) || api.repository == nil || !validIdempotencyKey(r.Header.Get("Idempotency-Key")) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	scope, ok := ScopeFromContext(r.Context())
	if !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	operation, err := api.repository.Operation(r.Context(), scope, id)
	if err != nil || (operation.State != marketplacepublication.StateUnknown && operation.State != marketplacepublication.StateNeedsAttention) {
		writeProblem(w, http.StatusConflict, "Only unresolved publication operations can be retried")
		return
	}
	operation.State = marketplacepublication.StateQueued
	operation.UpdatedAt = api.now()
	if err := api.repository.UpdateState(r.Context(), scope, operation, operation.Version); err != nil {
		writeMarketplacePublicationError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, operation)
}

func qualityReceiptMatches(receipt publicationquality.PublicationGateReceipt, snapshot marketplacepublication.Snapshot, now time.Time) bool {
	if receipt.Target.ChannelFamily != "marketplace" || !sameQualityTarget(receipt.Target, snapshot.Target) {
		return false
	}
	return receipt.ProductVersion == snapshot.CatalogVersion && receipt.PriceVersion == snapshot.PriceVersion && receipt.MediaVersion == snapshot.MediaVersion && receipt.MappingVersion == snapshot.MappingVersion && receipt.CapabilityVersion == snapshot.CapabilityVersion && receipt.Decision.AllowsPublication() && receipt.ValidUntil.After(now)
}

func sameQualityTarget(receipt publicationquality.Target, target marketplacepublication.Target) bool {
	left := strings.Join([]string{receipt.OrganizationID, receipt.WorkspaceID, receipt.ProductID, receipt.OfferID, receipt.ConnectorAccountID, receipt.ConnectorID, receipt.ChannelFamily, receipt.Locale, receipt.Jurisdiction}, "\x00")
	right := strings.Join([]string{target.OrganizationID, target.WorkspaceID, target.ProductID, target.OfferID, target.ConnectorAccountID, target.ConnectorID, "marketplace", target.Locale, target.Jurisdiction}, "\x00")
	return bytes.Equal([]byte(left), []byte(right))
}

func writeMarketplacePublicationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplacepublication.ErrInvalid), errors.Is(err, sdk.ErrInvalidProductPublication):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, marketplacepublication.ErrConflict), errors.Is(err, marketplacepublicationrepo.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	case errors.Is(err, marketplacepublicationrepo.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}

func safePublicationID(value string) bool {
	return value != "" && len(value) <= 192 && !strings.ContainsAny(value, "/\x00\r\n")
}

var _ interface {
	Enqueue(context.Context, tenancy.Scope, marketplacepublication.Operation) (marketplacepublication.Operation, error)
} = (*marketplacepublicationrepo.Repository)(nil)
