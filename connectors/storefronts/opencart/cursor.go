package opencart

import (
	"encoding/base64"
	"encoding/json"
)

type pageCursor struct {
	Page        int    `json:"page"`
	Fingerprint string `json:"fingerprint"`
}

func decodePageCursor(v, fp string) (int, error) {
	if v == "" {
		return 1, nil
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) > 512 {
		return 0, ErrInvalidResponse
	}
	var c pageCursor
	if json.Unmarshal(raw, &c) != nil || c.Page < 1 || c.Page > 100000 || c.Fingerprint != fp {
		return 0, ErrInvalidResponse
	}
	return c.Page, nil
}
func encodePageCursor(page int, fp string) (string, error) {
	raw, e := json.Marshal(pageCursor{Page: page, Fingerprint: fp})
	if e != nil || page < 1 || len(fp) != 64 {
		return "", ErrInvalidResponse
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func nextCursor(page, total int, fp string) (string, error) {
	if total <= page {
		return "", nil
	}
	return encodePageCursor(page+1, fp)
}
