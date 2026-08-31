package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/procurementrepo"
	"github.com/torgnexa/torgnexa/internal/platform/procurement"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

const (
	ProcurementSuppliersPath      = "/api/v1/procurement/suppliers"
	ProcurementOffersPath         = "/api/v1/procurement/offers"
	ProcurementPriceListsPath     = "/api/v1/procurement/price-lists"
	ProcurementPurchaseOrdersPath = "/api/v1/procurement/purchase-orders"
	ProcurementFindingsPath       = "/api/v1/procurement/reconciliation"
)

type procurementApprovalReader interface {
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
}

type procurementAPI struct {
	repository *procurementrepo.Repository
	approvals  procurementApprovalReader
	uploadGate uploadReleaseGate
	uploads    uploads.ReleaseReader
}

func newProcurementRoutes(repository *procurementrepo.Repository, approvals procurementApprovalReader, uploadGate uploadReleaseGate, content uploads.ReleaseReader) []ProtectedRoute {
	a := procurementAPI{repository: repository, approvals: approvals, uploadGate: uploadGate, uploads: content}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: ProcurementSuppliersPath, Permission: "procurement.suppliers.read", Handler: http.HandlerFunc(a.suppliers)},
		{Method: http.MethodPost, Path: ProcurementSuppliersPath, Permission: "procurement.suppliers.write", Handler: http.HandlerFunc(a.suppliers)},
		{Method: http.MethodGet, Path: ProcurementSuppliersPath + "/", PathPrefix: true, Permission: "procurement.suppliers.read", Handler: http.HandlerFunc(a.supplier)},
		{Method: http.MethodPatch, Path: ProcurementSuppliersPath + "/", PathPrefix: true, Permission: "procurement.suppliers.write", Handler: http.HandlerFunc(a.supplier)},
		{Method: http.MethodGet, Path: ProcurementOffersPath, Permission: "procurement.offers.read", Handler: http.HandlerFunc(a.offers)},
		{Method: http.MethodPost, Path: ProcurementOffersPath, Permission: "procurement.offers.write", Handler: http.HandlerFunc(a.offers)},
		{Method: http.MethodPost, Path: ProcurementPriceListsPath + "/preview", Permission: "procurement.price_lists.write", Handler: http.HandlerFunc(a.priceListPreview)},
		{Method: http.MethodPost, Path: ProcurementPriceListsPath + "/commit", Permission: "procurement.price_lists.write", Handler: http.HandlerFunc(a.priceListCommit)},
		{Method: http.MethodGet, Path: ProcurementPurchaseOrdersPath, Permission: "procurement.purchase_orders.read", Handler: http.HandlerFunc(a.purchaseOrders)},
		{Method: http.MethodPost, Path: ProcurementPurchaseOrdersPath, Permission: "procurement.purchase_orders.write", Handler: http.HandlerFunc(a.purchaseOrders)},
		{Method: http.MethodPost, Path: ProcurementPurchaseOrdersPath + "/from-recommendations", Permission: "procurement.purchase_orders.write", Handler: http.HandlerFunc(a.purchaseOrders)},
		{Method: http.MethodGet, Path: ProcurementPurchaseOrdersPath + "/", PathPrefix: true, Permission: "procurement.purchase_orders.read", Handler: http.HandlerFunc(a.purchaseOrder)},
		{Method: http.MethodPost, Path: ProcurementPurchaseOrdersPath + "/", PathPrefix: true, Permission: "procurement.purchase_orders.write", Handler: http.HandlerFunc(a.purchaseOrder)},
		{Method: http.MethodGet, Path: ProcurementFindingsPath, Permission: "procurement.reconciliation.read", Handler: http.HandlerFunc(a.findings)},
	}
}

type supplierRequest struct {
	ID                   string                         `json:"id,omitempty"`
	LegalPartyID         string                         `json:"legal_party_id"`
	Name                 string                         `json:"name"`
	PaymentTerms         string                         `json:"payment_terms,omitempty"`
	Status               procurement.SupplierStatus     `json:"status,omitempty"`
	Currency             string                         `json:"currency"`
	LeadTimeDays         int                            `json:"lead_time_days"`
	MinimumOrderMinor    int64                          `json:"minimum_order_minor"`
	MinimumOrderCurrency string                         `json:"minimum_order_currency"`
	Contacts             []procurement.SupplierContact  `json:"contacts,omitempty"`
	Contracts            []procurement.SupplierContract `json:"contracts,omitempty"`
	ExpectedVersion      int64                          `json:"expected_version,omitempty"`
}

type offerRequest struct {
	ID                   string    `json:"id,omitempty"`
	SupplierID           string    `json:"supplier_id"`
	CanonicalOfferID     string    `json:"canonical_offer_id"`
	SupplierSKU          string    `json:"supplier_sku,omitempty"`
	GTIN                 string    `json:"gtin,omitempty"`
	SKU                  string    `json:"sku"`
	Unit                 string    `json:"unit"`
	UnitPriceMinor       int64     `json:"unit_price_minor"`
	MinimumOrderMinor    int64     `json:"minimum_order_minor"`
	Currency             string    `json:"currency"`
	MinimumOrderCurrency string    `json:"minimum_order_currency,omitempty"`
	MOQ                  string    `json:"moq"`
	CasePack             string    `json:"case_pack"`
	LeadTimeDays         int       `json:"lead_time_days"`
	Priority             int       `json:"priority"`
	ValidFrom            time.Time `json:"valid_from,omitempty"`
	ValidUntil           time.Time `json:"valid_until,omitempty"`
}

type priceListRequest struct {
	SupplierID        string                       `json:"supplier_id"`
	UploadID          string                       `json:"upload_id"`
	Format            string                       `json:"format"`
	Mapping           procurement.PriceListMapping `json:"mapping"`
	CanonicalMappings map[string]string            `json:"canonical_mappings,omitempty"`
}

type priceListCommitRequest struct {
	PreviewID    string `json:"preview_id"`
	AllowPartial bool   `json:"allow_partial"`
}

type purchaseOrderRequest struct {
	ID                   string                     `json:"id"`
	SupplierID           string                     `json:"supplier_id"`
	WarehouseID          string                     `json:"warehouse_id"`
	Currency             string                     `json:"currency"`
	RecommendationID     string                     `json:"recommendation_id,omitempty"`
	RecommendationDigest string                     `json:"recommendation_digest,omitempty"`
	ExpectedReceiptAt    *time.Time                 `json:"expected_receipt_at,omitempty"`
	Lines                []purchaseOrderLineRequest `json:"lines"`
}
type purchaseOrderLineRequest struct {
	ID             string `json:"id"`
	OfferID        string `json:"offer_id"`
	SKU            string `json:"sku"`
	Quantity       string `json:"quantity"`
	Unit           string `json:"unit"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
}

type receivingRequest struct {
	WarehouseID     string `json:"warehouse_id"`
	LineID          string `json:"line_id"`
	Quantity        string `json:"quantity"`
	Unit            string `json:"unit"`
	Status          string `json:"status,omitempty"`
	DiscrepancyCode string `json:"discrepancy_code,omitempty"`
	Note            string `json:"note,omitempty"`
}

func (a procurementAPI) suppliers(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if r.Method == http.MethodGet {
		items, err := a.repository.ListSuppliers(r.Context(), scope, 200)
		writeProcurement(w, http.StatusOK, map[string]any{"items": items}, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input supplierRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	item, err := supplierFromRequest(input, defaultID(input.ID, "sup_"+digest([]byte("supplier:" + key))[:32]), scope)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	created, err := a.repository.CreateSupplier(r.Context(), scope, item, procurementMutation(principal, r))
	writeProcurement(w, http.StatusCreated, created, err)
}

func (a procurementAPI) supplier(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	id := pathID(r.URL.Path, ProcurementSuppliersPath)
	if id == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if r.Method == http.MethodGet {
		item, err := a.repository.Supplier(r.Context(), scope, id)
		writeProcurement(w, http.StatusOK, item, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input supplierRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	input.ID = id
	item, err := supplierFromRequest(input, id, scope)
	if err != nil || input.ExpectedVersion < 1 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	updated, err := a.repository.UpdateSupplier(r.Context(), scope, item, input.ExpectedVersion, procurementMutation(principal, r))
	writeProcurement(w, http.StatusOK, updated, err)
}

func (a procurementAPI) offers(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if r.Method == http.MethodGet {
		items, err := a.repository.ListOffers(r.Context(), scope, r.URL.Query().Get("supplier_id"), 500)
		writeProcurement(w, http.StatusOK, map[string]any{"items": items}, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input offerRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	item, err := offerFromRequest(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	created, err := a.repository.CreateOffer(r.Context(), scope, item, procurementMutation(principal, r))
	writeProcurement(w, http.StatusCreated, created, err)
}

func (a procurementAPI) priceListPreview(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil || a.uploadGate == nil || a.uploads == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Price-list import is unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input priceListRequest
	if decodeStrictJSON(r, &input) != nil || input.SupplierID == "" || input.UploadID == "" || input.Format == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	ref, err := a.uploadGate.ResolveReleased(r.Context(), scope, uploads.ID(input.UploadID))
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Released upload is required")
		return
	}
	object, err := a.uploads.OpenReleased(r.Context(), scope, ref.UploadID(), ref.ObjectKey())
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "Released upload is unavailable")
		return
	}
	defer object.Close()
	data, err := io.ReadAll(io.LimitReader(object, ref.SizeBytes()+1))
	if err != nil || int64(len(data)) != ref.SizeBytes() || digest(data) != ref.SHA256() {
		writeProblem(w, http.StatusUnprocessableEntity, "Released upload integrity check failed")
		return
	}
	rows, parseErrors, err := procurement.ParsePriceList(data, input.Format, input.Mapping)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Price-list format or mapping is invalid")
		return
	}
	for i := range rows {
		if mapped := input.CanonicalMappings[strconv.Itoa(rows[i].Row)]; mapped != "" {
			rows[i].CanonicalOfferID = mapped
		}
	}
	preview := procurement.PriceListPreview{ID: "plp_" + digest([]byte("price-list:" + key))[:32], SupplierID: input.SupplierID, UploadID: input.UploadID, SourceSHA256: digest(data), MappingFingerprint: input.Mapping.Fingerprint(), Rows: rows, Errors: parseErrors}
	created, err := a.repository.CreatePriceListPreview(r.Context(), scope, preview, procurementMutation(principal, r))
	writeProcurement(w, http.StatusCreated, created, err)
}

func (a procurementAPI) priceListCommit(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input priceListCommitRequest
	if decodeStrictJSON(r, &input) != nil || input.PreviewID == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	item, err := a.repository.CommitPriceList(r.Context(), scope, input.PreviewID, input.AllowPartial, procurementMutation(principal, r))
	writeProcurement(w, http.StatusOK, item, err)
}

func (a procurementAPI) purchaseOrders(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if r.Method == http.MethodGet {
		items, err := a.repository.ListPurchaseOrders(r.Context(), scope, r.URL.Query().Get("status"), r.URL.Query().Get("supplier_id"), 200)
		writeProcurement(w, http.StatusOK, map[string]any{"items": items}, err)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	var input purchaseOrderRequest
	if decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	item, err := purchaseOrderFromRequest(input, scope, principal.Subject, key)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	if r.URL.Path == ProcurementPurchaseOrdersPath+"/from-recommendations" && (item.RecommendationID == "" || len(item.RecommendationDigest) != 64) {
		writeProblem(w, http.StatusConflict, "Fresh recommendation snapshot is required")
		return
	}
	created, err := a.repository.CreatePurchaseOrder(r.Context(), scope, item, procurementMutation(principal, r))
	writeProcurement(w, http.StatusCreated, created, err)
}

func (a procurementAPI) purchaseOrder(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	tail := strings.Trim(strings.TrimPrefix(r.URL.Path, ProcurementPurchaseOrdersPath+"/"), "/")
	parts := strings.Split(tail, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if r.Method == http.MethodGet && len(parts) == 1 {
		item, err := a.repository.PurchaseOrder(r.Context(), scope, parts[0])
		writeProcurement(w, http.StatusOK, item, err)
		return
	}
	if r.Method != http.MethodPost || len(parts) != 2 {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key is required")
		return
	}
	item, err := a.repository.PurchaseOrder(r.Context(), scope, parts[0])
	if err != nil {
		writeProcurement(w, http.StatusNotFound, item, err)
		return
	}
	m := procurementMutation(principal, r)
	approvalID := ""
	if parts[1] == "receive" {
		var input receivingRequest
		if decodeStrictJSON(r, &input) != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		quantity, err := domain.ParseDecimal(input.Quantity)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		unit, err := domain.NewUnitCode(input.Unit)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		receiptQuantity, _ := domain.NewQuantity(quantity, unit)
		receipt := procurement.ReceivingRecord{ID: "rec_" + strings.TrimPrefix(newApprovalID(), "approval_"), PurchaseOrderID: item.ID, WarehouseID: input.WarehouseID, LineID: input.LineID, Quantity: receiptQuantity, Status: input.Status, DiscrepancyCode: input.DiscrepancyCode, Note: input.Note, IdempotencyKey: key}
		if receipt.Status == "" {
			receipt.Status = "accepted"
		}
		updated, err := a.repository.Receive(r.Context(), scope, receipt, m)
		writeProcurement(w, http.StatusOK, updated, err)
		return
	}
	if parts[1] == "retry" {
		updated, err := a.repository.RetrySend(r.Context(), scope, item.ID, item.Version, m)
		writeProcurement(w, http.StatusAccepted, updated, err)
		return
	}
	if parts[1] == "send-timeout" {
		updated, err := a.repository.MarkSendUnknown(r.Context(), scope, item.ID, item.Version, m)
		writeProcurement(w, http.StatusAccepted, updated, err)
		return
	}
	if parts[1] == "approve" || parts[1] == "send" {
		approvalID := strings.TrimSpace(r.Header.Get("Approval-Request-ID"))
		if approvalID == "" || a.approvals == nil {
			writeProblem(w, http.StatusConflict, "Approved matching request required")
			return
		}
		request, err := a.approvals.Request(r.Context(), scope, approvalID)
		if err != nil || request.State != approval.StateApproved || request.ResourceID != item.ID {
			writeProblem(w, http.StatusConflict, "Approved matching request required")
			return
		}
		approvalID = request.ID
	}
	next := map[string]procurement.POStatus{"approve": procurement.POApproved, "send": procurement.POSent, "cancel": procurement.POCancelled}[parts[1]]
	if !next.Valid() {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	updated, err := a.repository.ChangePurchaseOrderStatus(r.Context(), scope, item.ID, next, item.Version, parts[1], approvalID, m)
	writeProcurement(w, http.StatusAccepted, updated, err)
}

func (a procurementAPI) findings(w http.ResponseWriter, r *http.Request) {
	scope, principal, ok := procurementScope(r)
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	m := procurementMutation(principal, r)
	_ = a.repository.DetectFindings(r.Context(), scope, time.Now().UTC(), m)
	items, err := a.repository.ListFindings(r.Context(), scope, 200)
	writeProcurement(w, http.StatusOK, map[string]any{"items": items}, err)
}

func procurementScope(r *http.Request) (tenancy.Scope, Principal, bool) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	return scope, principal, scopeOK && principalOK
}

func procurementMutation(p Principal, r *http.Request) procurement.Mutation {
	eventID := newApprovalID()
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if correlation == "" {
		correlation = eventID
	}
	return procurement.Mutation{EventID: eventID, AuditID: newApprovalID(), ActorID: p.Subject, Source: "api", CorrelationID: correlation, OccurredAt: time.Now().UTC()}
}

func supplierFromRequest(input supplierRequest, id string, scope tenancy.Scope) (procurement.SupplierRecord, error) {
	currency := input.Currency
	if currency == "" {
		currency = "RUB"
	}
	minCurrency := input.MinimumOrderCurrency
	if minCurrency == "" {
		minCurrency = currency
	}
	base, err := domain.NewCurrency(currency)
	if err != nil {
		return procurement.SupplierRecord{}, err
	}
	minimum, err := domain.NewCurrency(minCurrency)
	if err != nil {
		return procurement.SupplierRecord{}, err
	}
	status := input.Status
	if status == "" {
		status = procurement.SupplierActive
	}
	return procurement.SupplierRecord{ID: id, LegalPartyID: input.LegalPartyID, Name: input.Name, Status: status, PaymentTerms: input.PaymentTerms, Currency: base, LeadTimeDays: input.LeadTimeDays, MinimumOrderMinor: input.MinimumOrderMinor, MinimumOrderCurrency: minimum, Contacts: input.Contacts, Contracts: input.Contracts, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}, nil
}

func offerFromRequest(input offerRequest) (procurement.SupplierOfferRecord, error) {
	currency, err := domain.NewCurrency(input.Currency)
	if err != nil {
		return procurement.SupplierOfferRecord{}, err
	}
	minimumCurrency := input.MinimumOrderCurrency
	if minimumCurrency == "" {
		minimumCurrency = input.Currency
	}
	minCurrency, err := domain.NewCurrency(minimumCurrency)
	if err != nil {
		return procurement.SupplierOfferRecord{}, err
	}
	if input.Unit == "" {
		input.Unit = "PCS"
	}
	moq, err := domain.ParseDecimal(defaultValue(input.MOQ, "1"))
	if err != nil {
		return procurement.SupplierOfferRecord{}, err
	}
	pack, err := domain.ParseDecimal(defaultValue(input.CasePack, "1"))
	if err != nil {
		return procurement.SupplierOfferRecord{}, err
	}
	unit, err := domain.NewUnitCode(input.Unit)
	if err != nil {
		return procurement.SupplierOfferRecord{}, err
	}
	quantityMOQ, _ := domain.NewQuantity(moq, unit)
	quantityPack, _ := domain.NewQuantity(pack, unit)
	if input.ValidFrom.IsZero() {
		input.ValidFrom = time.Now().UTC()
	}
	if input.ValidUntil.IsZero() {
		input.ValidUntil = input.ValidFrom.Add(30 * 24 * time.Hour)
	}
	return procurement.SupplierOfferRecord{ID: input.ID, SupplierID: input.SupplierID, CanonicalOfferID: input.CanonicalOfferID, SupplierSKU: input.SupplierSKU, GTIN: input.GTIN, SKU: input.SKU, Unit: unit.String(), UnitPriceMinor: input.UnitPriceMinor, MinimumOrderMinor: input.MinimumOrderMinor, Currency: currency, MinimumOrderCurrency: minCurrency, MOQ: quantityMOQ, CasePack: quantityPack, LeadTimeDays: input.LeadTimeDays, Priority: input.Priority, ValidFrom: input.ValidFrom.UTC(), ValidUntil: input.ValidUntil.UTC(), Version: 1}, nil
}

func purchaseOrderFromRequest(input purchaseOrderRequest, scope tenancy.Scope, creator, key string) (procurement.PurchaseOrderRecord, error) {
	currency, err := domain.NewCurrency(input.Currency)
	if err != nil {
		return procurement.PurchaseOrderRecord{}, err
	}
	if input.ID == "" || input.SupplierID == "" || input.WarehouseID == "" || len(input.Lines) == 0 {
		return procurement.PurchaseOrderRecord{}, errors.New("purchase order is incomplete")
	}
	item := procurement.PurchaseOrderRecord{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), WarehouseID: input.WarehouseID, RecommendationID: input.RecommendationID, RecommendationDigest: input.RecommendationDigest, IdempotencyKey: key, CreatorID: creator}
	item.PurchaseOrder = procurement.PurchaseOrder{ID: input.ID, SupplierID: input.SupplierID, Status: procurement.PODraft, Currency: currency, Version: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	for _, line := range input.Lines {
		value, err := domain.ParseDecimal(line.Quantity)
		if err != nil {
			return procurement.PurchaseOrderRecord{}, err
		}
		unit, err := domain.NewUnitCode(line.Unit)
		if err != nil {
			return procurement.PurchaseOrderRecord{}, err
		}
		quantity, _ := domain.NewQuantity(value, unit)
		price, err := domain.NewMoney(line.UnitPriceMinor, currency)
		if err != nil {
			return procurement.PurchaseOrderRecord{}, err
		}
		item.Lines = append(item.Lines, procurement.Line{ID: line.ID, OfferID: line.OfferID, SKU: line.SKU, Quantity: quantity, UnitPrice: price})
	}
	return item, nil
}

func pathID(path, prefix string) string {
	tail := strings.Trim(strings.TrimPrefix(path, prefix+"/"), "/")
	if strings.Contains(tail, "/") {
		return strings.Split(tail, "/")[0]
	}
	return tail
}
func defaultValue(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
func defaultID(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}
func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func writeProcurement(w http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		if errors.Is(err, procurement.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if errors.Is(err, procurementrepo.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Not Found")
			return
		}
		if errors.Is(err, procurementrepo.ErrConflict) || errors.Is(err, procurement.ErrInvalidState) {
			writeProblem(w, http.StatusConflict, "Conflict")
			return
		}
		if errors.Is(err, procurementrepo.ErrNotReady) {
			writeProblem(w, http.StatusUnprocessableEntity, "Preview is not ready")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, status, value)
}
