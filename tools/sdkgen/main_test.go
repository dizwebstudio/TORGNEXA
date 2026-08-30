package main

import (
	"strings"
	"testing"
)

func TestParseSpecExtractsOperationsAndRejectsInternalModels(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.2.3
servers:
  - url: /api/v1
paths:
  /items/{item_id}:
    get:
      operationId: getItem
      parameters:
        - {name: item_id, in: path, required: true, schema: {type: string, minLength: 1}}
        - {$ref: '#/components/parameters/Cursor'}
        - {$ref: '#/components/parameters/IdempotencyKey'}
      responses: {'200': {description: ok}}
components:
  parameters:
    Cursor:
      name: cursor
    IdempotencyKey:
      name: Idempotency-Key
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || s.Operations[0].OperationID != "getItem" {
		t.Fatalf("unexpected operations: %#v", s.Operations)
	}
	if got := len(s.Operations[0].Parameters); got != 3 || s.Operations[0].Parameters[2].Location != "header" || s.Operations[0].Parameters[2].Name != "Idempotency-Key" {
		t.Fatalf("parameters=%d", got)
	}

	_, err = parseSpec([]byte(strings.Replace(input, "description: ok", "description: internal/database/sql", 1)))
	if err == nil {
		t.Fatal("expected internal model token rejection")
	}
}

func TestGeneratedArtifactsDoNotReferenceInternalDatabaseModels(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /health:
    get:
      operationId: getHealth
      responses: {'200': {description: ok}}
components: {}
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		lower := strings.ToLower(string(file.Data))
		for _, forbidden := range []string{"internal/", "database/sql", "pgx", "gorm", "sqlc"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("%s contains %q", file.Path, forbidden)
			}
		}
	}
}

func TestGeneratedClientsSendRequiredIdempotencyHeader(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /jobs:
    post:
      operationId: createJob
      parameters:
        - {$ref: '#/components/parameters/IdempotencyKey'}
      requestBody: {required: true}
      responses: {'202': {description: accepted}}
components:
  parameters:
    IdempotencyKey:
      name: Idempotency-Key
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	files, err := generate(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"sdk/go/torgnexa/client.gen.go", "sdk/typescript/src/client.gen.mjs", "sdk/python/torgnexa_sdk/client_gen.py"} {
		found := false
		for _, file := range files {
			if file.Path == path {
				found = strings.Contains(string(file.Data), "Idempotency-Key")
			}
		}
		if !found {
			t.Fatalf("%s does not bind Idempotency-Key", path)
		}
	}
}

func TestParseSpecPreservesActionSuffixInPath(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /connector-accounts:check:
    post:
      operationId: checkConnectorAccount
      requestBody: {required: true}
      responses: {'200': {description: ok}}
components: {}
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || s.Operations[0].Path != "/connector-accounts:check" || !s.Operations[0].HasBody {
		t.Fatalf("unexpected action operation: %#v", s.Operations)
	}
}

func TestParseSpecInheritsPathLevelParameters(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /products/{product_id}:
    parameters:
      - {name: product_id, in: path, required: true, schema: {type: string}}
    get:
      operationId: getProduct
      responses: {'200': {description: ok}}
    patch:
      operationId: updateProduct
      parameters:
        - {name: product_id, in: path, required: true, schema: {type: string}}
      requestBody: {required: true}
      responses: {'200': {description: ok}}
components: {}
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 2 {
		t.Fatalf("operations=%d", len(s.Operations))
	}
	for _, operation := range s.Operations {
		if len(operation.Parameters) != 1 || operation.Parameters[0].Name != "product_id" || operation.Parameters[0].Location != "path" {
			t.Fatalf("%s parameters=%#v", operation.OperationID, operation.Parameters)
		}
	}
}

func TestParseSpecInheritsCompactPathLevelParameter(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /inventory/positions/{position_id}:
    parameters: [{name: position_id, in: path, required: true, schema: {type: string}}]
    get:
      operationId: getInventoryPosition
      responses: {'200': {description: ok}}
components: {}
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || len(s.Operations[0].Parameters) != 1 || s.Operations[0].Parameters[0].Name != "position_id" {
		t.Fatalf("unexpected operation: %#v", s.Operations)
	}
}

// This is the exact shape `contracts/openapi/torgnexa-v1.yaml` used for every
// operation-level Idempotency-Key requirement (parameters as a single-line
// inline array containing one $ref, not the multi-line `- {$ref: ...}` list
// item form). It silently produced zero parameters until resolveParameterRef
// grew a matching regex, which meant 26 write operations shipped generated
// clients that never sent a header the backend requires and returned 400 for.
func TestParseSpecResolvesCompactInlineParameterRef(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /jobs:
    post:
      operationId: createJob
      parameters: [{$ref: '#/components/parameters/IdempotencyKey'}]
      requestBody: {required: true}
      responses: {'202': {description: accepted}}
components:
  parameters:
    IdempotencyKey:
      name: Idempotency-Key
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || len(s.Operations[0].Parameters) != 1 {
		t.Fatalf("unexpected operation: %#v", s.Operations)
	}
	p := s.Operations[0].Parameters[0]
	if p.Name != "Idempotency-Key" || p.Location != "header" || !p.Required {
		t.Fatalf("unexpected parameter: %#v", p)
	}
	files, err := generate(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"sdk/go/torgnexa/client.gen.go", "sdk/typescript/src/client.gen.mjs", "sdk/python/torgnexa_sdk/client_gen.py"} {
		found := false
		for _, file := range files {
			if file.Path == path {
				found = strings.Contains(string(file.Data), "Idempotency-Key")
			}
		}
		if !found {
			t.Fatalf("%s does not bind Idempotency-Key for the compact inline $ref form", path)
		}
	}
	for _, file := range files {
		if file.Path == "sdk/typescript/src/client.gen.d.mts" && !strings.Contains(string(file.Data), "idempotencyKey") {
			t.Fatalf("client.gen.d.mts does not type idempotencyKey for the compact inline $ref form")
		}
	}
}

func TestParseSpecResolvesSortableIDPathReference(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /returns/{return_id}:
    get:
      operationId: getReturn
      parameters:
        - {name: return_id, in: path, required: true, schema: {$ref: '#/components/schemas/SortableID'}}
      responses: {'200': {description: ok}}
components:
  schemas:
    SortableID:
      type: string
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || len(s.Operations[0].Parameters) != 1 || s.Operations[0].Parameters[0].Type != "string" {
		t.Fatalf("unexpected operation: %#v", s.Operations)
	}
	files, err := generate(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Path == "sdk/typescript/src/client.gen.mjs" {
			content := string(file.Data)
			if !strings.Contains(content, "input.returnId") || strings.Contains(content, `path.replace("{return_id}", encodeURIComponent(undefined))`) {
				t.Fatalf("typescript client does not bind SortableID path parameter: %s", content)
			}
		}
	}
}

// An optional parameter is legitimately written without a `required:` key at
// all in the source spec (omitted means false per OpenAPI 3.1), e.g. the
// `limit`/`base`/`quote` query parameters on listFXRates. The old regex
// mandated a literal `required: (true|false)` substring, so every one of
// these silently produced zero parameters and the query string was dropped.
func TestParseSpecAcceptsOptionalParameterWithoutRequiredKey(t *testing.T) {
	input := `openapi: 3.1.0
info:
  title: demo
  version: 1.0.0
servers:
  - url: /api/v1
paths:
  /fx/rates:
    get:
      operationId: listFXRates
      parameters:
        - {name: base, in: query, schema: {type: string}}
        - {name: limit, in: query, schema: {type: integer}}
      responses: {'200': {description: ok}}
components: {}
`
	s, err := parseSpec([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Operations) != 1 || len(s.Operations[0].Parameters) != 2 {
		t.Fatalf("unexpected operation: %#v", s.Operations)
	}
	for _, p := range s.Operations[0].Parameters {
		if p.Location != "query" || p.Required {
			t.Fatalf("unexpected parameter: %#v", p)
		}
	}
	files, err := generate(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Path == "sdk/typescript/src/client.gen.mjs" && !strings.Contains(string(file.Data), `query["base"]`) {
			t.Fatalf("client.gen.mjs does not forward the optional base query parameter")
		}
	}
}
