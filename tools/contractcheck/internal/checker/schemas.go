package checker

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const draft2020 = "https://json-schema.org/draft/2020-12/schema"

type offlineSchemaLoader struct{}

func (offlineSchemaLoader) Load(location string) (any, error) {
	return nil, fmt.Errorf("external schema loading is forbidden: %s", location)
}

func checkJSONSchemas(ctx context.Context, files []contractFile, parsed map[string]any, problems *diagnostics) map[string]*jsonschema.Schema {
	ids := make(map[string]string, len(files))
	for _, file := range files {
		if !checkContext(ctx, problems) {
			return nil
		}
		document, ok := parsed[file.Rel].(map[string]any)
		if !ok {
			if _, exists := parsed[file.Rel]; exists {
				problems.add(file.Rel, "JSON Schema root must be an object")
			}
			continue
		}
		dialect, _ := document["$schema"].(string)
		if dialect != draft2020 {
			problems.add(file.Rel, "$schema must be %q", draft2020)
		}
		title, _ := document["title"].(string)
		if strings.TrimSpace(title) == "" {
			problems.add(file.Rel, "title must be a non-empty string")
		}
		id, _ := document["$id"].(string)
		expectedID := expectedSchemaID(file.Rel)
		if id != expectedID {
			problems.add(file.Rel, "$id must be %q", expectedID)
			continue
		}
		if previous, duplicate := ids[id]; duplicate {
			problems.add(file.Rel, "duplicate $id also used by %s", previous)
			continue
		}
		ids[id] = file.Rel
	}

	for _, file := range files {
		if !checkContext(ctx, problems) {
			return nil
		}
		document, ok := parsed[file.Rel]
		if !ok {
			continue
		}
		id := expectedSchemaID(file.Rel)
		checkSchemaRefs(ctx, file.Rel, id, document, ids, 0, problems)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(offlineSchemaLoader{})
	for _, file := range files {
		if !checkContext(ctx, problems) {
			return nil
		}
		if ids[expectedSchemaID(file.Rel)] != file.Rel {
			continue
		}
		if err := compiler.AddResource(expectedSchemaID(file.Rel), parsed[file.Rel]); err != nil {
			problems.add(file.Rel, "register JSON Schema: %v", err)
		}
	}
	compiled := make(map[string]*jsonschema.Schema, len(files))
	for _, file := range files {
		if !checkContext(ctx, problems) {
			return compiled
		}
		id := expectedSchemaID(file.Rel)
		if ids[id] != file.Rel {
			continue
		}
		schema, err := compiler.Compile(id)
		if err != nil {
			problems.add(file.Rel, "compile JSON Schema: %v", err)
			continue
		}
		compiled[file.Rel] = schema
	}
	return compiled
}

func expectedSchemaID(relative string) string {
	if relative == "events/event-envelope.schema.json" {
		return "https://torgnexa.local/schemas/event-envelope.json"
	}
	name := strings.TrimSuffix(relative, ".schema.json") + ".json"
	return "https://torgnexa.local/schemas/" + name
}

func checkSchemaRefs(ctx context.Context, relative, currentID string, value any, knownIDs map[string]string, depth int, problems *diagnostics) {
	if !checkContext(ctx, problems) {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "$id" && depth > 0 {
				problems.add(relative, "nested $id values are forbidden")
				continue
			}
			if key == "$ref" || key == "$dynamicRef" {
				reference, ok := child.(string)
				if !ok {
					problems.add(relative, "%s must be a string", key)
					continue
				}
				if err := validateSchemaRef(currentID, reference, knownIDs); err != nil {
					problems.add(relative, "%s %q: %v", key, reference, err)
				}
				continue
			}
			checkSchemaRefs(ctx, relative, currentID, child, knownIDs, depth+1, problems)
		}
	case []any:
		for _, child := range typed {
			checkSchemaRefs(ctx, relative, currentID, child, knownIDs, depth+1, problems)
		}
	}
}

func validateSchemaRef(currentID, reference string, knownIDs map[string]string) error {
	parsed, err := url.Parse(reference)
	if err != nil {
		return fmt.Errorf("invalid URI")
	}
	if parsed.Scheme == "" && parsed.Host == "" && parsed.Path == "" && parsed.Fragment != "" {
		return nil
	}
	base, err := url.Parse(currentID)
	if err != nil {
		return fmt.Errorf("invalid current schema ID")
	}
	resolved := base.ResolveReference(parsed)
	resolved.Fragment = ""
	resolved.RawFragment = ""
	if _, ok := knownIDs[resolved.String()]; !ok {
		return fmt.Errorf("target is not a registered repository schema")
	}
	return nil
}

func safeContractPath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("path must be a non-empty repository-relative slash path")
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path traversal is forbidden")
	}
	return cleaned, nil
}
