package pochtarussia

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	return callback([]byte(`{"token":"synthetic-token","key":"c3ludGhldGljLXVzZXI6cGFzc3dvcmQ=","tracking_login":"synthetic-login","tracking_password":"synthetic-password"}`))
}

func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{
		ID: "pochta-russia-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002",
		ConnectorID: "pochta-russia", Family: sdk.FamilyLogistics, Status: sdk.AccountActive,
		SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1,
		Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at,
	}
}

func TestHealthUsesCandidateTransport(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	health, err := New(candidateTransport{}, func() time.Time { return now }).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthHealthy || !health.CheckedAt.Equal(now) {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}

type rejectingSecrets struct{}

func (rejectingSecrets) UseSecret(context.Context, sdk.SecretReference, func([]byte) error) error {
	return errors.New("secret provider unavailable")
}

type rejectingRuntime struct{}

func (rejectingRuntime) Secrets() sdk.SecretAccessor { return rejectingSecrets{} }

func TestHealthPropagatesSecretProviderFailure(t *testing.T) {
	_, err := New(candidateTransport{}, nil).Health(context.Background(), testAccount(), rejectingRuntime{})
	if err == nil || err.Error() != "secret provider unavailable" {
		t.Fatalf("expected secret provider failure, got %v", err)
	}
}

func TestPickupUsesCandidateTransport(t *testing.T) {
	points, err := New(candidateTransport{}, nil).ReadPickupPoints(context.Background(), testAccount(), testRuntime{}, sdk.PickupPointQuery{
		Country: "RU", City: "Москва", Limit: 10,
	})
	if err != nil || len(points) != 1 || points[0].RemoteID != "101000" || points[0].Address == "" {
		t.Fatalf("points=%+v err=%v", points, err)
	}
}

func TestTrackingUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ReadLogisticsTracking(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentStatusRequest{RemoteID: "RA644000001RU"})
	if err != nil || result.RemoteID != "RA644000001RU" || result.Status != "in_transit" || result.TrackingNumber != "RA644000001RU" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchOrdersUseCandidateTransport(t *testing.T) {
	orders, err := New(candidateTransport{}, nil).ReadLogisticsBatchOrders(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchOrdersQuery{BatchID: "24", Limit: 10})
	if err != nil || len(orders) != 2 || orders[0].BatchID != "24" || orders[0].RemoteID != "57565818" || orders[0].TrackingNumber != "80084740397510" || orders[0].Status != "created" {
		t.Fatalf("orders=%+v err=%v", orders, err)
	}
}

func TestBatchLookupUsesCandidateTransport(t *testing.T) {
	batch, err := New(candidateTransport{}, nil).ReadLogisticsBatchByName(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchLookupQuery{BatchID: "24"})
	if err != nil || batch.RemoteID != "24" || batch.Status != "CREATED" || batch.ShipmentCount != 2 || batch.ObservedAt.IsZero() {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}
}

func TestOrderInBatchUsesCandidateTransport(t *testing.T) {
	order, err := New(candidateTransport{}, nil).ReadLogisticsOrder(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsOrderQuery{RemoteID: "57565818"})
	if err != nil || order.RemoteID != "57565818" || order.BatchID != "24" || order.TrackingNumber != "80084740397510" || order.Status != "created" {
		t.Fatalf("order=%+v err=%v", order, err)
	}
}

func TestOrderSearchUsesCandidateTransport(t *testing.T) {
	orders, err := New(candidateTransport{}, nil).SearchLogisticsOrders(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsOrderSearchQuery{ExternalID: "shop-order-1", Limit: 10})
	if err != nil || len(orders) != 1 || orders[0].RemoteID != "57565818" || orders[0].ExternalID != "shop-order-1" || orders[0].Status != "created" {
		t.Fatalf("orders=%+v err=%v", orders, err)
	}
}

func TestShipmentCreationUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).CreateLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCreateRequest{
		ExternalID: "order-001", ServiceCode: "pochta_parcel_online", IdempotencyKey: "idem-001",
		From:      sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"},
		To:        sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		Parcels:   []sdk.Parcel{{WeightGrams: 1000, LengthMM: 100, WidthMM: 100, HeightMM: 100}},
		Sender:    sdk.LogisticsContact{Name: "Иван Иванов", Phone: "+79990000000"},
		Recipient: sdk.LogisticsContact{Name: "Пётр Петров", Phone: "+79990000001"},
	})
	if err != nil || result.RemoteID != "57565818" || result.Status != "created" || result.Cost.Currency != "RUB" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestShipmentCancellationUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).CancelLogisticsShipment(context.Background(), testAccount(), testRuntime{}, sdk.ShipmentCancelRequest{RemoteID: "57565818", IdempotencyKey: "idem-cancel-001"})
	if err != nil || result.RemoteID != "57565818" || result.Status != "cancelled" || result.Cost.Currency != "RUB" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSeparateReturnUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).CreateLogisticsSeparateReturn(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsSeparateReturnRequest{
		From:              sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"},
		To:                &sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		InsuredValueMinor: 129900, MailType: "ONLINE_PARCEL", OrderNumber: "return-001", PostOfficeCode: "101000",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "separate-return-001",
	})
	if err != nil || result.RemoteID != "RA644000003RU" || result.TrackingNumber != result.RemoteID || result.Status != "created" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSeparateReturnDeletionUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).DeleteLogisticsSeparateReturn(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsSeparateReturnDeleteRequest{ReturnBarcode: "RA644000003RU", IdempotencyKey: "delete-return-001"})
	if err != nil || result.RemoteID != "RA644000003RU" || result.Status != "DELETED" || !result.Deleted || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestSeparateReturnEditUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).EditLogisticsSeparateReturn(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsSeparateReturnUpdateRequest{
		ReturnBarcode:     "RA644000003RU",
		From:              sdk.Address{Country: "RU", PostalCode: "101000", City: "Москва", Line1: "Мясницкая, 1"},
		To:                &sdk.Address{Country: "RU", PostalCode: "190000", City: "Санкт-Петербург", Line1: "Невский, 1"},
		InsuredValueMinor: 129900, MailType: "ONLINE_PARCEL", OrderNumber: "return-001", PostOfficeCode: "101000",
		RecipientName: "Пётр Петров", SenderName: "Иван Иванов", IdempotencyKey: "edit-return-001",
	})
	if err != nil || result.RemoteID != "RA644000003RU" || result.Status != "UPDATED" || !result.Updated || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchCreationUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).CreateLogisticsBatch(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchCreateRequest{
		OrderIDs: []string{"57565818", "57565819"}, SendingDate: "2026-08-31", UseOnlineBalance: true, IdempotencyKey: "batch-idem-001",
	})
	if err != nil || result.RemoteID != "batch-conformance-created-001" || result.Status != "CREATED" || result.ShipmentCount != 2 || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchSubmissionUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).SubmitLogisticsBatch(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchSubmitRequest{
		BatchID: "batch-conformance-001", UseOnlineBalance: true, IdempotencyKey: "submit-idem-001",
	})
	if err != nil || result.RemoteID != "batch-conformance-001" || result.Status != "SUBMITTED" || !result.Accepted || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchArchiveUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ArchiveLogisticsBatch(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchArchiveRequest{
		BatchID: "batch-conformance-001", IdempotencyKey: "archive-idem-001",
	})
	if err != nil || result.RemoteID != "batch-conformance-001" || result.Status != "ARCHIVED" || !result.Archived || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchSendingDateUpdateUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).UpdateLogisticsBatchSendingDate(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchSendingDateRequest{BatchID: "24", SendingDate: "2026-09-05", IdempotencyKey: "sending-date-idem-001"})
	if err != nil || result.RemoteID != "24" || result.SendingDate != "2026-09-05" || result.Status != "UPDATED" || !result.Updated || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchUnarchiveUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).UnarchiveLogisticsBatch(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsBatchUnarchiveRequest{
		BatchID: "batch-conformance-001", IdempotencyKey: "restore-idem-001",
	})
	if err != nil || result.RemoteID != "batch-conformance-001" || result.Status != "RESTORED" || result.Archived || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestOrderRestoreUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).RestoreLogisticsOrders(context.Background(), testAccount(), testRuntime{}, sdk.LogisticsOrderRestoreRequest{
		OrderIDs: []string{"57565818", "57565819"}, IdempotencyKey: "restore-orders-001",
	})
	if err != nil || !sameStringSet(result.OrderIDs, []string{"57565818", "57565819"}) || result.Status != "restored" || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestReturnCreationUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).CreateLogisticsReturn(context.Background(), testAccount(), testRuntime{}, sdk.ReturnCreateRequest{
		OriginalRemoteID: "RA644000001RU", ExternalID: "return-001", MailType: "POSTAL_PARCEL", IdempotencyKey: "return-idem-001",
	})
	if err != nil || result.RemoteID != "RA644000002RU" || result.Status != "created" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestLabelUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ReadLogisticsLabel(context.Background(), testAccount(), testRuntime{}, sdk.LabelRequest{RemoteID: "310115153", Format: "pdf"})
	if err != nil || result.ArtifactRef != "pochta-russia:form:backlog:310115153" || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestBatchF103LabelUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ReadLogisticsLabel(context.Background(), testAccount(), testRuntime{}, sdk.LabelRequest{RemoteID: "28", Format: "batch_f103_pdf"})
	if err != nil || !strings.HasPrefix(result.ArtifactRef, "pochta-russia:form:batch-f103:28") || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestFormedOrderLabelUsesCandidateTransport(t *testing.T) {
	result, err := New(candidateTransport{}, nil).ReadLogisticsLabel(context.Background(), testAccount(), testRuntime{}, sdk.LabelRequest{RemoteID: "310115153", Format: "formed_order_pdf"})
	if err != nil || !strings.HasPrefix(result.ArtifactRef, "pochta-russia:form:formed-order:310115153") || result.MediaType != "application/pdf" || result.ObservedAt.IsZero() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
