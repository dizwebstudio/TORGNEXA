// Package signing defines isolated UKEP signing and machine-readable power-of-attorney references.
package signing

import (
	"context"
	"errors"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"regexp"
	"sync"
	"time"
)

var (
	ErrInvalid          = errors.New("signing: invalid value")
	ErrApprovalRequired = errors.New("signing: approval required")
)
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

type CertificateMetadata struct {
	ID, Serial, Thumbprint, SubjectRef, IssuerRef, Algorithm string
	Qualified                                                bool
	NotBefore, NotAfter                                      time.Time
}

func (c CertificateMetadata) Valid() bool {
	return c.ID != "" && c.Serial != "" && hex64.MatchString(c.Thumbprint) && c.SubjectRef != "" && c.IssuerRef != "" && c.Algorithm != "" && c.Qualified && !c.NotBefore.IsZero() && c.NotAfter.After(c.NotBefore)
}

type MChDAuthority struct {
	ID, RegistryRef, PrincipalRef, RepresentativeRef string
	Powers                                           []string
	ValidFrom, ValidUntil                            time.Time
	Revoked                                          bool
}

func (a MChDAuthority) Valid() bool {
	return a.ID != "" && a.RegistryRef != "" && a.PrincipalRef != "" && a.RepresentativeRef != "" && len(a.Powers) > 0 && !a.ValidFrom.IsZero() && a.ValidUntil.After(a.ValidFrom)
}

type Request struct {
	ID, ArtifactRef, DigestHex, CertificateID, MChDRef, Purpose, ApprovalRef, IdempotencyKey string
	RequestedAt                                                                              time.Time
}

func (r Request) Valid() bool {
	return r.ID != "" && r.ArtifactRef != "" && hex64.MatchString(r.DigestHex) && r.CertificateID != "" && r.Purpose != "" && r.IdempotencyKey != "" && !r.RequestedAt.IsZero()
}

type Result struct {
	RequestID, SignatureRef, Algorithm, CertificateID, MChDRef string
	SignedAt                                                   time.Time
}
type IsolatedSigner interface {
	Sign(context.Context, tenancy.Scope, Request) (Result, error)
}
type Evidence struct {
	RequestID, SignatureRef, CertificateID, MChDRef, ApprovalRef, DigestHex string
	SignedAt                                                                time.Time
}
type Service struct {
	mu       sync.Mutex
	signer   IsolatedSigner
	seen     map[string]Result
	evidence map[string][]Evidence
}

func NewService(s IsolatedSigner) *Service {
	return &Service{signer: s, seen: map[string]Result{}, evidence: map[string][]Evidence{}}
}
func skey(s tenancy.Scope) string {
	return s.OrganizationID().String() + "/" + s.WorkspaceID().String()
}
func (s *Service) Sign(ctx context.Context, scope tenancy.Scope, r Request) (Result, error) {
	if !scope.Valid() || !r.Valid() || s.signer == nil {
		return Result{}, ErrInvalid
	}
	if r.ApprovalRef == "" {
		return Result{}, ErrApprovalRequired
	}
	k := skey(scope) + "/" + r.IdempotencyKey
	s.mu.Lock()
	if x, ok := s.seen[k]; ok {
		s.mu.Unlock()
		return x, nil
	}
	s.mu.Unlock()
	res, err := s.signer.Sign(ctx, scope, r)
	if err != nil {
		return Result{}, err
	}
	if res.RequestID != r.ID || res.SignatureRef == "" || res.CertificateID != r.CertificateID || res.SignedAt.IsZero() {
		return Result{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen[k] = res
	s.evidence[skey(scope)] = append(s.evidence[skey(scope)], Evidence{r.ID, res.SignatureRef, r.CertificateID, r.MChDRef, r.ApprovalRef, r.DigestHex, res.SignedAt})
	return res, nil
}
func (s *Service) Evidence(scope tenancy.Scope) []Evidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Evidence(nil), s.evidence[skey(scope)]...)
}
