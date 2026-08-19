package checker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	architectureReviewSchema = "governance/architecture-review.schema.json"
	architectureReviewsDir   = "architecture/reviews"
	maxArchitectureReviews   = 1000
	maxArchitectureReview    = 256 << 10
)

// Keep staged review filenames aligned with the architecture gate, which uses
// an optional a/b suffix for multi-stage task reviews (for example 088a/088b).
var architectureReviewName = regexp.MustCompile(`^[0-9]{3}(?:[ab])?-[a-z0-9]+(?:-[a-z0-9]+)*\.json$`)

func checkGovernanceInstances(ctx context.Context, root string, schemas map[string]*jsonschema.Schema, problems *diagnostics) {
	schema, exists := schemas[architectureReviewSchema]
	if !exists {
		problems.add(architectureReviewSchema, "compiled governance schema is required")
		return
	}
	directory, err := resolveGovernanceDirectory(root, architectureReviewsDir)
	if err != nil {
		problems.add(architectureReviewsDir, "%v", err)
		return
	}
	// #nosec G304 -- directory is resolved beneath the real repository root.
	handle, err := os.Open(directory)
	if err != nil {
		problems.add(architectureReviewsDir, "open directory: %v", err)
		return
	}
	defer handle.Close()
	entries, err := handle.ReadDir(maxArchitectureReviews + 1)
	if err != nil && err != io.EOF {
		problems.add(architectureReviewsDir, "read directory: %v", err)
		return
	}
	if len(entries) > maxArchitectureReviews {
		problems.add(architectureReviewsDir, "review count exceeds %d", maxArchitectureReviews)
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	count := 0
	for _, entry := range entries {
		if !checkContext(ctx, problems) {
			return
		}
		relative := architectureReviewsDir + "/" + entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !architectureReviewName.MatchString(entry.Name()) {
			problems.add(relative, "only regular NNN[stage]-kebab-case.json review files are allowed")
			continue
		}
		count++
		data, ok := readRepositoryFile(root, relative, problems)
		if !ok {
			continue
		}
		if len(data) > maxArchitectureReview {
			problems.add(relative, "review exceeds %d bytes", maxArchitectureReview)
			continue
		}
		remaining := maxJSONDocumentNodes
		value, err := parseStrictJSONWithBudget(ctx, data, &remaining)
		if err != nil {
			problems.add(relative, "invalid review JSON: %v", err)
			continue
		}
		if err := schema.Validate(value); err != nil {
			problems.add(relative, "does not satisfy %s: %v", architectureReviewSchema, err)
		}
	}
	if count == 0 {
		problems.add(architectureReviewsDir, "at least one architecture review is required")
	}
}

func resolveGovernanceDirectory(root, relative string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("repository root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	return resolveRealRepositoryPath(absolute, relative, true)
}
