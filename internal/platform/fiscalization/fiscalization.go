// Package fiscalization defines vendor-neutral fiscal receipt/refund/correction workflows.
package fiscalization

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"regexp"
	"sync"
	"time"
)

var ErrInvalid = errors.New("fiscalization: invalid value")
var fingerprintPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var statusPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Kind string

const (
	Sale       Kind = "sale"
	Refund     Kind = "refund"
	Correction Kind = "correction"
)

func (k Kind) Valid() bool { return k == Sale || k == Refund || k == Correction }

type MarkingLink struct{ CodeFingerprint, VerificationStatus string }
type Request struct {
	ID, ExternalRef, IdempotencyKey, EmailOrPhoneRef string
	Kind                                             Kind
	Total                                            domain.Money
	Marking                                          []MarkingLink
	CorrectionOf                                     string
	CreatedAt                                        time.Time
}
type Status struct {
	RequestID, RemoteID, State, FiscalDocumentRef string
	ObservedAt                                    time.Time
}
type Gateway interface {
	Create(context.Context, tenancy.Scope, Request) (Status, error)
	Status(context.Context, tenancy.Scope, string) (Status, error)
}
type Service struct {
	mu      sync.Mutex
	gateway Gateway
	seen    map[string]Status
}

func NewService(g Gateway) *Service { return &Service{gateway: g, seen: map[string]Status{}} }
func (r Request) Valid() bool {
	if r.ID == "" || r.ExternalRef == "" || r.IdempotencyKey == "" || !r.Kind.Valid() || r.Total.Validate() != nil || r.Total.MinorUnits() <= 0 || r.CreatedAt.IsZero() || (r.Kind == Correction && r.CorrectionOf == "") || len(r.Marking) > 1000 {
		return false
	}
	for _, link := range r.Marking {
		if !fingerprintPattern.MatchString(link.CodeFingerprint) || !statusPattern.MatchString(link.VerificationStatus) {
			return false
		}
	}
	return true
}
func (s *Service) Create(ctx context.Context, sc tenancy.Scope, r Request) (Status, error) {
	if !sc.Valid() || !r.Valid() || s.gateway == nil {
		return Status{}, ErrInvalid
	}
	key := sc.OrganizationID().String() + "/" + sc.WorkspaceID().String() + "/" + r.IdempotencyKey
	s.mu.Lock()
	if x, ok := s.seen[key]; ok {
		s.mu.Unlock()
		return x, nil
	}
	s.mu.Unlock()
	x, e := s.gateway.Create(ctx, sc, r)
	if e != nil {
		return Status{}, e
	}
	if x.RequestID != r.ID || x.RemoteID == "" || x.State == "" || x.ObservedAt.IsZero() {
		return Status{}, ErrInvalid
	}
	s.mu.Lock()
	s.seen[key] = x
	s.mu.Unlock()
	return x, nil
}
func (s *Service) Refresh(ctx context.Context, sc tenancy.Scope, remoteID string) (Status, error) {
	if !sc.Valid() || remoteID == "" || s.gateway == nil {
		return Status{}, ErrInvalid
	}
	return s.gateway.Status(ctx, sc, remoteID)
}
