package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/trustcontrolrepo"
	"github.com/torgnexa/torgnexa/internal/platform/trustcontrol"
)

const (
	SecurityEvidencePath       = "/api/v1/settings/security/evidence"
	AIEgressPolicyPath         = "/api/v1/settings/ai-egress-policy"
	AIEgressPreviewPath        = "/api/v1/settings/ai-egress-policy:preview"
	ConnectorReplayPath        = "/api/v1/settings/connector-replay:run"
	ProfitabilityScenariosPath = "/api/v1/profitability/scenarios"
)

type trustControlStore interface {
	ListEvidence(context.Context, tenancy.Scope, int, time.Time) ([]trustcontrol.Evidence, error)
	CurrentPolicy(context.Context, tenancy.Scope) (trustcontrol.Policy, error)
	PutPolicy(context.Context, tenancy.Scope, string, string, string, int64, trustcontrol.Policy, []byte) (trustcontrol.Policy, bool, error)
	EvaluateAI(context.Context, tenancy.Scope, trustcontrol.EgressRequest) (trustcontrol.Policy, error)
	CreateReplay(context.Context, tenancy.Scope, string, string, string, string, string, json.RawMessage, json.RawMessage, []byte, string) (trustcontrolrepo.ReplayRun, bool, error)
	CreateScenario(context.Context, tenancy.Scope, string, string, string, trustcontrol.ScenarioInput, trustcontrol.ScenarioResult, []byte, []byte, []byte) (trustcontrolrepo.Scenario, bool, error)
}

type trustControlAPI struct{ store trustControlStore }

func newTrustControlRoutes(store trustControlStore) []ProtectedRoute {
	api := &trustControlAPI{store: store}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: SecurityEvidencePath, Permission: "settings.security.evidence.read", Handler: http.HandlerFunc(api.evidence)},
		{Method: http.MethodGet, Path: AIEgressPolicyPath, Permission: "settings.ai_governance.read", Handler: http.HandlerFunc(api.getPolicy)},
		{Method: http.MethodPut, Path: AIEgressPolicyPath, Permission: "settings.ai_governance.write", Handler: http.HandlerFunc(api.putPolicy)},
		{Method: http.MethodPost, Path: AIEgressPreviewPath, Permission: "ai.analyze", Handler: http.HandlerFunc(api.preview)},
		{Method: http.MethodPost, Path: ConnectorReplayPath, Permission: "connectors.replay.run", Handler: http.HandlerFunc(api.replay)},
		{Method: http.MethodPost, Path: ProfitabilityScenariosPath, Permission: "profitability.scenarios.write", Handler: http.HandlerFunc(api.scenario)},
	}
}

func (api *trustControlAPI) evidence(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
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
	var before time.Time
	if raw := r.URL.Query().Get("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		before = parsed
	}
	items, err := api.store.ListEvidence(r.Context(), scope, limit, before)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		view := map[string]any{"id": item.ID, "type": item.Type, "actor_ref": item.ActorRef, "resource_type": item.ResourceType, "resource_id": item.ResourceID, "correlation_id": item.CorrelationID, "decision": item.Decision, "summary": item.Summary, "occurred_at": item.OccurredAt}
		if len(item.RequestSHA256) == 32 {
			view["request_sha256"] = hex.EncodeToString(item.RequestSHA256)
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views})
}

func (api *trustControlAPI) getPolicy(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	policy, err := api.store.CurrentPolicy(r.Context(), scope)
	if errors.Is(err, trustcontrolrepo.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

type aiEgressPolicyRequest struct {
	ExpectedVersion     int64    `json:"expected_version"`
	Enabled             bool     `json:"enabled"`
	AllowedDataClasses  []string `json:"allowed_data_classes"`
	AllowedProviders    []string `json:"allowed_providers"`
	AllowedModels       []string `json:"allowed_models"`
	MaxPromptBytes      int      `json:"max_prompt_bytes"`
	MonthlyRequestLimit int      `json:"monthly_request_limit"`
}

func (api *trustControlAPI) putPolicy(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || key == "" || len(key) > 128 || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input aiEgressPolicyRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	policy := trustcontrol.Policy{Enabled: input.Enabled, AllowedDataClasses: input.AllowedDataClasses, AllowedDestinations: input.AllowedProviders, AllowedModels: input.AllowedModels, MaxPromptBytes: input.MaxPromptBytes, MonthlyRequestLimit: input.MonthlyRequestLimit}
	_, digest, err := trustcontrol.DigestJSON(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	policy, replayed, err := api.store.PutPolicy(r.Context(), scope, newApprovalID(), principal.SubjectRef, key, input.ExpectedVersion, policy, digest)
	if errors.Is(err, trustcontrolrepo.ErrConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"policy": policy, "replayed": replayed})
}

type aiEgressPreviewRequest struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	DataClasses []string `json:"data_classes"`
	Prompt      string   `json:"prompt"`
}

func (api *trustControlAPI) preview(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input aiEgressPreviewRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	request := trustcontrol.EgressRequest{Destination: strings.TrimSpace(input.Provider), Model: strings.TrimSpace(input.Model), DataClasses: input.DataClasses, PromptBytes: len(input.Prompt)}
	policy, err := api.store.EvaluateAI(r.Context(), scope, request)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"allowed": false, "reason": "policy_denied"})
		return
	}
	redacted, err := trustcontrol.RedactPrompt(input.Prompt, policy.MaxPromptBytes)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"allowed": true, "redacted_prompt": redacted, "policy_version": policy.Version, "persisted": false})
}

type connectorReplayRequest struct {
	Family     string          `json:"connector_family"`
	Capability string          `json:"capability"`
	Fixture    json.RawMessage `json:"fixture"`
}

func (api *trustControlAPI) replay(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || key == "" || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input connectorReplayRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	input.Family = strings.TrimSpace(input.Family)
	input.Capability = strings.TrimSpace(input.Capability)
	if len(key) > 128 || trustcontrol.ValidateReplayTarget(input.Family, input.Capability) != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	result, canonical, err := trustcontrol.ValidateSyntheticFixture(input.Fixture)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Synthetic fixture required")
		return
	}
	_, digest, _ := trustcontrol.DigestJSON(map[string]any{"connector_family": input.Family, "capability": input.Capability, "fixture": json.RawMessage(canonical)})
	resultJSON, _ := json.Marshal(result)
	run, replayed, err := api.store.CreateReplay(r.Context(), scope, newApprovalID(), principal.SubjectRef, key, input.Family, input.Capability, canonical, resultJSON, digest, "passed")
	if errors.Is(err, trustcontrolrepo.ErrConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": run.ID, "status": run.Status, "result": json.RawMessage(run.Result), "created_at": run.CreatedAt, "replayed": replayed})
}

func (api *trustControlAPI) scenario(w http.ResponseWriter, r *http.Request) {
	scope, scopeOK := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !scopeOK || !principalOK || key == "" || len(key) > 128 || api == nil || api.store == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input trustcontrol.ScenarioInput
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	result, err := trustcontrol.CalculateScenario(input)
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	inputJSON, digest, _ := trustcontrol.DigestJSON(input)
	resultJSON, _, _ := trustcontrol.DigestJSON(result)
	scenario, replayed, err := api.store.CreateScenario(r.Context(), scope, newApprovalID(), principal.SubjectRef, key, input, result, inputJSON, resultJSON, digest)
	if errors.Is(err, trustcontrolrepo.ErrConflict) {
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": scenario.ID, "name": scenario.Name, "algorithm_version": scenario.AlgorithmVersion, "result": json.RawMessage(scenario.Result), "created_at": scenario.CreatedAt, "replayed": replayed})
}
