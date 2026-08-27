package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/social"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/socialdispatchrepo"
)

type socialPublicationStoreStub struct {
	publication social.Publication
	changes     []social.ChangePublicationStatus
}

func (stub *socialPublicationStoreStub) Publication(context.Context, social.Scope, social.PublicationID) (social.Publication, error) {
	return stub.publication, nil
}
func (*socialPublicationStoreStub) Variant(context.Context, social.Scope, social.VariantID) (social.ContentVariant, error) {
	return social.ContentVariant{}, errors.New("not used")
}
func (*socialPublicationStoreStub) ChannelAccount(context.Context, social.Scope, social.ChannelAccountID) (social.ChannelAccount, error) {
	return social.ChannelAccount{}, errors.New("not used")
}
func (stub *socialPublicationStoreStub) ChangePublicationStatus(_ context.Context, _ social.Scope, command social.ChangePublicationStatus, _ social.Mutation) (social.Publication, error) {
	stub.changes = append(stub.changes, command)
	stub.publication.Status = command.Status
	stub.publication.ReasonCode = command.ReasonCode
	stub.publication.Version++
	return stub.publication, nil
}

type socialReceiptStoreStub struct{ receipt *socialdispatchrepo.Receipt }

func (stub socialReceiptStoreStub) Receipt(context.Context, tenancy.Scope, string) (socialdispatchrepo.Receipt, error) {
	if stub.receipt == nil {
		return socialdispatchrepo.Receipt{}, socialdispatchrepo.ErrNotFound
	}
	return *stub.receipt, nil
}
func (stub socialReceiptStoreStub) Record(context.Context, tenancy.Scope, socialdispatchrepo.Receipt) (socialdispatchrepo.Receipt, error) {
	return socialdispatchrepo.Receipt{}, errors.New("not used")
}

func TestRecoverSocialPublicationFinalizesPersistedReceiptWithoutResend(t *testing.T) {
	tenantScope, _ := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	scope, _ := social.ParseScope(tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String())
	publication := publishingSocialPublication()
	store := &socialPublicationStoreStub{publication: publication}
	receipt := socialdispatchrepo.Receipt{PublicationID: publication.ID.String(), ConnectorAccountID: "telegram-main", RemotePublicationID: "tg:-100123:42", ObservedAt: time.Now().UTC()}
	if err := recoverSocialPublication(context.Background(), store, socialReceiptStoreStub{receipt: &receipt}, tenantScope, scope, publication); err != nil {
		t.Fatal(err)
	}
	if len(store.changes) != 1 || store.changes[0].Status != social.PublicationPublished || store.changes[0].ReasonCode != "" {
		t.Fatalf("unexpected recovery transition: %#v", store.changes)
	}
}

func TestRecoverSocialPublicationFailsUnknownOutcomeWithoutReceipt(t *testing.T) {
	tenantScope, _ := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	scope, _ := social.ParseScope(tenantScope.OrganizationID().String(), tenantScope.WorkspaceID().String())
	publication := publishingSocialPublication()
	store := &socialPublicationStoreStub{publication: publication}
	if err := recoverSocialPublication(context.Background(), store, socialReceiptStoreStub{}, tenantScope, scope, publication); err != nil {
		t.Fatal(err)
	}
	if len(store.changes) != 1 || store.changes[0].Status != social.PublicationFailed || store.changes[0].ReasonCode != "write_outcome_unknown" {
		t.Fatalf("unexpected recovery transition: %#v", store.changes)
	}
}

func publishingSocialPublication() social.Publication {
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	return social.Publication{
		ID:               social.PublicationID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204"),
		OrganizationID:   "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001",
		WorkspaceID:      "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		VariantID:        social.VariantID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0203"),
		ChannelAccountID: social.ChannelAccountID("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0202"),
		Schedule:         social.ImmediateSchedule(), Status: social.PublicationPublishing,
		Attempt: 1, Version: 2, CreatedAt: now, UpdatedAt: now,
	}
}
