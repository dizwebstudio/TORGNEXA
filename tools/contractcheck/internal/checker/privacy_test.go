package checker

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPublicContractsRejectOIDCSubject(t *testing.T) {
	t.Parallel()
	openAPI, err := parseStrictYAML([]byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    Member:\n      properties:\n        oidc_subject: {type: string}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var problems diagnostics
	checkPublicIdentityReferences(
		[]contractFile{{Rel: "openapi/public.yaml"}},
		[]contractFile{{Rel: "events/member-v1.schema.json"}},
		map[string]*yaml.Node{"openapi/public.yaml": openAPI},
		map[string]any{"events/member-v1.schema.json": map[string]any{"required": []any{"oidc_subject"}}},
		&problems,
	)
	err = problems.err()
	assertErrorContains(t, err, "forbidden in public REST contracts")
	assertErrorContains(t, err, "forbidden in event contracts")
}

func TestPublicContractsAllowIdentityBound(t *testing.T) {
	t.Parallel()
	openAPI, err := parseStrictYAML([]byte("openapi: 3.1.0\ncomponents:\n  schemas:\n    Member:\n      properties:\n        identity_bound: {type: boolean}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var problems diagnostics
	checkPublicIdentityReferences(
		[]contractFile{{Rel: "openapi/public.yaml"}},
		[]contractFile{{Rel: "events/member-v1.schema.json"}},
		map[string]*yaml.Node{"openapi/public.yaml": openAPI},
		map[string]any{"events/member-v1.schema.json": map[string]any{"properties": map[string]any{"identity_bound": map[string]any{"type": "boolean"}}}},
		&problems,
	)
	if err := problems.err(); err != nil {
		t.Fatal(err)
	}
}
