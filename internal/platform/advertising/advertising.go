// Package advertising defines provider-neutral campaign planning and guarded execution.
package advertising

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"time"
)

var (
	ErrInvalid          = errors.New("advertising: invalid value")
	ErrBudgetExceeded   = errors.New("advertising: budget/action limit exceeded")
	ErrApprovalRequired = errors.New("advertising: approval required")
)

type Status string

const (
	StatusDraft  Status = "draft"
	StatusActive Status = "active"
	StatusPaused Status = "paused"
	StatusEnded  Status = "ended"
)

func (s Status) Valid() bool {
	return s == StatusDraft || s == StatusActive || s == StatusPaused || s == StatusEnded
}

type Attribution struct{ Source, Medium, Campaign string }

func (a Attribution) Validate() error {
	if a.Source == "" || a.Medium == "" || a.Campaign == "" {
		return ErrInvalid
	}
	return nil
}

type Campaign struct {
	ID, Name                 string
	Status                   Status
	DailyBudget, TotalBudget domain.Money
	Attribution              Attribution
	Version                  int64
}

func (c Campaign) Validate() error {
	if c.ID == "" || c.Name == "" || !c.Status.Valid() || c.Version < 1 || c.DailyBudget.Validate() != nil || c.TotalBudget.Validate() != nil || c.DailyBudget.Currency() != c.TotalBudget.Currency() || c.DailyBudget.MinorUnits() < 0 || c.TotalBudget.MinorUnits() < c.DailyBudget.MinorUnits() || c.Attribution.Validate() != nil {
		return ErrInvalid
	}
	return nil
}

type AdGroup struct {
	ID, CampaignID string
	Bid            domain.Money
	DailyBudget    domain.Money
}
type Creative struct{ ID, AdGroupID, AssetRef, Text string }
type Action struct {
	Campaign       Campaign
	AdGroups       []AdGroup
	Creatives      []Creative
	RequestedSpend domain.Money
	DryRun         bool
	ApprovalRef    string
}
type Limits struct {
	MaxActionSpend            domain.Money
	MaxAdGroups, MaxCreatives int
	ApprovalThreshold         domain.Money
}
type Plan struct {
	AffectedAdGroups, AffectedCreatives int
	Spend                               domain.Money
	ApprovalRequired, DryRun            bool
	Attribution                         Attribution
}

func Preview(a Action, l Limits) (Plan, error) {
	if a.Campaign.Validate() != nil || a.RequestedSpend.Validate() != nil || l.MaxActionSpend.Validate() != nil || l.ApprovalThreshold.Validate() != nil || a.RequestedSpend.Currency() != a.Campaign.TotalBudget.Currency() || a.RequestedSpend.Currency() != l.MaxActionSpend.Currency() || a.RequestedSpend.Currency() != l.ApprovalThreshold.Currency() || a.RequestedSpend.MinorUnits() < 0 || l.MaxAdGroups < 1 || l.MaxCreatives < 1 || len(a.AdGroups) > l.MaxAdGroups || len(a.Creatives) > l.MaxCreatives {
		return Plan{}, ErrInvalid
	}
	if a.RequestedSpend.MinorUnits() > a.Campaign.DailyBudget.MinorUnits() || a.RequestedSpend.MinorUnits() > a.Campaign.TotalBudget.MinorUnits() || a.RequestedSpend.MinorUnits() > l.MaxActionSpend.MinorUnits() {
		return Plan{}, ErrBudgetExceeded
	}
	for _, g := range a.AdGroups {
		if g.ID == "" || g.CampaignID != a.Campaign.ID || g.Bid.Validate() != nil || g.DailyBudget.Validate() != nil || g.Bid.Currency() != a.RequestedSpend.Currency() || g.DailyBudget.Currency() != a.RequestedSpend.Currency() {
			return Plan{}, ErrInvalid
		}
	}
	for _, c := range a.Creatives {
		if c.ID == "" || c.AdGroupID == "" || c.AssetRef == "" {
			return Plan{}, ErrInvalid
		}
	}
	return Plan{len(a.AdGroups), len(a.Creatives), a.RequestedSpend, a.RequestedSpend.MinorUnits() >= l.ApprovalThreshold.MinorUnits(), a.DryRun, a.Campaign.Attribution}, nil
}

type Executor interface {
	Apply(context.Context, tenancy.Scope, Action) error
}
type Engine struct {
	Executor Executor
	Limits   Limits
}

func (e Engine) Execute(ctx context.Context, s tenancy.Scope, a Action) (Plan, error) {
	if !s.Valid() {
		return Plan{}, ErrInvalid
	}
	p, err := Preview(a, e.Limits)
	if err != nil {
		return Plan{}, err
	}
	if p.ApprovalRequired && a.ApprovalRef == "" {
		return p, ErrApprovalRequired
	}
	if a.DryRun {
		return p, nil
	}
	if e.Executor == nil {
		return Plan{}, ErrInvalid
	}
	if err := e.Executor.Apply(ctx, s, a); err != nil {
		return Plan{}, err
	}
	return p, nil
}

var _ = time.UTC
