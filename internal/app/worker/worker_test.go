package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

func TestKafkaConsumerTopicsIncludesRetryTopics(t *testing.T) {
	topics, err := kafkaConsumerTopics([]string{"commerce.orders.events.v1", "security.upload.events.v1"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"commerce.orders.events.v1", "commerce.orders.events.v1.retry", "security.upload.events.v1", "security.upload.events.v1.retry"}
	if len(topics) != len(want) {
		t.Fatalf("topics=%v", topics)
	}
	for i := range want {
		if topics[i] != want[i] {
			t.Fatalf("topics[%d]=%q want %q", i, topics[i], want[i])
		}
	}
}

func TestSupervisorCancelsAllComponents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	started := make(chan struct{}, 2)
	componentFn := func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	result := make(chan error, 1)
	go func() {
		result <- supervise(ctx, logger, []component{{name: "one", run: componentFn}, {name: "two", run: componentFn}})
	}()
	<-started
	<-started
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("supervisor did not stop")
	}
}

func TestSupervisorPropagatesComponentFailure(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	boom := errors.New("boom")
	err := supervise(context.Background(), logger, []component{{name: "failed", run: func(context.Context) error { return boom }}})
	var runtimeErr *runtimeError
	if !errors.As(err, &runtimeErr) || runtimeErr.ErrorCode() != "worker_component_failed" {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("wrapped error lost: %v", err)
	}
}

func TestUploadMutationIsValid(t *testing.T) {
	mutation, err := uploadMutation("upl_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if err := mutation.Validate(); err != nil {
		t.Fatalf("invalid mutation: %v", err)
	}
}

func TestActionPolicyUsesOnlyExecutableWrites(t *testing.T) {
	remote := actionPolicyFor(syncengine.Policy{Direction: syncengine.DirectionInbound, SourceOfTruth: syncengine.SourceRemote}, false)
	if remote.Content != reconciliation.ActionAutoFix || remote.StatusMismatch != reconciliation.ActionAutoFix {
		t.Fatalf("remote policy=%+v", remote)
	}
	writable := actionPolicyFor(syncengine.Policy{Direction: syncengine.DirectionOutbound, SourceOfTruth: syncengine.SourceLocal}, true)
	if writable.Content != reconciliation.ActionAutoFix {
		t.Fatalf("writable policy=%+v", writable)
	}
	unsupported := actionPolicyFor(syncengine.Policy{Direction: syncengine.DirectionOutbound, SourceOfTruth: syncengine.SourceLocal}, false)
	if unsupported.Content != reconciliation.ActionNotify || unsupported.StatusMismatch != reconciliation.ActionNotify {
		t.Fatalf("unsupported outbound policy=%+v", unsupported)
	}
}

func TestStableUUIDIsDeterministicAndApprovalCompatible(t *testing.T) {
	first := stableUUID("same-key")
	second := stableUUID("same-key")
	if first != second || len(first) != 36 {
		t.Fatalf("uuid=%q second=%q", first, second)
	}
	if stableUUID("other-key") == first {
		t.Fatal("different idempotency keys collided")
	}
}

func TestCatalogStatusDoesNotInventUnknownProviderState(t *testing.T) {
	if got, ok := catalogStatus("published"); !ok || got != catalog.StatusActive {
		t.Fatalf("published=%q ok=%v", got, ok)
	}
	if got, ok := catalogStatus("archived"); !ok || got != catalog.StatusArchived {
		t.Fatalf("archived=%q ok=%v", got, ok)
	}
	if _, ok := catalogStatus("provider-private-state"); ok {
		t.Fatal("unknown provider state must fail closed")
	}
}

func TestCommerceReconciliationValueNormalizers(t *testing.T) {
	cases := []struct {
		value, currency string
		want            int64
	}{
		{value: "123.45", currency: "RUB", want: 12345},
		{value: "123.450", currency: "RUB", want: 12345},
		{value: "1500", currency: "JPY", want: 1500},
		{value: "1.234", currency: "KWD", want: 1234},
	}
	for _, test := range cases {
		got, err := remotePriceToMinor(test.value, test.currency)
		if err != nil || got != test.want {
			t.Fatalf("remotePriceToMinor(%q, %q) = %d, %v; want %d", test.value, test.currency, got, err, test.want)
		}
	}
	if _, err := remotePriceToMinor("1.239", "RUB"); err == nil {
		t.Fatal("price precision beyond currency scale must fail closed")
	}
	if got := minorUnitsToMajor(12345, "RUB"); got != "123.45" {
		t.Fatalf("minorUnitsToMajor = %q", got)
	}
	remoteID := inventoryRemoteID("warehouse::main", "offer.1")
	location, variant, ok := splitInventoryRemoteID(remoteID)
	if !ok || location != "warehouse::main" || variant != "offer.1" {
		t.Fatalf("inventory remote id round trip = %q, %q, %v", location, variant, ok)
	}
	if offset, err := inventoryCursorOffset("inventory:25"); err != nil || offset != 25 {
		t.Fatalf("inventory cursor = %d, %v", offset, err)
	}
	if _, err := inventoryCursorOffset("inventory:-1"); err == nil {
		t.Fatal("negative inventory cursor must fail closed")
	}
}
