package magnitmarket

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
	Page        int    `json:"p,omitempty"`
	LastKey     int64  `json:"k,omitempty"`
	Token       string `json:"t,omitempty"`
	WindowFrom  string `json:"from,omitempty"`
	WindowTo    string `json:"to,omitempty"`
	Fingerprint string `json:"f"`
}

func makePageCursor(page int, fingerprint string) (string, error) {
	if page <= 0 {
		return "", nil
	}
	return encodeCursor(pageCursor{Version: 1, Page: page, Fingerprint: fingerprint})
}

func parsePageCursor(value, fingerprint string) (int, error) {
	cursor, err := decodeCursor(value, fingerprint)
	if err != nil || cursor.LastKey != 0 || cursor.Token != "" || cursor.WindowFrom != "" || cursor.WindowTo != "" || cursor.Page < 0 {
		return 0, ErrInvalidResponse
	}
	return cursor.Page, nil
}

func makeLastKeyCursor(lastKey int64, fingerprint string) (string, error) {
	if lastKey <= 0 {
		return "", nil
	}
	return encodeCursor(pageCursor{Version: 1, LastKey: lastKey, Fingerprint: fingerprint})
}

func parseLastKeyCursor(value, fingerprint string) (int64, error) {
	cursor, err := decodeCursor(value, fingerprint)
	if err != nil || cursor.Page != 0 || cursor.Token != "" || cursor.WindowFrom != "" || cursor.WindowTo != "" || cursor.LastKey < 0 {
		return 0, ErrInvalidResponse
	}
	return cursor.LastKey, nil
}

func makeOrderCursor(token string, from, to time.Time, fingerprint string) (string, error) {
	if token == "" {
		return "", nil
	}
	if !validTokenText(token) || from.IsZero() || to.IsZero() || !from.Before(to) {
		return "", ErrInvalidResponse
	}
	return encodeCursor(pageCursor{Version: 1, Token: token, WindowFrom: from.UTC().Format(time.RFC3339Nano), WindowTo: to.UTC().Format(time.RFC3339Nano), Fingerprint: fingerprint})
}

func parseOrderCursor(value, fingerprint string, now time.Time, windowDays int) (string, time.Time, time.Time, error) {
	if value == "" {
		to := now.UTC()
		from := to.Add(-time.Duration(windowDays) * 24 * time.Hour)
		return "", from, to, nil
	}
	cursor, err := decodeCursor(value, fingerprint)
	if err != nil || cursor.Page != 0 || cursor.LastKey != 0 || !validTokenText(cursor.Token) || cursor.WindowFrom == "" || cursor.WindowTo == "" {
		return "", time.Time{}, time.Time{}, ErrInvalidResponse
	}
	from, err := parseUTC(cursor.WindowFrom)
	if err != nil {
		return "", time.Time{}, time.Time{}, ErrInvalidResponse
	}
	to, err := parseUTC(cursor.WindowTo)
	if err != nil || !from.Before(to) || to.Sub(from) > 90*24*time.Hour {
		return "", time.Time{}, time.Time{}, ErrInvalidResponse
	}
	return cursor.Token, from, to, nil
}

func encodeCursor(cursor pageCursor) (string, error) {
	if len(cursor.Fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	body, err := json.Marshal(cursor)
	if err != nil {
		return "", ErrInvalidResponse
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func decodeCursor(value, fingerprint string) (pageCursor, error) {
	if value == "" {
		return pageCursor{Version: 1, Fingerprint: fingerprint}, nil
	}
	if len(value) > 4096 {
		return pageCursor{}, ErrInvalidResponse
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 3072 {
		return pageCursor{}, ErrInvalidResponse
	}
	var cursor pageCursor
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Version != 1 || cursor.Fingerprint != fingerprint {
		return pageCursor{}, ErrInvalidResponse
	}
	if decoder.Decode(&struct{}{}) == nil {
		return pageCursor{}, ErrInvalidResponse
	}
	if cursor.Token != "" && !validTokenText(cursor.Token) {
		return pageCursor{}, ErrInvalidResponse
	}
	return cursor, nil
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

func strictPositiveMoney(value string) bool {
	if value == "" || strings.ContainsAny(value, "eE+-") || len(value) > 28 || value[0] == '.' || value[len(value)-1] == '.' {
		return false
	}
	dots := 0
	digits := 0
	nonZero := false
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits++
			nonZero = nonZero || r != '0'
		case r == '.':
			dots++
			if dots > 1 {
				return false
			}
		default:
			return false
		}
	}
	return digits > 0 && nonZero
}

func validCurrency(value string) bool {
	if len(value) < 3 || len(value) > 8 || value != strings.ToUpper(value) {
		return false
	}
	for _, r := range value {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func digestStrings(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func int64String(value int64) string { return strconv.FormatInt(value, 10) }
