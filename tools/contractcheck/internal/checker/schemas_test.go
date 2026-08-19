package checker

import (
	"context"
	"strings"
	"testing"
)

func TestJSONSchemasCompileOfflineReferencesAndFormats(t *testing.T) {
	t.Parallel()
	files, parsed := schemaDocuments(t, map[string]string{
		"models/a.schema.json": `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/models/a.json",
  "title":"A",
  "type":"object",
  "required":["child","occurred_at"],
  "properties":{
    "child":{"$ref":"b.json"},
    "occurred_at":{"type":"string","format":"date-time"}
  }
}`,
		"models/b.schema.json": `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/models/b.json",
  "title":"B",
  "type":"string",
  "minLength":1
}`,
	})
	var problems diagnostics
	schemas := checkJSONSchemas(context.Background(), files, parsed, &problems)
	if err := problems.err(); err != nil {
		t.Fatalf("compile schemas: %v", err)
	}
	schema := schemas["models/a.schema.json"]
	if schema == nil {
		t.Fatal("compiled schema is missing")
	}
	if err := schema.Validate(map[string]any{"child": "ok", "occurred_at": "not-a-date"}); err == nil {
		t.Fatal("format assertions were not enforced")
	}
}

func TestJSONSchemasRejectUnsafeIdentityAndReferences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{
			name: "external reference",
			doc: `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/sample.json",
  "title":"Sample",
  "$ref":"https://example.invalid/remote.json"
}`,
			wantErr: "not a registered repository schema",
		},
		{
			name: "file reference",
			doc: `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/sample.json",
  "title":"Sample",
  "$ref":"file:///tmp/remote.json"
}`,
			wantErr: "not a registered repository schema",
		},
		{
			name: "nested ID",
			doc: `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/sample.json",
  "title":"Sample",
  "$defs":{"nested":{"$id":"https://example.invalid/nested.json","type":"string"}}
}`,
			wantErr: "nested $id values are forbidden",
		},
		{
			name: "wrong ID",
			doc: `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/wrong.json",
  "title":"Sample",
  "type":"string"
}`,
			wantErr: `$id must be "https://torgnexa.local/schemas/sample.json"`,
		},
		{
			name: "missing title",
			doc: `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/sample.json",
  "type":"string"
}`,
			wantErr: "title must be a non-empty string",
		},
		{
			name: "malformed schema keyword",
			doc: `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/sample.json",
  "title":"Sample",
  "type":"object",
  "required":"value"
}`,
			wantErr: "compile JSON Schema",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			files, parsed := schemaDocuments(t, map[string]string{"sample.schema.json": test.doc})
			var problems diagnostics
			checkJSONSchemas(context.Background(), files, parsed, &problems)
			assertErrorContains(t, problems.err(), test.wantErr)
		})
	}
}

func TestValidateSchemaRef(t *testing.T) {
	t.Parallel()
	const current = "https://torgnexa.local/schemas/models/a.json"
	known := map[string]string{
		current: "models/a.schema.json",
		"https://torgnexa.local/schemas/models/b.json": "models/b.schema.json",
	}
	tests := []struct {
		reference string
		wantErr   string
	}{
		{reference: "#/$defs/value"},
		{reference: "b.json#/$defs/value"},
		{reference: "https://torgnexa.local/schemas/models/b.json"},
		{reference: "https://example.invalid/schema.json", wantErr: "not a registered"},
		{reference: "file:///tmp/schema.json", wantErr: "not a registered"},
		{reference: "../outside.json", wantErr: "not a registered"},
	}
	for _, test := range tests {
		t.Run(test.reference, func(t *testing.T) {
			t.Parallel()
			assertErrorContains(t, validateSchemaRef(current, test.reference, known), test.wantErr)
		})
	}
}

func TestSchemaIDKeepsPublishedEnvelopeIdentifier(t *testing.T) {
	t.Parallel()
	if got := expectedSchemaID("events/event-envelope.schema.json"); got != "https://torgnexa.local/schemas/event-envelope.json" {
		t.Fatalf("published envelope ID changed: %s", got)
	}
}

func TestSafeContractPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path    string
		wantErr bool
	}{
		{path: "events/order-v1.schema.json"},
		{path: "", wantErr: true},
		{path: "/absolute", wantErr: true},
		{path: "../outside", wantErr: true},
		{path: "events/../outside", wantErr: true},
		{path: `events\outside`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(strings.ReplaceAll(test.path, "/", "_"), func(t *testing.T) {
			t.Parallel()
			_, err := safeContractPath(test.path)
			if (err != nil) != test.wantErr {
				t.Fatalf("safeContractPath(%q) error = %v, wantErr %v", test.path, err, test.wantErr)
			}
		})
	}
}

func schemaDocuments(t *testing.T, documents map[string]string) ([]contractFile, map[string]any) {
	t.Helper()
	files := make([]contractFile, 0, len(documents))
	parsed := make(map[string]any, len(documents))
	for relative, source := range documents {
		data := []byte(source)
		value, err := parseStrictJSON(data)
		if err != nil {
			t.Fatalf("parse %s: %v", relative, err)
		}
		files = append(files, contractFile{Rel: relative, Data: data})
		parsed[relative] = value
	}
	return files, parsed
}
