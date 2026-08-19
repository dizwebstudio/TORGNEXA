package architecture

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func decodeStrictJSON(ctx context.Context, data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	if err := validateJSONValue(ctx, decoder, 0, &nodes); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are forbidden")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}

	known := json.NewDecoder(bytes.NewReader(data))
	known.DisallowUnknownFields()
	if err := known.Decode(target); err != nil {
		return fmt.Errorf("decode known fields: %w", err)
	}
	if err := known.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are forbidden")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	return nil
}

func validateJSONValue(ctx context.Context, decoder *json.Decoder, depth int, nodes *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxJSONDepth {
		return fmt.Errorf("JSON exceeds maximum depth %d", maxJSONDepth)
	}
	*nodes++
	if *nodes > maxJSONNodes {
		return fmt.Errorf("JSON exceeds maximum node count %d", maxJSONNodes)
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON token: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			if err := ctx.Err(); err != nil {
				return err
			}
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode JSON object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			*nodes++
			if *nodes > maxJSONNodes {
				return fmt.Errorf("JSON exceeds maximum node count %d", maxJSONNodes)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(ctx, decoder, depth+1, nodes); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return fmt.Errorf("unterminated JSON object")
		}
	case '[':
		for decoder.More() {
			if err := validateJSONValue(ctx, decoder, depth+1, nodes); err != nil {
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
