package architecture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

var providerCategoryByFamily = map[string]string{
	"ai":           "ai",
	"classified":   "classified",
	"crm":          "crm",
	"edo":          "edo",
	"erp":          "erp",
	"fx":           "finance",
	"government":   "government",
	"logistics":    "logistics",
	"marketplace":  "marketplaces",
	"notification": "notifications",
	"payment":      "payments",
	"pickup":       "logistics",
	"social":       "social",
	"storefront":   "storefronts",
}

// canonicalProviderImplementation reports whether value is either a legacy
// provider path (connectors/<id>) or the current categorized form
// (connectors/<category>/<id>). Keeping the legacy form valid lets old
// architecture fixtures remain useful while all admitted providers migrate
// incrementally to categories.
func canonicalProviderImplementation(value, id string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if parts[0] != "connectors" && parts[0] != "plugins" {
		return false
	}
	if len(parts) == 2 {
		return parts[1] == id && providerIDPattern.MatchString(parts[1])
	}
	return parts[2] == id && providerIDPattern.MatchString(parts[1]) && providerIDPattern.MatchString(parts[2])
}

func canonicalProviderManifest(value, id string) bool {
	if !strings.HasSuffix(value, "/manifest.json") {
		return false
	}
	return canonicalProviderImplementation(strings.TrimSuffix(value, "/manifest.json"), id)
}

// providerImplementationMatchesFamily enforces the category convention for
// new paths while retaining direct legacy paths for compatibility fixtures.
func providerImplementationMatchesFamily(value, family string) bool {
	parts := strings.Split(value, "/")
	if len(parts) == 2 {
		return true
	}
	expected, known := providerCategoryByFamily[family]
	return known && len(parts) == 3 && parts[1] == expected
}

type discoveredProvider struct {
	ID   string
	Path string
}

// discoverProviderImplementations accepts one category level beneath each
// provider root. A direct provider directory remains supported for legacy
// fixtures; categorized directories are identified by their child manifests.
func (r *repository) discoverProviderImplementations(ctx context.Context, root string, found *problems) []discoveredProvider {
	absolute := filepath.Join(r.root, root)
	entries, exceeded, err := readBoundedDirectory(absolute, maxReviews)
	if os.IsNotExist(err) {
		return nil
	}
	if exceeded {
		found.add(root, "provider directory count exceeds %d", maxReviews)
	}
	if err != nil {
		found.add(root, "read provider root: %v", err)
		return nil
	}
	result := make([]discoveredProvider, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result
		}
		relative := root + "/" + entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			found.add(relative, "provider symlinks are forbidden")
			continue
		}
		if !entry.IsDir() {
			if entry.Name() != "README.md" {
				found.add(relative, "provider roots may contain only provider directories")
			}
			continue
		}

		// A manifest directly inside the directory identifies a legacy provider.
		manifest := filepath.Join(absolute, entry.Name(), "manifest.json")
		if info, statErr := os.Lstat(manifest); statErr == nil && info.Mode().IsRegular() {
			if !providerIDPattern.MatchString(entry.Name()) {
				found.add(relative, "invalid provider directory name")
				continue
			}
			result = append(result, discoveredProvider{ID: entry.Name(), Path: relative})
			continue
		}

		// Otherwise this is a category and each direct child is a provider.
		if !providerIDPattern.MatchString(entry.Name()) {
			found.add(relative, "invalid provider category directory name")
			continue
		}
		categoryAbsolute := filepath.Join(absolute, entry.Name())
		children, childExceeded, childErr := readBoundedDirectory(categoryAbsolute, maxReviews)
		if childExceeded {
			found.add(relative, "provider category count exceeds %d", maxReviews)
		}
		if childErr != nil {
			found.add(relative, "read provider category: %v", childErr)
			continue
		}
		for _, child := range children {
			if err := ctx.Err(); err != nil {
				return result
			}
			childRelative := relative + "/" + child.Name()
			if child.Type()&os.ModeSymlink != 0 {
				found.add(childRelative, "provider symlinks are forbidden")
				continue
			}
			if !child.IsDir() {
				if child.Name() != "README.md" {
					found.add(childRelative, "provider categories may contain only provider directories")
				}
				continue
			}
			if !providerIDPattern.MatchString(child.Name()) {
				found.add(childRelative, "invalid provider directory name")
				continue
			}
			result = append(result, discoveredProvider{ID: child.Name(), Path: childRelative})
		}
	}
	return result
}
