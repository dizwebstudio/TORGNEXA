// Package complianceguard binds the product-compliance evaluator to host-side connector publication enforcement.
package complianceguard

import (
	"context"
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/compliance"
	"github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
)

var ErrDenied = errors.New("compliance guard: publication denied")

type Resolver interface {
	Resolve(context.Context, connectorsandbox.Operation) (compliance.EvaluationContext, error)
}
type ApprovalAuthorizer interface {
	Authorize(context.Context, compliance.Evaluation, connectorsandbox.Operation) error
}

type Guard struct {
	evaluator compliance.Evaluator
	scope     compliance.Scope
	resolver  Resolver
	approvals ApprovalAuthorizer
}

func New(e compliance.Evaluator, s compliance.Scope, r Resolver, a ApprovalAuthorizer) (*Guard, error) {
	if e == nil || !s.Valid() || r == nil {
		return nil, compliance.ErrInvalid
	}
	return &Guard{evaluator: e, scope: s, resolver: r, approvals: a}, nil
}
func (g *Guard) Authorize(ctx context.Context, op connectorsandbox.Operation) error {
	if g == nil || ctx == nil {
		return ErrDenied
	}
	c, e := g.resolver.Resolve(ctx, op)
	if e != nil {
		return ErrDenied
	}
	result, e := g.evaluator.Evaluate(ctx, g.scope, c)
	if e != nil {
		return ErrDenied
	}
	switch result.Outcome {
	case compliance.OutcomeAllow, compliance.OutcomeWarn:
		return nil
	case compliance.OutcomeApproval:
		if g.approvals != nil && g.approvals.Authorize(ctx, result, op) == nil {
			return nil
		}
		return ErrDenied
	default:
		return ErrDenied
	}
}
