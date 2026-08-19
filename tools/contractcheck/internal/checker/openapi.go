package checker

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"gopkg.in/yaml.v3"
)

func checkOpenAPI(ctx context.Context, files []contractFile, yamlNodes map[string]*yaml.Node, problems *diagnostics) {
	for _, file := range files {
		node := yamlNodes[file.Rel]
		if node == nil {
			continue
		}
		checkOpenAPIRefs(file.Rel, node, problems)
		loader := openapi3.NewLoader()
		loader.Context = ctx
		loader.IsExternalRefsAllowed = false
		document, err := loader.LoadFromData(file.Data)
		if err != nil {
			problems.add(file.Rel, "load OpenAPI: %v", err)
			continue
		}
		if document.OpenAPI != "3.1.0" {
			problems.add(file.Rel, "OpenAPI version must be 3.1.0")
		}
		if document.Paths == nil {
			problems.add(file.Rel, "paths are required")
			continue
		}
		if checkOpenAPIOperations(file.Rel, document.Paths, problems) {
			continue
		}
		if err := document.Validate(ctx); err != nil {
			problems.add(file.Rel, "validate OpenAPI: %v", err)
		}
	}
}

func checkOpenAPIOperations(relative string, paths *openapi3.Paths, problems *diagnostics) bool {
	invalid := false
	operationIDs := make(map[string]string)
	pathItems := paths.Map()
	routes := make([]string, 0, len(pathItems))
	for route := range pathItems {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		item := pathItems[route]
		if !strings.HasPrefix(route, "/") {
			problems.add(relative, "path %q must start with /", route)
			invalid = true
		}
		if item == nil {
			problems.add(relative, "path %q has no path item", route)
			invalid = true
			continue
		}
		operations := item.Operations()
		methods := make([]string, 0, len(operations))
		for method := range operations {
			methods = append(methods, method)
		}
		sort.Strings(methods)
		for _, method := range methods {
			operation := operations[method]
			location := fmt.Sprintf("%s %s", strings.ToUpper(method), route)
			if operation == nil {
				problems.add(relative, "%s has no operation", location)
				invalid = true
				continue
			}
			if strings.TrimSpace(operation.OperationID) == "" {
				problems.add(relative, "%s has no operationId", location)
				invalid = true
				continue
			}
			if previous, duplicate := operationIDs[operation.OperationID]; duplicate {
				problems.add(relative, "operationId %q is duplicated by %s and %s", operation.OperationID, previous, location)
				invalid = true
			}
			operationIDs[operation.OperationID] = location
		}
	}
	return invalid
}

func checkOpenAPIRefs(relative string, node *yaml.Node, problems *diagnostics) {
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			switch key.Value {
			case "$ref":
				if value.Kind != yaml.ScalarNode || !strings.HasPrefix(value.Value, "#/") {
					problems.add(relative, "OpenAPI $ref %q must be an internal fragment", value.Value)
				}
			case "$dynamicRef":
				problems.add(relative, "OpenAPI $dynamicRef is forbidden")
			}
		}
	}
	for _, child := range node.Content {
		checkOpenAPIRefs(relative, child, problems)
	}
}
