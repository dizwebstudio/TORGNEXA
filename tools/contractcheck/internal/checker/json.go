package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

const maxDocumentDepth = 128

const (
	maxJSONDocumentNodes = 100_000
	maxJSONTotalNodes    = 1_000_000
)

var (
	errJSONDocumentNodeLimit = errors.New("JSON document node limit exceeded")
	errJSONTotalNodeLimit    = errors.New("total JSON node limit exceeded")
)

func checkJSONSyntax(ctx context.Context, files []contractFile, problems *diagnostics) map[string]any {
	parsed := make(map[string]any, len(files))
	totalRemaining := maxJSONTotalNodes
	for _, file := range files {
		if !checkContext(ctx, problems) {
			break
		}
		value, err := parseStrictJSONWithBudget(ctx, file.Data, &totalRemaining)
		if err != nil {
			problems.add(file.Rel, "invalid JSON: %v", err)
			if errors.Is(err, errJSONTotalNodeLimit) {
				break
			}
			continue
		}
		parsed[file.Rel] = value
	}
	return parsed
}

func parseStrictJSON(data []byte) (any, error) {
	totalRemaining := maxJSONDocumentNodes
	return parseStrictJSONWithBudget(context.Background(), data, &totalRemaining)
}

func parseStrictJSONWithBudget(ctx context.Context, data []byte, totalRemaining *int) (any, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("input is not valid UTF-8")
	}
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if totalRemaining == nil {
		return nil, fmt.Errorf("total JSON node budget is required")
	}
	budget := jsonNodeBudget{
		ctx:               ctx,
		documentRemaining: maxJSONDocumentNodes,
		totalRemaining:    totalRemaining,
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := parseJSONValue(decoder, 0, &budget)
	if err != nil {
		return nil, err
	}
	if err := budget.checkContext(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, fmt.Errorf("trailing data: %w", err)
	}
	return value, nil
}

func decodeKnownFields(value, target any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return fmt.Errorf("trailing data: %w", err)
	}
	return nil
}

type jsonNodeBudget struct {
	ctx               context.Context
	documentRemaining int
	totalRemaining    *int
}

func (b *jsonNodeBudget) consume() error {
	if err := b.checkContext(); err != nil {
		return err
	}
	if b.documentRemaining <= 0 {
		return fmt.Errorf("%w: document exceeds %d nodes", errJSONDocumentNodeLimit, maxJSONDocumentNodes)
	}
	if *b.totalRemaining <= 0 {
		return fmt.Errorf("%w: contract corpus exceeds %d nodes", errJSONTotalNodeLimit, maxJSONTotalNodes)
	}
	b.documentRemaining--
	*b.totalRemaining = *b.totalRemaining - 1
	return nil
}

func (b *jsonNodeBudget) checkContext() error {
	if err := b.ctx.Err(); err != nil {
		return fmt.Errorf("validation interrupted: %w", err)
	}
	return nil
}

func parseJSONValue(decoder *json.Decoder, depth int, budget *jsonNodeBudget) (any, error) {
	if depth > maxDocumentDepth {
		return nil, fmt.Errorf("document nesting exceeds %d", maxDocumentDepth)
	}
	if err := budget.consume(); err != nil {
		return nil, err
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil
	}
	switch delim {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			if err := budget.consume(); err != nil {
				return nil, err
			}
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("duplicate object key %q", key)
			}
			value, err := parseJSONValue(decoder, depth+1, budget)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return nil, fmt.Errorf("unterminated object")
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			value, err := parseJSONValue(decoder, depth+1, budget)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim(']') {
			return nil, fmt.Errorf("unterminated array")
		}
		return array, nil
	default:
		return nil, fmt.Errorf("unexpected delimiter %q", delim)
	}
}
