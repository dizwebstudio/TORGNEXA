// Package replenishment creates explainable, non-executing purchase recommendations.
package replenishment

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"sort"
	"sync"
	"time"
)

var ErrInvalid = errors.New("replenishment: invalid value")

type Input struct {
	SKU, SupplierOfferID                                                         string
	SalesUnitsPerDay, LeadTimeDays, SafetyStockUnits, OnHandUnits, ReservedUnits int64
}
type Snapshot struct {
	ID, AlgorithmVersion string
	Inputs               []Input
	CapturedAt           time.Time
	Digest               string
}
type Recommendation struct {
	ID, SnapshotID, AlgorithmVersion, SKU, SupplierOfferID string
	RecommendedUnits                                       int64
	Explanation                                            string
	CreatedAt                                              time.Time
	AutoSendPO                                             bool
}

func BuildSnapshot(id, version string, inputs []Input, at time.Time) (Snapshot, error) {
	if id == "" || version == "" || len(inputs) == 0 || at.IsZero() {
		return Snapshot{}, ErrInvalid
	}
	cp := append([]Input(nil), inputs...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].SKU < cp[j].SKU })
	h := sha256.New()
	for _, x := range cp {
		if x.SKU == "" || x.SupplierOfferID == "" || x.SalesUnitsPerDay < 0 || x.LeadTimeDays < 0 || x.SafetyStockUnits < 0 || x.OnHandUnits < 0 || x.ReservedUnits < 0 {
			return Snapshot{}, ErrInvalid
		}
		fmt.Fprintf(h, "%s|%s|%d|%d|%d|%d|%d\n", x.SKU, x.SupplierOfferID, x.SalesUnitsPerDay, x.LeadTimeDays, x.SafetyStockUnits, x.OnHandUnits, x.ReservedUnits)
	}
	return Snapshot{id, version, cp, at.UTC(), hex.EncodeToString(h.Sum(nil))}, nil
}
func Recommend(scope tenancy.Scope, s Snapshot, at time.Time) ([]Recommendation, error) {
	if !scope.Valid() || s.ID == "" || s.AlgorithmVersion == "" || s.Digest == "" || at.IsZero() {
		return nil, ErrInvalid
	}
	out := make([]Recommendation, 0, len(s.Inputs))
	for i, x := range s.Inputs {
		target := x.SalesUnitsPerDay*x.LeadTimeDays + x.SafetyStockUnits
		available := x.OnHandUnits - x.ReservedUnits
		if available < 0 {
			available = 0
		}
		qty := target - available
		if qty < 0 {
			qty = 0
		}
		out = append(out, Recommendation{fmt.Sprintf("%s-%03d", s.ID, i+1), s.ID, s.AlgorithmVersion, x.SKU, x.SupplierOfferID, qty, fmt.Sprintf("target=%d (= velocity %d × lead_time %d + safety %d); available=%d", target, x.SalesUnitsPerDay, x.LeadTimeDays, x.SafetyStockUnits, available), at.UTC(), false})
	}
	return out, nil
}

// Repository persists immutable planning evidence and recommendations.
type Repository interface {
	SaveSnapshot(tenancy.Scope, Snapshot) error
	SaveRecommendations(tenancy.Scope, []Recommendation) error
}

type MemoryRepository struct {
	mu              sync.Mutex
	snapshots       map[string]Snapshot
	recommendations map[string][]Recommendation
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{snapshots: map[string]Snapshot{}, recommendations: map[string][]Recommendation{}}
}
func tenantKey(scope tenancy.Scope, id string) string {
	return scope.OrganizationID().String() + "/" + scope.WorkspaceID().String() + "/" + id
}
func (r *MemoryRepository) SaveSnapshot(scope tenancy.Scope, s Snapshot) error {
	if !scope.Valid() || s.ID == "" || s.Digest == "" {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := tenantKey(scope, s.ID)
	if old, ok := r.snapshots[key]; ok && old.Digest != s.Digest {
		return ErrInvalid
	}
	r.snapshots[key] = s
	return nil
}
func (r *MemoryRepository) SaveRecommendations(scope tenancy.Scope, recs []Recommendation) error {
	if !scope.Valid() || len(recs) == 0 {
		return ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := append([]Recommendation(nil), recs...)
	r.recommendations[tenantKey(scope, recs[0].SnapshotID)] = cp
	return nil
}
func (r *MemoryRepository) Snapshot(scope tenancy.Scope, id string) (Snapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.snapshots[tenantKey(scope, id)]
	return v, ok
}

type Service struct{ Repository Repository }

func (svc Service) Generate(scope tenancy.Scope, snapshot Snapshot, at time.Time) ([]Recommendation, error) {
	if svc.Repository == nil {
		return nil, ErrInvalid
	}
	if err := svc.Repository.SaveSnapshot(scope, snapshot); err != nil {
		return nil, err
	}
	recs, err := Recommend(scope, snapshot, at)
	if err != nil {
		return nil, err
	}
	if err := svc.Repository.SaveRecommendations(scope, recs); err != nil {
		return nil, err
	}
	return recs, nil
}
