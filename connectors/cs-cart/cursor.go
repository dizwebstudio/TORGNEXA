package cscart

import (
	"encoding/base64"
	"encoding/json"
)

type pageCursor struct {
	Page        int    `json:"page"`
	Fingerprint string `json:"fingerprint"`
}

func decodePageCursor(value, fingerprint string) (int, error) {
	if value == "" {
		return 1, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 512 {
		return 0, ErrInvalidResponse
	}
	var cursor pageCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Page < 1 || cursor.Page > 1_000_000 || cursor.Fingerprint != fingerprint {
		return 0, ErrInvalidResponse
	}
	return cursor.Page, nil
}

func encodePageCursor(page int, fingerprint string) (string, error) {
	if page < 1 || page > 1_000_000 || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(pageCursor{Page: page, Fingerprint: fingerprint})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
