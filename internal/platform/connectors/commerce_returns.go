package connectors

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"
)

var ErrInvalidReturnRead = errors.New("connectors: invalid return read")

type ReturnQuery struct {
	OrderRemoteID string `json:"order_remote_id"`
	Cursor        string `json:"cursor,omitempty"`
	Limit         int    `json:"limit"`
}

func (query ReturnQuery) Validate(maxLimit int) error {
	if !validRemoteID(query.OrderRemoteID) || query.Limit < 1 || query.Limit > maxLimit || len(query.Cursor) > 4096 || !utf8.ValidString(query.Cursor) {
		return ErrInvalidReturnRead
	}
	return nil
}

type RemoteReturn struct {
	RemoteID      string    `json:"remote_id"`
	OrderRemoteID string    `json:"order_remote_id"`
	Amount        string    `json:"amount"`
	Currency      string    `json:"currency"`
	Reason        string    `json:"reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (item RemoteReturn) Validate() error {
	if !validRemoteID(item.RemoteID) || !validRemoteID(item.OrderRemoteID) || !validUnsignedMoney(item.Amount) || !validCurrency(item.Currency) || !validOptionalWriteText(item.Reason, 2000) || item.CreatedAt.IsZero() || item.CreatedAt.Location() != time.UTC {
		return ErrInvalidReturnRead
	}
	return nil
}

type ReturnPage struct {
	Items      []RemoteReturn `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

func (page ReturnPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 || !utf8.ValidString(page.NextCursor) {
		return ErrInvalidReturnRead
	}
	seen := map[string]struct{}{}
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidReturnRead
		}
		if _, ok := seen[item.RemoteID]; ok {
			return ErrInvalidReturnRead
		}
		seen[item.RemoteID] = struct{}{}
	}
	return nil
}

type ReturnReader interface {
	ReadReturns(context.Context, Account, Runtime, ReturnQuery) (ReturnPage, error)
}
