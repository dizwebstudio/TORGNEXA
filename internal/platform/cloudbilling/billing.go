// Package cloudbilling implements TORGNEXA Cloud commercial lifecycle separately from commerce payments.
package cloudbilling

import (
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"sort"
	"sync"
	"time"
)

var ErrInvalid = errors.New("cloudbilling: invalid value")
var ErrCommunityBypass = errors.New("cloudbilling: community bypass")

type Mode string

const (
	ModeCommunity Mode = "community"
	ModeCloud     Mode = "cloud"
)

type PlanVersion struct {
	PlanID       string
	Version      int64
	Name         string
	MonthlyPrice domain.Money
	Entitlements map[string]int64
	EffectiveAt  time.Time
}

func (p PlanVersion) Validate() error {
	if p.PlanID == "" || p.Version < 1 || p.Name == "" || p.MonthlyPrice.Validate() != nil || p.EffectiveAt.IsZero() {
		return ErrInvalid
	}
	return nil
}

type SubscriptionState string

const (
	Trial     SubscriptionState = "trial"
	Active    SubscriptionState = "active"
	PastDue   SubscriptionState = "past_due"
	Grace     SubscriptionState = "grace"
	Suspended SubscriptionState = "suspended"
	Cancelled SubscriptionState = "cancelled"
)

func (s SubscriptionState) Valid() bool {
	return s == Trial || s == Active || s == PastDue || s == Grace || s == Suspended || s == Cancelled
}

type Subscription struct {
	ID, PlanID                                      string
	PlanVersion                                     int64
	State                                           SubscriptionState
	CurrentPeriodStart, CurrentPeriodEnd, UpdatedAt time.Time
	Version                                         int64
}
type UsageRecord struct {
	ID, Meter, SourceEventID string
	Quantity                 int64
	OccurredAt               time.Time
}
type Invoice struct {
	ID, SubscriptionID     string
	PeriodStart, PeriodEnd time.Time
	Amount                 domain.Money
	State                  string
	ProviderPaymentRef     string
	Version                int64
}
type Adjustment struct {
	ID, InvoiceID, Reason string
	Amount                domain.Money
	CreatedAt             time.Time
}
type EntitlementSink interface {
	Sync(tenancy.Scope, string, map[string]int64, int64) error
}
type Store struct {
	mu          sync.Mutex
	subs        map[string]map[string]Subscription
	usage       map[string]map[string]UsageRecord
	invoices    map[string]map[string]Invoice
	adjustments map[string]map[string]Adjustment
}

func NewStore() *Store {
	return &Store{subs: map[string]map[string]Subscription{}, usage: map[string]map[string]UsageRecord{}, invoices: map[string]map[string]Invoice{}, adjustments: map[string]map[string]Adjustment{}}
}
func key(s tenancy.Scope) string { return s.OrganizationID().String() + "/" + s.WorkspaceID().String() }
func (s *Store) RecordUsage(scope tenancy.Scope, u UsageRecord) error {
	if !scope.Valid() || u.ID == "" || u.Meter == "" || u.SourceEventID == "" || u.Quantity <= 0 || u.OccurredAt.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(scope)
	if s.usage[k] == nil {
		s.usage[k] = map[string]UsageRecord{}
	}
	if old, ok := s.usage[k][u.SourceEventID]; ok {
		if old.ID != u.ID || old.Quantity != u.Quantity {
			return ErrInvalid
		}
		return nil
	}
	s.usage[k][u.SourceEventID] = u
	return nil
}
func (s *Store) PutSubscription(scope tenancy.Scope, sub Subscription) error {
	if !scope.Valid() || sub.ID == "" || sub.PlanID == "" || sub.PlanVersion < 1 || !sub.State.Valid() || sub.Version < 1 || sub.CurrentPeriodEnd.Before(sub.CurrentPeriodStart) {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(scope)
	if s.subs[k] == nil {
		s.subs[k] = map[string]Subscription{}
	}
	if old, ok := s.subs[k][sub.ID]; ok && old.Version >= sub.Version {
		return ErrInvalid
	}
	s.subs[k][sub.ID] = sub
	return nil
}
func NextState(current SubscriptionState, paymentSucceeded bool, graceExpired bool) SubscriptionState {
	switch current {
	case Trial:
		if paymentSucceeded {
			return Active
		}
		return PastDue
	case Active:
		if paymentSucceeded {
			return Active
		}
		return PastDue
	case PastDue:
		return Grace
	case Grace:
		if paymentSucceeded {
			return Active
		}
		if graceExpired {
			return Suspended
		}
		return Grace
	case Suspended:
		if paymentSucceeded {
			return Active
		}
		return Suspended
	default:
		return current
	}
}
func BuildInvoice(id string, sub Subscription, usage []UsageRecord, base domain.Money, perUnit int64) (Invoice, error) {
	if id == "" || base.Validate() != nil || perUnit < 0 {
		return Invoice{}, ErrInvalid
	}
	total := base.MinorUnits()
	sort.Slice(usage, func(i, j int) bool { return usage[i].ID < usage[j].ID })
	for _, u := range usage {
		if u.Quantity < 0 {
			return Invoice{}, ErrInvalid
		}
		if u.Quantity > 0 && perUnit > (1<<62)/u.Quantity {
			return Invoice{}, ErrInvalid
		}
		total += u.Quantity * perUnit
	}
	m, err := domain.NewMoney(total, base.Currency())
	if err != nil {
		return Invoice{}, ErrInvalid
	}
	return Invoice{ID: id, SubscriptionID: sub.ID, PeriodStart: sub.CurrentPeriodStart, PeriodEnd: sub.CurrentPeriodEnd, Amount: m, State: "issued", Version: 1}, nil
}
func RequireBilling(mode Mode) error {
	if mode == ModeCommunity {
		return ErrCommunityBypass
	}
	if mode != ModeCloud {
		return ErrInvalid
	}
	return nil
}

type PaymentObservation struct {
	Reference  string
	Succeeded  bool
	ObservedAt time.Time
}

// ApplyPaymentObservation reconciles a cloud invoice with an explicit payment reference.
// It never creates a commerce payment or mutates usage facts.
func ApplyPaymentObservation(invoice Invoice, observation PaymentObservation) (Invoice, error) {
	if invoice.ID == "" || invoice.Version < 1 || observation.Reference == "" || observation.ObservedAt.IsZero() {
		return Invoice{}, ErrInvalid
	}
	currentReference := invoice.ProviderPaymentRef
	if currentReference != "" && currentReference != observation.Reference {
		return Invoice{}, ErrInvalid
	}
	invoice.ProviderPaymentRef = observation.Reference
	if observation.Succeeded {
		invoice.State = "paid"
	} else {
		invoice.State = "payment_failed"
	}
	invoice.Version++
	return invoice, nil
}

func SyncEntitlements(scope tenancy.Scope, subscription Subscription, plan PlanVersion, sink EntitlementSink) error {
	if !scope.Valid() || sink == nil || plan.Validate() != nil || subscription.ID == "" || subscription.PlanID != plan.PlanID || subscription.PlanVersion != plan.Version || !subscription.State.Valid() {
		return ErrInvalid
	}
	if subscription.State == Suspended || subscription.State == Cancelled {
		return sink.Sync(scope, subscription.ID, map[string]int64{}, plan.Version)
	}
	copyEntitlements := make(map[string]int64, len(plan.Entitlements))
	for key, value := range plan.Entitlements {
		if key == "" || value < 0 {
			return ErrInvalid
		}
		copyEntitlements[key] = value
	}
	return sink.Sync(scope, subscription.ID, copyEntitlements, plan.Version)
}

func (s *Store) RecordAdjustment(scope tenancy.Scope, adjustment Adjustment) error {
	if s == nil || !scope.Valid() || adjustment.ID == "" || adjustment.InvoiceID == "" || adjustment.Reason == "" || adjustment.Amount.Validate() != nil || adjustment.CreatedAt.IsZero() {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(scope)
	if s.adjustments[k] == nil {
		s.adjustments[k] = map[string]Adjustment{}
	}
	if old, ok := s.adjustments[k][adjustment.ID]; ok {
		if old.InvoiceID != adjustment.InvoiceID || old.Reason != adjustment.Reason || old.Amount.MinorUnits() != adjustment.Amount.MinorUnits() || old.Amount.Currency() != adjustment.Amount.Currency() || !old.CreatedAt.Equal(adjustment.CreatedAt) {
			return ErrInvalid
		}
		return nil
	}
	s.adjustments[k][adjustment.ID] = adjustment
	return nil
}
