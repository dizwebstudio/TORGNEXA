package chestnyznak

import (
	"context"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"strings"
	"testing"
	"time"
)

type ft struct{}

func (ft) Ping(context.Context, []byte) error { return nil }
func (ft) ProductByGTIN(_ context.Context, _ []byte, g string) (ProductResponse, error) {
	return ProductResponse{GTIN: g, RemoteID: "product:1", Status: "published", Name: "Synthetic", ObservedAt: time.Now().UTC()}, nil
}
func (ft) CodeStatuses(_ context.Context, _ []byte, c []string) ([]CodeResponse, error) {
	return []CodeResponse{{Code: c[0], Status: "circulation", GTIN: "04601234567890", ObservedAt: time.Now().UTC()}}, nil
}

type rt struct{}

func (rt) Secrets() sdk.SecretAccessor { return sec{} }

type sec struct{}

func (sec) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn([]byte("secret"))
}
func acc() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "cz", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "chestny-znak", Family: sdk.FamilyGovernment, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func TestStatusProtectsRawCode(t *testing.T) {
	code := "010460123456789021SERIAL93SECRET"
	o, e := New(ft{}, nil).ReadMarkingStatus(context.Background(), acc(), rt{}, sdk.MarkingStatusRequest{Codes: []string{code}})
	if e != nil {
		t.Fatal(e)
	}
	if o.Items[0].CodeFingerprint == code || strings.Contains(o.Items[0].CodeFingerprint, "SERIAL") {
		t.Fatal("raw code leaked")
	}
}
