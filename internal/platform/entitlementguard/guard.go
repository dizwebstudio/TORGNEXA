// Package entitlementguard provides a fail-closed host-side feature/quota gate.
package entitlementguard

import (
	"context"
	"errors"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
)

var ErrDenied = errors.New("entitlement guard: denied")

type FeatureEvaluator interface {
	Evaluate(context.Context, entitlements.Scope, entitlements.FeatureKey, time.Time) (entitlements.Evaluation, error)
}
type QuotaConsumer interface {
	Consume(context.Context, entitlements.Scope, entitlements.Consumption) (entitlements.QuotaStatus, error)
}

type Requirement struct {
	Feature       entitlements.FeatureKey
	Metric        entitlements.MetricKey
	Amount        int64
	UsageID       string
	CorrelationID string
	At            time.Time
}

func (r Requirement) Validate() error {
	if !r.Feature.Valid() || r.At.IsZero() || !r.At.Equal(r.At.UTC()) {
		return entitlements.ErrInvalid
	}
	if r.Metric.Valid() {
		c := entitlements.Consumption{ID: r.UsageID, Metric: r.Metric, Amount: r.Amount, CorrelationID: r.CorrelationID, OccurredAt: r.At}
		return c.Validate()
	}
	if r.Metric != "" || r.Amount != 0 || r.UsageID != "" || r.CorrelationID != "" {
		return entitlements.ErrInvalid
	}
	return nil
}

type Decision struct {
	Feature entitlements.Evaluation
	Quota   *entitlements.QuotaStatus
}

type Guard struct {
	features FeatureEvaluator
	quotas   QuotaConsumer
}

func New(f FeatureEvaluator, q QuotaConsumer) (*Guard, error) {
	if f == nil {
		return nil, entitlements.ErrInvalid
	}
	return &Guard{features: f, quotas: q}, nil
}
func (g *Guard) Authorize(ctx context.Context, scope entitlements.Scope, req Requirement) (Decision, error) {
	if g == nil || ctx == nil || !scope.Valid() || req.Validate() != nil {
		return Decision{}, ErrDenied
	}
	feature, err := g.features.Evaluate(ctx, scope, req.Feature, req.At)
	if err != nil || !feature.Allowed {
		return Decision{Feature: feature}, ErrDenied
	}
	out := Decision{Feature: feature}
	if req.Metric.Valid() {
		if g.quotas == nil {
			return out, ErrDenied
		}
		status, err := g.quotas.Consume(ctx, scope, entitlements.Consumption{ID: req.UsageID, Metric: req.Metric, Amount: req.Amount, CorrelationID: req.CorrelationID, OccurredAt: req.At})
		if err != nil {
			return out, ErrDenied
		}
		out.Quota = &status
	}
	return out, nil
}
