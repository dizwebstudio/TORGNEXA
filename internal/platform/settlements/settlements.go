// Package settlements implements the append-oriented marketplace settlement ledger.
package settlements

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"io"
	"sync"
	"time"
)

var (
	ErrInvalid  = errors.New("settlements: invalid value")
	ErrConflict = errors.New("settlements: conflicting provider reference")
)

type Kind string

const (
	KindSale         Kind = "sale"
	KindFee          Kind = "fee"
	KindRefund       Kind = "refund"
	KindPayout       Kind = "payout"
	KindAdjustment   Kind = "adjustment"
	KindLogistics    Kind = "logistics"
	KindStorage      Kind = "storage"
	KindAdvertising  Kind = "advertising"
	KindPenalty      Kind = "penalty"
	KindCompensation Kind = "compensation"
	KindWithholding  Kind = "withholding"
)

func (k Kind) Valid() bool {
	return k == KindSale || k == KindFee || k == KindRefund || k == KindPayout || k == KindAdjustment || k == KindLogistics || k == KindStorage || k == KindAdvertising || k == KindPenalty || k == KindCompensation || k == KindWithholding
}

type Entry struct {
	ID, SourceSystem, SourceAccountID, SourceEntryRef, OrderID, AdjustsEntryID, FeeCode, FXRateRef string
	Kind                                                                                           Kind
	Amount                                                                                         domain.Money
	OccurredAt, ImportedAt                                                                         time.Time
	Disputed                                                                                       bool
}

func (e Entry) Validate() error {
	if e.ID == "" || e.SourceSystem == "" || e.SourceAccountID == "" || e.SourceEntryRef == "" || !e.Kind.Valid() || e.Amount.Validate() != nil || e.OccurredAt.IsZero() || e.ImportedAt.IsZero() || !e.OccurredAt.Equal(e.OccurredAt.UTC()) || !e.ImportedAt.Equal(e.ImportedAt.UTC()) {
		return ErrInvalid
	}
	if e.Kind == KindAdjustment && e.AdjustsEntryID == "" {
		return ErrInvalid
	}
	if e.Kind != KindAdjustment && e.AdjustsEntryID != "" {
		return ErrInvalid
	}
	return nil
}

type Store interface {
	Append(context.Context, tenancy.Scope, Entry) error
	List(context.Context, tenancy.Scope) []Entry
}
type MemoryStore struct {
	mu           sync.Mutex
	entries      map[string][]Entry
	providerRefs map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{entries: map[string][]Entry{}, providerRefs: map[string]string{}}
}
func scopeKey(s tenancy.Scope) string {
	return s.OrganizationID().String() + "/" + s.WorkspaceID().String()
}
func providerKey(s tenancy.Scope, e Entry) string {
	return scopeKey(s) + "\x00" + e.SourceSystem + "\x00" + e.SourceAccountID + "\x00" + e.SourceEntryRef
}
func (s *MemoryStore) Append(_ context.Context, scope tenancy.Scope, e Entry) error {
	if !scope.Valid() || e.Validate() != nil {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pk := providerKey(scope, e)
	if existing, ok := s.providerRefs[pk]; ok {
		if existing == e.ID {
			return nil
		}
		return ErrConflict
	}
	s.providerRefs[pk] = e.ID
	k := scopeKey(scope)
	s.entries[k] = append(s.entries[k], e)
	return nil
}
func (s *MemoryStore) List(_ context.Context, scope tenancy.Scope) []Entry {
	if !scope.Valid() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries[scopeKey(scope)]...)
}

type importEntry struct {
	ID              string    `json:"id"`
	SourceSystem    string    `json:"provider"`
	SourceAccountID string    `json:"provider_account_id"`
	SourceEntryRef  string    `json:"provider_entry_ref"`
	OrderID         string    `json:"order_id"`
	AdjustsEntryID  string    `json:"adjusts_entry_id"`
	FeeCode         string    `json:"fee_code"`
	FXRateRef       string    `json:"fx_rate_ref"`
	Kind            Kind      `json:"kind"`
	MinorUnits      int64     `json:"minor_units"`
	Currency        string    `json:"currency"`
	OccurredAt      time.Time `json:"occurred_at"`
	Disputed        bool      `json:"disputed"`
}

func ImportJSON(ctx context.Context, store Store, scope tenancy.Scope, data []byte, importedAt time.Time) (int, error) {
	if store == nil || !scope.Valid() || importedAt.IsZero() {
		return 0, ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var rows []importEntry
	if err := dec.Decode(&rows); err != nil {
		return 0, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return 0, ErrInvalid
	}
	count := 0
	for _, r := range rows {
		c, err := domain.NewCurrency(r.Currency)
		if err != nil {
			return count, ErrInvalid
		}
		m, err := domain.NewMoney(r.MinorUnits, c)
		if err != nil {
			return count, ErrInvalid
		}
		e := Entry{r.ID, r.SourceSystem, r.SourceAccountID, r.SourceEntryRef, r.OrderID, r.AdjustsEntryID, r.FeeCode, r.FXRateRef, r.Kind, m, r.OccurredAt.UTC(), importedAt.UTC(), r.Disputed}
		if err := store.Append(ctx, scope, e); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
