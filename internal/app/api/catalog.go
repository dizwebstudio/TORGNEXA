package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/pim"
	"github.com/torgnexa/torgnexa/internal/core/pricing"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogimagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pimrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/pricingrepo"
)

const CatalogCategoriesPath = "/api/v1/catalog/categories"

type catalogAPI struct {
	catalog *catalogrepo.Repository
	prices  *pricingrepo.Repository
	pim     *pimrepo.Repository
	images  *catalogimagerepo.Repository
}
type productInput struct {
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     int64  `json:"version"`
	Status      string `json:"status"`
	CategoryID  string `json:"category_id"`
}
type offerInput struct {
	SKU     string `json:"sku"`
	GTIN    string `json:"gtin"`
	Version int64  `json:"version"`
	Status  string `json:"status"`
}
type priceInput struct {
	Kind       string `json:"kind"`
	MinorUnits int64  `json:"minor_units"`
	Currency   string `json:"currency"`
	Version    int64  `json:"version"`
}
type categoryInput struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
}
type imageInput struct {
	URL      string `json:"url"`
	AltText  string `json:"alt_text"`
	Position int    `json:"position"`
	Version  int64  `json:"version"`
}

func newCatalogRoutes(a catalogAPI) []ProtectedRoute {
	return []ProtectedRoute{
		{Method: http.MethodPost, Path: ProductSearchPath, Permission: "products.write", Handler: http.HandlerFunc(a.createProduct)},
		{Method: http.MethodGet, Path: CatalogCategoriesPath, Permission: "products.read", Handler: http.HandlerFunc(a.categories)},
		{Method: http.MethodPost, Path: CatalogCategoriesPath, Permission: "products.write", Handler: http.HandlerFunc(a.createCategory)},
		{Method: http.MethodGet, Path: ProductSearchPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(a.productSubroute)},
		{Method: http.MethodPost, Path: ProductSearchPath + "/", PathPrefix: true, Permission: "products.write", Handler: http.HandlerFunc(a.productSubroute)},
		{Method: http.MethodPatch, Path: ProductSearchPath + "/", PathPrefix: true, Permission: "products.write", Handler: http.HandlerFunc(a.productSubroute)},
	}
}

func catalogScopes(r *http.Request) (tenancy.Scope, catalog.Scope, pricing.Scope, pim.Scope, Principal, bool) {
	s, ok := ScopeFromContext(r.Context())
	p, pok := PrincipalFromContext(r.Context())
	if !ok || !pok {
		return tenancy.Scope{}, catalog.Scope{}, pricing.Scope{}, pim.Scope{}, Principal{}, false
	}
	cs, e1 := catalog.ParseScope(s.OrganizationID().String(), s.WorkspaceID().String())
	ps, e2 := pricing.ParseScope(s.OrganizationID().String(), s.WorkspaceID().String())
	ms, e3 := pim.ParseScope(s.OrganizationID().String(), s.WorkspaceID().String())
	return s, cs, ps, ms, p, e1 == nil && e2 == nil && e3 == nil
}
func catalogMutation(p Principal, r *http.Request) catalog.Mutation {
	id := newApprovalID()
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if correlation == "" {
		correlation = id
	}
	return catalog.Mutation{EventID: id, OccurredAt: time.Now().UTC(), Source: "api", CorrelationID: correlation, ActorID: p.Subject}
}
func pricingMutation(p Principal, r *http.Request) pricing.Mutation {
	id := newApprovalID()
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if correlation == "" {
		correlation = id
	}
	return pricing.Mutation{EventID: id, AuditID: newApprovalID(), ActorID: p.Subject, Source: "api", CorrelationID: correlation, OccurredAt: time.Now().UTC()}
}
func pimMutation(p Principal, r *http.Request) pim.Mutation {
	id := newApprovalID()
	correlation := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if correlation == "" {
		correlation = id
	}
	return pim.Mutation{EventID: id, AuditID: newApprovalID(), ActorID: p.Subject, Source: "api", CorrelationID: correlation, OccurredAt: time.Now().UTC()}
}

func (a catalogAPI) createProduct(w http.ResponseWriter, r *http.Request) {
	_, cs, _, ms, p, ok := catalogScopes(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	var in productInput
	if !decodeCatalogJSON(w, r, &in) {
		return
	}
	id, _ := catalog.ParseProductID(newApprovalID())
	v, err := a.catalog.CreateProduct(r.Context(), cs, catalog.CreateProduct{ID: id, Code: in.Code, Title: in.Title, Description: in.Description}, catalogMutation(p, r))
	if err == nil && in.CategoryID != "" {
		err = a.assignCategory(r.Context(), ms, p, r, v.ID.String(), in.CategoryID)
	}
	writeCatalogResult(w, http.StatusCreated, v, err)
}

func (a catalogAPI) productSubroute(w http.ResponseWriter, r *http.Request) {
	_, cs, ps, ms, p, ok := catalogScopes(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, ProductSearchPath+"/")
	parts := strings.Split(strings.Trim(tail, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(w, 404, "Not Found")
		return
	}
	pid, e := catalog.ParseProductID(parts[0])
	if e != nil {
		writeProblem(w, 400, "Bad Request")
		return
	}
	if len(parts) == 1 {
		if r.Method == http.MethodGet {
			a.detail(w, r, cs, ps, ms, pid)
			return
		}
		var in productInput
		if !decodeCatalogJSON(w, r, &in) {
			return
		}
		var v catalog.Product
		if in.Status != "" {
			v, e = a.catalog.ChangeProductStatus(r.Context(), cs, catalog.ChangeProductStatus{ID: pid, ExpectedVersion: in.Version, Status: catalog.Status(in.Status)}, catalogMutation(p, r))
		} else {
			v, e = a.catalog.UpdateProduct(r.Context(), cs, catalog.UpdateProduct{ID: pid, ExpectedVersion: in.Version, Title: in.Title, Description: in.Description}, catalogMutation(p, r))
		}
		writeCatalogResult(w, 200, v, e)
		return
	}
	switch parts[1] {
	case "offers":
		if len(parts) == 2 && r.Method == http.MethodPost {
			var in offerInput
			if !decodeCatalogJSON(w, r, &in) {
				return
			}
			oid, _ := catalog.ParseOfferID(newApprovalID())
			v, e := a.catalog.CreateOffer(r.Context(), cs, catalog.CreateOffer{ID: oid, ProductID: pid, SKU: in.SKU, GTIN: in.GTIN}, catalogMutation(p, r))
			writeCatalogResult(w, 201, v, e)
			return
		}
		if len(parts) == 3 && r.Method == http.MethodPatch {
			oid, e := catalog.ParseOfferID(parts[2])
			if e != nil {
				writeProblem(w, 400, "Bad Request")
				return
			}
			var in offerInput
			if !decodeCatalogJSON(w, r, &in) {
				return
			}
			var v catalog.Offer
			if in.Status != "" {
				v, e = a.catalog.ChangeOfferStatus(r.Context(), cs, catalog.ChangeOfferStatus{ID: oid, ExpectedVersion: in.Version, Status: catalog.Status(in.Status)}, catalogMutation(p, r))
			} else {
				v, e = a.catalog.UpdateOffer(r.Context(), cs, catalog.UpdateOffer{ID: oid, ExpectedVersion: in.Version, GTIN: in.GTIN}, catalogMutation(p, r))
			}
			writeCatalogResult(w, 200, v, e)
			return
		}
	case "prices":
		if len(parts) == 3 && r.Method == http.MethodPost {
			oid, e := pricing.ParseOfferID(parts[2])
			if e != nil {
				writeProblem(w, 400, "Bad Request")
				return
			}
			var in priceInput
			if !decodeCatalogJSON(w, r, &in) {
				return
			}
			currency, e := pricing.NewCurrency(strings.ToUpper(in.Currency))
			money, me := pricing.NewMoney(in.MinorUnits, currency)
			if e != nil || me != nil {
				writeProblem(w, 400, "Bad Request")
				return
			}
			id, _ := pricing.ParsePriceID(newApprovalID())
			v, e := a.prices.Create(r.Context(), ps, pricing.CreatePrice{ID: id, OfferID: oid, Kind: pricing.Kind(in.Kind), Amount: money}, pricingMutation(p, r))
			writeCatalogResult(w, 201, priceViewOf(v), e)
			return
		}
		if len(parts) == 3 && r.Method == http.MethodPatch {
			id, e := pricing.ParsePriceID(parts[2])
			var in priceInput
			if e != nil || !decodeCatalogJSON(w, r, &in) {
				return
			}
			old, e := a.prices.Price(r.Context(), ps, id)
			if e != nil {
				writeCatalogResult(w, 0, nil, e)
				return
			}
			money, _ := pricing.NewMoney(in.MinorUnits, old.Amount.Currency())
			v, e := a.prices.Update(r.Context(), ps, pricing.UpdatePrice{ID: id, ExpectedVersion: in.Version, Amount: money}, pricingMutation(p, r))
			writeCatalogResult(w, 200, priceViewOf(v), e)
			return
		}
	case "categories":
		if len(parts) == 3 && r.Method == http.MethodPost {
			e := a.assignCategory(r.Context(), ms, p, r, pid.String(), parts[2])
			writeCatalogResult(w, 200, map[string]bool{"assigned": e == nil}, e)
			return
		}
	case "images":
		if len(parts) == 2 && r.Method == http.MethodPost {
			var in imageInput
			if !decodeCatalogJSON(w, r, &in) {
				return
			}
			v, e := a.images.Create(r.Context(), mustScope(r), catalogimagerepo.Image{ID: newApprovalID(), ProductID: pid.String(), URL: in.URL, AltText: in.AltText, Position: in.Position})
			writeCatalogResult(w, 201, v, e)
			return
		}
		if len(parts) == 3 && r.Method == http.MethodPatch {
			var in imageInput
			if !decodeCatalogJSON(w, r, &in) {
				return
			}
			v, e := a.images.Update(r.Context(), mustScope(r), catalogimagerepo.Image{ID: parts[2], ProductID: pid.String(), URL: in.URL, AltText: in.AltText, Position: in.Position, Version: in.Version})
			writeCatalogResult(w, 200, v, e)
			return
		}
	}
	writeProblem(w, 404, "Not Found")
}

func (a catalogAPI) detail(w http.ResponseWriter, r *http.Request, cs catalog.Scope, ps pricing.Scope, ms pim.Scope, pid catalog.ProductID) {
	product, e := a.catalog.Product(r.Context(), cs, pid)
	if e != nil {
		writeCatalogResult(w, 0, nil, e)
		return
	}
	offers, e := a.catalog.OffersByProduct(r.Context(), cs, pid, 100)
	if e != nil {
		writeCatalogResult(w, 0, nil, e)
		return
	}
	ov := make([]map[string]any, 0, len(offers))
	for _, o := range offers {
		prices, e := a.prices.PricesByOffer(r.Context(), ps, pricing.OfferID(o.ID.String()), 100)
		if e != nil {
			writeCatalogResult(w, 0, nil, e)
			return
		}
		pv := make([]any, 0, len(prices))
		for _, v := range prices {
			pv = append(pv, priceViewOf(v))
		}
		ov = append(ov, map[string]any{"id": o.ID, "product_id": o.ProductID, "sku": o.SKU, "gtin": o.GTIN, "status": o.Status, "version": o.Version, "created_at": o.CreatedAt, "updated_at": o.UpdatedAt, "prices": pv})
	}
	cats, e := a.pim.CategoriesByProduct(r.Context(), ms, pim.ID(pid.String()))
	if e != nil {
		writeCatalogResult(w, 0, nil, e)
		return
	}
	images, e := a.images.List(r.Context(), mustScope(r), pid.String())
	if e != nil {
		writeCatalogResult(w, 0, nil, e)
		return
	}
	writeJSON(w, 200, map[string]any{"product": product, "offers": ov, "categories": cats, "images": images})
}
func (a catalogAPI) categories(w http.ResponseWriter, r *http.Request) {
	_, _, _, ms, _, ok := catalogScopes(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	v, e := a.pim.Categories(r.Context(), ms, 500)
	writeCatalogResult(w, 200, map[string]any{"items": v}, e)
}
func (a catalogAPI) createCategory(w http.ResponseWriter, r *http.Request) {
	_, _, _, ms, p, ok := catalogScopes(r)
	if !ok {
		writeProblem(w, 401, "Unauthorized")
		return
	}
	var in categoryInput
	if !decodeCatalogJSON(w, r, &in) {
		return
	}
	var parent pim.ID
	if in.ParentID != "" {
		parent, _ = pim.ParseID(in.ParentID)
	}
	now := time.Now().UTC()
	id, _ := pim.ParseID(newApprovalID())
	v, e := a.pim.CreateCategory(r.Context(), ms, pim.Category{ID: id, OrganizationID: ms.OrganizationID(), WorkspaceID: ms.WorkspaceID(), Code: in.Code, Name: in.Name, ParentID: parent, Status: pim.StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}, pimMutation(p, r))
	writeCatalogResult(w, 201, v, e)
}
func (a catalogAPI) assignCategory(ctx context.Context, ms pim.Scope, p Principal, r *http.Request, productID, categoryID string) error {
	pid, e := pim.ParseID(productID)
	if e != nil {
		return e
	}
	cid, e := pim.ParseID(categoryID)
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	_, e = a.pim.SetProductCategory(ctx, ms, pim.ProductCategory{OrganizationID: ms.OrganizationID(), WorkspaceID: ms.WorkspaceID(), ProductID: pid, CategoryID: cid, IsPrimary: true, Source: "manual", Version: 1, Active: true, CreatedAt: now, UpdatedAt: now}, pimMutation(p, r))
	return e
}
func mustScope(r *http.Request) tenancy.Scope { s, _ := ScopeFromContext(r.Context()); return s }
func priceViewOf(v pricing.Price) any {
	return map[string]any{"id": v.ID, "offer_id": v.OfferID, "kind": v.Kind, "minor_units": v.Amount.MinorUnits(), "currency": v.Amount.Currency(), "version": v.Version, "created_at": v.CreatedAt, "updated_at": v.UpdatedAt}
}
func decodeCatalogJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil {
		writeProblem(w, 400, "Bad Request")
		return false
	}
	return true
}
func writeCatalogResult(w http.ResponseWriter, status int, v any, err error) {
	if err == nil {
		writeJSON(w, status, v)
		return
	}
	switch {
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, pricing.ErrNotFound), errors.Is(err, sql.ErrNoRows):
		writeProblem(w, 404, "Not Found")
	case errors.Is(err, catalog.ErrConflict), errors.Is(err, pricing.ErrConflict), errors.Is(err, pim.ErrConflict):
		writeProblem(w, 409, "Conflict")
	case errors.Is(err, catalog.ErrInvalidRecord), errors.Is(err, catalog.ErrInvalidState), errors.Is(err, pricing.ErrInvalidRecord), errors.Is(err, pim.ErrInvalid), errors.Is(err, catalogimagerepo.ErrInvalid):
		writeProblem(w, 400, "Bad Request")
	default:
		writeProblem(w, 500, "Internal Server Error")
	}
}
