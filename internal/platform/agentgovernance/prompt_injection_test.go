package agentgovernance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type promptFixture struct {
	SchemaVersion int `json:"schema_version"`
	Cases         []struct {
		ID               string `json:"id"`
		ExternalText     string `json:"external_text"`
		RequestedTool    string `json:"requested_tool"`
		Permission       string `json:"permission"`
		Risk             Risk   `json:"risk"`
		ApprovalBoundary bool   `json:"approval_boundary"`
		Money            *Money `json:"money,omitempty"`
		Expected         string `json:"expected"`
	} `json:"cases"`
}

func TestPromptInjectionRegressionCorpus(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "contracts", "ai", "prompt-injection-regressions-v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture promptFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SchemaVersion != 1 || len(fixture.Cases) < 5 {
		t.Fatalf("fixture=%+v", fixture)
	}

	at := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	policy := testPolicy(at)
	policy.Rules = append(policy.Rules,
		ToolRule{Tool: "review.reply", Permission: "review.reply", Risk: RiskSafeWrite, Limits: ActionLimits{MaxCalls: 5, WindowSeconds: 60}},
		ToolRule{Tool: "signing.private_key.export", Permission: "signing.private_key.export", Risk: RiskProhibited},
	)
	limiter := &limiterStub{allowed: true}
	service, err := newService(policyStub{policy: policy}, killStub{state: KillState{Version: 1}}, limiter, fixedClock{at})
	if err != nil {
		t.Fatal(err)
	}

	for index, tc := range fixture.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			if tc.ExternalText == "" {
				t.Fatal("fixture must contain hostile external text")
			}
			request := Request{Agent: testAgent(), Tool: tc.RequestedTool, Permission: tc.Permission, Risk: tc.Risk, Trust: TrustUntrustedExternal, ApprovalBoundary: tc.ApprovalBoundary, CorrelationID: "fixture:" + tc.ID, InvocationID: "fixture-" + string(rune('a'+index)), At: at}
			request.Metrics.Money = tc.Money
			decision, err := service.AuthorizeCall(context.Background(), testScope(t), request)
			switch tc.Expected {
			case "allowed_read":
				if err != nil || decision.Risk != RiskRead {
					t.Fatalf("decision=%+v err=%v", decision, err)
				}
			case "allowed_approval_only":
				if err != nil || !decision.ApprovalRequired || decision.Risk != RiskSensitiveWrite {
					t.Fatalf("decision=%+v err=%v", decision, err)
				}
			case "denied":
				if err == nil || (!errors.Is(err, ErrDenied) && !errors.Is(err, ErrRateLimit)) {
					t.Fatalf("decision=%+v err=%v", decision, err)
				}
			default:
				t.Fatalf("unknown expected=%q", tc.Expected)
			}
		})
	}
}
