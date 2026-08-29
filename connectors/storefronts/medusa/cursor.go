package medusa

import (
	"encoding/base64"
	"encoding/json"
)

type pageCursor struct {
	Offset      int    `json:"offset"`
	Fingerprint string `json:"fingerprint"`
}

func decodePageCursor(value, fingerprint string) (int, error) {
	if value == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 512 {
		return 0, ErrInvalidResponse
	}
	var cursor pageCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Offset < 0 || cursor.Offset > 10_000_000 || cursor.Fingerprint != fingerprint {
		return 0, ErrInvalidResponse
	}
	return cursor.Offset, nil
}
func encodePageCursor(offset int, fingerprint string) (string, error) {
	if offset < 0 || offset > 10_000_000 || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(pageCursor{Offset: offset, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// nextCursor follows Medusa's own offset/limit/count pagination contract
// (the body carries "count", not a page-number or Link-header cursor).
func nextCursor(offset, limit, count int, fingerprint string) (string, error) {
	if offset+limit >= count {
		return "", nil
	}
	return encodePageCursor(offset+limit, fingerprint)
}
