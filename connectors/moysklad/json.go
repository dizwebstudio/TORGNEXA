package moysklad

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

type metaEnvelope struct {
	Size     int    `json:"size"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
	NextHref string `json:"nextHref,omitempty"`
}
type listEnvelope struct {
	Meta metaEnvelope      `json:"meta"`
	Rows []json.RawMessage `json:"rows"`
}

func decodeListEnvelope(body []byte, expectedLimit, expectedOffset int) (listEnvelope, error) {
	if len(body) == 0 || len(body) > maxBodyBytes {
		return listEnvelope{}, ErrInvalidResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var envelope listEnvelope
	if err := decoder.Decode(&envelope); err != nil || decoder.More() || envelope.Meta.Size < 0 || envelope.Meta.Limit != expectedLimit || envelope.Meta.Offset != expectedOffset || len(envelope.Rows) > expectedLimit || envelope.Meta.Size < expectedOffset+len(envelope.Rows) {
		return listEnvelope{}, ErrInvalidResponse
	}
	var extra any
	if decoder.Decode(&extra) == nil {
		return listEnvelope{}, ErrInvalidResponse
	}
	return envelope, nil
}

type cursor struct {
	Version     int    `json:"v"`
	Offset      int    `json:"offset"`
	Row         int    `json:"row,omitempty"`
	Inner       int    `json:"inner,omitempty"`
	Fingerprint string `json:"fp"`
}

func fingerprint(surface string) string {
	d := sha256.Sum256([]byte("moysklad\x00" + surface + "\x00v1"))
	return hex.EncodeToString(d[:])
}
func makeCursor(c cursor) (string, error) {
	c.Version = 1
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func parseCursor(value, fp string) (cursor, error) {
	if value == "" {
		return cursor{Version: 1, Fingerprint: fp}, nil
	}
	if len(value) > 4096 {
		return cursor{}, ErrInvalidResponse
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) > 1024 {
		return cursor{}, ErrInvalidResponse
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c cursor
	if dec.Decode(&c) != nil {
		return cursor{}, ErrInvalidResponse
	}
	var extra any
	if dec.Decode(&extra) == nil {
		return cursor{}, ErrInvalidResponse
	}
	if c.Version != 1 || c.Fingerprint != fp || c.Offset < 0 || c.Offset > 100000000 || c.Row < 0 || c.Row > 1000 || c.Inner < 0 || c.Inner > 10000 {
		return cursor{}, ErrInvalidResponse
	}
	return c, nil
}
func pageQuery(limit, offset int, extra ...QueryParam) []QueryParam {
	q := []QueryParam{{"limit", strconv.Itoa(limit)}, {"offset", strconv.Itoa(offset)}}
	return append(q, extra...)
}

func decodeObject(raw json.RawMessage, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(target) != nil {
		return ErrInvalidResponse
	}
	var extra any
	if d.Decode(&extra) == nil {
		return ErrInvalidResponse
	}
	return nil
}

func safeText(value string, max int, required bool) bool {
	if !utf8.ValidString(value) || value != strings.TrimSpace(value) || len(value) > max || (required && value == "") {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func exactJSONDecimal(n json.Number) (string, error) {
	s := n.String()
	if s == "" || strings.ContainsAny(s, "eE+") {
		return "", ErrInvalidResponse
	}
	if !validDecimalString(s) {
		return "", ErrInvalidResponse
	}
	return s, nil
}
func validDecimalString(s string) bool {
	if len(s) > 40 || s == "" {
		return false
	}
	if s[0] == '-' {
		s = s[1:]
		if s == "" {
			return false
		}
	}
	dot := false
	digits := 0
	for _, r := range s {
		if r == '.' && !dot {
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
		digits++
	}
	return digits > 0 && !strings.HasPrefix(s, ".") && !strings.HasSuffix(s, ".")
}

const entityPrefix = "https://api.moysklad.ru/api/remap/1.2/entity/"

func idFromMetaHref(href, kind string) (string, error) {
	prefix := entityPrefix + kind + "/"
	if !strings.HasPrefix(href, prefix) {
		return "", ErrInvalidResponse
	}
	rest := strings.TrimPrefix(href, prefix)
	if i := strings.IndexByte(rest, '?'); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" || strings.ContainsAny(rest, "/#") || !safeText(rest, 128, true) {
		return "", ErrInvalidResponse
	}
	return rest, nil
}
