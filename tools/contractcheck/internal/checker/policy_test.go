package checker

import (
	"context"
	"fmt"
	"testing"
)

func TestEventPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		catalog string
		schemas map[string]string
		wantErr string
	}{
		{
			name:    "valid",
			catalog: eventCatalogJSON("commerce.order.created.v1", "events/order-v1.schema.json"),
			schemas: map[string]string{"events/order-v1.schema.json": "commerce.order.created.v1"},
		},
		{
			name:    "version gap",
			catalog: eventCatalogJSON("commerce.order.created.v2", "events/order-v2.schema.json"),
			schemas: map[string]string{"events/order-v2.schema.json": "commerce.order.created.v2"},
			wantErr: "version gap before v2",
		},
		{
			name:    "filename mismatch",
			catalog: eventCatalogJSON("commerce.order.created.v2", "events/order-v1.schema.json"),
			schemas: map[string]string{"events/order-v1.schema.json": "commerce.order.created.v2"},
			wantErr: "filename version v1 does not match event type v2",
		},
		{
			name:    "orphan schema",
			catalog: eventCatalogJSON("commerce.order.created.v1", "events/order-v1.schema.json"),
			schemas: map[string]string{
				"events/order-v1.schema.json": "commerce.order.created.v1",
				"events/stock-v1.schema.json": "commerce.stock.changed.v1",
			},
			wantErr: "event schema is not registered",
		},
		{
			name:    "version overflow",
			catalog: eventCatalogJSON("commerce.order.created.v1000", "events/order-v1000.schema.json"),
			schemas: map[string]string{"events/order-v1000.schema.json": "commerce.order.created.v1000"},
			wantErr: "must not exceed 999",
		},
		{
			name:    "invalid grammar",
			catalog: eventCatalogJSON("Commerce.order.created.v1", "events/order-v1.schema.json"),
			schemas: map[string]string{"events/order-v1.schema.json": "Commerce.order.created.v1"},
			wantErr: "does not match canonical policy",
		},
		{
			name:    "unknown catalog field",
			catalog: `{"version":1,"events":[],"unexpected":true}`,
			schemas: map[string]string{},
			wantErr: "unknown field",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			files := make([]contractFile, 0, len(test.schemas))
			parsed := map[string]any{
				"events/event-catalog.json":         mustJSONValue(t, test.catalog),
				"events/event-envelope.schema.json": mustJSONValue(t, canonicalEnvelopeSchema),
			}
			for relative, title := range test.schemas {
				files = append(files, contractFile{Rel: relative})
				parsed[relative] = map[string]any{"title": title}
			}
			var problems diagnostics
			checkEvents(context.Background(), files, parsed, &problems)
			assertErrorContains(t, problems.err(), test.wantErr)
		})
	}
}

func TestEventCatalogMustBeSortedAndUnique(t *testing.T) {
	t.Parallel()
	catalog := `{
  "version":1,
  "events":[
    {"event_type":"commerce.stock.changed.v1","payload_schema":"events/stock-v1.schema.json"},
    {"event_type":"commerce.order.created.v1","payload_schema":"events/order-v1.schema.json"},
    {"event_type":"commerce.order.created.v1","payload_schema":"events/order-copy-v1.schema.json"}
  ]
}`
	parsed := map[string]any{
		"events/event-catalog.json":         mustJSONValue(t, catalog),
		"events/event-envelope.schema.json": mustJSONValue(t, canonicalEnvelopeSchema),
		"events/stock-v1.schema.json":       map[string]any{"title": "commerce.stock.changed.v1"},
		"events/order-v1.schema.json":       map[string]any{"title": "commerce.order.created.v1"},
		"events/order-copy-v1.schema.json":  map[string]any{"title": "commerce.order.created.v1"},
	}
	files := []contractFile{
		{Rel: "events/stock-v1.schema.json"},
		{Rel: "events/order-v1.schema.json"},
		{Rel: "events/order-copy-v1.schema.json"},
	}
	var problems diagnostics
	checkEvents(context.Background(), files, parsed, &problems)
	err := problems.err()
	assertErrorContains(t, err, "strictly sorted")
	assertErrorContains(t, err, "duplicate event type")
}

func TestEventTypeVersionBound(t *testing.T) {
	t.Parallel()
	if !eventTypeRE.MatchString("commerce.order.created.v999") {
		t.Fatal("v999 must remain valid")
	}
	if eventTypeRE.MatchString("commerce.order.created.v1000") {
		t.Fatal("v1000 must be rejected by the canonical pattern")
	}
}

func TestFixturePolicy(t *testing.T) {
	t.Parallel()
	files, parsed := schemaDocuments(t, map[string]string{
		"sample.schema.json": `{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "$id":"https://torgnexa.local/schemas/sample.json",
  "title":"Sample",
  "type":"object",
  "additionalProperties":false,
  "required":["value"],
  "properties":{"value":{"type":"string"}}
}`,
	})
	var schemaProblems diagnostics
	schemas := checkJSONSchemas(context.Background(), files, parsed, &schemaProblems)
	if err := schemaProblems.err(); err != nil {
		t.Fatalf("compile fixture schema: %v", err)
	}
	tests := []struct {
		name    string
		catalog string
		wantErr string
	}{
		{
			name:    "valid pair",
			catalog: `{"version":1,"cases":[{"schema":"sample.schema.json","valid":{"value":"ok"},"invalid":{"value":1}}]}`,
		},
		{
			name:    "invalid fixture accepted",
			catalog: `{"version":1,"cases":[{"schema":"sample.schema.json","valid":{"value":"ok"},"invalid":{"value":"also valid"}}]}`,
			wantErr: "invalid fixture was accepted",
		},
		{
			name:    "valid fixture rejected",
			catalog: `{"version":1,"cases":[{"schema":"sample.schema.json","valid":{"value":1},"invalid":{}}]}`,
			wantErr: "valid fixture was rejected",
		},
		{
			name:    "unknown field",
			catalog: `{"version":1,"cases":[],"unexpected":true}`,
			wantErr: "unknown field",
		},
		{
			name:    "path traversal",
			catalog: `{"version":1,"cases":[{"schema":"../sample.schema.json","valid":{},"invalid":{}}]}`,
			wantErr: "path traversal is forbidden",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixtureParsed := make(map[string]any, len(parsed)+1)
			for key, value := range parsed {
				fixtureParsed[key] = value
			}
			fixtureParsed["fixtures/schema-fixtures.json"] = mustJSONValue(t, test.catalog)
			var problems diagnostics
			checkFixtures(context.Background(), fixtureParsed, files, schemas, &problems)
			assertErrorContains(t, problems.err(), test.wantErr)
		})
	}
}

func eventCatalogJSON(eventType, schema string) string {
	return fmt.Sprintf(`{"version":1,"events":[{"event_type":%q,"payload_schema":%q}]}`, eventType, schema)
}

func mustJSONValue(t *testing.T, source string) any {
	t.Helper()
	value, err := parseStrictJSON([]byte(source))
	if err != nil {
		t.Fatalf("parse test JSON: %v", err)
	}
	return value
}

const canonicalEnvelopeSchema = `{
  "properties":{
    "event_type":{"pattern":"^[a-z][a-z0-9]*(_[a-z0-9]+)*\\.[a-z][a-z0-9]*(_[a-z0-9]+)*\\.[a-z][a-z0-9]*(_[a-z0-9]+)*\\.v[1-9][0-9]{0,2}$"}
  },
  "required":[
    "event_id","event_type","occurred_at","organization_id","workspace_id",
    "correlation_id","causation_id","entity_type","entity_id","source","data"
  ]
}`
