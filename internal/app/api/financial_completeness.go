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
	financialCompletenessPath                 = "/api/v1/financial-completeness"
	financialCompletenessAccountsPath         = financialCompletenessPath + "/accounts"
	financialCompletenessStatementsPath       = financialCompletenessPath + "/statements"
	financialCompletenessStatementPreviewPath = financialCompletenessStatementsPath + ":preview"
	financialCompletenessCOGSBackfillsPath    = financialCompletenessPath + "/cogs-backfills"
	financialCompletenessCOGSPreviewPath      = financialCompletenessCOGSBackfillsPath + ":preview"
	financialCompletenessSourcesPath          = financialCompletenessPath + "/sources"
	financialCompletenessFindingsPath         = financialCompletenessPath + "/findings"
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
		{Method: http.MethodGet, Path: financialCompletenessAccountsPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.accounts)},
		{Method: http.MethodPost, Path: financialCompletenessAccountsPath, Permission: "finance.sources.write", Handler: http.HandlerFunc(api.appendAccount)},
		{Method: http.MethodPost, Path: financialCompletenessStatementsPath, Permission: "finance.sources.write", Handler: http.HandlerFunc(api.appendStatement)},
		{Method: http.MethodPost, Path: financialCompletenessStatementPreviewPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.previewStatement)},
		{Method: http.MethodGet, Path: financialCompletenessCOGSBackfillsPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.cogsBackfills)},
		{Method: http.MethodPost, Path: financialCompletenessCOGSBackfillsPath, Permission: "finance.sources.write", Handler: http.HandlerFunc(api.appendCOGSBackfill)},
		{Method: http.MethodPost, Path: financialCompletenessCOGSPreviewPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.previewCOGSBackfill)},
		{Method: http.MethodGet, Path: financialCompletenessSourcesPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.sources)},
		{Method: http.MethodPost, Path: financialCompletenessSourcesPath, Permission: "finance.sources.write", Handler: http.HandlerFunc(api.appendSource)},
		{Method: http.MethodGet, Path: financialCompletenessFindingsPath, Permission: "finance.reports.read", Handler: http.HandlerFunc(api.findings)},
	}
}

func (api financialCompletenessAPI) cogsBackfills(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	limit, ok := boundedLimit(r, 50, 100)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "Invalid limit")
		return
	}
	after, err := decodeAccountCursor(strings.TrimSpace(r.URL.Query().Get("cursor")))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid cursor")
		return
	}
	page, err := api.repository.ListCOGSBackfillJobs(r.Context(), scope, after, limit)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "COGS backfills unavailable")
		return
	}
	next := ""
	if page.HasMore && len(page.Items) > 0 {
		next = encodeAccountCursor(page.Items[len(page.Items)-1].ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items, "next_cursor": next})
}

func (api financialCompletenessAPI) appendCOGSBackfill(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var job core.COGSBackfillJob
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(&job); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid COGS backfill")
		return
	}
	if job.ID == "" {
		job.ID = stableID("cogs-backfill-", 24, scope, key)
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.Status == "" {
		job.Status = "queued"
	}
	if err := job.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bounded COGS backfill")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.financial_completeness", Action: "financial.cogs_backfill.append", ResourceType: "financial_cogs_backfill_job", ResourceID: job.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"from": job.From, "to": job.To, "sku": job.SKU, "warehouse_id": job.WarehouseID, "channel_ref": job.ChannelRef, "preview_digest": job.PreviewDigest}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Financial source audit unavailable")
		return
	}
	if err := api.repository.AppendCOGSBackfillJob(r.Context(), scope, job); err != nil {
		if errors.Is(err, financialcompletenessrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "COGS backfill conflicts with existing job")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "COGS backfill append failed")
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (api financialCompletenessAPI) previewCOGSBackfill(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	var job core.COGSBackfillJob
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(&job); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid COGS backfill")
		return
	}
	job.Status = "preview"
	if err := job.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bounded COGS backfill")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "preview", "job": job, "commit_required": true})
}

func (api financialCompletenessAPI) accounts(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api.repository == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	page, err := api.repository.ListBankAccounts(r.Context(), scope, 100)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Bank accounts unavailable")
		return
	}
	for index := range page.Items {
		page.Items[index].SecretReference = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page.Items})
}

func (api financialCompletenessAPI) appendAccount(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var account core.BankAccount
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(&account); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bank account")
		return
	}
	if account.ID == "" {
		account.ID = stableID("bank-account-", 24, scope, key)
	}
	if account.Status == "" {
		account.Status = "active"
	}
	if account.CreatedAt.IsZero() {
		account.CreatedAt = time.Now().UTC()
	}
	if err := account.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid or unsafe bank account")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.financial_completeness", Action: "financial.bank_account.append", ResourceType: "financial_bank_account", ResourceID: account.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"provider": account.Provider, "masked_reference": account.MaskedReference, "currency": account.Currency}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Financial source audit unavailable")
		return
	}
	if err := api.repository.AppendBankAccount(r.Context(), scope, account); err != nil {
		if errors.Is(err, financialcompletenessrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Bank account conflicts with existing binding")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Bank account append failed")
		return
	}
	account.SecretReference = ""
	writeJSON(w, http.StatusCreated, account)
}

func (api financialCompletenessAPI) appendStatement(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || api.repository == nil || api.audit == nil || !validIdempotencyKey(key) {
		writeProblem(w, http.StatusBadRequest, "Idempotency-Key and authenticated context are required")
		return
	}
	var statement core.BankStatement
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(&statement); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bank statement")
		return
	}
	if statement.ID == "" {
		statement.ID = stableID("statement-", 24, scope, key)
	}
	if statement.CreatedAt.IsZero() {
		statement.CreatedAt = time.Now().UTC()
	}
	if err := statement.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bank statement")
		return
	}
	if _, err := api.audit.Capture(r.Context(), scope, audit.Entry{ActorID: boundedActorRef(principal.Subject), Source: "api.financial_completeness", Action: "financial.bank_statement.append", ResourceType: "financial_bank_statement", ResourceID: statement.ID, CorrelationID: key, Risk: audit.RiskWriteSensitive, Summary: audit.Summary{"account_id": statement.AccountID, "source_reference": statement.SourceReference, "transaction_count": statement.TransactionCount}}); err != nil {
		writeProblem(w, http.StatusServiceUnavailable, "Financial source audit unavailable")
		return
	}
	if err := api.repository.AppendStatement(r.Context(), scope, statement); err != nil {
		if errors.Is(err, financialcompletenessrepo.ErrConflict) {
			writeProblem(w, http.StatusConflict, "Bank statement conflicts with existing evidence")
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Bank statement append failed")
		return
	}
	writeJSON(w, http.StatusCreated, statement)
}

func (api financialCompletenessAPI) previewStatement(w http.ResponseWriter, r *http.Request) {
	if _, ok := ScopeFromContext(r.Context()); !ok {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	var statement core.BankStatement
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	if err := decoder.Decode(&statement); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bank statement")
		return
	}
	statement.State = "preview"
	if err := statement.Validate(); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid bank statement")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": "preview", "statement": statement, "commit_required": true})
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
