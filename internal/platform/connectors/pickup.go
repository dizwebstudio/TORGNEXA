package connectors

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidPickupRequest = errors.New("connectors: invalid pickup request")

type PickupPointQuery struct {
	Country, City string
	Limit         int
}
type PickupPoint struct {
	RemoteID, Name, Country, City, Address string
	Active                                 bool
	UpdatedAt                              time.Time
}
type PickupCapacityRequest struct {
	RemoteID string
	Day      time.Time
}
type PickupCapacity struct {
	RemoteID        string
	AvailableOrders int64
	ObservedAt      time.Time
}

func (q PickupPointQuery) Validate(max int) error {
	if len(q.Country) != 2 || q.City == "" || q.Limit < 1 || q.Limit > max {
		return ErrInvalidPickupRequest
	}
	return nil
}

type PickupPointReader interface {
	ReadPickupPoints(context.Context, Account, Runtime, PickupPointQuery) ([]PickupPoint, error)
}
type PickupCapacityReader interface {
	ReadPickupCapacity(context.Context, Account, Runtime, PickupCapacityRequest) (PickupCapacity, error)
}
