package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	core "github.com/torgnexa/torgnexa/internal/core/financialcompleteness"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/financialcompletenessrepo"
)

const (
	financialCompletenessPath         = "/api/v1/financial-completeness"
	financialCompletenessSourcesPath  = financialCompletenessPath + "/sources"
	financialCompletenessFindingsPath = financialCompletenessPath + "/findings"
)

type financialCompletenessAPI struct {
	repository *financialcompletenessrepo.Repository
	audit      auditCapturer
}

type financialCompletenessResponse struct {
	Matrix           []core.Requirement `json:"matrix"`
	Evaluation       core.Evaluation    `json:"evaluation"`
	SourceCount      int                `json:"source_count"`
	OpenFindingCount int                `json:"open_finding_count"`
	LastObservedAt   time.Time          `json:"last_observed_at,omitempty"`
}

func newFinancialCompletenessRoutes(repository *financialcompletenessrepo.Repository, auditor auditCapturer) []ProtectedRoute {
	api := financialCompletenessAPI{repository: repository, audit: auditor}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: financialCompletenessPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.summary)},
		{Method: http.MethodGet, Path: financialCompletenessSourcesPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.sources)},
		{Method: http.MethodPost, Path: financialCompletenessSourcesPath, Permission: "finance.sources.write", Handler: http.HandlerFunc(api.appendSource)},
		{Method: http.MethodGet, Path: financialCompletenessFindingsPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.findings)},
	}
}

func (api financialCompletenessAPI) summary(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	basis, from, to, currency, err := parseCompletenessQuery(r)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid completeness filters")
		return
	}
	value, err := api.repository.Summary(r.Context(), scope, basis, from, to, currency)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Financial completeness unavailable")
		return
	}
	writeJSON(w, http.StatusOK, financialCompletenessResponse{Matrix: value.Matrix, Evaluation: value.Evaluation, SourceCount: value.SourceCount, OpenFindingCount: value.OpenFindingCount, LastObservedAt: value.LastObservedAt})
}

func (api financialCompletenessAPI) sources(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	basis, from, to, _, err := parseCompletenessQuery(r)
	if err != nil || !basis.Valid() {
		writeProblem(w, http.StatusBadRequest, "Invalid completeness filters")
		return
	}
	limit, ok := boundedLimit(r, 50, 200)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	after, err := decodeAccountCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid cursor")
		return
	}
	kind := core.SourceKind(strings.TrimSpace(r.URL.Query().Get("kind")))
	quality := core.Quality(strings.TrimSpace(r.URL.Query().Get("quality")))
	if kind != "" && !kind.Valid() || quality != "" && !quality.Valid() {
		writeProblem(w, http.StatusBadRequest, "Invalid source filter")
		return
	}
	page, err := api.repository.ListSources(r.Context(), scope, financialcompletenessrepo.Filter{Kind: kind, Quality: quality, AfterID: after, From: from, To: to, Limit: limit})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Financial sources unavailable")
		return
	}
	next := ""
	if page.HasMore && len(page.Items) > 0 {
		next = encodeAccountCursor(page.Items[len(page.Items)-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": next})
}

func (api financialCompletenessAPI) findings(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit, ok := boundedLimit(r, 50, 200)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	after, err := decodeAccountCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid cursor")
		return
	}
	page, err := api.repository.ListFindings(r.Context(), scope, after, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Financial findings unavailable")
		return
	}
	next := ""
	if page.HasMore && len(page.Items) > 0 {
		next = encodeAccountCursor(page.Items[len(page.Items)-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": next})
}

func (api financialCompletenessAPI) appendSource(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var record core.SourceRecord
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	if err := decoder.Decode(&record); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid financial source")
		return
	}
	if record.ID == "" {
		record.ID = stableID("source-", 24, scope, key)
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	if err := record.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid financial source")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.financial_completeness", Action: "financial.source.append", ResourceType: "financial_source_record", ResourceID: record.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"kind": record.Kind, "source_system": record.SourceSystem, "source_ref": record.SourceRef, "amount_minor_units": record.AmountMinor, "currency": record.Currency, "quality": record.Quality}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Financial source audit unavailable")
		return
	}
	if err := api.repository.AppendSource(r.Context(), scope, record); err != nil {
		if errors.Is(err, financialcompletenessrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Financial source conflicts with existing evidence")
			return
		}
		if errors.Is(err, core.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Invalid financial source")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Financial source append failed")
		return
	}
	writeJSON(w, http.StatusCreated, record)
}

func parseCompletenessQuery(r *http.Request) (core.Basis, time.Time, time.Time, string, error) {
	q := r.URL.Query()
	basis := core.Basis(strings.TrimSpace(q.Get("basis")))
	if basis == "" {
		basis = core.BasisOrderAccrual
	}
	if !basis.Valid() {
		return "", time.Time{}, time.Time{}, "", errors.New("invalid basis")
	}
	to := time.Now().UTC()
	from := to.Add(-30 * 24 * time.Hour)
	var err error
	if value := strings.TrimSpace(q.Get("from")); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return "", time.Time{}, time.Time{}, "", err
		}
		from = from.UTC()
	}
	if value := strings.TrimSpace(q.Get("to")); value != "" {
		to, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return "", time.Time{}, time.Time{}, "", err
		}
		to = to.UTC()
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return "", time.Time{}, time.Time{}, "", errors.New("invalid range")
	}
	currency := strings.ToUpper(strings.TrimSpace(q.Get("currency")))
	if currency != "" && (len(currency) != 3 || currency[0] < 'A' || currency[0] > 'Z' || currency[1] < 'A' || currency[1] > 'Z' || currency[2] < 'A' || currency[2] > 'Z') {
		return "", time.Time{}, time.Time{}, "", errors.New("invalid currency")
	}
	return basis, from, to, currency, nil
}
