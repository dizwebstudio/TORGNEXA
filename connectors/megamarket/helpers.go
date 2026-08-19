package megamarket

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
	Offset      int    `json:"o,omitempty"`
	Fingerprint string `json:"f"`
}

func makeTokenCursor(token, fp string) (string, error) {
	if token == "" {
		return "", nil
	}
	if !validTokenText(token) || len(fp) != 64 {
		return "", ErrInvalidResponse
	}
	b, _ := json.Marshal(pageCursor{Version: 1, Token: token, Fingerprint: fp})
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func parseTokenCursor(value, fp string) (string, error) {
	c, err := decodeCursor(value, fp)
	if err != nil {
		return "", err
	}
	return c.Token, nil
}
func makeOffsetCursor(offset int, fp string) (string, error) {
	if offset <= 0 {
		return "", nil
	}
	b, _ := json.Marshal(pageCursor{Version: 1, Offset: offset, Fingerprint: fp})
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func parseOffsetCursor(value, fp string) (int, error) {
	c, err := decodeCursor(value, fp)
	if err != nil {
		return 0, err
	}
	if c.Token != "" || c.Offset < 0 {
		return 0, ErrInvalidResponse
	}
	return c.Offset, nil
}
func decodeCursor(value, fp string) (pageCursor, error) {
	if value == "" {
		return pageCursor{Version: 1, Fingerprint: fp}, nil
	}
	if len(value) > 4096 {
		return pageCursor{}, ErrInvalidResponse
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 3072 {
		return pageCursor{}, ErrInvalidResponse
	}
	var c pageCursor
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || c.Version != 1 || c.Fingerprint != fp {
		return pageCursor{}, ErrInvalidResponse
	}
	if dec.Decode(&struct{}{}) == nil {
		return pageCursor{}, ErrInvalidResponse
	}
	if c.Token != "" && !validTokenText(c.Token) {
		return pageCursor{}, ErrInvalidResponse
	}
	return c, nil
}
func validText(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validOptionalText(v string, max int) bool { return v == "" || validText(v, max) }
func validTokenText(v string) bool             { return validText(v, 2048) }
func parseUTC(v string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil {
		return time.Time{}, ErrInvalidResponse
	}
	return t.UTC(), nil
}
func digestStrings(v ...string) string {
	d := sha256.Sum256([]byte(strings.Join(v, "\x00")))
	return hex.EncodeToString(d[:])
}
func intString(v int) string { return strconv.Itoa(v) }
