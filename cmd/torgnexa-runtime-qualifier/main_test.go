package main

import (
	"encoding/json"
	"testing"
)

func TestQualificationEvidenceIncludesP3WarehouseExecutionFields(t *testing.T) {
	payload, err := json.Marshal(result{
		Status:                           "PASS",
		WarehouseRoutedCount:             1,
		WarehouseReroutedAllocationCount: 1,
		WarehouseExecutionAttentionCount: 0,
		WarehouseSourceUnchanged:         true,
		WarehouseDestinationValid:        true,
		WarehouseAllocationRerouted:      true,
		WarehouseSourceReservationFreed:  true,
		WarehouseDestinationReserved:     true,
		FulfillmentOutboxObserved:        true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(payload, &evidence); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"warehouse_rerouted_allocation_count",
		"warehouse_execution_attention_count",
		"warehouse_source_stock_unchanged",
		"warehouse_allocation_rerouted",
		"warehouse_source_reservation_released",
		"warehouse_destination_reserved",
		"fulfillment_outbox_observed",
	} {
		if _, ok := evidence[key]; !ok {
			t.Fatalf("qualification evidence missing %q", key)
		}
	}
}
