package saleor

import (
	"encoding/base64"
	"encoding/json"
)

// relayCursor wraps Saleor's own opaque Relay "endCursor" string (returned
// by a *CountableConnection field's pageInfo) with a fingerprint binding so
// a cursor minted for one account/surface cannot be replayed against
// another, the same integrity envelope every other connector in this
// repository already applies to its own pagination state.
type relayCursor struct {
	After       string `json:"after"`
	Fingerprint string `json:"fingerprint"`
}

func decodeRelayCursor(value, fingerprint string) (string, error) {
	if value == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 1024 {
		return "", ErrInvalidResponse
	}
	var cursor relayCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Fingerprint != fingerprint || cursor.After == "" {
		return "", ErrInvalidResponse
	}
	return cursor.After, nil
}

func encodeRelayCursor(after, fingerprint string) (string, error) {
	if after == "" || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(relayCursor{After: after, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// nextRelayCursor follows Saleor's own connection contract: only mint a
// continuation when the server actually reported one more page.
func nextRelayCursor(hasNextPage bool, endCursor, fingerprint string) (string, error) {
	if !hasNextPage || endCursor == "" {
		return "", nil
	}
	return encodeRelayCursor(endCursor, fingerprint)
}

// offsetCursor paginates a plain (non-connection) list field that Saleor
// returns in full on every request -- Order.grantedRefunds has no first/
// after arguments of its own, unlike every *CountableConnection field this
// connector otherwise walks -- by windowing the already-fetched slice
// in-memory, the same integer-offset shape Magento's page cursor uses.
type offsetCursor struct {
	Offset      int    `json:"offset"`
	Fingerprint string `json:"fingerprint"`
}

func decodeOffsetCursor(value, fingerprint string) (int, error) {
	if value == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 512 {
		return 0, ErrInvalidResponse
	}
	var cursor offsetCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Offset < 1 || cursor.Offset > 1_000_000 || cursor.Fingerprint != fingerprint {
		return 0, ErrInvalidResponse
	}
	return cursor.Offset, nil
}

func encodeOffsetCursor(offset int, fingerprint string) (string, error) {
	if offset < 1 || offset > 1_000_000 || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(offsetCursor{Offset: offset, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func nextOffsetCursor(offset, pageSize, total int, fingerprint string) (string, error) {
	if offset+pageSize >= total {
		return "", nil
	}
	return encodeOffsetCursor(offset+pageSize, fingerprint)
}
