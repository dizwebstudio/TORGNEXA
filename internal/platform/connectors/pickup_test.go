package connectors

import (
	"strings"
	"testing"
	"time"
)

func TestPickupPointQueryAndPointLimits(t *testing.T) {
	if (PickupPointQuery{Country: "ru", City: "Москва", Limit: 1}).Validate(500) == nil {
		t.Fatal("lowercase country accepted")
	}
	if (PickupPointQuery{Country: "RU", City: strings.Repeat("а", 201), Limit: 1}).Validate(500) == nil {
		t.Fatal("overlong city accepted")
	}
	if (PickupPointQuery{Country: "RU", City: "Москва", Limit: 501}).Validate(500) == nil {
		t.Fatal("overlong result limit accepted")
	}
	point := PickupPoint{RemoteID: "pvz:1", Name: "Пункт выдачи", Country: "RU", City: "Москва", Address: "Тверская, 1", UpdatedAt: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	if err := point.Validate(); err != nil {
		t.Fatalf("valid pickup point rejected: %v", err)
	}
	point.Address = strings.Repeat("а", 501)
	if point.Validate() == nil {
		t.Fatal("overlong pickup point address accepted")
	}
}
