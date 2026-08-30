package connectors

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"
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
	if len(q.Country) != 2 || !asciiUpperCountry(q.Country) || !utf8.ValidString(q.City) || q.City == "" || utf8.RuneCountInString(q.City) > 200 || q.Limit < 1 || q.Limit > max {
		return ErrInvalidPickupRequest
	}
	return nil
}

func asciiUpperCountry(value string) bool {
	for _, symbol := range []byte(value) {
		if symbol < 'A' || symbol > 'Z' {
			return false
		}
	}
	return true
}

// Validate checks the bounded provider-neutral shape of one pickup point.
func (p PickupPoint) Validate() error {
	if !logisticsRefPattern.MatchString(p.RemoteID) || !utf8.ValidString(p.Name) || p.Name == "" || utf8.RuneCountInString(p.Name) > 300 || !asciiUpperCountry(p.Country) || !utf8.ValidString(p.City) || p.City == "" || utf8.RuneCountInString(p.City) > 200 || !utf8.ValidString(p.Address) || p.Address == "" || utf8.RuneCountInString(p.Address) > 500 || p.UpdatedAt.IsZero() || p.UpdatedAt.Location() != time.UTC {
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
