package bitrix24

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

var errCursor = errors.New("bitrix24: invalid cursor")

type pageCursor struct {
	Start       int    `json:"s"`
	Skip        int    `json:"k"`
	Fingerprint string `json:"f"`
}

func decodeCursor(raw, fp string) (pageCursor, error) {
	if raw == "" {
		return pageCursor{Fingerprint: fp}, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || len(b) > 2048 {
		return pageCursor{}, errCursor
	}
	var c pageCursor
	if json.Unmarshal(b, &c) != nil || c.Start < 0 || c.Start%50 != 0 || c.Skip < 0 || c.Skip >= 50 || c.Fingerprint != fp {
		return pageCursor{}, errCursor
	}
	return c, nil
}
func encodeCursor(c pageCursor) (string, error) {
	if c.Start < 0 || c.Start%50 != 0 || c.Skip < 0 || c.Skip >= 50 || len(c.Fingerprint) != 64 {
		return "", errCursor
	}
	b, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func nextCursor(start, skip, count, total, limit int, fp string) (string, error) {
	used := skip + count
	if count == 0 || used >= 50 || start+used >= total {
		if start+50 >= total {
			return "", nil
		}
		return encodeCursor(pageCursor{Start: start + 50, Fingerprint: fp})
	}
	if count < limit {
		return "", nil
	}
	return encodeCursor(pageCursor{Start: start, Skip: used, Fingerprint: fp})
}
