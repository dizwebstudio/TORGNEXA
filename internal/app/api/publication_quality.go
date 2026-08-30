package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/publicationqualityrepo"
	"github.com/torgnexa/torgnexa/internal/platform/publicationquality"
)

const (
	PublicationQualityRunsPath     = "/api/v1/publication-quality/runs"
	PublicationQualityReceiptsPath = "/api/v1/publication-quality/receipts"
)

type publicationQualityAPI struct {
	repository *publicationqualityrepo.Repository
}

type publicationQualityRunSummary struct {
	ID                 string                      `json:"id"`
	ProductID          string                      `json:"product_id"`
	OfferID            string                      `json:"offer_id,omitempty"`
	ConnectorAccountID string                      `json:"connector_account_id"`
	ConnectorID        string                      `json:"connector_id"`
	ChannelFamily      string                      `json:"channel_family"`
	Decision           publicationquality.Decision `json:"decision"`
	ScoreBPS           int64                       `json:"score_bps"`
	EvaluatedAt        string                      `json:"evaluated_at"`
	ValidUntil         string                      `json:"valid_until"`
	Version            int64                       `json:"version"`
}

func newPublicationQualityRoutes(repository *publicationqualityrepo.Repository) []ProtectedRoute {
	api := publicationQualityAPI{repository: repository}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: PublicationQualityRunsPath, Permission: "products.read", Handler: http.HandlerFunc(api.listRuns)},
		{Method: http.MethodGet, Path: PublicationQualityRunsPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.runRoute)},
		{Method: http.MethodGet, Path: PublicationQualityReceiptsPath + "/", PathPrefix: true, Permission: "products.read", Handler: http.HandlerFunc(api.receiptRoute)},
	}
}

func (a publicationQualityAPI) listRuns(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Publication quality is unavailable")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		limit = parsed
	}
	productID := strings.TrimSpace(r.URL.Query().Get("product_id"))
	if strings.ContainsAny(productID, "/\x00\r\n") || len(productID) > 192 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	items, err := a.repository.ListRuns(r.Context(), scope, productID, limit)
	if err != nil {
		writePublicationQualityError(w, err)
		return
	}
	views := make([]publicationQualityRunSummary, 0, len(items))
	for _, item := range items {
		views = append(views, publicationQualityRunSummary{ID: item.ID, ProductID: item.ProductID, OfferID: item.OfferID, ConnectorAccountID: item.ConnectorAccountID, ConnectorID: item.ConnectorID, ChannelFamily: item.ChannelFamily, Decision: item.Decision, ScoreBPS: item.ScoreBPS, EvaluatedAt: item.EvaluatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), ValidUntil: item.ValidUntil.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), Version: item.Version})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "limit": limit})
}

func (a publicationQualityAPI) runRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Publication quality is unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, PublicationQualityRunsPath+"/")
	if !safePublicationQualityID(id) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	run, err := a.repository.Run(r.Context(), scope, id)
	if err != nil {
		writePublicationQualityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, run)
}

func (a publicationQualityAPI) receiptRoute(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || a.repository == nil {
		writeProblem(w, http.StatusServiceUnavailable, "Publication quality is unavailable")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, PublicationQualityReceiptsPath+"/")
	if !safePublicationQualityID(id) {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	receipt, err := a.repository.Receipt(r.Context(), scope, id)
	if err != nil {
		writePublicationQualityError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, receipt)
}

func writePublicationQualityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, publicationquality.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found")
	case errors.Is(err, publicationquality.ErrInvalid):
		writeProblem(w, http.StatusBadRequest, "Bad Request")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
	}
}

func safePublicationQualityID(value string) bool {
	return value != "" && len(value) <= 192 && !strings.ContainsAny(value, "/\x00\r\n")
}

var _ interface {
	ListRuns(context.Context, tenancy.Scope, string, int) ([]publicationqualityrepo.RunSummary, error)
} = (*publicationqualityrepo.Repository)(nil)
