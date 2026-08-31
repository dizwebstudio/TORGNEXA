// Package advertising contains provider-neutral advertising facts and
// deterministic marketplace performance calculations.
package advertising

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("advertising: invalid value")
	ErrConflict = errors.New("advertising: duplicate fact conflicts")
)

// Status is the normalized lifecycle state of an advertising object.
type Status string

const (
	StatusDraft    Status = "draft"
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusStopped  Status = "stopped"
	StatusArchived Status = "archived"
	StatusUnknown  Status = "unknown"
)

func (s Status) Valid() bool {
	return s == StatusDraft || s == StatusActive || s == StatusPaused || s == StatusStopped || s == StatusArchived || s == StatusUnknown
}

// Quality describes how much confidence can be placed in a remote fact.
type Quality string

const (
	QualityObserved  Quality = "observed"
	QualityConfirmed Quality = "confirmed"
	QualityEstimated Quality = "estimated"
	QualityPartial   Quality = "partial"
	QualityDelayed   Quality = "delayed"
	QualityUnknown   Quality = "unknown"
	QualityConflict  Quality = "conflict"
)

func (q Quality) Valid() bool {
	return q == QualityObserved || q == QualityConfirmed || q == QualityEstimated || q == QualityPartial || q == QualityDelayed || q == QualityUnknown || q == QualityConflict
}

// Campaign is the provider-neutral campaign projection. Remote IDs are kept
// as data and never used for provider branching in Core.
type Campaign struct {
	ID               string    `json:"id"`
	AccountID        string    `json:"account_id"`
	Channel          string    `json:"channel"`
	RemoteID         string    `json:"remote_id"`
	Name             string    `json:"name"`
	Status           Status    `json:"status"`
	Currency         string    `json:"currency"`
	DailyBudgetMinor int64     `json:"daily_budget_minor"`
	TotalBudgetMinor int64     `json:"total_budget_minor"`
	ObservedAt       time.Time `json:"observed_at"`
	EffectiveFrom    time.Time `json:"effective_from,omitempty"`
	EffectiveTo      time.Time `json:"effective_to,omitempty"`
	Version          int64     `json:"version"`
}

// AdGroup is a normalized campaign group.
type AdGroup struct {
	ID         string `json:"id"`
	CampaignID string `json:"campaign_id"`
	RemoteID   string `json:"remote_id"`
	Name       string `json:"name"`
	Status     Status `json:"status"`
}

// Ad is a normalized advertisement.
type Ad struct {
	ID        string `json:"id"`
	AdGroupID string `json:"ad_group_id"`
	RemoteID  string `json:"remote_id"`
	Name      string `json:"name"`
	Status    Status `json:"status"`
}

// CampaignProduct links an internal SKU to a remote advertising object.
type CampaignProduct struct {
	ID              string    `json:"id"`
	CampaignID      string    `json:"campaign_id"`
	SKU             string    `json:"sku"`
	RemoteProductID string    `json:"remote_product_id"`
	ObservedAt      time.Time `json:"observed_at"`
}

// SpendFact is an immutable normalized advertising cost fact.
type SpendFact struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"account_id"`
	Channel      string    `json:"channel"`
	CampaignID   string    `json:"campaign_id"`
	AdID         string    `json:"ad_id,omitempty"`
	SKU          string    `json:"sku,omitempty"`
	RemoteFactID string    `json:"remote_fact_id"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	AmountMinor  int64     `json:"amount_minor"`
	Currency     string    `json:"currency"`
	Source       string    `json:"source"`
	ObservedAt   time.Time `json:"observed_at"`
	EffectiveAt  time.Time `json:"effective_at"`
	Quality      Quality   `json:"quality"`
}

// PerformanceFact is an immutable normalized delivery/conversion fact.
type PerformanceFact struct {
	ID           string    `json:"id"`
	AccountID    string    `json:"account_id"`
	Channel      string    `json:"channel"`
	CampaignID   string    `json:"campaign_id"`
	AdID         string    `json:"ad_id,omitempty"`
	SKU          string    `json:"sku,omitempty"`
	RemoteFactID string    `json:"remote_fact_id"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	Impressions  int64     `json:"impressions"`
	Clicks       int64     `json:"clicks"`
	Orders       int64     `json:"orders"`
	RevenueMinor int64     `json:"revenue_minor"`
	Currency     string    `json:"currency"`
	Source       string    `json:"source"`
	ObservedAt   time.Time `json:"observed_at"`
	EffectiveAt  time.Time `json:"effective_at"`
	Quality      Quality   `json:"quality"`
}

// Attribution records the confidence of linking advertising delivery to
// orders/revenue. It is explicit so a missing attribution never becomes zero.
type Attribution struct {
	ID           string    `json:"id"`
	CampaignID   string    `json:"campaign_id"`
	SKU          string    `json:"sku,omitempty"`
	OrderID      string    `json:"order_id,omitempty"`
	Orders       int64     `json:"orders"`
	RevenueMinor int64     `json:"revenue_minor"`
	Currency     string    `json:"currency"`
	Source       string    `json:"source"`
	Confidence   Quality   `json:"confidence"`
	ObservedAt   time.Time `json:"observed_at"`
}

// Metric is a derived read model. Rates are integer basis points and therefore
// remain deterministic across Go, SQL and generated clients.
type Metric struct {
	Channel         string  `json:"channel"`
	CampaignID      string  `json:"campaign_id"`
	SKU             string  `json:"sku,omitempty"`
	Currency        string  `json:"currency"`
	SpendMinor      int64   `json:"spend_minor"`
	Impressions     int64   `json:"impressions"`
	Clicks          int64   `json:"clicks"`
	Orders          int64   `json:"orders"`
	RevenueMinor    int64   `json:"revenue_minor"`
	ROASBasisPoints int64   `json:"roas_basis_points"`
	ROMIBasisPoints int64   `json:"romi_basis_points"`
	DRRBasisPoints  int64   `json:"drr_basis_points"`
	OrderCostMinor  int64   `json:"order_cost_minor"`
	Quality         Quality `json:"quality"`
}

// Finding is a persisted reconciliation result, not a mutable status flag.
type Finding struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	CampaignID      string    `json:"campaign_id,omitempty"`
	RemoteReference string    `json:"remote_reference,omitempty"`
	ExpectedMinor   int64     `json:"expected_minor"`
	ActualMinor     int64     `json:"actual_minor"`
	Severity        string    `json:"severity"`
	Explanation     string    `json:"explanation"`
	ObservedAt      time.Time `json:"observed_at"`
}

func (c Campaign) Validate() error {
	if !validRef(c.ID) || !validRef(c.AccountID) || !validRef(c.Channel) || !validRef(c.RemoteID) || !validText(c.Name, 300) || !c.Status.Valid() || !validCurrency(c.Currency) || c.DailyBudgetMinor < 0 || c.TotalBudgetMinor < c.DailyBudgetMinor || c.ObservedAt.IsZero() || !utc(c.ObservedAt) || c.Version < 1 || (!c.EffectiveFrom.IsZero() && !utc(c.EffectiveFrom)) || (!c.EffectiveTo.IsZero() && !utc(c.EffectiveTo)) || (!c.EffectiveTo.IsZero() && !c.EffectiveFrom.IsZero() && !c.EffectiveTo.After(c.EffectiveFrom)) {
		return ErrInvalid
	}
	return nil
}

func (f SpendFact) Validate() error {
	return validateFact(f.ID, f.AccountID, f.Channel, f.CampaignID, f.RemoteFactID, f.PeriodStart, f.PeriodEnd, f.AmountMinor, f.Currency, f.Source, f.ObservedAt, f.EffectiveAt, f.Quality)
}

func (f PerformanceFact) Validate() error {
	if err := validateFact(f.ID, f.AccountID, f.Channel, f.CampaignID, f.RemoteFactID, f.PeriodStart, f.PeriodEnd, f.RevenueMinor, f.Currency, f.Source, f.ObservedAt, f.EffectiveAt, f.Quality); err != nil || f.Impressions < 0 || f.Clicks < 0 || f.Orders < 0 {
		return ErrInvalid
	}
	return nil
}

func validateFact(id, account, channel, campaign, remote string, start, end time.Time, amount int64, currency, source string, observed, effective time.Time, quality Quality) error {
	if !validRef(id) || !validRef(account) || !validRef(channel) || !validRef(campaign) || !validRef(remote) || start.IsZero() || end.IsZero() || !utc(start) || !utc(end) || !end.After(start) || amount < 0 || !validCurrency(currency) || !validRef(source) || observed.IsZero() || effective.IsZero() || !utc(observed) || !utc(effective) || !quality.Valid() {
		return ErrInvalid
	}
	return nil
}

// DeduplicateSpend keeps the first identical fact and rejects conflicting
// payloads sharing the same provider identity and period.
func DeduplicateSpend(facts []SpendFact) ([]SpendFact, error) {
	seen := make(map[string]SpendFact, len(facts))
	for _, fact := range facts {
		if err := fact.Validate(); err != nil {
			return nil, err
		}
		key := fact.AccountID + "\x00" + fact.RemoteFactID + "\x00" + fact.PeriodStart.Format(time.RFC3339Nano) + "\x00" + fact.PeriodEnd.Format(time.RFC3339Nano)
		if previous, ok := seen[key]; ok {
			if FingerprintSpend(previous) != FingerprintSpend(fact) {
				return nil, ErrConflict
			}
			continue
		}
		seen[key] = fact
	}
	result := make([]SpendFact, 0, len(seen))
	for _, fact := range seen {
		result = append(result, fact)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].PeriodStart.Before(result[j].PeriodStart) || (result[i].PeriodStart.Equal(result[j].PeriodStart) && result[i].ID < result[j].ID)
	})
	return result, nil
}

// FingerprintSpend creates a stable non-secret identity for deduplication and
// evidence. It intentionally excludes raw provider payloads.
func FingerprintSpend(f SpendFact) string {
	value := strings.Join([]string{f.AccountID, f.Channel, f.CampaignID, f.AdID, f.SKU, f.RemoteFactID, f.PeriodStart.Format(time.RFC3339Nano), f.PeriodEnd.Format(time.RFC3339Nano), big.NewInt(f.AmountMinor).String(), f.Currency, f.Source, string(f.Quality)}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Aggregate combines normalized spend and performance facts for one scope.
func Aggregate(spends []SpendFact, performance []PerformanceFact) ([]Metric, error) {
	type key struct{ channel, campaign, sku, currency string }
	values := map[key]*Metric{}
	quality := func(current Quality, next Quality) Quality {
		if current == "" {
			return next
		}
		if current == next {
			return current
		}
		return QualityPartial
	}
	for _, fact := range spends {
		if err := fact.Validate(); err != nil {
			return nil, err
		}
		k := key{fact.Channel, fact.CampaignID, fact.SKU, fact.Currency}
		m := values[k]
		if m == nil {
			m = &Metric{Channel: fact.Channel, CampaignID: fact.CampaignID, SKU: fact.SKU, Currency: fact.Currency}
			values[k] = m
		}
		m.SpendMinor += fact.AmountMinor
		m.Quality = quality(m.Quality, fact.Quality)
	}
	for _, fact := range performance {
		if err := fact.Validate(); err != nil {
			return nil, err
		}
		k := key{fact.Channel, fact.CampaignID, fact.SKU, fact.Currency}
		m := values[k]
		if m == nil {
			m = &Metric{Channel: fact.Channel, CampaignID: fact.CampaignID, SKU: fact.SKU, Currency: fact.Currency}
			values[k] = m
		}
		m.Impressions += fact.Impressions
		m.Clicks += fact.Clicks
		m.Orders += fact.Orders
		m.RevenueMinor += fact.RevenueMinor
		m.Quality = quality(m.Quality, fact.Quality)
	}
	result := make([]Metric, 0, len(values))
	for _, m := range values {
		if m.SpendMinor > 0 {
			m.ROASBasisPoints = ratio(m.RevenueMinor, m.SpendMinor)
			m.ROMIBasisPoints = ratio(m.RevenueMinor-m.SpendMinor, m.SpendMinor)
			if m.Orders > 0 {
				m.OrderCostMinor = m.SpendMinor / m.Orders
			}
		}
		if m.RevenueMinor > 0 {
			m.DRRBasisPoints = ratio(m.SpendMinor, m.RevenueMinor)
		}
		if m.Quality == "" {
			m.Quality = QualityUnknown
		}
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Channel != result[j].Channel {
			return result[i].Channel < result[j].Channel
		}
		if result[i].CampaignID != result[j].CampaignID {
			return result[i].CampaignID < result[j].CampaignID
		}
		return result[i].SKU < result[j].SKU
	})
	return result, nil
}

func ratio(numerator, denominator int64) int64 {
	if denominator <= 0 {
		return 0
	}
	n := new(big.Int).Mul(big.NewInt(numerator), big.NewInt(10000))
	n.Quo(n, big.NewInt(denominator))
	if !n.IsInt64() {
		return 0
	}
	return n.Int64()
}

func validRef(value string) bool {
	return value != "" && len(value) <= 192 && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}
func validText(value string, max int) bool {
	return value != "" && len(value) <= max && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\x00\r\n")
}
func validCurrency(value string) bool {
	return len(value) == 3 && value == strings.ToUpper(value) && value >= "A" && value <= "ZZZ"
}
func utc(value time.Time) bool { return value.Location() == time.UTC }
