package releasecheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const (
	maxJSONDepth = 128
	maxJSONNodes = 200_000
)

func decodeStrictJSON(ctx context.Context, data []byte, target any) error {
	if target == nil {
		return fmt.Errorf("JSON target is required")
	}
	if err := validateJSONSyntax(ctx, data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := expectJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func decodeJSONValue(ctx context.Context, data []byte) (any, error) {
	if err := validateJSONSyntax(ctx, data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := expectJSONEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func validateJSONSyntax(ctx context.Context, data []byte) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("input is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	if err := consumeJSONValue(ctx, decoder, 0, &nodes); err != nil {
		return err
	}
	return expectJSONEOF(decoder)
}

func consumeJSONValue(ctx context.Context, decoder *json.Decoder, depth int, nodes *int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validation interrupted: %w", err)
	}
	if depth > maxJSONDepth {
		return fmt.Errorf("JSON nesting exceeds %d", maxJSONDepth)
	}
	(*nodes)++
	if *nodes > maxJSONNodes {
		return fmt.Errorf("JSON document exceeds %d nodes", maxJSONNodes)
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeJSONValue(ctx, decoder, depth+1, nodes); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(ctx, decoder, depth+1, nodes); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return fmt.Errorf("unterminated JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func expectJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}
