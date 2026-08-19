package complianceguard

import (
	"context"
	"github.com/torgnexa/torgnexa/internal/core/compliance"
	"github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"testing"
	"time"
)

type eval compliance.Evaluation

func (e eval) Evaluate(context.Context, compliance.Scope, compliance.EvaluationContext) (compliance.Evaluation, error) {
	return compliance.Evaluation(e), nil
}

type resolver struct{ c compliance.EvaluationContext }

func (r resolver) Resolve(context.Context, connectorsandbox.Operation) (compliance.EvaluationContext, error) {
	return r.c, nil
}

type approval bool

func (a approval) Authorize(context.Context, compliance.Evaluation, connectorsandbox.Operation) error {
	if a {
		return nil
	}
	return ErrDenied
}
func TestGuard(t *testing.T) {
	s, _ := compliance.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	c := compliance.EvaluationContext{Operation: compliance.OperationPublication, Jurisdiction: "RU", ProductID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", At: time.Date(2026, 8, 10, 1, 0, 0, 0, time.UTC)}
	op := connectorsandbox.Operation{RequestID: "req-1", ExtensionID: "shop", ExtensionVersion: "1.0.0", Capability: "products.write", ResourceType: "product", ResourceID: "product-1"}
	g, _ := New(eval(compliance.Evaluation{Outcome: compliance.OutcomeBlock}), s, resolver{c}, nil)
	if g.Authorize(context.Background(), op) == nil {
		t.Fatal("block bypassed")
	}
	g, _ = New(eval(compliance.Evaluation{Outcome: compliance.OutcomeApproval}), s, resolver{c}, approval(true))
	if e := g.Authorize(context.Background(), op); e != nil {
		t.Fatalf("approved denied %v", e)
	}
}
