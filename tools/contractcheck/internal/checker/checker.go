// Package checker validates all repository-managed public contracts offline.
package checker

import (
	"context"
	"fmt"
)

// Check validates contract syntax, schemas, references, fixtures, OpenAPI,
// protobuf, and event naming policy beneath the supplied repository root.
func Check(ctx context.Context, root string) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if root == "" {
		return fmt.Errorf("repository root is required")
	}
	var problems diagnostics
	files := scanRepository(ctx, root, &problems)
	if !checkContext(ctx, &problems) {
		return problems.err()
	}
	jsonDocuments := checkJSONSyntax(ctx, files.jsonFiles, &problems)
	yamlDocuments := checkYAMLSyntax(ctx, files.yamlFiles, &problems)
	schemas := checkJSONSchemas(ctx, files.schemaFiles, jsonDocuments, &problems)
	checkGovernanceInstances(ctx, root, schemas, &problems)
	checkOpenAPI(ctx, files.openAPIFiles, yamlDocuments, &problems)
	checkProtobuf(ctx, files.protoFiles, &problems)
	checkEvents(ctx, files.schemaFiles, jsonDocuments, &problems)
	checkFixtures(ctx, jsonDocuments, files.schemaFiles, schemas, &problems)
	return problems.err()
}

func checkContext(ctx context.Context, problems *diagnostics) bool {
	if err := ctx.Err(); err != nil {
		problems.add("contracts", "validation interrupted: %v", err)
		return false
	}
	return true
}
