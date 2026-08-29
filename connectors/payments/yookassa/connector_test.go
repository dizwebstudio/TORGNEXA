package yookassa

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return candidateSecrets{} }
func acc() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "y", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "yookassa", Family: sdk.FamilyPayment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestPaymentAndFullPartialRefund(t *testing.T) {
	c := New(candidateTransport{}, nil)
	q := sdk.PaymentCreateRequest{ExternalID: "order:1", IdempotencyKey: "idem:1", Purpose: "Order", Amount: sdk.PaymentAmount{MinorUnits: 10000, Currency: "RUB"}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	p, e := c.CreatePayment(context.Background(), acc(), rt{}, q)
	if e != nil {
		t.Fatal(e)
	}
	for i, amt := range []int64{10000, 2500} {
		r := sdk.PaymentRefundRequest{RemotePaymentID: p.RemoteID, ExternalID: "refund:test", IdempotencyKey: string(rune('a' + i)), Amount: sdk.PaymentAmount{MinorUnits: amt, Currency: "RUB"}}
		if _, e := c.RefundPayment(context.Background(), acc(), rt{}, r); e != nil {
			t.Fatal(e)
		}
	}
}
func TestIdempotenceKeyBounded(t *testing.T) {
	c := New(candidateTransport{}, nil)
	q := sdk.PaymentCreateRequest{ExternalID: "o", IdempotencyKey: string(make([]byte, 65)), Purpose: "x", Amount: sdk.PaymentAmount{MinorUnits: 1, Currency: "RUB"}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if _, e := c.CreatePayment(context.Background(), acc(), rt{}, q); e == nil {
		t.Fatal("oversized idempotence key accepted")
	}
}
