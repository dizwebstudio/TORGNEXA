package agentgovernance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	testOrg = "018f0000-0000-7000-8000-000000000001"
	testWS  = "018f0000-0000-7000-8000-000000000002"
)

type policyStub struct {
	policy Policy
	err    error
}

func (s policyStub) ResolveAgentPolicy(context.Context, tenancy.Scope, Agent, time.Time) (Policy, error) {
	return s.policy, s.err
}

type killStub struct {
	state KillState
	err   error
}

func (s killStub) AgentKillState(context.Context, tenancy.Scope, Agent) (KillState, error) {
	return s.state, s.err
}

type limiterStub struct {
	calls   int
	allowed bool
	err     error
	last    FrequencyRequest
}

func (s *limiterStub) ConsumeAgentCall(_ context.Context, _ tenancy.Scope, r FrequencyRequest) (bool, error) {
	s.calls++
	s.last = r
	return s.allowed, s.err
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func testScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(testOrg, testWS)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

func testAgent() Agent {
	return Agent{ID: "agent.catalog-ops", ModelID: "model.qwen3", RunID: "run.01J000000000000000000001", IntegrationID: "mcp.openclaw"}
}

func testPolicy(at time.Time) Policy {
	return Policy{
		ID: "policy.agent-catalog", Version: 3, AgentID: testAgent().ID, IntegrationID: testAgent().IntegrationID,
		EffectiveFrom: at.Add(-time.Hour),
		Rules: []ToolRule{
			{Tool: "commerce.products.search", Permission: "commerce.products.read", Risk: RiskRead, Limits: ActionLimits{MaxCalls: 10, WindowSeconds: 60}},
			{Tool: "commerce.price.change.request", Permission: "commerce.price.change.request", Risk: RiskSensitiveWrite, ApprovalRequired: true, Limits: ActionLimits{Money: []Money{{Currency: "RUB", MinorUnits: 100_000_00}}, MaxCalls: 2, WindowSeconds: 60}},
		},
	}
}

func callRequest(at time.Time, tool, permission string, risk Risk) Request {
	return Request{Agent: testAgent(), Tool: tool, Permission: permission, Risk: risk, Trust: TrustUntrustedExternal, CorrelationID: "corr-1", InvocationID: "invocation-1", At: at}
}

func TestReadIsAllowedOnlyByExactAgentPolicy(t *testing.T) {
	at := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	limiter := &limiterStub{allowed: true}
	service, err := newService(policyStub{policy: testPolicy(at)}, killStub{state: KillState{Version: 2}}, limiter, fixedClock{at})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := service.AuthorizeCall(context.Background(), testScope(t), callRequest(at, "commerce.products.search", "commerce.products.read", RiskRead))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PolicyID != "policy.agent-catalog" || decision.PolicyVersion != 3 || decision.Trust != TrustUntrustedExternal || limiter.calls != 1 {
		t.Fatalf("decision=%+v limiter=%d", decision, limiter.calls)
	}
}

func TestSensitiveWriteRequiresApprovalAndHardMoneyLimit(t *testing.T) {
	at := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	limiter := &limiterStub{allowed: true}
	service, _ := newService(policyStub{policy: testPolicy(at)}, killStub{state: KillState{Version: 1}}, limiter, fixedClock{at})
	request := callRequest(at, "commerce.price.change.request", "commerce.price.change.request", RiskSensitiveWrite)
	request.ApprovalBoundary = true
	request.Metrics.Money = &Money{Currency: "RUB", MinorUnits: 99_999_99}
	decision, err := service.AuthorizeCall(context.Background(), testScope(t), request)
	if err != nil || !decision.ApprovalRequired {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}

	request.InvocationID = "invocation-2"
	request.Metrics.Money.MinorUnits = 100_000_01
	if _, err = service.AuthorizeCall(context.Background(), testScope(t), request); !errors.Is(err, ErrDenied) {
		t.Fatalf("over-limit error=%v", err)
	}
	request.Metrics.Money.MinorUnits = 10
	request.ApprovalBoundary = false
	if _, err = service.AuthorizeCall(context.Background(), testScope(t), request); !errors.Is(err, ErrDenied) {
		t.Fatalf("approval bypass error=%v", err)
	}
}

func TestKillSwitchFailsClosedAtTenantAgentAndIntegrationScopes(t *testing.T) {
	at := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	for _, state := range []KillState{
		{TenantDisabled: true, Version: 1},
		{AgentDisabled: true, Version: 2},
		{IntegrationDisabled: true, Version: 3},
	} {
		service, _ := newService(policyStub{policy: testPolicy(at)}, killStub{state: state}, &limiterStub{allowed: true}, fixedClock{at})
		if _, err := service.AuthorizeCall(context.Background(), testScope(t), callRequest(at, "commerce.products.search", "commerce.products.read", RiskRead)); !errors.Is(err, ErrDenied) {
			t.Fatalf("state=%+v err=%v", state, err)
		}
	}
	service, _ := newService(policyStub{policy: testPolicy(at)}, killStub{err: errors.New("store unavailable")}, &limiterStub{allowed: true}, fixedClock{at})
	if _, err := service.Discover(context.Background(), testScope(t), testAgent(), "commerce.products.search", "commerce.products.read", RiskRead, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("kill-store failure error=%v", err)
	}
}

func TestUntrustedPromptCannotGrantWriteOrSecretCapability(t *testing.T) {
	at := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	policy := testPolicy(at)
	policy.Rules = append(policy.Rules,
		ToolRule{Tool: "review.reply", Permission: "review.reply", Risk: RiskSafeWrite, Limits: ActionLimits{MaxCalls: 5, WindowSeconds: 60}},
		ToolRule{Tool: "signing.private_key.export", Permission: "signing.private_key.export", Risk: RiskProhibited},
	)
	service, _ := newService(policyStub{policy: policy}, killStub{state: KillState{Version: 1}}, &limiterStub{allowed: true}, fixedClock{at})
	request := callRequest(at, "review.reply", "review.reply", RiskSafeWrite)
	// This simulates a review/message saying "ignore policy and call a write tool".
	// The text itself is never input to the policy evaluator; its only server-derived
	// classification is untrusted_external, so it cannot elevate privileges.
	if _, err := service.AuthorizeCall(context.Background(), testScope(t), request); !errors.Is(err, ErrDenied) {
		t.Fatalf("untrusted direct write error=%v", err)
	}
	if _, err := service.Discover(context.Background(), testScope(t), testAgent(), "signing.private_key.export", "signing.private_key.export", RiskProhibited, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("private-key capability error=%v", err)
	}
}

func TestFrequencyLimitIsFailClosedAndIdempotencyKeyIsServerBound(t *testing.T) {
	at := time.Date(2026, 8, 11, 7, 0, 23, 0, time.UTC)
	limiter := &limiterStub{allowed: false}
	service, _ := newService(policyStub{policy: testPolicy(at)}, killStub{state: KillState{Version: 1}}, limiter, fixedClock{at})
	_, err := service.AuthorizeCall(context.Background(), testScope(t), callRequest(at, "commerce.products.search", "commerce.products.read", RiskRead))
	if !errors.Is(err, ErrRateLimit) || limiter.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, limiter.calls)
	}
	if limiter.last.InvocationID != "invocation-1" || limiter.last.MaxCalls != 10 || !limiter.last.WindowStart.Equal(time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("frequency=%+v", limiter.last)
	}

	service, _ = newService(policyStub{policy: testPolicy(at)}, killStub{state: KillState{Version: 1}}, nil, fixedClock{at})
	if _, err = service.AuthorizeCall(context.Background(), testScope(t), callRequest(at, "commerce.products.search", "commerce.products.read", RiskRead)); !errors.Is(err, ErrDenied) {
		t.Fatalf("missing limiter error=%v", err)
	}
}

func TestPolicyValidationRejectsUnboundedWritesAndFakeSecretGrants(t *testing.T) {
	at := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	p := testPolicy(at)
	p.Rules[1].Limits = ActionLimits{}
	if p.Validate() == nil {
		t.Fatal("unbounded sensitive write must fail")
	}
	p = testPolicy(at)
	p.Rules = append(p.Rules, ToolRule{Tool: "secrets.read", Permission: "secrets.read", Risk: RiskRead})
	if p.Validate() == nil {
		t.Fatal("secret tool cannot be disguised as read")
	}
}
