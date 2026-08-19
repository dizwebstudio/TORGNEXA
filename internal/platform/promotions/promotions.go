// Package promotions implements provider-neutral promotions and pricing guards.
package promotions

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalid          = errors.New("promotions: invalid value")
	ErrGuardViolation   = errors.New("promotions: pricing guard violation")
	ErrApprovalRequired = errors.New("promotions: approval required")
)

type PromotionKind string

const (
	KindDiscount PromotionKind = "discount"
	KindCoupon   PromotionKind = "coupon"
)

func (k PromotionKind) Valid() bool { return k == KindDiscount || k == KindCoupon }

type Promotion struct {
	ID, Name         string
	Kind             PromotionKind
	StartsAt, EndsAt time.Time
	Version          int64
}

func (p Promotion) Validate() error {
	if p.ID == "" || p.Name == "" || !p.Kind.Valid() || p.Version < 1 || p.StartsAt.IsZero() || p.EndsAt.IsZero() || !p.EndsAt.After(p.StartsAt) || !p.StartsAt.Equal(p.StartsAt.UTC()) || !p.EndsAt.Equal(p.EndsAt.UTC()) {
		return ErrInvalid
	}
	return nil
}

type GuardPolicy struct {
	ID                   string
	FloorPrice           domain.Money
	MinimumMarginBPS     int64 // 0..10000, margin = (price-cost)/price.
	MaxAffectedSKUs      int
	RequireApprovalAbove int
}

func (g GuardPolicy) Validate() error {
	if g.ID == "" || g.FloorPrice.Validate() != nil || g.FloorPrice.MinorUnits() < 0 || g.MinimumMarginBPS < 0 || g.MinimumMarginBPS > 10000 || g.MaxAffectedSKUs < 1 || g.RequireApprovalAbove < 0 || g.RequireApprovalAbove > g.MaxAffectedSKUs {
		return ErrInvalid
	}
	return nil
}

type Participation struct {
	PromotionID string
	SKU         string
	Proposed    domain.Money
	ApprovalRef string
	Version     int64
}

func (p Participation) Validate() error {
	if p.PromotionID == "" || p.SKU == "" || p.Proposed.Validate() != nil || p.Proposed.MinorUnits() < 0 || p.Version < 1 {
		return ErrInvalid
	}
	return nil
}

type Candidate struct {
	SKU                         string
	Current, Proposed, UnitCost domain.Money
}

type Violation struct{ SKU, Code string }
type Preview struct {
	AffectedSKUs     []string
	Violations       []Violation
	ApprovalRequired bool
}

func PreviewMassWrite(policy GuardPolicy, items []Candidate) (Preview, error) {
	if policy.Validate() != nil || len(items) == 0 || len(items) > policy.MaxAffectedSKUs {
		return Preview{}, ErrInvalid
	}
	out := Preview{AffectedSKUs: make([]string, 0, len(items))}
	seen := map[string]struct{}{}
	for _, it := range items {
		if it.SKU == "" || it.Current.Validate() != nil || it.Proposed.Validate() != nil || it.UnitCost.Validate() != nil || it.Current.Currency() != it.Proposed.Currency() || it.Proposed.Currency() != it.UnitCost.Currency() || it.Proposed.Currency() != policy.FloorPrice.Currency() || it.Proposed.MinorUnits() < 0 || it.UnitCost.MinorUnits() < 0 {
			return Preview{}, ErrInvalid
		}
		if _, ok := seen[it.SKU]; ok {
			return Preview{}, ErrInvalid
		}
		seen[it.SKU] = struct{}{}
		out.AffectedSKUs = append(out.AffectedSKUs, it.SKU)
		if it.Proposed.MinorUnits() < policy.FloorPrice.MinorUnits() {
			out.Violations = append(out.Violations, Violation{it.SKU, "floor_price"})
		}
		if it.Proposed.MinorUnits() == 0 || (it.Proposed.MinorUnits()-it.UnitCost.MinorUnits())*10000 < it.Proposed.MinorUnits()*policy.MinimumMarginBPS {
			out.Violations = append(out.Violations, Violation{it.SKU, "minimum_margin"})
		}
	}
	sort.Strings(out.AffectedSKUs)
	sort.Slice(out.Violations, func(i, j int) bool {
		if out.Violations[i].SKU == out.Violations[j].SKU {
			return out.Violations[i].Code < out.Violations[j].Code
		}
		return out.Violations[i].SKU < out.Violations[j].SKU
	})
	out.ApprovalRequired = policy.RequireApprovalAbove > 0 && len(items) >= policy.RequireApprovalAbove
	return out, nil
}

func AuthorizeMassWrite(scope tenancy.Scope, policy GuardPolicy, items []Candidate, approved bool) (Preview, error) {
	if !scope.Valid() {
		return Preview{}, ErrInvalid
	}
	p, err := PreviewMassWrite(policy, items)
	if err != nil {
		return Preview{}, err
	}
	if len(p.Violations) > 0 {
		return p, fmt.Errorf("%w: %d violation(s)", ErrGuardViolation, len(p.Violations))
	}
	if p.ApprovalRequired && !approved {
		return p, ErrApprovalRequired
	}
	return p, nil
}
