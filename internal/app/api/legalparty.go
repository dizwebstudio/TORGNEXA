package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
)

const LegalPartySearchPath = "/api/v1/counterparties/search"

type LegalPartyScopeResolver interface {
	LegalPartyScope(*http.Request) (legalparty.Scope, error)
}
type LegalPartySearcher interface {
	Search(context.Context, legalparty.Scope, legalparty.SearchQuery) (legalparty.SearchPage, error)
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

func newLegalPartyRoutes(searcher LegalPartySearcher) []ProtectedRoute {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legalPartySearch(w, r, searcher, productionScopeResolver{})
	})
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: LegalPartySearchPath, Permission: "counterparties.read", Handler: handler},
		{Method: http.MethodHead, Path: LegalPartySearchPath, Permission: "counterparties.read", Handler: handler},
	}
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
