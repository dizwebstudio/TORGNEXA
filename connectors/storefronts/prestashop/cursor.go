package prestashop

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
	var c pageCursor
	if json.Unmarshal(raw, &c) != nil || c.Offset < 0 || c.Offset > 100000000 || c.Fingerprint != fingerprint {
		return 0, ErrInvalidResponse
	}
	return c.Offset, nil
}
func encodePageCursor(offset int, fingerprint string) (string, error) {
	if offset < 0 || offset > 100000000 || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(pageCursor{Offset: offset, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func nextCursor(offset, limit, count int, fingerprint string) (string, error) {
	if count < limit {
		return "", nil
	}
	return encodePageCursor(offset+limit, fingerprint)
}
