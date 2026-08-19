package signing

import (
	"context"
	"encoding/json"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"strings"
	"testing"
	"time"
)

type signer struct{}

func (signer) Sign(_ context.Context, _ tenancy.Scope, r Request) (Result, error) {
	return Result{RequestID: r.ID, SignatureRef: "sig:1", Algorithm: "gost", CertificateID: r.CertificateID, MChDRef: r.MChDRef, SignedAt: r.RequestedAt}, nil
}
func sc(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestApprovalIdempotencyAndNoPrivateKeyBoundary(t *testing.T) {
	svc := NewService(signer{})
	r := Request{ID: "r1", ArtifactRef: "artifact:1", DigestHex: strings.Repeat("a", 64), CertificateID: "cert:1", MChDRef: "mchd:1", Purpose: "edo", ApprovalRef: "approval:1", IdempotencyKey: "idem:1", RequestedAt: time.Now().UTC()}
	a, e := svc.Sign(context.Background(), sc(t), r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := svc.Sign(context.Background(), sc(t), r)
	if e != nil || a != b {
		t.Fatal("not idempotent")
	}
	raw, _ := json.Marshal(r)
	if strings.Contains(strings.ToLower(string(raw)), "private") || strings.Contains(strings.ToLower(string(raw)), "key_bytes") {
		t.Fatal("private key crossed boundary")
	}
}
func TestApprovalRequired(t *testing.T) {
	r := Request{ID: "r1", ArtifactRef: "a", DigestHex: strings.Repeat("a", 64), CertificateID: "c", Purpose: "edo", IdempotencyKey: "i", RequestedAt: time.Now().UTC()}
	_, e := NewService(signer{}).Sign(context.Background(), sc(t), r)
	if e != ErrApprovalRequired {
		t.Fatalf("%v", e)
	}
}
