package yandexmarket

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type pageCursor struct {
	Version     int    `json:"v"`
	Token       string `json:"t,omitempty"`
	Fingerprint string `json:"f"`
}

func makeCursor(token, fingerprint string) (string, error) {
	if token == "" {
		return "", nil
	}
	if !validTokenText(token) || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	body, err := json.Marshal(pageCursor{Version: 1, Token: token, Fingerprint: fingerprint})
	if err != nil {
		return "", ErrInvalidResponse
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func parseCursor(value, fingerprint string) (string, error) {
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
	var cursor pageCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Version != 1 || cursor.Fingerprint != fingerprint || !validTokenText(cursor.Token) {
		return "", ErrInvalidResponse
	}
	if decoder.Decode(&struct{}{}) == nil {
		return "", ErrInvalidResponse
	}
	return cursor.Token, nil
}

func businessPath(id int64, suffix string) string {
	return "/v2/businesses/" + strconv.FormatInt(id, 10) + suffix
}
func businessV1Path(id int64, suffix string) string {
	return "/v1/businesses/" + strconv.FormatInt(id, 10) + suffix
}
func businessV3Path(id int64, suffix string) string {
	return "/v3/businesses/" + strconv.FormatInt(id, 10) + suffix
}
func campaignPath(id int64, suffix string) string {
	return "/v2/campaigns/" + strconv.FormatInt(id, 10) + suffix
}
func intString(value int) string     { return strconv.Itoa(value) }
func int64String(value int64) string { return strconv.FormatInt(value, 10) }

func validText(value string, max int) bool {
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

func validOptionalText(value string, max int) bool { return value == "" || validText(value, max) }
func validTokenText(value string) bool             { return validText(value, 2048) }

func parseUTC(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidResponse
	}
	return parsed.UTC(), nil
}

func digestStrings(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}
