package checker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type fixtureCatalog struct {
	Version int           `json:"version"`
	Cases   []fixtureCase `json:"cases"`
}

type fixtureCase struct {
	Schema  string          `json:"schema"`
	Valid   json.RawMessage `json:"valid"`
	Invalid json.RawMessage `json:"invalid"`
}

func checkFixtures(ctx context.Context, parsed map[string]any, schemaFiles []contractFile, schemas map[string]*jsonschema.Schema, problems *diagnostics) {
	const fixturePath = "fixtures/schema-fixtures.json"
	value, exists := parsed[fixturePath]
	if !exists {
		problems.add(fixturePath, "schema fixture catalog is required")
		return
	}
	var catalog fixtureCatalog
	if err := decodeKnownFields(value, &catalog); err != nil {
		problems.add(fixturePath, "decode fixtures: %v", err)
		return
	}
	if catalog.Version != 1 {
		problems.add(fixturePath, "version must be 1")
	}
	seen := make(map[string]struct{}, len(catalog.Cases))
	previous := ""
	for _, fixture := range catalog.Cases {
		if !checkContext(ctx, problems) {
			return
		}
		cleaned, err := safeContractPath(fixture.Schema)
		if err != nil {
			problems.add(fixturePath, "schema %q: %v", fixture.Schema, err)
			continue
		}
		if previous != "" && cleaned <= previous {
			problems.add(fixturePath, "cases must be strictly sorted by schema")
		}
		previous = cleaned
		if _, duplicate := seen[cleaned]; duplicate {
			problems.add(fixturePath, "duplicate fixture case for %q", cleaned)
		}
		seen[cleaned] = struct{}{}
		schema := schemas[cleaned]
		if schema == nil {
			problems.add(fixturePath, "fixture references uncompiled schema %q", cleaned)
			continue
		}
		valid, err := decodeFixture(fixture.Valid)
		if err != nil {
			problems.add(fixturePath, "%s valid fixture: %v", cleaned, err)
		} else if err := schema.Validate(valid); err != nil {
			problems.add(fixturePath, "%s valid fixture was rejected: %v", cleaned, err)
		}
		invalid, err := decodeFixture(fixture.Invalid)
		if err != nil {
			problems.add(fixturePath, "%s invalid fixture is not JSON: %v", cleaned, err)
		} else if err := schema.Validate(invalid); err == nil {
			problems.add(fixturePath, "%s invalid fixture was accepted", cleaned)
		}
	}
	var expected []string
	for _, file := range schemaFiles {
		expected = append(expected, file.Rel)
	}
	sort.Strings(expected)
	for _, relative := range expected {
		if _, ok := seen[relative]; !ok {
			problems.add(fixturePath, "schema %q has no valid/invalid fixture case", relative)
		}
	}
}

func decodeFixture(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("fixture is missing")
	}
	if _, err := parseStrictJSON(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
