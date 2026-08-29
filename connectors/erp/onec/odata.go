package onec

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"
)

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,9})?$`)

const (
	maxPageLimit = 200
	maxOffset    = 10_000_000
)

type cursorPayload struct {
	Version     int    `json:"v"`
	Offset      int    `json:"offset"`
	Fingerprint string `json:"fingerprint"`
}

func parseCursor(cursor, fingerprint string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if len(cursor) > 4096 {
		return 0, ErrInvalidResponse
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) > 1024 {
		return 0, ErrInvalidResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var payload cursorPayload
	if err := decoder.Decode(&payload); err != nil || payload.Version != 1 || payload.Offset < 1 || payload.Offset > maxOffset || payload.Fingerprint != fingerprint {
		return 0, ErrInvalidResponse
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return 0, ErrInvalidResponse
	}
	return payload.Offset, nil
}

func makeCursor(offset int, fingerprint string) (string, error) {
	if offset < 1 || offset > maxOffset || len(fingerprint) != 64 {
		return "", ErrInvalidResponse
	}
	raw, err := json.Marshal(cursorPayload{Version: 1, Offset: offset, Fingerprint: fingerprint})
	if err != nil {
		return "", ErrInvalidResponse
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

type odataEnvelope struct {
	Value []map[string]json.RawMessage
}

func decodeEnvelope(body []byte, maxItems int) (odataEnvelope, error) {
	if len(body) == 0 || len(body) > maxBodyBytes || maxItems < 1 || maxItems > maxPageLimit || !json.Valid(body) {
		return odataEnvelope{}, ErrInvalidResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var top map[string]json.RawMessage
	if err := decoder.Decode(&top); err != nil {
		return odataEnvelope{}, ErrInvalidResponse
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return odataEnvelope{}, ErrInvalidResponse
	}
	raw, ok := top["value"]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return odataEnvelope{}, ErrInvalidResponse
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || len(rows) > maxItems {
		return odataEnvelope{}, ErrInvalidResponse
	}
	return odataEnvelope{Value: rows}, nil
}

func requiredString(row map[string]json.RawMessage, field string, max int) (string, error) {
	raw, ok := row[field]
	if !ok || !utf8.Valid(raw) {
		return "", ErrInvalidResponse
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" || value != strings.TrimSpace(value) || len([]rune(value)) > max {
		return "", ErrInvalidResponse
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", ErrInvalidResponse
		}
	}
	return value, nil
}

func optionalString(row map[string]json.RawMessage, field string, max int) (string, error) {
	if field == "" {
		return "", nil
	}
	raw, ok := row[field]
	if !ok || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	if !utf8.Valid(raw) {
		return "", ErrInvalidResponse
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrInvalidResponse
	}
	if value == "" {
		return "", nil
	}
	if value != strings.TrimSpace(value) || len([]rune(value)) > max {
		return "", ErrInvalidResponse
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", ErrInvalidResponse
		}
	}
	return value, nil
}

func requiredBool(row map[string]json.RawMessage, field string) (bool, error) {
	raw, ok := row[field]
	if !ok {
		return false, ErrInvalidResponse
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return false, ErrInvalidResponse
	}
	return value, nil
}

func exactDecimal(row map[string]json.RawMessage, field string) (string, error) {
	raw, ok := row[field]
	if !ok || len(raw) == 0 || len(raw) > 64 {
		return "", ErrInvalidResponse
	}
	var value string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &value) != nil {
			return "", ErrInvalidResponse
		}
	} else {
		value = string(raw)
	}
	if !decimalPattern.MatchString(value) {
		return "", ErrInvalidResponse
	}
	return value, nil
}

func joinFields(values ...string) string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return strings.Join(result, ",")
}

func pageQuery(limit, offset int, selectFields, orderBy string) []QueryParam {
	return []QueryParam{
		{Name: "$format", Value: "json"},
		{Name: "$select", Value: selectFields},
		{Name: "$orderby", Value: orderBy},
		{Name: "$skip", Value: fmt.Sprintf("%d", offset)},
		{Name: "$top", Value: fmt.Sprintf("%d", limit)},
	}
}
