package checker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	maxYAMLNodes      = 100_000
	maxYAMLTotalNodes = 1_000_000
)

var errYAMLTotalNodeLimit = errors.New("total YAML node limit exceeded")

func checkYAMLSyntax(ctx context.Context, files []contractFile, problems *diagnostics) map[string]*yaml.Node {
	parsed := make(map[string]*yaml.Node, len(files))
	totalRemaining := maxYAMLTotalNodes
	for _, file := range files {
		if !checkContext(ctx, problems) {
			break
		}
		node, err := parseStrictYAMLWithBudget(ctx, file.Data, &totalRemaining)
		if err != nil {
			problems.add(file.Rel, "invalid YAML: %v", err)
			if errors.Is(err, errYAMLTotalNodeLimit) {
				break
			}
			continue
		}
		parsed[file.Rel] = node
	}
	return parsed
}

func parseStrictYAML(data []byte) (*yaml.Node, error) {
	totalRemaining := maxYAMLNodes
	return parseStrictYAMLWithBudget(context.Background(), data, &totalRemaining)
}

func parseStrictYAMLWithBudget(ctx context.Context, data []byte, totalRemaining *int) (*yaml.Node, error) {
	return parseYAMLWithBudget(ctx, data, totalRemaining, false)
}

// parseComposeYAMLWithBudget retains strict syntax and node limits while
// allowing Compose's standard extension anchors and merge aliases. Alias
// targets are never recursively expanded by the checker, so anchors cannot
// amplify the validation workload or hide image nodes from the tree walk.
func parseComposeYAMLWithBudget(ctx context.Context, data []byte, totalRemaining *int) (*yaml.Node, error) {
	return parseYAMLWithBudget(ctx, data, totalRemaining, true)
}

func parseYAMLWithBudget(ctx context.Context, data []byte, totalRemaining *int, allowComposeAliases bool) (*yaml.Node, error) {
	if ctx == nil {
		return nil, fmt.Errorf("context is required")
	}
	if totalRemaining == nil {
		return nil, fmt.Errorf("total YAML node budget is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("validation interrupted: %w", err)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("input is not valid UTF-8")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("validation interrupted: %w", err)
	}
	if len(document.Content) == 0 {
		return nil, fmt.Errorf("document is empty")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple YAML documents are forbidden")
		}
		return nil, err
	}
	count := 0
	if err := validateYAMLNodeWithOptions(ctx, &document, 0, &count, totalRemaining, allowComposeAliases); err != nil {
		return nil, err
	}
	return &document, nil
}

func validateYAMLNode(ctx context.Context, node *yaml.Node, depth int, count, totalRemaining *int) error {
	return validateYAMLNodeWithOptions(ctx, node, depth, count, totalRemaining, false)
}

func validateYAMLNodeWithOptions(ctx context.Context, node *yaml.Node, depth int, count, totalRemaining *int, allowComposeAliases bool) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("validation interrupted: %w", err)
	}
	if *count >= maxYAMLNodes {
		return fmt.Errorf("document exceeds %d nodes", maxYAMLNodes)
	}
	if *totalRemaining <= 0 {
		return fmt.Errorf("%w: contract corpus exceeds %d nodes", errYAMLTotalNodeLimit, maxYAMLTotalNodes)
	}
	(*count)++
	*totalRemaining = *totalRemaining - 1
	if depth > maxDocumentDepth {
		return fmt.Errorf("document nesting exceeds %d", maxDocumentDepth)
	}
	if !allowComposeAliases && (node.Kind == yaml.AliasNode || node.Anchor != "") {
		return fmt.Errorf("aliases and anchors are forbidden")
	}
	if !isAllowedYAMLTag(node.Tag) && !(allowComposeAliases && node.Tag == "!!merge") {
		return fmt.Errorf("YAML tag %q is forbidden", node.Tag)
	}
	if len(node.Content) > maxYAMLNodes-*count {
		return fmt.Errorf("document exceeds %d nodes", maxYAMLNodes)
	}
	if len(node.Content) > *totalRemaining {
		return fmt.Errorf("%w: contract corpus exceeds %d nodes", errYAMLTotalNodeLimit, maxYAMLTotalNodes)
	}
	if node.Kind == yaml.MappingNode {
		if len(node.Content)%2 != 0 {
			return fmt.Errorf("mapping has an odd number of nodes")
		}
		seen := make(map[string]struct{}, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("validation interrupted: %w", err)
			}
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode {
				return fmt.Errorf("mapping keys must be scalars")
			}
			if key.Value == "<<" && !allowComposeAliases {
				return fmt.Errorf("YAML merge keys are forbidden")
			}
			if _, duplicate := seen[key.Value]; duplicate {
				return fmt.Errorf("duplicate mapping key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := validateYAMLNodeWithOptions(ctx, child, depth+1, count, totalRemaining, allowComposeAliases); err != nil {
			return err
		}
	}
	return nil
}

func isAllowedYAMLTag(tag string) bool {
	switch tag {
	case "", "!!map", "!!seq", "!!str", "!!null", "!!bool", "!!int", "!!float", "!!timestamp":
		return true
	default:
		return false
	}
}
