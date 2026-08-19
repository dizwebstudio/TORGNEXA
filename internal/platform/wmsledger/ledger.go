// Package wmsledger implements append-only warehouse stock movements and atomic reservations.
package wmsledger

import (
	"errors"
	"sync"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrInvalid      = errors.New("wmsledger: invalid value")
	ErrInsufficient = errors.New("wmsledger: insufficient available stock")
	ErrNotFound     = errors.New("wmsledger: not found")
)

type LocationKind string

const (
	LocationStorage    LocationKind = "storage"
	LocationPicking    LocationKind = "picking"
	LocationQuarantine LocationKind = "quarantine"
	LocationReceiving  LocationKind = "receiving"
)

func (k LocationKind) Valid() bool {
	return k == LocationStorage || k == LocationPicking || k == LocationQuarantine || k == LocationReceiving
}

type Location struct {
	ID, WarehouseID, Code string
	Kind                  LocationKind
	Active                bool
}

func (l Location) Validate() error {
	if l.ID == "" || l.WarehouseID == "" || l.Code == "" || !l.Kind.Valid() {
		return ErrInvalid
	}
	return nil
}

type Lot struct {
	ID, SKU   string
	ExpiresAt *time.Time
}

func (l Lot) Validate() error {
	if l.ID == "" || l.SKU == "" {
		return ErrInvalid
	}
	if l.ExpiresAt != nil && (l.ExpiresAt.IsZero() || !l.ExpiresAt.Equal(l.ExpiresAt.UTC())) {
		return ErrInvalid
	}
	return nil
}

type StockKey struct{ SKU, LocationID, LotID, Serial string }
type EntryKind string

const (
	EntryReceive    EntryKind = "receive"
	EntryMoveIn     EntryKind = "move_in"
	EntryMoveOut    EntryKind = "move_out"
	EntryAdjust     EntryKind = "adjust"
	EntryQuarantine EntryKind = "quarantine"
	EntryRelease    EntryKind = "release"
	EntryReserve    EntryKind = "reserve"
	EntryUnreserve  EntryKind = "unreserve"
	EntryConsume    EntryKind = "consume"
)

func (k EntryKind) Valid() bool {
	switch k {
	case EntryReceive, EntryMoveIn, EntryMoveOut, EntryAdjust, EntryQuarantine, EntryRelease, EntryReserve, EntryUnreserve, EntryConsume:
		return true
	}
	return false
}

type Entry struct {
	ID                        string
	Key                       StockKey
	Kind                      EntryKind
	Quantity                  int64
	Reference, IdempotencyKey string
	At                        time.Time
}
type Balance struct{ OnHand, Reserved, Quarantined int64 }

func (b Balance) Available() int64 {
	v := b.OnHand - b.Reserved - b.Quarantined
	if v < 0 {
		return 0
	}
	return v
}

type tenantState struct {
	entries  []Entry
	balances map[StockKey]Balance
	ids      map[string]struct{}
	lots     map[string]Lot
}
type MemoryLedger struct {
	mu      sync.Mutex
	tenants map[string]*tenantState
}

func NewMemoryLedger() *MemoryLedger { return &MemoryLedger{tenants: map[string]*tenantState{}} }
func scopeKey(scope tenancy.Scope) string {
	return scope.OrganizationID().String() + "/" + scope.WorkspaceID().String()
}
func (l *MemoryLedger) state(scope tenancy.Scope) *tenantState {
	k := scopeKey(scope)
	st := l.tenants[k]
	if st == nil {
		st = &tenantState{balances: map[StockKey]Balance{}, ids: map[string]struct{}{}, lots: map[string]Lot{}}
		l.tenants[k] = st
	}
	return st
}
func validKey(k StockKey) bool { return k.SKU != "" && k.LocationID != "" }
func (l *MemoryLedger) RegisterLot(scope tenancy.Scope, lot Lot) error {
	if !scope.Valid() || lot.Validate() != nil {
		return ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.state(scope).lots[lot.ID] = lot
	return nil
}
func (l *MemoryLedger) Append(scope tenancy.Scope, e Entry) error {
	if !scope.Valid() || e.ID == "" || !validKey(e.Key) || !e.Kind.Valid() || e.Quantity <= 0 || e.IdempotencyKey == "" || e.At.IsZero() {
		return ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.state(scope)
	if _, ok := st.ids[e.IdempotencyKey]; ok {
		return nil
	}
	if e.Key.Serial != "" && e.Quantity != 1 {
		return ErrInvalid
	}
	if e.Key.LotID != "" {
		lot, ok := st.lots[e.Key.LotID]
		if !ok || lot.SKU != e.Key.SKU {
			return ErrInvalid
		}
		if e.Kind == EntryReserve && lot.ExpiresAt != nil && !e.At.Before(*lot.ExpiresAt) {
			return ErrInsufficient
		}
	}
	b := st.balances[e.Key]
	switch e.Kind {
	case EntryReceive, EntryMoveIn:
		b.OnHand += e.Quantity
	case EntryMoveOut, EntryConsume:
		if b.OnHand-b.Reserved < e.Quantity {
			return ErrInsufficient
		}
		b.OnHand -= e.Quantity
	case EntryAdjust:
		b.OnHand += e.Quantity
	case EntryQuarantine:
		if b.Available() < e.Quantity {
			return ErrInsufficient
		}
		b.Quarantined += e.Quantity
	case EntryRelease:
		if b.Quarantined < e.Quantity {
			return ErrInsufficient
		}
		b.Quarantined -= e.Quantity
	case EntryReserve:
		if b.Available() < e.Quantity {
			return ErrInsufficient
		}
		b.Reserved += e.Quantity
	case EntryUnreserve:
		if b.Reserved < e.Quantity {
			return ErrInsufficient
		}
		b.Reserved -= e.Quantity
	}
	st.balances[e.Key] = b
	st.entries = append(st.entries, e)
	st.ids[e.IdempotencyKey] = struct{}{}
	return nil
}
func (l *MemoryLedger) Reserve(scope tenancy.Scope, id string, key StockKey, qty int64, ref, idempotency string, at time.Time) error {
	return l.Append(scope, Entry{id, key, EntryReserve, qty, ref, idempotency, at.UTC()})
}
func (l *MemoryLedger) Balance(scope tenancy.Scope, key StockKey) (Balance, error) {
	if !scope.Valid() || !validKey(key) {
		return Balance{}, ErrInvalid
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.tenants[scopeKey(scope)]
	if st == nil {
		return Balance{}, ErrNotFound
	}
	b, ok := st.balances[key]
	if !ok {
		return Balance{}, ErrNotFound
	}
	return b, nil
}
func (l *MemoryLedger) Entries(scope tenancy.Scope) []Entry {
	if !scope.Valid() {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	st := l.tenants[scopeKey(scope)]
	if st == nil {
		return nil
	}
	return append([]Entry(nil), st.entries...)
}
