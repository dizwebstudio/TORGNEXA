// Package api: MCP agent governance (ADR 0098's still-open gap). An enabled
// mcp_client_accounts row lets a bearer token authenticate to POST /mcp, but
// internal/app/mcp/server.go's real agentgovernance.Service denies every
// tools/list and tools/call until a matching Policy is installed for that
// account's (agent_id, integration_id) pair. This file is the admin surface
// that closes that gap: it derives a policy's tool rules entirely from the
// account's own already-granted permissions (risk and approval_required per
// tool are fixed by the MCP tool catalog in tools.go, never admin-editable,
// so an install can never produce a rule the governance evaluator would
// silently mismatch) plus an operator-supplied spending limit for the one
// sensitive-write tool, and a tenant-wide emergency kill switch.
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/agentgovernance"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/mcpaccounts"
)

const (
	mcpAccountsPrefix      = MCPAccountsPath + "/"
	MCPAgentKillSwitchPath = "/api/v1/settings/mcp-agents:kill-switch"
)

// mcpToolCatalog mirrors internal/app/mcp/tools.go's fixed descriptors. Risk
// and ApprovalRequired here MUST match tools.go exactly: agentgovernance's
// evaluateBase denies a call whenever rule.Risk/ApprovalRequired differs from
// the tool descriptor's fixed values, so these are constants, never request input.
var mcpToolCatalog = []struct {
	Tool             string
	Permission       string
	Risk             agentgovernance.Risk
	ApprovalRequired bool
}{
	{"commerce.products.search", "commerce.products.read", agentgovernance.RiskRead, false},
	{"commerce.orders.list", "commerce.orders.read", agentgovernance.RiskRead, false},
	{"party.counterparties.search", "party.counterparties.read", agentgovernance.RiskRead, false},
	{"commerce.price.change.request", "commerce.price.change.request", agentgovernance.RiskSensitiveWrite, true},
}

// mcpTenantScopeProbeAgent never matches a real agent/integration kill-switch
// row (no real account uses this literal id), which isolates
// AgentKillState's combined tenant/agent/integration result down to exactly
// the tenant-scope row: its returned KillState.Version is then the tenant
// row's own version, safe to increment for the next RecordKillSwitch call.
var mcpTenantScopeProbeAgent = agentgovernance.Agent{ID: "tenant-scope-probe", ModelID: "tenant-scope-probe", RunID: "tenant-scope-probe", IntegrationID: "tenant-scope-probe"}

type mcpAccountFinder interface {
	FindByID(context.Context, tenancy.Scope, string) (mcpaccounts.Account, []byte, error)
}

type agentPolicyStore interface {
	ResolveAgentPolicy(context.Context, tenancy.Scope, agentgovernance.Agent, time.Time) (agentgovernance.Policy, error)
	InstallPolicy(context.Context, tenancy.Scope, agentgovernance.Policy, agentgovernance.Change) error
	LatestPolicyVersion(context.Context, tenancy.Scope, string) (uint64, error)
}

type agentKillSwitchStore interface {
	AgentKillState(context.Context, tenancy.Scope, agentgovernance.Agent) (agentgovernance.KillState, error)
	RecordKillSwitch(context.Context, tenancy.Scope, agentgovernance.KillChange) error
}

type mcpAgentPoliciesAPI struct {
	accounts mcpAccountFinder
	policies agentPolicyStore
	kills    agentKillSwitchStore
	audit    auditCapturer
}

func newMCPAgentPolicyRoutes(accounts mcpAccountFinder, policies agentPolicyStore, kills agentKillSwitchStore, auditService auditCapturer) []ProtectedRoute {
	api := &mcpAgentPoliciesAPI{accounts: accounts, policies: policies, kills: kills, audit: auditService}
	return []ProtectedRoute{
		{Method: http.MethodGet, Path: mcpAccountsPrefix, PathPrefix: true, Permission: "settings.mcp_accounts.read", Handler: http.HandlerFunc(api.accountAction)},
		{Method: http.MethodPost, Path: mcpAccountsPrefix, PathPrefix: true, Permission: "settings.mcp_accounts.write", Handler: http.HandlerFunc(api.accountAction)},
		{Method: http.MethodGet, Path: MCPAgentKillSwitchPath, Permission: "settings.mcp_accounts.read", Handler: http.HandlerFunc(api.getKillSwitch)},
		{Method: http.MethodPost, Path: MCPAgentKillSwitchPath, Permission: "settings.mcp_accounts.write", Handler: http.HandlerFunc(api.setKillSwitch)},
	}
}

func mcpAccountActionPath(path string) (id string, action string, ok bool) {
	tail := strings.TrimPrefix(path, mcpAccountsPrefix)
	if tail == path || tail == "" || strings.Contains(tail, "/") {
		return "", "", false
	}
	id, action, hasAction := strings.Cut(tail, ":")
	if id == "" || !hasAction || action == "" {
		return "", "", false
	}
	return id, action, true
}

func (api *mcpAgentPoliciesAPI) accountAction(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	accountID, action, pathOK := mcpAccountActionPath(r.URL.Path)
	if !ok || !pathOK || api == nil || api.accounts == nil || api.policies == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	account, _, err := api.accounts.FindByID(r.Context(), scope, accountID)
	if err != nil {
		writeProblem(w, http.StatusNotFound, "Not Found")
		return
	}
	switch {
	case r.Method == http.MethodGet && action == "policy":
		api.getPolicy(w, r, scope, account)
	case r.Method == http.MethodPost && action == "install-policy":
		api.installPolicy(w, r, scope, account)
	default:
		writeProblem(w, http.StatusNotFound, "Not Found")
	}
}

func mcpAdminLookupAgent(account mcpaccounts.Account) agentgovernance.Agent {
	return agentgovernance.Agent{ID: account.AgentID, ModelID: account.ModelID, RunID: "settings-console", IntegrationID: account.IntegrationID}
}

type mcpMoneyView struct {
	Currency   string `json:"currency"`
	MinorUnits int64  `json:"minor_units"`
}
type mcpAgentPolicyRuleView struct {
	Tool             string         `json:"tool"`
	Permission       string         `json:"permission"`
	Risk             string         `json:"risk"`
	ApprovalRequired bool           `json:"approval_required"`
	Money            []mcpMoneyView `json:"money,omitempty"`
	MaxCalls         int64          `json:"max_calls,omitempty"`
	WindowSeconds    int64          `json:"window_seconds,omitempty"`
}
type mcpAgentPolicyView struct {
	Installed      bool                     `json:"installed"`
	PolicyID       string                   `json:"policy_id,omitempty"`
	Version        uint64                   `json:"version,omitempty"`
	Rules          []mcpAgentPolicyRuleView `json:"rules,omitempty"`
	EffectiveFrom  *time.Time               `json:"effective_from,omitempty"`
	EffectiveUntil *time.Time               `json:"effective_until,omitempty"`
}

func mcpAgentPolicyToView(policy agentgovernance.Policy) mcpAgentPolicyView {
	rules := make([]mcpAgentPolicyRuleView, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		money := make([]mcpMoneyView, 0, len(rule.Limits.Money))
		for _, m := range rule.Limits.Money {
			money = append(money, mcpMoneyView{Currency: m.Currency, MinorUnits: m.MinorUnits})
		}
		rules = append(rules, mcpAgentPolicyRuleView{
			Tool: rule.Tool, Permission: rule.Permission, Risk: string(rule.Risk), ApprovalRequired: rule.ApprovalRequired,
			Money: money, MaxCalls: rule.Limits.MaxCalls, WindowSeconds: rule.Limits.WindowSeconds,
		})
	}
	from := policy.EffectiveFrom
	return mcpAgentPolicyView{Installed: true, PolicyID: policy.ID, Version: policy.Version, Rules: rules, EffectiveFrom: &from, EffectiveUntil: policy.EffectiveUntil}
}

func (api *mcpAgentPoliciesAPI) getPolicy(w http.ResponseWriter, r *http.Request, scope tenancy.Scope, account mcpaccounts.Account) {
	policy, err := api.policies.ResolveAgentPolicy(r.Context(), scope, mcpAdminLookupAgent(account), time.Now().UTC())
	if errors.Is(err, agentgovernance.ErrNotFound) {
		writeJSON(w, http.StatusOK, mcpAgentPolicyView{Installed: false})
		return
	}
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, mcpAgentPolicyToView(policy))
}

type mcpMoneyInput struct {
	Currency      string `json:"currency"`
	MaxMinorUnits int64  `json:"max_minor_units"`
}
type mcpAgentPolicyInstallRequest struct {
	PriceChangeMoneyLimits   []mcpMoneyInput `json:"price_change_money_limits"`
	PriceChangeMaxCalls      int64           `json:"price_change_max_calls"`
	PriceChangeWindowSeconds int64           `json:"price_change_window_seconds"`
}

// mcpAgentPolicyRulesFor builds one ToolRule per MCP tool the account's own
// permission list already grants. It never accepts risk/approval as input —
// those come only from mcpToolCatalog — so a caller can widen what an agent
// is allowed to spend on the one write tool, never what class of action it
// performs.
func mcpAgentPolicyRulesFor(account mcpaccounts.Account, input mcpAgentPolicyInstallRequest) []agentgovernance.ToolRule {
	granted := make(map[string]bool, len(account.Permissions))
	for _, permission := range account.Permissions {
		granted[permission] = true
	}
	rules := make([]agentgovernance.ToolRule, 0, len(mcpToolCatalog))
	for _, entry := range mcpToolCatalog {
		if !granted[entry.Permission] {
			continue
		}
		rule := agentgovernance.ToolRule{Tool: entry.Tool, Permission: entry.Permission, Risk: entry.Risk, ApprovalRequired: entry.ApprovalRequired}
		if entry.Tool == "commerce.price.change.request" {
			money := make([]agentgovernance.Money, 0, len(input.PriceChangeMoneyLimits))
			for _, limit := range input.PriceChangeMoneyLimits {
				money = append(money, agentgovernance.Money{Currency: limit.Currency, MinorUnits: limit.MaxMinorUnits})
			}
			rule.Limits = agentgovernance.ActionLimits{Money: money, MaxCalls: input.PriceChangeMaxCalls, WindowSeconds: input.PriceChangeWindowSeconds}
		}
		rules = append(rules, rule)
	}
	return rules
}

func (api *mcpAgentPoliciesAPI) installPolicy(w http.ResponseWriter, r *http.Request, scope tenancy.Scope, account mcpaccounts.Account) {
	principal, principalOK := PrincipalFromContext(r.Context())
	if !principalOK || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input mcpAgentPolicyInstallRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	rules := mcpAgentPolicyRulesFor(account, input)
	if len(rules) == 0 {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	policyID := stableID("aip_", 40, scope, account.AgentID+"\x00"+account.IntegrationID)
	latest, err := api.policies.LatestPolicyVersion(r.Context(), scope, policyID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	now := time.Now().UTC()
	policy := agentgovernance.Policy{ID: policyID, Version: latest + 1, AgentID: account.AgentID, IntegrationID: account.IntegrationID, Rules: rules, EffectiveFrom: now}
	change := agentgovernance.Change{ActorID: boundedActorRef(principal.Subject), Reason: "settings.mcp_accounts:install-policy", OccurredAt: now}
	if err := api.policies.InstallPolicy(r.Context(), scope, policy, change); err != nil {
		if errors.Is(err, agentgovernance.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	_, _ = api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "mcp_agent_policy.install", ResourceType: "mcp_agent_policy",
		ResourceID: policyID, CorrelationID: newApprovalID(), Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"account_id": account.ID, "agent_id": account.AgentID, "integration_id": account.IntegrationID, "version": policy.Version, "tool_count": len(rules)},
	})
	writeJSON(w, http.StatusOK, mcpAgentPolicyToView(policy))
}

func (api *mcpAgentPoliciesAPI) getKillSwitch(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	if !ok || api == nil || api.kills == nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	state, err := api.kills.AgentKillState(r.Context(), scope, mcpTenantScopeProbeAgent)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"disabled": state.TenantDisabled, "version": state.Version})
}

type mcpAgentKillSwitchRequest struct {
	Disabled bool   `json:"disabled"`
	Reason   string `json:"reason"`
}

func (api *mcpAgentPoliciesAPI) setKillSwitch(w http.ResponseWriter, r *http.Request) {
	scope, ok := ScopeFromContext(r.Context())
	principal, principalOK := PrincipalFromContext(r.Context())
	if !ok || !principalOK || api == nil || api.kills == nil || api.audit == nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	var input mcpAgentKillSwitchRequest
	if !decodeCatalogJSON(w, r, &input) {
		return
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		writeProblem(w, http.StatusBadRequest, "Bad Request")
		return
	}
	state, err := api.kills.AgentKillState(r.Context(), scope, mcpTenantScopeProbeAgent)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	now := time.Now().UTC()
	change := agentgovernance.KillChange{
		Scope: agentgovernance.KillTenant, SubjectID: "*", Version: state.Version + 1, Disabled: input.Disabled,
		Change: agentgovernance.Change{ActorID: boundedActorRef(principal.Subject), Reason: reason, OccurredAt: now},
	}
	if err := api.kills.RecordKillSwitch(r.Context(), scope, change); err != nil {
		if errors.Is(err, agentgovernance.ErrInvalid) {
			writeProblem(w, http.StatusBadRequest, "Bad Request")
			return
		}
		writeProblem(w, http.StatusConflict, "Conflict")
		return
	}
	_, _ = api.audit.Capture(r.Context(), scope, audit.Entry{
		ActorID: principal.Subject, Source: "api", Action: "mcp_agent_kill_switch.set", ResourceType: "mcp_agent_kill_switch",
		ResourceID: "tenant", CorrelationID: newApprovalID(), Risk: audit.RiskWriteSensitive,
		Summary: audit.Summary{"disabled": input.Disabled, "reason": reason, "version": change.Version},
	})
	writeJSON(w, http.StatusOK, map[string]any{"disabled": input.Disabled, "version": change.Version})
}
