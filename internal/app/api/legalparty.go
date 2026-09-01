package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
)

const LegalPartySearchPath = "/api/v1/counterparties/search"
const LegalPartyPath = "/api/v1/legal-parties"

type LegalPartyScopeResolver interface {
	LegalPartyScope(*http.Request) (legalparty.Scope, error)
}
type LegalPartySearcher interface {
	Search(context.Context, legalparty.Scope, legalparty.SearchQuery) (legalparty.SearchPage, error)
}

type legalPartyMasterReader interface {
	LegalEntity(context.Context, legalparty.Scope, legalparty.ID) (legalparty.LegalEntity, error)
	IndividualEntrepreneur(context.Context, legalparty.Scope, legalparty.ID) (legalparty.IndividualEntrepreneur, error)
	Branch(context.Context, legalparty.Scope, legalparty.ID) (legalparty.Branch, error)
}

type legalPartyWriter interface {
	legalPartyMasterReader
	CreateLegalEntity(context.Context, legalparty.Scope, legalparty.LegalEntity, legalparty.Mutation) (legalparty.LegalEntity, error)
	CreateIndividualEntrepreneur(context.Context, legalparty.Scope, legalparty.IndividualEntrepreneur, legalparty.Mutation) (legalparty.IndividualEntrepreneur, error)
	CreateBranch(context.Context, legalparty.Scope, legalparty.Branch, legalparty.Mutation) (legalparty.Branch, error)
}

func newHandlerWithLegalPartySearch(logger *slog.Logger, searcher LegalPartySearcher, resolver LegalPartyScopeResolver) http.Handler {
	return recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == LegalPartySearchPath {
			legalPartySearch(w, r, searcher, resolver)
			return
		}
		route(w, r)
	}))
}

func newLegalPartyRoutes(searcher LegalPartySearcher, writers ...legalPartyWriter) []ProtectedRoute {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legalPartySearch(w, r, searcher, productionScopeResolver{})
	})
	routes := []ProtectedRoute{
		{Method: http.MethodGet, Path: LegalPartySearchPath, Permission: "counterparties.read", Handler: handler},
		{Method: http.MethodHead, Path: LegalPartySearchPath, Permission: "counterparties.read", Handler: handler},
	}
	var writer legalPartyWriter
	if len(writers) > 0 {
		writer = writers[0]
	}
	routes = append(routes, ProtectedRoute{Method: http.MethodPost, Path: LegalPartyPath, Permission: "counterparties.write", Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writer == nil {
			writeProblem(w, http.StatusServiceUnavailable, "Legal-party writer is unavailable")
			return
		}
		createLegalParty(w, r, writer)
	})})
	return routes
}

type legalPartyCreateInput struct {
	PartyType     legalparty.PartyType `json:"party_type"`
	Code          string               `json:"code"`
	LegalName     string               `json:"legal_name,omitempty"`
	ShortName     string               `json:"short_name,omitempty"`
	FullName      string               `json:"full_name,omitempty"`
	Name          string               `json:"name,omitempty"`
	CountryCode   string               `json:"country_code"`
	INN           string               `json:"inn,omitempty"`
	KPP           string               `json:"kpp,omitempty"`
	OGRN          string               `json:"ogrn,omitempty"`
	OGRNIP        string               `json:"ogrnip,omitempty"`
	LegalEntityID string               `json:"legal_entity_id,omitempty"`
}

func createLegalParty(w http.ResponseWriter, r *http.Request, writer legalPartyWriter) {
	scope, scopeErr := productionScopeResolver{}.LegalPartyScope(r)
	principal, identified := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	var input legalPartyCreateInput
	if scopeErr != nil || !scope.Valid() || !identified || !principal.Valid() || writer == nil || !validIdempotencyKey(key) || decodeStrictJSON(r, &input) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	input.Code = strings.TrimSpace(input.Code)
	input.CountryCode = strings.ToUpper(strings.TrimSpace(input.CountryCode))
	input.LegalName = strings.TrimSpace(input.LegalName)
	input.ShortName = strings.TrimSpace(input.ShortName)
	input.FullName = strings.TrimSpace(input.FullName)
	input.Name = strings.TrimSpace(input.Name)
	input.INN = strings.TrimSpace(input.INN)
	input.KPP = strings.TrimSpace(input.KPP)
	input.OGRN = strings.TrimSpace(input.OGRN)
	input.OGRNIP = strings.TrimSpace(input.OGRNIP)
	input.LegalEntityID = strings.TrimSpace(input.LegalEntityID)
	now := time.Now().UTC()
	id, parseErr := legalparty.ParseID(stableLegalPartyID("party", scope, key))
	if parseErr != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	mutation := legalPartyMutation(principal, key, now)

	switch input.PartyType {
	case legalparty.PartyLegalEntity:
		if input.LegalName == "" || input.FullName != "" || input.Name != "" || input.OGRNIP != "" || input.LegalEntityID != "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		created, err := writer.CreateLegalEntity(r.Context(), scope, legalparty.LegalEntity{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Code: input.Code, LegalName: input.LegalName, ShortName: input.ShortName, CountryCode: input.CountryCode, INN: input.INN, KPP: input.KPP, OGRN: input.OGRN, Status: legalparty.StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}, mutation)
		if err != nil {
			if errors.Is(err, legalparty.ErrConflict) {
				if existing, lookupErr := writer.LegalEntity(r.Context(), scope, id); lookupErr == nil && sameLegalEntity(existing, input) {
					writeJSON(w, http.StatusCreated, legalEntitySearchResult(existing))
					return
				}
				writeProblem(w, http.StatusConflict, "Conflict")
				return
			}
			writeLegalPartyWriteError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, legalEntitySearchResult(created))
	case legalparty.PartyIndividualEntrepreneur:
		if input.FullName == "" || input.LegalName != "" || input.ShortName != "" || input.Name != "" || input.KPP != "" || input.OGRN != "" || input.LegalEntityID != "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		created, err := writer.CreateIndividualEntrepreneur(r.Context(), scope, legalparty.IndividualEntrepreneur{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Code: input.Code, FullName: input.FullName, CountryCode: input.CountryCode, INN: input.INN, OGRNIP: input.OGRNIP, Status: legalparty.StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}, mutation)
		if err != nil {
			if errors.Is(err, legalparty.ErrConflict) {
				if existing, lookupErr := writer.IndividualEntrepreneur(r.Context(), scope, id); lookupErr == nil && sameIndividualEntrepreneur(existing, input) {
					writeJSON(w, http.StatusCreated, individualEntrepreneurSearchResult(existing))
					return
				}
				writeProblem(w, http.StatusConflict, "Conflict")
				return
			}
			writeLegalPartyWriteError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, individualEntrepreneurSearchResult(created))
	case legalparty.PartyBranch:
		parentID, err := legalparty.ParseID(input.LegalEntityID)
		if err != nil || input.Name == "" || input.LegalName != "" || input.ShortName != "" || input.FullName != "" || input.OGRN != "" || input.OGRNIP != "" {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		if _, err := writer.LegalEntity(r.Context(), scope, parentID); err != nil {
			if errors.Is(err, legalparty.ErrNotFound) {
				writeProblem(w, http.StatusUnprocessableEntity, "Parent legal entity not found")
				return
			}
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}
		created, err := writer.CreateBranch(r.Context(), scope, legalparty.Branch{ID: id, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), LegalEntityID: parentID, Code: input.Code, Name: input.Name, CountryCode: input.CountryCode, KPP: input.KPP, Status: legalparty.StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}, mutation)
		if err != nil {
			if errors.Is(err, legalparty.ErrConflict) {
				if existing, lookupErr := writer.Branch(r.Context(), scope, id); lookupErr == nil && sameBranch(existing, input, parentID) {
					writeJSON(w, http.StatusCreated, branchSearchResult(existing))
					return
				}
				writeProblem(w, http.StatusConflict, "Conflict")
				return
			}
			writeLegalPartyWriteError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, branchSearchResult(created))
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	}
}

func legalPartyMutation(principal Principal, key string, now time.Time) legalparty.Mutation {
	return legalparty.Mutation{EventID: newApprovalID(), AuditID: newApprovalID(), Source: "api.legal_party", ActorID: boundedActorRef(principal.Subject), CorrelationID: key, OccurredAt: now}
}

func stableLegalPartyID(prefix string, scope legalparty.Scope, key string) string {
	digest := sha256.Sum256([]byte(prefix + "\x00" + scope.OrganizationID() + "\x00" + scope.WorkspaceID() + "\x00" + key))
	raw := []byte(hex.EncodeToString(digest[:])[:32])
	raw[12] = '7'
	raw[16] = '8'
	return fmt.Sprintf("%s-%s-%s-%s-%s", raw[0:8], raw[8:12], raw[12:16], raw[16:20], raw[20:32])
}

func writeLegalPartyWriteError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, legalparty.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	case errors.Is(err, legalparty.ErrConflict):
		writeProblem(w, http.StatusConflict, "Conflict")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}

func legalEntitySearchResult(v legalparty.LegalEntity) legalparty.SearchResult {
	name := v.ShortName
	if name == "" {
		name = v.LegalName
	}
	return legalparty.SearchResult{PartyType: legalparty.PartyLegalEntity, PartyID: v.ID.String(), Code: v.Code, DisplayName: name, INN: v.INN, RegistrationID: v.OGRN, Status: v.Status}
}

func individualEntrepreneurSearchResult(v legalparty.IndividualEntrepreneur) legalparty.SearchResult {
	return legalparty.SearchResult{PartyType: legalparty.PartyIndividualEntrepreneur, PartyID: v.ID.String(), Code: v.Code, DisplayName: v.FullName, INN: v.INN, RegistrationID: v.OGRNIP, Status: v.Status}
}

func branchSearchResult(v legalparty.Branch) legalparty.SearchResult {
	return legalparty.SearchResult{PartyType: legalparty.PartyBranch, PartyID: v.ID.String(), Code: v.Code, DisplayName: v.Name, RegistrationID: v.KPP, Status: v.Status}
}

func sameLegalEntity(v legalparty.LegalEntity, input legalPartyCreateInput) bool {
	return v.Code == input.Code && v.LegalName == input.LegalName && v.ShortName == input.ShortName && v.CountryCode == input.CountryCode && v.INN == input.INN && v.KPP == input.KPP && v.OGRN == input.OGRN
}

func sameIndividualEntrepreneur(v legalparty.IndividualEntrepreneur, input legalPartyCreateInput) bool {
	return v.Code == input.Code && v.FullName == input.FullName && v.CountryCode == input.CountryCode && v.INN == input.INN && v.OGRNIP == input.OGRNIP
}

func sameBranch(v legalparty.Branch, input legalPartyCreateInput, parentID legalparty.ID) bool {
	return v.Code == input.Code && v.Name == input.Name && v.LegalEntityID == parentID && v.CountryCode == input.CountryCode && v.KPP == input.KPP
}
func legalPartySearch(w http.ResponseWriter, r *http.Request, searcher LegalPartySearcher, resolver LegalPartyScopeResolver) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeProblem(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if searcher == nil || resolver == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	scope, err := resolver.LegalPartyScope(r)
	if err != nil || !scope.Valid() {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = n
	}
	q := legalparty.SearchQuery{Text: r.URL.Query().Get("q"), INN: r.URL.Query().Get("inn"), RegistrationID: r.URL.Query().Get("registration_id"), PartyType: legalparty.PartyType(r.URL.Query().Get("party_type")), Limit: limit}
	if q.Validate() != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	page, err := searcher.Search(r.Context(), scope, q)
	if err != nil {
		if errors.Is(err, legalparty.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_ = jsonEncode(w, page)
	}
}
