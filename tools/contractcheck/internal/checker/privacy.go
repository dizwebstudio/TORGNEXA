package checker

import (
	"strings"

	"gopkg.in/yaml.v3"
)

const internalIdentityReferenceField = "oidc_subject"

// checkPublicIdentityReferences keeps provider subject references out of REST
// and event contracts. The reference remains an internal authentication and
// privacy-workflow locator; public contracts expose only identity_bound.
func checkPublicIdentityReferences(openAPIFiles, schemaFiles []contractFile, yamlDocuments map[string]*yaml.Node, jsonDocuments map[string]any, problems *diagnostics) {
	for _, file := range openAPIFiles {
		if containsYAMLScalar(yamlDocuments[file.Rel], internalIdentityReferenceField) {
			problems.add(file.Rel, "internal identity reference field %q is forbidden in public REST contracts", internalIdentityReferenceField)
		}
	}
	for _, file := range schemaFiles {
		if !strings.HasPrefix(file.Rel, "events/") {
			continue
		}
		if containsJSONToken(jsonDocuments[file.Rel], internalIdentityReferenceField) {
			problems.add(file.Rel, "internal identity reference field %q is forbidden in event contracts", internalIdentityReferenceField)
		}
	}
}

func containsYAMLScalar(node *yaml.Node, target string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.ScalarNode && node.Value == target {
		return true
	}
	for _, child := range node.Content {
		if containsYAMLScalar(child, target) {
			return true
		}
	}
	return false
}

func containsJSONToken(value any, target string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == target || containsJSONToken(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsJSONToken(child, target) {
				return true
			}
		}
	case string:
		return typed == target
	}
	return false
}
