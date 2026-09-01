package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

type marketplaceListingStoreStub struct {
	taxonomy marketplacelisting.Taxonomy
	run      marketplacelisting.BatchRun
}

func (stub *marketplaceListingStoreStub) SaveTaxonomy(_ context.Context, _ tenancy.Scope, taxonomy marketplacelisting.Taxonomy) error {
	stub.taxonomy = taxonomy
	return nil
}

func (stub *marketplaceListingStoreStub) Taxonomy(_ context.Context, _ tenancy.Scope, _ string) (marketplacelisting.Taxonomy, error) {
	return stub.taxonomy, nil
}

func (stub *marketplaceListingStoreStub) SaveBatch(_ context.Context, _ tenancy.Scope, run marketplacelisting.BatchRun) (marketplacelisting.BatchRun, error) {
	stub.run = run
	return run, nil
}

func (stub *marketplaceListingStoreStub) Batch(_ context.Context, _ tenancy.Scope, _ string) (marketplacelisting.BatchRun, error) {
	return stub.run, nil
}

func TestMarketplaceListingPreviewIsTenantScopedAndDeterministic(t *testing.T) {
	scope := validTestScope(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	taxonomy := marketplacelisting.DemoTaxonomy("demo", "ru-RU", "RU", now)
	fingerprint, err := taxonomy.ComputeFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	draft := marketplacelisting.ListingDraft{
		ID: "listing-1", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ProductID: "product-1", OfferID: "offer-1", SKU: "SKU-1", CategoryCode: "demo.product", TaxonomyFingerprint: fingerprint, ProductVersion: 1,
		Content:    marketplacelisting.Content{Locale: "ru-RU", Title: "Товар"},
		Attributes: map[string]marketplacelisting.AttributeValue{"color": {Value: "white"}},
		Media:      []marketplacelisting.MediaRef{{ID: "media-1", Slot: "main", ReleasedObjectRef: "upl_demo", Digest: strings.Repeat("a", 64), Format: "image/jpeg", Bytes: 1024, Width: 1000, Height: 1000, Released: true, Safe: true}},
	}
	input := marketplaceListingPreviewRequest{ChannelAccountID: "account-1", ChannelID: "demo", Taxonomy: taxonomy, Items: []marketplacelisting.BatchItem{{SKU: draft.SKU, Before: draft}}, Operations: []marketplacelisting.BatchOperation{{Kind: marketplacelisting.BatchSet, Field: "content.title", Value: "Новый товар"}}}
	body, _ := json.Marshal(input)
	store := &marketplaceListingStoreStub{}
	api := marketplaceListingAPI{store: store, now: func() time.Time { return now }}
	request := httptest.NewRequest(http.MethodPost, MarketplaceListingBatchPreviewPath, strings.NewReader(string(body))).WithContext(context.WithValue(context.Background(), requestScopeKey{}, scope))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.preview(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", response.Code, response.Body.String())
	}
	var preview marketplacelisting.BatchPreview
	if err := json.Unmarshal(response.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.EligibleCount != 1 || !preview.Rows[0].Changed || preview.Rows[0].Before.OrganizationID != scope.OrganizationID().String() {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestMarketplaceListingApplyRequiresMatchingApproval(t *testing.T) {
	scope := validTestScope(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	taxonomy := marketplacelisting.DemoTaxonomy("demo", "ru-RU", "RU", now)
	fingerprint, _ := taxonomy.ComputeFingerprint()
	draft := marketplacelisting.ListingDraft{ID: "listing-1", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ProductID: "product-1", SKU: "SKU-1", CategoryCode: "demo.product", TaxonomyFingerprint: fingerprint, ProductVersion: 1, Content: marketplacelisting.Content{Locale: "ru-RU", Title: "Товар"}, Attributes: map[string]marketplacelisting.AttributeValue{"color": {Value: "white"}}, Media: []marketplacelisting.MediaRef{{ID: "media-1", Slot: "main", ReleasedObjectRef: "upl_demo", Digest: strings.Repeat("a", 64), Format: "image/jpeg", Bytes: 1024, Width: 1000, Height: 1000, Released: true, Safe: true}}}
	preview, err := marketplacelisting.BuildBatchPreview("preview-1", scope.OrganizationID().String(), scope.WorkspaceID().String(), "account-1", "demo", taxonomy, []marketplacelisting.BatchItem{{SKU: draft.SKU, Before: draft}}, []marketplacelisting.BatchOperation{{Kind: marketplacelisting.BatchSet, Field: "content.title", Value: "Новый товар"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	store := &marketplaceListingStoreStub{}
	api := marketplaceListingAPI{store: store, approvals: logisticsApprovalStub{request: approval.Request{ID: "approval-1", Action: "marketplace.listings.batch.apply", ResourceType: "marketplace_listing_batch", ResourceID: preview.ID, State: approval.StateApproved}}, now: func() time.Time { return now }}
	body, _ := json.Marshal(marketplaceListingApplyRequest{Preview: preview})
	request := httptest.NewRequest(http.MethodPost, MarketplaceListingBatchApplyPath, strings.NewReader(string(body))).WithContext(context.WithValue(context.WithValue(context.Background(), requestScopeKey{}, scope), requestIdentityKey{}, Principal{Issuer: "issuer", Subject: "operator"}))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "018f1c8a-7b3c-7def-8000-000000000001")
	request.Header.Set("Approval-Request-ID", "approval-1")
	response := httptest.NewRecorder()
	api.apply(response, request)
	if response.Code != http.StatusAccepted || store.run.State != marketplacelisting.BatchQueued {
		t.Fatalf("apply status=%d body=%s run=%+v", response.Code, response.Body.String(), store.run)
	}
}

func TestMarketplaceListingRemoteIdentityMatchesOperation(t *testing.T) {
	cases := []struct {
		name                        string
		kind                        marketplacepublication.OperationKind
		remoteID                    string
		remoteOperationID           string
		want                        bool
	}{
		{name: "create has no remote identity", kind: marketplacepublication.OperationCreateProduct, want: true},
		{name: "create rejects prefilled identity", kind: marketplacepublication.OperationCreateProduct, remoteID: "remote-1", want: false},
		{name: "update needs product identity", kind: marketplacepublication.OperationUpdateProduct, remoteID: "remote-1", want: true},
		{name: "update rejects operation-only identity", kind: marketplacepublication.OperationUpdateProduct, remoteOperationID: "operation-1", want: false},
		{name: "status read accepts operation identity", kind: marketplacepublication.OperationStatusRead, remoteOperationID: "operation-1", want: true},
		{name: "status read needs an identity", kind: marketplacepublication.OperationStatusRead, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := validRemotePublicationIdentity(test.kind, test.remoteID, test.remoteOperationID); got != test.want {
				t.Fatalf("validRemotePublicationIdentity() = %v, want %v", got, test.want)
			}
		})
	}
}
