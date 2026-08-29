package aliexpressru

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

type productCursor struct {
	Version       int    `json:"v"`
	LastProductID string `json:"last_product_id"`
	Fingerprint   string `json:"f"`
}

func productFingerprint() string {
	digest := sha256.Sum256([]byte(apiHost + "\x00" + productsPath + "\x00products-v1"))
	return hex.EncodeToString(digest[:])
}

func makeProductCursor(lastProductID string) (string, error) {
	if lastProductID == "" {
		return "", nil
	}
	if !validRemoteText(lastProductID, 128) {
		return "", ErrInvalidResponse
	}
	body, err := json.Marshal(productCursor{Version: 1, LastProductID: lastProductID, Fingerprint: productFingerprint()})
	if err != nil {
		return "", ErrInvalidResponse
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func parseProductCursor(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if len(value) > 4096 {
		return "", ErrInvalidResponse
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 3072 {
		return "", ErrInvalidResponse
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var cursor productCursor
	if decoder.Decode(&cursor) != nil || cursor.Version != 1 || cursor.Fingerprint != productFingerprint() || !validRemoteText(cursor.LastProductID, 128) {
		return "", ErrInvalidResponse
	}
	if decoder.Decode(&struct{}{}) == nil {
		return "", ErrInvalidResponse
	}
	return cursor.LastProductID, nil
}

func decodeUseNumber(body []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) == nil {
		return ErrInvalidResponse
	}
	return nil
}

func validRemoteText(value string, max int) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func parseUTC(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidResponse
	}
	return parsed.UTC(), nil
}
