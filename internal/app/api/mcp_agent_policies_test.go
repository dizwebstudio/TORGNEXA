package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/agentgovernance"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
)

type mcpAgentPolicyAuditStub struct{ actions []string }

func (s *mcpAgentPolicyAuditStub) Capture(_ context.Context, _ tenancy.Scope, entry audit.Entry) (audit.Record, error) {
	s.actions = append(s.actions, entry.Action)
	return audit.Record{}, nil
}

func (s *mcpAgentPolicyAuditStub) WithinTransaction(ctx context.Context, _ tenancy.Scope, operation func(context.Context) error) error {
	return operation(ctx)
}

type mcpAccountFinderStub struct {
	accounts map[string]mcpaccounts.Account
}

func (s *mcpAccountFinderStub) FindByID(_ context.Context, _ tenancy.Scope, id string) (mcpaccounts.Account, []byte, error) {
	account, ok := s.accounts[id]
	if !ok {
		return mcpaccounts.Account{}, nil, mcpaccounts.ErrNotFound
	}
	return account, nil, nil
}

// fakeAgentPolicyStore is an in-memory single-version-history store that
// exercises the same Validate() gates the real repository does, so a rule
// set the handler builds incorrectly (e.g. a sensitive-write tool with no
// spending limit) is rejected here exactly as it would be in production.
type fakeAgentPolicyStore struct {
	installed map[string][]agentgovernance.Policy
	receipts  map[string]agentgovernance.Policy
}

func newFakeAgentPolicyStore() *fakeAgentPolicyStore {
	return &fakeAgentPolicyStore{installed: map[string][]agentgovernance.Policy{}, receipts: map[string]agentgovernance.Policy{}}
}

func (s *fakeAgentPolicyStore) ResolveAgentPolicy(_ context.Context, _ tenancy.Scope, agent agentgovernance.Agent, at time.Time) (agentgovernance.Policy, error) {
	for _, versions := range s.installed {
		for i := len(versions) - 1; i >= 0; i-- {
			policy := versions[i]
			if policy.AgentID == agent.ID && policy.IntegrationID == agent.IntegrationID && policy.Effective(at) {
				return policy, nil
			}
		}
	}
	return agentgovernance.Policy{}, agentgovernance.ErrNotFound
}

func (s *fakeAgentPolicyStore) InstallPolicy(_ context.Context, _ tenancy.Scope, policy agentgovernance.Policy, change agentgovernance.Change) error {
	if policy.Validate() != nil || change.Validate() != nil {
		return agentgovernance.ErrInvalid
	}
	s.installed[policy.ID] = append(s.installed[policy.ID], policy)
	return nil
}

func (s *fakeAgentPolicyStore) InstallPolicyGoverned(ctx context.Context, scope tenancy.Scope, policy agentgovernance.Policy, change agentgovernance.Change, key string, _ []byte) (agentgovernance.Policy, bool, error) {
	if stored, ok := s.receipts[key]; ok {
		return stored, true, nil
	}
	if err := s.InstallPolicy(ctx, scope, policy, change); err != nil {
		return agentgovernance.Policy{}, false, err
	}
	s.receipts[key] = policy
	return policy, false, nil
}

func (s *fakeAgentPolicyStore) LatestPolicyVersion(_ context.Context, _ tenancy.Scope, policyID string) (uint64, error) {
	versions := s.installed[policyID]
	if len(versions) == 0 {
		return 0, nil
	}
	return versions[len(versions)-1].Version, nil
}

type fakeAgentKillSwitchStore struct {
	tenantDisabled bool
	tenantVersion  uint64
	changes        []agentgovernance.KillChange
	receipts       map[string]agentgovernance.KillChange
}

func (s *fakeAgentKillSwitchStore) AgentKillState(context.Context, tenancy.Scope, agentgovernance.Agent) (agentgovernance.KillState, error) {
	return agentgovernance.KillState{TenantDisabled: s.tenantDisabled, Version: s.tenantVersion}, nil
}

func (s *fakeAgentKillSwitchStore) RecordKillSwitch(_ context.Context, _ tenancy.Scope, change agentgovernance.KillChange) error {
	if change.Validate() != nil {
		return agentgovernance.ErrInvalid
	}
	s.changes = append(s.changes, change)
	s.tenantDisabled = change.Disabled
	s.tenantVersion = change.Version
	return nil
}

func (s *fakeAgentKillSwitchStore) RecordKillSwitchGoverned(ctx context.Context, scope tenancy.Scope, change agentgovernance.KillChange, key string, _ []byte) (agentgovernance.KillChange, bool, error) {
	if s.receipts == nil {
		s.receipts = map[string]agentgovernance.KillChange{}
	}
	if stored, ok := s.receipts[key]; ok {
		return stored, true, nil
	}
	if err := s.RecordKillSwitch(ctx, scope, change); err != nil {
		return agentgovernance.KillChange{}, false, err
	}
	s.receipts[key] = change
	return change, false, nil
}

func testMCPAccount(id string) mcpaccounts.Account {
	return mcpaccounts.Account{ID: id, Label: "workflow", AgentID: "agent-1", ModelID: "model.test", IntegrationID: "integration.test", Permissions: []string{"commerce.products.read", "commerce.price.change.request"}, Enabled: true, Version: 1}
}

func TestGetMCPAccountPolicyReportsNotInstalledUntilFirstInstall(t *testing.T) {
	accounts := &mcpAccountFinderStub{accounts: map[string]mcpaccounts.Account{"mcp_1": testMCPAccount("mcp_1")}}
	routes := newMCPAgentPolicyRoutes(accounts, newFakeAgentPolicyStore(), &fakeAgentKillSwitchStore{}, &mcpAgentPolicyAuditStub{})

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/settings/mcp-accounts/mcp_1:policy", nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"installed":false`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestInstallMCPAccountPolicyDerivesRulesFromGrantedPermissionsAndIncrementsVersion(t *testing.T) {
	accounts := &mcpAccountFinderStub{accounts: map[string]mcpaccounts.Account{"mcp_1": testMCPAccount("mcp_1")}}
	policies := newFakeAgentPolicyStore()
	audit := &mcpAgentPolicyAuditStub{}
	routes := newMCPAgentPolicyRoutes(accounts, policies, &fakeAgentKillSwitchStore{}, audit)
	var installRoute, getRoute ProtectedRoute
	for _, route := range routes {
		if route.Method == http.MethodPost && route.Path == mcpAccountsPrefix {
			installRoute = route
		}
		if route.Method == http.MethodGet && route.Path == mcpAccountsPrefix {
			getRoute = route
		}
	}

	body := `{"price_change_money_limits":[{"currency":"RUB","max_minor_units":5000000}],"price_change_max_calls":10,"price_change_window_seconds":3600}`
	for attempt := 1; attempt <= 2; attempt++ {
		request := productionRequestContext(t, httptest.NewRequest(http.MethodPost, "/api/v1/settings/mcp-accounts/mcp_1:install-policy", strings.NewReader(body)))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "policy-install-"+strconv.Itoa(attempt))
		response := httptest.NewRecorder()
		installRoute.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d: status=%d body=%s", attempt, response.Code, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"version":`+strconv.Itoa(attempt)) {
			t.Fatalf("attempt %d did not report version %d: %s", attempt, attempt, response.Body.String())
		}
		if !strings.Contains(response.Body.String(), `"commerce.price.change.request"`) || strings.Count(response.Body.String(), `"tool":`) != 2 {
			t.Fatalf("attempt %d unexpected rule set: %s", attempt, response.Body.String())
		}
	}
	replay := productionRequestContext(t, httptest.NewRequest(http.MethodPost, "/api/v1/settings/mcp-accounts/mcp_1:install-policy", strings.NewReader(body)))
	replay.Header.Set("Content-Type", "application/json")
	replay.Header.Set("Idempotency-Key", "policy-install-2")
	replayResponse := httptest.NewRecorder()
	installRoute.Handler.ServeHTTP(replayResponse, replay)
	if replayResponse.Code != http.StatusOK || !strings.Contains(replayResponse.Body.String(), `"version":2`) || len(policies.receipts) != 2 || len(policies.installed) != 1 {
		t.Fatalf("replay status=%d body=%s", replayResponse.Code, replayResponse.Body.String())
	}
	for _, versions := range policies.installed {
		if len(versions) != 2 {
			t.Fatalf("policy replay appended a version: %+v", policies.installed)
		}
	}
	if len(audit.actions) != 2 || audit.actions[0] != "settings.mcp_agent_policy.installed" {
		t.Fatalf("audit actions = %v", audit.actions)
	}

	getRequest := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/settings/mcp-accounts/mcp_1:policy", nil))
	getResponse := httptest.NewRecorder()
	getRoute.Handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"version":2`) {
		t.Fatalf("status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
}

func TestInstallMCPAccountPolicyRejectsSensitiveToolWithoutSpendingLimit(t *testing.T) {
	accounts := &mcpAccountFinderStub{accounts: map[string]mcpaccounts.Account{"mcp_1": testMCPAccount("mcp_1")}}
	routes := newMCPAgentPolicyRoutes(accounts, newFakeAgentPolicyStore(), &fakeAgentKillSwitchStore{}, &mcpAgentPolicyAuditStub{})
	var installRoute ProtectedRoute
	for _, route := range routes {
		if route.Method == http.MethodPost && route.Path == mcpAccountsPrefix {
			installRoute = route
		}
	}

	request := productionRequestContext(t, httptest.NewRequest(http.MethodPost, "/api/v1/settings/mcp-accounts/mcp_1:install-policy", strings.NewReader(`{}`)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "policy-invalid")
	response := httptest.NewRecorder()
	installRoute.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestInstallMCPAccountPolicyRejectsUnknownAccount(t *testing.T) {
	accounts := &mcpAccountFinderStub{accounts: map[string]mcpaccounts.Account{}}
	routes := newMCPAgentPolicyRoutes(accounts, newFakeAgentPolicyStore(), &fakeAgentKillSwitchStore{}, &mcpAgentPolicyAuditStub{})

	request := productionRequestContext(t, httptest.NewRequest(http.MethodGet, "/api/v1/settings/mcp-accounts/mcp_missing:policy", nil))
	response := httptest.NewRecorder()
	routes[0].Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestMCPAgentKillSwitchTogglesTenantWideAndRequiresReason(t *testing.T) {
	accounts := &mcpAccountFinderStub{accounts: map[string]mcpaccounts.Account{}}
	kills := &fakeAgentKillSwitchStore{}
	audit := &mcpAgentPolicyAuditStub{}
	routes := newMCPAgentPolicyRoutes(accounts, newFakeAgentPolicyStore(), kills, audit)
	var getRoute, setRoute ProtectedRoute
	for _, route := range routes {
		if route.Path == MCPAgentKillSwitchPath && route.Method == http.MethodGet {
			getRoute = route
		}
		if route.Path == MCPAgentKillSwitchPath && route.Method == http.MethodPost {
			setRoute = route
		}
	}

	missingReason := productionRequestContext(t, httptest.NewRequest(http.MethodPost, MCPAgentKillSwitchPath, strings.NewReader(`{"disabled":true}`)))
	missingReason.Header.Set("Content-Type", "application/json")
	missingReason.Header.Set("Idempotency-Key", "kill-missing")
	missingReasonResponse := httptest.NewRecorder()
	setRoute.Handler.ServeHTTP(missingReasonResponse, missingReason)
	if missingReasonResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing reason status=%d", missingReasonResponse.Code)
	}

	stop := productionRequestContext(t, httptest.NewRequest(http.MethodPost, MCPAgentKillSwitchPath, strings.NewReader(`{"disabled":true,"reason":"suspected prompt injection incident"}`)))
	stop.Header.Set("Content-Type", "application/json")
	stop.Header.Set("Idempotency-Key", "kill-stop")
	stopResponse := httptest.NewRecorder()
	setRoute.Handler.ServeHTTP(stopResponse, stop)
	if stopResponse.Code != http.StatusOK || !strings.Contains(stopResponse.Body.String(), `"disabled":true`) {
		t.Fatalf("stop status=%d body=%s", stopResponse.Code, stopResponse.Body.String())
	}
	replayStop := productionRequestContext(t, httptest.NewRequest(http.MethodPost, MCPAgentKillSwitchPath, strings.NewReader(`{"disabled":true,"reason":"suspected prompt injection incident"}`)))
	replayStop.Header.Set("Content-Type", "application/json")
	replayStop.Header.Set("Idempotency-Key", "kill-stop")
	replayStopResponse := httptest.NewRecorder()
	setRoute.Handler.ServeHTTP(replayStopResponse, replayStop)
	if replayStopResponse.Code != http.StatusOK {
		t.Fatalf("kill replay status=%d body=%s", replayStopResponse.Code, replayStopResponse.Body.String())
	}

	getRequest := productionRequestContext(t, httptest.NewRequest(http.MethodGet, MCPAgentKillSwitchPath, nil))
	getResponse := httptest.NewRecorder()
	getRoute.Handler.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `"disabled":true`) {
		t.Fatalf("get status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
	if len(kills.changes) != 1 || kills.changes[0].Version != 1 || len(audit.actions) != 1 {
		t.Fatalf("changes=%+v audit=%v", kills.changes, audit.actions)
	}
}
