package checker

import (
	"context"
	"testing"

	"gopkg.in/yaml.v3"
)

const minimalOpenAPI = `openapi: 3.1.0
info:
  title: Test API
  version: 1.0.0
paths:
  /health:
    get:
      operationId: getHealth
      responses:
        "200":
          description: healthy
`

func TestOpenAPIValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "valid", source: minimalOpenAPI},
		{
			name: "wrong version",
			source: `openapi: 3.0.3
info: {title: Test API, version: 1.0.0}
paths: {}
`,
			wantErr: "OpenAPI version must be 3.1.0",
		},
		{
			name: "external reference",
			source: minimalOpenAPI + `components:
  schemas:
    Remote:
      $ref: https://example.invalid/schema.yaml
`,
			wantErr: "must be an internal fragment",
		},
		{
			name: "dynamic reference rejected",
			source: minimalOpenAPI + `components:
  schemas:
    Remote:
      $dynamicRef: https://example.invalid/schema.yaml
`,
			wantErr: "OpenAPI $dynamicRef is forbidden",
		},
		{
			name: "broken internal reference",
			source: minimalOpenAPI + `components:
  schemas:
    Broken:
      $ref: '#/components/schemas/Missing'
`,
			wantErr: "failed to resolve",
		},
		{
			name: "duplicate operation ID",
			source: minimalOpenAPI + `  /ready:
    get:
      operationId: getHealth
      responses:
        "200":
          description: ready
`,
			wantErr: "operationId \"getHealth\" is duplicated",
		},
		{
			name: "missing operation ID",
			source: `openapi: 3.1.0
info: {title: Test API, version: 1.0.0}
paths:
  /health:
    get:
      responses:
        "200": {description: healthy}
`,
			wantErr: "has no operationId",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := []byte(test.source)
			node, err := parseStrictYAML(data)
			if err != nil {
				t.Fatalf("parse YAML: %v", err)
			}
			file := contractFile{Rel: "openapi/test.yaml", Data: data}
			var problems diagnostics
			checkOpenAPI(context.Background(), []contractFile{file}, map[string]*yaml.Node{file.Rel: node}, &problems)
			assertErrorContains(t, problems.err(), test.wantErr)
		})
	}
}

func TestProtobufCompilationAndProto3Policy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name: "proto3 with standard import",
			source: `syntax = "proto3";
package test;
import "google/protobuf/timestamp.proto";
message Record { google.protobuf.Timestamp occurred_at = 1; }
`,
		},
		{
			name: "missing import",
			source: `syntax = "proto3";
package test;
import "missing.proto";
message Record {}
`,
			wantErr: "missing.proto",
		},
		{
			name: "proto2 rejected from descriptor",
			source: `/* syntax = "proto3"; */
syntax = "proto2";
package test;
message Record { optional string id = 1; }
`,
			wantErr: "syntax must be explicitly proto3",
		},
		{
			name: "type error",
			source: `syntax = "proto3";
package test;
message Record { Missing value = 1; }
`,
			wantErr: "Missing",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file := contractFile{Rel: "protobuf/test.proto", Data: []byte(test.source)}
			var problems diagnostics
			checkProtobuf(context.Background(), []contractFile{file}, &problems)
			assertErrorContains(t, problems.err(), test.wantErr)
		})
	}
}

func TestProtobufCannotOverrideStandardImports(t *testing.T) {
	t.Parallel()
	file := contractFile{
		Rel:  "protobuf/google/protobuf/timestamp.proto",
		Data: []byte(`syntax = "proto3"; package google.protobuf; message Timestamp { string unsafe = 1; }`),
	}
	var problems diagnostics
	checkProtobuf(context.Background(), []contractFile{file}, &problems)
	assertErrorContains(t, problems.err(), "may not override standard Google protobuf imports")
}
