package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalogbulk"
	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

type catalogBulkStoreStub struct {
	previews []catalogbulk.Preview
	runs     []catalogbulk.Run
	kill     catalogbulk.KillSwitch
}

func (s *catalogBulkStoreStub) SavePreview(_ context.Context, _ tenancy.Scope, item catalogbulk.Preview) (catalogbulk.Preview, error) {
	s.previews = append(s.previews, item)
	return item, nil
}

func (s *catalogBulkStoreStub) Preview(_ context.Context, _ tenancy.Scope, id string) (catalogbulk.Preview, error) {
	for _, item := range s.previews {
		if item.ID == id {
			return item, nil
		}
	}
	return catalogbulk.Preview{}, errCatalogBulkTestNotFound
}

func (s *catalogBulkStoreStub) ListPreviews(context.Context, tenancy.Scope, string, int) ([]catalogbulk.Preview, string, error) {
	return s.previews, "", nil
}

func (s *catalogBulkStoreStub) SaveRun(_ context.Context, _ tenancy.Scope, item catalogbulk.Run) (catalogbulk.Run, error) {
	s.runs = append(s.runs, item)
	return item, nil
}

func (s *catalogBulkStoreStub) Run(_ context.Context, _ tenancy.Scope, id string) (catalogbulk.Run, error) {
	for _, item := range s.runs {
		if item.ID == id {
			return item, nil
		}
	}
	return catalogbulk.Run{}, errCatalogBulkTestNotFound
}

func (s *catalogBulkStoreStub) ListRuns(context.Context, tenancy.Scope, string, int) ([]catalogbulk.Run, string, error) {
	return s.runs, "", nil
}

func (s *catalogBulkStoreStub) KillSwitch(context.Context, tenancy.Scope) (catalogbulk.KillSwitch, error) {
	if s.kill.Version == 0 {
		s.kill = catalogbulk.KillSwitch{Version: 1, UpdatedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	}
	return s.kill, nil
}

func (s *catalogBulkStoreStub) SetKillSwitch(_ context.Context, _ tenancy.Scope, control catalogbulk.KillSwitch) error {
	s.kill = control
	return nil
}

type catalogBulkApprovalStub struct{ request approval.Request }

func (s catalogBulkApprovalStub) Request(context.Context, tenancy.Scope, string) (approval.Request, error) {
	return s.request, nil
}

var errCatalogBulkTestNotFound = catalogbulk.ErrInvalid

func TestCatalogBulkPreviewApplyAndQualificationGuards(t *testing.T) {
	scope := validTestScope(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	taxonomy := marketplacelisting.DemoTaxonomy("demo", "ru-RU", "RU", now)
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	draft := marketplacelisting.ListingDraft{
		ID: "listing-1", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ProductID: "product-1", OfferID: "offer-1", SKU: "SKU-1", CategoryCode: "demo.product", TaxonomyFingerprint: fingerprint, ProductVersion: 1,
		Attributes: map[string]marketplacelisting.AttributeValue{"color": {Value: "white"}},
		Content:    marketplacelisting.Content{Locale: "ru-RU", Title: "Старое название"},
		Media:      []marketplacelisting.MediaRef{{ID: "media-1", Slot: "main", ReleasedObjectRef: "upl_media-1", Digest: strings.Repeat("a", 64), Format: "image/jpeg", Bytes: 2048, Width: 1200, Height: 1200, Released: true, Safe: true}},
	}
	target := catalogbulk.ChannelTarget{ChannelID: "demo", AccountID: "account-1", Label: "Demo", State: catalogbulk.CapabilityQualified, Capabilities: []string{"marketplace.listings.content.write"}, TaxonomyFingerprint: fingerprint, TaxonomyVersion: 1, MappingVersion: 1, ObservedAt: now, FreshUntil: now.Add(time.Hour)}
	selection := catalogbulk.SelectionSnapshot{FilterDigest: strings.Repeat("a", 64), Filter: "saved_filter", SKUs: []string{"SKU-1"}, Targets: []catalogbulk.ChannelTarget{target}, SnapshotVersion: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	projection := catalogbulk.Projection{SKU: "SKU-1", ProductID: "product-1", OfferID: "offer-1", ChannelID: "demo", AccountID: "account-1", Draft: draft, Currency: "RUB", PriceMinorUnits: 10000, Stock: 3, Version: 1}
	store := &catalogBulkStoreStub{}
	api := catalogBulkAPI{store: store, now: func() time.Time { return now }}
	ctx := context.WithValue(context.Background(), requestScopeKey{}, scope)
	body, _ := json.Marshal(catalogBulkPreviewRequest{Selection: selection, Projections: []catalogbulk.Projection{projection}, Changes: []catalogbulk.Change{{Kind: "set", Field: "content.title", Value: "Новое название"}}})
	request := httptest.NewRequest(http.MethodPost, CatalogBulkPreviewPath, strings.NewReader(string(body))).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.preview(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
	}
	var preview catalogbulk.Preview
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.EligibleRows != 1 || len(store.previews) != 1 {
		t.Fatalf("preview=%+v", preview)
	}

	approvalRef := "approval-1"
	api.approvals = catalogBulkApprovalStub{request: approval.Request{ID: approvalRef, Action: "catalog.bulk.apply", ResourceType: "catalog_bulk_preview", ResourceID: preview.ID, State: approval.StateApproved}}
	applyCtx := context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "issuer", Subject: "operator-1"})
	applyBody, _ := json.Marshal(catalogBulkApplyRequest{PreviewID: preview.ID})
	apply := httptest.NewRequest(http.MethodPost, CatalogBulkApplyPath, strings.NewReader(string(applyBody))).WithContext(applyCtx)
	apply.Header.Set("Content-Type", "application/json")
	apply.Header.Set("Idempotency-Key", "018f1c8a-7b3c-7def-8000-000000000001")
	apply.Header.Set("Approval-Request-ID", approvalRef)
	applyResponse := httptest.NewRecorder()
	api.apply(applyResponse, apply)
	if applyResponse.Code != http.StatusAccepted || len(store.runs) != 1 || store.runs[0].ActorRef != "operator-1" {
		t.Fatalf("apply status=%d body=%s runs=%+v", applyResponse.Code, applyResponse.Body.String(), store.runs)
	}
}

func TestCatalogBulkKillSwitchStopsApplyWithoutDeletingEvidence(t *testing.T) {
	scope := validTestScope(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	store := &catalogBulkStoreStub{kill: catalogbulk.KillSwitch{Version: 1, UpdatedAt: now}}
	api := catalogBulkAPI{store: store, now: func() time.Time { return now }}
	ctx := context.WithValue(context.Background(), requestScopeKey{}, scope)
	ctx = context.WithValue(ctx, requestIdentityKey{}, Principal{Issuer: "issuer", Subject: "operator-1"})
	body := `{"enabled":true,"reason":"mapping incident"}`
	request := httptest.NewRequest(http.MethodPost, CatalogBulkKillSwitchPath, strings.NewReader(body)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.setKillSwitch(response, request)
	if response.Code != http.StatusOK || !store.kill.Enabled || store.kill.Version != 2 {
		t.Fatalf("kill switch status=%d body=%s state=%+v", response.Code, response.Body.String(), store.kill)
	}
	if len(store.previews) != 0 || len(store.runs) != 0 {
		t.Fatal("kill switch changed evidence")
	}
}
