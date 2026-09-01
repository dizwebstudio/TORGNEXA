package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacegrowth"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacegrowthrepo"
)

const (
	MarketplaceGrowthRulesPath          = "/api/v1/marketplace-growth/rules"
	MarketplaceGrowthPreviewsPath       = "/api/v1/marketplace-growth/previews"
	MarketplaceGrowthPreviewItemPath    = MarketplaceGrowthPreviewsPath + "/"
	MarketplaceGrowthOperationsPath     = "/api/v1/marketplace-growth/operations"
	MarketplaceGrowthOperationItemPath  = MarketplaceGrowthOperationsPath + "/"
	MarketplaceGrowthReconciliationPath = "/api/v1/marketplace-growth/reconciliation"
	MarketplaceGrowthKillSwitchPath     = "/api/v1/marketplace-growth/kill-switch"
)

type marketplaceGrowthStore interface {
	SaveRule(context.Context, tenancy.Scope, marketplacegrowth.Rule) error
	ListRules(context.Context, tenancy.Scope, string, int) ([]marketplacegrowth.Rule, error)
	SavePreview(context.Context, tenancy.Scope, marketplacegrowth.Preview) error
	Preview(context.Context, tenancy.Scope, string) (marketplacegrowth.Preview, error)
	SaveOperation(context.Context, tenancy.Scope, marketplacegrowth.Operation) (marketplacegrowth.Operation, error)
	Operation(context.Context, tenancy.Scope, string) (marketplacegrowth.Operation, error)
	ListOperations(context.Context, tenancy.Scope, int) ([]marketplacegrowth.Operation, error)
	SaveDrifts(context.Context, tenancy.Scope, []marketplacegrowth.Drift) error
	ListDrifts(context.Context, tenancy.Scope, int) ([]marketplacegrowth.Drift, error)
	SetKillSwitch(context.Context, tenancy.Scope, marketplacegrowth.KillSwitch) error
	KillSwitch(context.Context, tenancy.Scope) (marketplacegrowth.KillSwitch, error)
}

type marketplaceGrowthAPI struct {
	store     marketplaceGrowthStore
	approvals marketplacePublicationApproval
	now       func() time.Time
}

func newMarketplaceGrowthRoutes(store *marketplacegrowthrepo.Repository, approvals marketplacePublicationApproval) []ProtectedRoute {
	api := marketplaceGrowthAPI{store: store, approvals: approvals, now: func() time.Time { return time.Now().UTC() }}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: MarketplaceGrowthRulesPath, Permission: "promotions.read", Handler: http.HandlerFunc(api.rules)},
		{Method: http.MethodPost, Path: MarketplaceGrowthPreviewsPath, Permission: "promotions.read", Handler: http.HandlerFunc(api.preview)},
		{Method: http.MethodGet, Path: MarketplaceGrowthPreviewItemPath, PathPrefix: true, Permission: "promotions.read", Handler: http.HandlerFunc(api.previewItem)},
		{Method: http.MethodGet, Path: MarketplaceGrowthOperationsPath, Permission: "ads.read", Handler: http.HandlerFunc(api.operations)},
		{Method: http.MethodPost, Path: MarketplaceGrowthOperationsPath, Permission: "ads.manage", Handler: http.HandlerFunc(api.apply)},
		{Method: http.MethodGet, Path: MarketplaceGrowthOperationItemPath, PathPrefix: true, Permission: "ads.read", Handler: http.HandlerFunc(api.operation)},
		{Method: http.MethodGet, Path: MarketplaceGrowthReconciliationPath, Permission: "ads.read", Handler: http.HandlerFunc(api.drifts)},
		{Method: http.MethodPost, Path: MarketplaceGrowthReconciliationPath, Permission: "ads.read", Handler: http.HandlerFunc(api.reconcile)},
		{Method: http.MethodGet, Path: MarketplaceGrowthKillSwitchPath, Permission: "ads.read", Handler: http.HandlerFunc(api.killSwitch)},
		{Method: http.MethodPost, Path: MarketplaceGrowthKillSwitchPath, Permission: "ads.manage", Handler: http.HandlerFunc(api.setKillSwitch)},
	}
}

func (api marketplaceGrowthAPI) rules(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace growth is unavailable")
		return
	}
	channel := strings.TrimSpace(r.URL.Query().Get("channel_id"))
	limit := growthLimit(r)
	if limit < 1 || len(channel) > 192 {
		writeProblem(w, http.StatusBadRequest, "Invalid growth rule filters")
		return
	}
	if channel == "demo" {
		writeJSON(w, http.StatusOK, map[string]any{"items": marketplacegrowth.DemoRules(api.now())})
		return
	}
	items, err := api.store.ListRules(r.Context(), scope, channel, limit)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api marketplaceGrowthAPI) preview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace growth is unavailable")
		return
	}
	var request marketplacegrowth.PreviewRequest
	if decodeStrictJSON(r, &request) != nil || request.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid promotion or advertising preview")
		return
	}
	now := api.now()
	data, err := jsonDigest(request)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid preview")
		return
	}
	preview, err := marketplacegrowth.BuildPreview(stableID("mgrp_", 32, scope, hex.EncodeToString(data[:])+now.Format(time.RFC3339Nano)), request, 1, now)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	if err := api.store.SavePreview(r.Context(), scope, preview); err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (api marketplaceGrowthAPI) previewItem(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, MarketplaceGrowthPreviewItemPath)
	if !ok || api.store == nil || !safeGrowthID(id) {
		writeProblem(w, http.StatusBadRequest, "Invalid preview id")
		return
	}
	preview, err := api.store.Preview(r.Context(), scope, id)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

type marketplaceGrowthApplyRequest struct {
	PreviewID string `json:"preview_id"`
}

func (api marketplaceGrowthAPI) apply(w http.ResponseWriter, r *http.Request) {
	scope, principalOK := ScopeFromContext(r.Context())
	_, hasPrincipal := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
	if !principalOK || !hasPrincipal || api.store == nil || api.approvals == nil || !validIdempotencyKey(key) || !safeGrowthID(approvalID) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and approved request are required")
		return
	}
	var request marketplaceGrowthApplyRequest
	if decodeStrictJSON(r, &request) != nil || !safeGrowthID(request.PreviewID) {
		writeProblem(w, http.StatusBadRequest, "Invalid preview id")
		return
	}
	preview, err := api.store.Preview(r.Context(), scope, request.PreviewID)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	approved, err := api.approvals.Request(r.Context(), scope, approvalID)
	if err != nil || approved.State != approval.StateApproved || approved.Action != "marketplace.growth.apply" || approved.ResourceType != "marketplace_growth_preview" || approved.ResourceID != preview.ID {
		writeProblem(w, http.StatusConflict, "Approved matching growth preview is required")
		return
	}
	now := api.now()
	operation, err := marketplacegrowth.NewOperation(stableID("mgop_", 32, scope, key), key, approvalID, preview, now)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	created, err := api.store.SaveOperation(r.Context(), scope, operation)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, created)
}

func (api marketplaceGrowthAPI) operations(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace growth is unavailable")
		return
	}
	limit := growthLimit(r)
	if limit < 1 {
		writeProblem(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	items, err := api.store.ListOperations(r.Context(), scope, limit)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api marketplaceGrowthAPI) operation(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	id := strings.TrimPrefix(r.URL.Path, MarketplaceGrowthOperationItemPath)
	if !ok || api.store == nil || !safeGrowthID(id) {
		writeProblem(w, http.StatusBadRequest, "Invalid operation id")
		return
	}
	item, err := api.store.Operation(r.Context(), scope, id)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (api marketplaceGrowthAPI) drifts(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace growth is unavailable")
		return
	}
	limit := growthLimit(r)
	if limit < 1 {
		writeProblem(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	items, err := api.store.ListDrifts(r.Context(), scope, limit)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (api marketplaceGrowthAPI) reconcile(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace growth is unavailable")
		return
	}
	var observation marketplacegrowth.Observation
	if decodeStrictJSON(r, &observation) != nil || !safeGrowthID(observation.OperationID) {
		writeProblem(w, http.StatusBadRequest, "Invalid growth observation")
		return
	}
	operation, err := api.store.Operation(r.Context(), scope, observation.OperationID)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	drifts, err := marketplacegrowth.Reconcile(operation, observation)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	if err := api.store.SaveDrifts(r.Context(), scope, drifts); err != nil {
		writeGrowthError(w, err)
		return
	}
	decision := "reconciled"
	if len(drifts) > 0 {
		decision = "needs_attention"
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": decision, "drifts": drifts})
}

type marketplaceGrowthKillSwitchRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (api marketplaceGrowthAPI) killSwitch(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.store == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Marketplace growth is unavailable")
		return
	}
	control, err := api.store.KillSwitch(r.Context(), scope)
	if err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, control)
}

func (api marketplaceGrowthAPI) setKillSwitch(w http.ResponseWriter, r *http.Request) {
	scope, principalOK := ScopeFromContext(r.Context())
	_, hasPrincipal := PrincipalFromContext(r.Context())
	if !principalOK || !hasPrincipal || api.store == nil {
		writeProblem(w, http.StatusForbidden, "Authenticated operator is required")
		return
	}
	var request marketplaceGrowthKillSwitchRequest
	if decodeStrictJSON(r, &request) != nil || len(request.Reason) > 500 || strings.TrimSpace(request.Reason) != request.Reason {
		writeProblem(w, http.StatusBadRequest, "Invalid kill switch request")
		return
	}
	control := marketplacegrowth.KillSwitch{Enabled: request.Enabled, Reason: request.Reason, UpdatedAt: api.now()}
	if err := api.store.SetKillSwitch(r.Context(), scope, control); err != nil {
		writeGrowthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, control)
}

func growthLimit(r *http.Request) int {
	value := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0
		}
		value = parsed
	}
	if value < 1 || value > 200 {
		return 0
	}
	return value
}

func jsonDigest(value any) ([32]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func safeGrowthID(value string) bool {
	return value != "" && len(value) <= 192 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n/")
}

func writeGrowthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, marketplacegrowth.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, marketplacegrowth.ErrFloorViolation):
		writeProblem(w, http.StatusUnprocessableEntity, "Pricing guard blocked this operation")
	case errors.Is(err, marketplacegrowth.ErrApprovalRequired):
		writeProblem(w, http.StatusConflict, "Approval is required")
	case errors.Is(err, marketplacegrowth.ErrConflict), errors.Is(err, marketplacegrowthrepo.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	case errors.Is(err, marketplacegrowthrepo.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	default:
		writeProblem(w, http.StatusInternalServerError, "Marketplace growth unavailable")
	}
}
