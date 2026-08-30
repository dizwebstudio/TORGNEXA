package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/integrationcenter"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reconciliationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/syncrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const (
	IntegrationCenterPath        = "/api/v1/integration-center"
	IntegrationCenterAccountPath = IntegrationCenterPath + "/"
)

type integrationCenterReadRequest struct {
	Cursor     string
	Limit      int
	Family     string
	Surface    string
	Overall    string
	Health     string
	Sync       string
	Capability string
	Issue      string
	Stale      bool
	AccountID  string
}

type integrationCenterReadResult struct {
	Rows             []integrationcenter.Snapshot
	NextCursor       string
	Partial          bool
	GeneratedAt      time.Time
	SourceWatermarks []string
}

type integrationCenterReader interface {
	Read(context.Context, tenancy.Scope, integrationCenterReadRequest) (integrationCenterReadResult, error)
}

// integrationCenterSource is an application adapter over the authoritative
// repositories. It never calls a remote connector and never reads secret
// bytes; the normal GET path is therefore safe to cache at the transport edge.
type integrationCenterSource struct {
	accounts       *connectorrepo.Repository
	configs        *connectorconfigrepo.Repository
	policies       *syncrepo.Repository
	reconciliation *reconciliationrepo.Repository
	runtime        *builtinruntime.Registry
}

func (s integrationCenterSource) Read(ctx context.Context, scope tenancy.Scope, request integrationCenterReadRequest) (integrationCenterReadResult, error) {
	if s.accounts == nil || s.runtime == nil || !scope.Valid() {
		return integrationCenterReadResult{}, errors.New("integration center: source unavailable")
	}
	accounts, err := s.accounts.ListAccounts(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), request.Cursor, request.Limit+1)
	if err != nil {
		return integrationCenterReadResult{}, err
	}
	partial := false
	capabilitiesReadable := true
	accountIDs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
	}
	capabilityByAccount := make(map[string][]sdk.AccountCapabilitySetting, len(accountIDs))
	if caps, capsErr := s.accounts.AccountCapabilitiesBulk(ctx, scope, accountIDs); capsErr != nil {
		partial = true
		capabilitiesReadable = false
	} else {
		capabilityByAccount = caps
	}
	configByAccount := make(map[string]connectorconfigrepo.State, len(accountIDs))
	configsReadable := s.configs != nil
	if s.configs != nil {
		if states, statesErr := s.configs.States(ctx, scope, accountIDs); statesErr != nil {
			partial = true
			configsReadable = false
		} else {
			configByAccount = states
		}
	} else {
		partial = true
	}
	policies := make([]syncengine.Policy, 0)
	policiesReadable := true
	if s.policies != nil {
		policies, err = s.policies.ListPolicies(ctx, scope, 100)
		if err != nil {
			partial = true
			policiesReadable = false
		}
	} else {
		partial = true
		policiesReadable = false
	}
	runs := make([]reconciliation.Run, 0)
	runsReadable := true
	if s.reconciliation != nil {
		runs, err = s.reconciliation.ListRuns(ctx, scope, 100)
		if err != nil {
			partial = true
			runsReadable = false
		}
	} else {
		partial = true
		runsReadable = false
	}
	drifts := make([]reconciliation.Drift, 0)
	driftsReadable := true
	if s.reconciliation != nil {
		drifts, err = s.reconciliation.ListRecentDrifts(ctx, scope, 100)
		if err != nil {
			partial = true
			driftsReadable = false
		}
	} else {
		partial = true
		driftsReadable = false
	}
	policyByAccount := make(map[string][]syncengine.Policy)
	for _, p := range policies {
		accountKey := p.ConnectorAccountID
		policyByAccount[accountKey] = append(policyByAccount[accountKey], p)
	}
	latestRunByPolicy := make(map[string]reconciliation.Run)
	for _, run := range runs {
		if old, ok := latestRunByPolicy[run.PolicyID]; !ok || run.UpdatedAt.After(old.UpdatedAt) {
			latestRunByPolicy[run.PolicyID] = run
		}
	}
	openDriftByPolicy := make(map[string]bool)
	for _, drift := range drifts {
		if drift.Status == reconciliation.DriftOpen {
			openDriftByPolicy[drift.PolicyID] = true
		}
	}
	rows := make([]integrationcenter.Snapshot, 0, centerMin(len(accounts), request.Limit))
	for _, account := range accounts {
		row, rowPartial := s.reduceAccount(account, policyByAccount[account.ID], latestRunByPolicy, openDriftByPolicy, capabilityByAccount[account.ID], configByAccount[account.ID], capabilitiesReadable, configsReadable, policiesReadable, runsReadable, driftsReadable)
		partial = partial || rowPartial
		if !centerMatches(row, request) {
			continue
		}
		rows = append(rows, row)
	}
	result := integrationCenterReadResult{Rows: rows, Partial: partial, GeneratedAt: time.Now().UTC(), SourceWatermarks: []string{accountWatermark(accounts), policyWatermark(policies), runWatermark(runs)}}
	if len(accounts) > request.Limit {
		result.NextCursor = encodeAccountCursor(accounts[request.Limit-1].ID)
		result.Rows = result.Rows[:centerMin(len(result.Rows), request.Limit)]
	}
	return result, nil
}

func (s integrationCenterSource) reduceAccount(account sdk.Account, policies []syncengine.Policy, runs map[string]reconciliation.Run, drifts map[string]bool, settings []sdk.AccountCapabilitySetting, configState connectorconfigrepo.State, capabilitiesReadable, configsReadable, policiesReadable, runsReadable, driftsReadable bool) (integrationcenter.Snapshot, bool) {
	now := time.Now().UTC()
	partial := false
	support, supported := builtinruntime.SupportFor(account.ConnectorID)
	runtimeStatus := integrationcenter.RuntimeNotRegistered
	if supported {
		switch {
		case support.HealthOnly:
			runtimeStatus = integrationcenter.RuntimeHealthOnly
		case support.Stage == builtinruntime.SupportReady:
			runtimeStatus = integrationcenter.RuntimeReady
		case support.Stage == builtinruntime.SupportSeparateSurface:
			runtimeStatus = integrationcenter.RuntimeSeparateSurface
		default:
			runtimeStatus = integrationcenter.RuntimeUnsupported
		}
	}
	e := centerEvidence(now, account.UpdatedAt, "connector_account", account.ID, "", integrationcenter.VisibilityFull)
	accountStatus := integrationcenter.AccountActive
	switch account.Status {
	case sdk.AccountDisabled:
		accountStatus = integrationcenter.AccountDisabled
	case sdk.AccountSuspended:
		accountStatus = integrationcenter.AccountSuspended
	case sdk.AccountError:
		accountStatus = integrationcenter.AccountError
	}
	credential := integrationcenter.CredentialUnknown
	if account.SecretReference == "" {
		credential = integrationcenter.CredentialMissing
	} else if account.Health.Status == sdk.HealthHealthy && !account.Health.CheckedAt.IsZero() {
		// A non-empty opaque reference only proves that a record exists. The
		// host-owned health flow must have authenticated successfully before the
		// center can classify credentials as present.
		credential = integrationcenter.CredentialPresent
	} else if account.Health.ReasonCode == "oauth_reauthorization_required" || account.Health.ReasonCode == "oauth_refresh_failed" {
		credential = integrationcenter.CredentialReauthorizationRequired
	} else if account.Health.ReasonCode == "credentials_invalid" || account.Health.ReasonCode == "auth_rejected" {
		credential = integrationcenter.CredentialInvalid
	}
	configuration := integrationcenter.ConfigurationValid
	configurationVisibility := integrationcenter.VisibilityFull
	if support.RuntimeConfigRequired {
		if !configsReadable {
			configuration = integrationcenter.ConfigurationUnknown
			configurationVisibility = integrationcenter.VisibilityRedacted
		} else {
			switch {
			case !configState.Present:
				configuration = integrationcenter.ConfigurationMissing
			case !configState.Valid:
				configuration = integrationcenter.ConfigurationInvalid
			default:
				configuration = integrationcenter.ConfigurationValid
			}
		}
	}
	healthStatus := integrationcenter.HealthUnknown
	if account.Health.Status == sdk.HealthHealthy {
		healthStatus = integrationcenter.HealthHealthy
	} else if account.Health.Status == sdk.HealthDegraded {
		healthStatus = integrationcenter.HealthDegraded
	} else if account.Health.Status == sdk.HealthUnavailable {
		healthStatus = integrationcenter.HealthUnavailable
	}
	healthChecked := account.Health.CheckedAt
	healthReason := account.Health.ReasonCode
	if healthStatus != integrationcenter.HealthUnknown && healthChecked.IsZero() {
		// A healthy value without completion time is not evidence. Keep the
		// source timestamp valid for the envelope, but fail closed to unknown.
		healthStatus = integrationcenter.HealthUnknown
		healthReason = "health_check_missing"
	}
	healthEvidence := centerEvidence(now, healthChecked, "connector_health", account.ID, healthReason, integrationcenter.VisibilityFull)
	capabilities := make([]integrationcenter.Capability, 0)
	capStatus := integrationcenter.CapabilityNotDeclared
	capabilityVisibility := integrationcenter.VisibilityFull
	manifest, manifestErr := sdk.CatalogManifest(account.ConnectorID)
	if manifestErr == nil && len(manifest.Capabilities) > 0 {
		bindingID := account.ConnectorID
		if !capabilitiesReadable {
			capStatus = integrationcenter.CapabilityStale
			capabilityVisibility = integrationcenter.VisibilityRedacted
		} else {
			capStatus = integrationcenter.CapabilityDeclared
			if len(settings) == 0 {
				capStatus = integrationcenter.CapabilityBlocked
			} else {
				capStatus = integrationcenter.CapabilityGranted
				for _, setting := range settings {
					status := integrationcenter.CapabilityDeclared
					if setting.Enabled {
						if builtinruntime.SupportsCapability(bindingID, string(setting.Capability)) {
							status = integrationcenter.CapabilityEnabled
						} else {
							status = integrationcenter.CapabilityQualificationRequired
						}
					}
					capabilities = append(capabilities, integrationcenter.Capability{Name: string(setting.Capability), Direction: string(setting.Direction), Status: status, ApprovalRequired: setting.ApprovalRequired, Risk: string(setting.Risk)})
					if status == integrationcenter.CapabilityEnabled {
						capStatus = integrationcenter.CapabilityEnabled
					}
				}
			}
		}
	}
	syncStatus := integrationcenter.SyncNotConfigured
	syncVisibility := integrationcenter.VisibilityFull
	if !policiesReadable || !runsReadable {
		syncStatus = "unknown"
		syncVisibility = integrationcenter.VisibilityRedacted
	} else if len(policies) > 0 {
		syncStatus = integrationcenter.SyncIdle
		allDisabled := true
		for _, policy := range policies {
			if policy.Enabled {
				allDisabled = false
			}
			if run, ok := runs[policy.ID]; ok {
				if run.Status == reconciliation.RunRunning {
					syncStatus = integrationcenter.SyncRunning
				}
				if run.Status == reconciliation.RunInterrupted && syncStatus != integrationcenter.SyncRunning {
					syncStatus = integrationcenter.SyncRetrying
				}
			}
		}
		if allDisabled {
			syncStatus = integrationcenter.SyncPaused
		}
	}
	reconStatus := integrationcenter.ReconciliationNotConfigured
	reconVisibility := integrationcenter.VisibilityFull
	if !policiesReadable || !driftsReadable {
		reconStatus = "unknown"
		reconVisibility = integrationcenter.VisibilityRedacted
	} else if len(policies) > 0 {
		reconStatus = integrationcenter.ReconciliationHealthy
		for _, policy := range policies {
			if drifts[policy.ID] {
				reconStatus = integrationcenter.ReconciliationDriftOpen
			}
		}
	}
	configurationEvidence := e
	configurationEvidence.Visibility = configurationVisibility
	capabilityEvidence := e
	capabilityEvidence.Visibility = capabilityVisibility
	syncEvidence := e
	syncEvidence.Visibility = syncVisibility
	reconEvidence := e
	reconEvidence.Visibility = reconVisibility
	dims := integrationcenter.Dimensions{Runtime: integrationcenter.Dimension{Status: string(runtimeStatus), Evidence: e}, Account: integrationcenter.Dimension{Status: string(accountStatus), Evidence: e}, Credential: integrationcenter.Dimension{Status: string(credential), Evidence: e}, Configuration: integrationcenter.Dimension{Status: string(configuration), Evidence: configurationEvidence}, Health: integrationcenter.Dimension{Status: string(healthStatus), Evidence: healthEvidence}, Capability: integrationcenter.Dimension{Status: string(capStatus), Evidence: capabilityEvidence}, Sync: integrationcenter.Dimension{Status: syncStatus, Evidence: syncEvidence}, Reconciliation: integrationcenter.Dimension{Status: reconStatus, Evidence: reconEvidence}, Webhook: integrationcenter.Dimension{Status: string(integrationcenter.WebhookNotConfigured), Evidence: e}, RateLimit: integrationcenter.Dimension{Status: string(integrationcenter.RateLimitNotObserved), Evidence: e}}
	input := integrationcenter.Input{AccountID: account.ID, ConnectorID: account.ConnectorID, Family: string(account.Family), Surface: support.Surface, Version: account.Version, Dimensions: dims, Capabilities: capabilities, Now: now}
	if !supported {
		input.Surface = "unknown"
	}
	row, err := integrationcenter.Reduce(input)
	if err != nil {
		partial = true
		input.Dimensions.Account.Status = string(integrationcenter.AccountError)
		row, _ = integrationcenter.Reduce(input)
	}
	row.Partial = partial
	return row, partial
}

func centerEvidence(now, checked time.Time, kind, ref, reason string, visibility integrationcenter.Visibility) integrationcenter.EvidenceRef {
	if checked.IsZero() {
		checked = now
	}
	checked = checked.UTC()
	observed := checked
	if observed.After(now) {
		observed = now
	}
	return integrationcenter.EvidenceRef{ObservedAt: observed, CheckedAt: checked, SourceKind: kind, SourceRef: ref, ReasonCode: reason, Visibility: visibility, StaleAfterSeconds: 3600, AgeSeconds: maxInt64(0, int64(now.Sub(checked).Seconds()))}
}
func accountWatermark(accounts []sdk.Account) string {
	values := make([]string, 0, len(accounts))
	for _, account := range accounts {
		values = append(values, account.ID+":"+strconv.FormatInt(account.Version, 10)+":"+strconv.FormatInt(account.UpdatedAt.UnixNano(), 10))
	}
	return "connector_accounts:" + watermarkDigest(values)
}

func policyWatermark(policies []syncengine.Policy) string {
	values := make([]string, 0, len(policies))
	for _, policy := range policies {
		values = append(values, policy.ID+":"+strconv.FormatInt(policy.Version, 10)+":"+strconv.FormatInt(policy.UpdatedAt.UnixNano(), 10))
	}
	return "sync_policies:" + watermarkDigest(values)
}

func runWatermark(runs []reconciliation.Run) string {
	values := make([]string, 0, len(runs))
	for _, run := range runs {
		values = append(values, run.ID+":"+string(run.Status)+":"+strconv.FormatInt(run.UpdatedAt.UnixNano(), 10))
	}
	return "reconciliation:" + watermarkDigest(values)
}

func watermarkDigest(values []string) string {
	sort.Strings(values)
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])[:16]
}

func centerMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func centerMatches(row integrationcenter.Snapshot, request integrationCenterReadRequest) bool {
	if request.AccountID != "" && row.AccountID != request.AccountID {
		return false
	}
	if request.Family != "" && row.Family != request.Family {
		return false
	}
	if request.Surface != "" && row.Surface != request.Surface {
		return false
	}
	if request.Overall != "" && string(row.Overall) != request.Overall {
		return false
	}
	if request.Health != "" && row.Dimensions.Health.Status != request.Health {
		return false
	}
	if request.Sync != "" && row.Dimensions.Sync.Status != request.Sync {
		return false
	}
	if request.Capability != "" {
		found := false
		for _, c := range row.Capabilities {
			if c.Name == request.Capability || string(c.Status) == request.Capability {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if request.Issue != "" {
		found := false
		for _, issue := range row.Issues {
			if issue.Code == request.Issue || issue.Dimension == request.Issue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if request.Stale && row.Overall != integrationcenter.OverallStale {
		return false
	}
	return true
}

func newIntegrationCenterRoutes(reader integrationCenterReader) []ProtectedRoute {
	list := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { integrationCenterList(w, r, reader) })
	detail := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { integrationCenterDetail(w, r, reader) })
	return []ProtectedRoute{{Method: http.MethodGet, Path: IntegrationCenterPath, Permission: "integrations.center.read", Handler: list}, {Method: http.MethodGet, Path: IntegrationCenterAccountPath, PathPrefix: true, Permission: "integrations.center.read", Handler: detail}}
}

type integrationCenterResponse struct {
	SnapshotVersion  int                          `json:"snapshot_version"`
	SnapshotDigest   string                       `json:"snapshot_digest"`
	GeneratedAt      time.Time                    `json:"generated_at"`
	Partial          bool                         `json:"partial"`
	Consistency      string                       `json:"consistency"`
	SourceWatermarks []string                     `json:"source_watermarks"`
	Summary          integrationcenter.Summary    `json:"summary"`
	Items            []integrationcenter.Snapshot `json:"items"`
	NextCursor       string                       `json:"next_cursor,omitempty"`
}

func integrationCenterList(w http.ResponseWriter, r *http.Request, reader integrationCenterReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || reader == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	request, valid := parseIntegrationCenterRequest(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	result, err := reader.Read(r.Context(), scope, request)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	response := integrationCenterResponse{SnapshotVersion: 1, GeneratedAt: result.GeneratedAt, Partial: result.Partial, Consistency: "best_effort", SourceWatermarks: result.SourceWatermarks, Summary: integrationcenter.BuildSummary(result.Rows), Items: result.Rows, NextCursor: result.NextCursor}
	response.SnapshotDigest = integrationCenterDigest(result.Rows, result.SourceWatermarks)
	response.SnapshotVersion = 1
	etag := `"` + response.SnapshotDigest + `"`
	setIntegrationCenterHeaders(w, etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func integrationCenterDetail(w http.ResponseWriter, r *http.Request, reader integrationCenterReader) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || reader == nil {
		writeProblem(w, http.StatusForbidden, "Forbidden")
		return
	}
	accountID := strings.TrimPrefix(r.URL.Path, IntegrationCenterAccountPath)
	if accountID == "" || strings.Contains(accountID, "/") || len(accountID) > 128 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	request, valid := parseIntegrationCenterRequest(r)
	if !valid {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	request.AccountID = accountID
	request.Limit = 1
	result, err := reader.Read(r.Context(), scope, request)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if len(result.Rows) != 1 {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	row := result.Rows[0]
	etag := `"` + row.SnapshotDigest + `"`
	setIntegrationCenterHeaders(w, etag)
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func setIntegrationCenterHeaders(w http.ResponseWriter, etag string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", etag)
}

func etagMatches(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == current {
			return true
		}
		if strings.HasPrefix(candidate, "W/") && strings.TrimPrefix(candidate, "W/") == current {
			return true
		}
	}
	return false
}

func parseIntegrationCenterRequest(r *http.Request) (integrationCenterReadRequest, bool) {
	q := r.URL.Query()
	if q.Get("organization_id") != "" || q.Get("workspace_id") != "" || q.Get("tenant") != "" {
		return integrationCenterReadRequest{}, false
	}
	limit := 50
	if raw := q.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return integrationCenterReadRequest{}, false
		}
		limit = value
	}
	cursor := q.Get("cursor")
	if len(cursor) > 256 {
		return integrationCenterReadRequest{}, false
	}
	stale, err := strconv.ParseBool(q.Get("stale"))
	if q.Get("stale") != "" && err != nil {
		return integrationCenterReadRequest{}, false
	}
	allowed := func(v string, max int) bool {
		return len(v) <= max && v == strings.TrimSpace(v) && !strings.ContainsAny(v, "?#\x00\r\n")
	}
	values := []string{q.Get("family"), q.Get("surface"), q.Get("overall"), q.Get("health"), q.Get("sync"), q.Get("capability"), q.Get("issue"), q.Get("account_id")}
	for _, value := range values {
		if !allowed(value, 128) {
			return integrationCenterReadRequest{}, false
		}
	}
	return integrationCenterReadRequest{Cursor: cursor, Limit: limit, Family: values[0], Surface: values[1], Overall: values[2], Health: values[3], Sync: values[4], Capability: values[5], Issue: values[6], AccountID: values[7], Stale: stale}, true
}

func integrationCenterDigest(rows []integrationcenter.Snapshot, watermarks []string) string {
	sort.Slice(watermarks, func(i, j int) bool { return watermarks[i] < watermarks[j] })
	raw, _ := json.Marshal(struct {
		Rows       []integrationcenter.Snapshot `json:"rows"`
		Watermarks []string                     `json:"watermarks"`
	}{rows, watermarks})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
