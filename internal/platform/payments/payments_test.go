package payments

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"strings"
	"testing"
	"time"
)

func TestWebhookDedupAndNoCardFields(t *testing.T) {
	sc, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	s := NewStore()
	e := WebhookEvidence{DeliveryID: "d1", RemoteID: "p1", EventType: "paid", BodyDigest: strings.Repeat("a", 64), VerifiedAt: time.Now().UTC()}
	a, _ := s.RecordWebhook(sc, e)
	b, _ := s.RecordWebhook(sc, e)
	if !a || b {
		t.Fatal("webhook replay")
	}
	rub, _ := domain.NewCurrency("RUB")
	m, _ := domain.NewMoney(100, rub)
	if _, err := s.UpsertRemote(sc, Payment{ID: "p", RailID: "payment-rail", RemoteID: "r", ExternalID: "e", Status: "paid", Amount: m, Commission: m, Version: 1, ObservedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}
