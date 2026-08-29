package architecture

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/connectors/conformance"
)

var (
	modulePathPattern     = regexp.MustCompile(`^github\.com/[a-z0-9][a-z0-9_.-]*/[a-z0-9][a-z0-9_.-]*$`)
	moduleRootPattern     = regexp.MustCompile(`^internal/(app|core|platform)/[a-z][a-z0-9_/]*$`)
	reviewFilePattern     = regexp.MustCompile(`^([0-9]{3})([ab])?-[a-z0-9]+(?:-[a-z0-9]+)*\.json$`)
	reviewIDPattern       = regexp.MustCompile(`^ARCH-[0-9]{3}(?:[AB])?$`)
	taskPattern           = regexp.MustCompile(`^[0-9]{3}$`)
	taskIssuePathPattern  = regexp.MustCompile(`^tasks/issues/[0-9]{3}-[a-z0-9]+(?:-[a-z0-9]+)*\.md$`)
	providerIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	repositoryPathPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(?:/[A-Za-z0-9._-]+)*$`)
	adrPathPattern        = regexp.MustCompile(`^adr/[0-9]{4}-[a-z0-9]+(?:-[a-z0-9]+)*\.md$`)
	adrInlineStatus       = regexp.MustCompile(`(?mi)^Status:\s*(Accepted|Proposed|Superseded)\b`)
	adrSectionStatus      = regexp.MustCompile(`(?mi)^## Status\s*\r?\n+\s*(Accepted|Proposed|Superseded)\b`)
)

// CheckRepository validates the current architecture policy, inventory,
// review records, references, and Go dependency direction without Git state.
func CheckRepository(ctx context.Context, root string) (Report, error) {
	repository, err := openRepository(root)
	if err != nil {
		return Report{}, err
	}
	return repository.check(ctx)
}

type repository struct {
	root string
}

func openRepository(root string) (*repository, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("repository root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat repository root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("repository root is not a directory")
	}
	return &repository{root: resolved}, nil
}

func (r *repository) check(ctx context.Context) (Report, error) {
	var found problems
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	policyData, err := r.readRegular(policyPath, maxPolicyBytes)
	if err != nil {
		return Report{}, fmt.Errorf("%s: %w", policyPath, err)
	}
	var configuration policy
	if err := decodeStrictJSON(ctx, policyData, &configuration); err != nil {
		return Report{}, fmt.Errorf("%s: %w", policyPath, err)
	}
	r.validatePolicy(ctx, &configuration, &found)
	reviews := r.loadReviews(ctx, &configuration, &found)
	modules := r.checkGoTree(ctx, &configuration, &found)
	r.checkProviderLifecycleInventory(ctx, &configuration, reviews, &found)
	r.checkProviderInventory(ctx, &configuration, reviews, &found)
	r.checkFreezeDocumentation(&found)
	r.checkIntegrationFiles(&found)
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if err := found.err(); err != nil {
		return Report{}, err
	}
	return Report{
		Modules:   modules,
		Providers: len(configuration.Providers),
		Reviews:   len(reviews),
	}, nil
}

func (r *repository) checkIntegrationFiles(found *problems) {
	requirements := map[string][]string{
		"Makefile": {
			"architecture:",
			"./scripts/check-architecture.sh",
			"check: fmt-check test vet contracts architecture migrations policy",
		},
		"scripts/check-architecture.sh": {
			"set -euo pipefail",
			"go run -mod=readonly ./tools/architecturecheck --root .",
		},
		".github/workflows/ci.yml": {
			"fetch-depth: 0",
			"github.event.pull_request.base.sha",
			"github.event.pull_request.head.sha",
			"worktree add --detach \"$trusted_base\" \"$BASE_REVISION\"",
			"\"$trusted_checker\" --root \"$GITHUB_WORKSPACE\" --base \"$BASE_REVISION\" --head \"$HEAD_REVISION\"",
		},
		".github/workflows/release.yml": {
			"Validate the frozen architecture tree",
			"run: ./scripts/check-architecture.sh",
		},
		".github/PULL_REQUEST_TEMPLATE.md": {
			"architecture/reviews/NNN-*.json",
			"frozen-pillar",
			"Provider changes",
		},
		"templates/adr.md": {
			"## Compatibility impact",
			"## Migration and data impact",
			"## Security and privacy impact",
			"## Operational impact",
		},
		"prompts/05-architecture-gap-audit.txt": {
			"privacy/data governance",
			"reconciliation/idempotency",
			"architecture/reviews/NNN-*.json",
		},
	}
	for relative, tokens := range requirements {
		data, err := r.readRegular(relative, maxPolicyBytes)
		if err != nil {
			found.add(relative, "%v", err)
			continue
		}
		text := string(data)
		for _, token := range tokens {
			if !strings.Contains(text, token) {
				found.add(relative, "missing architecture gate token %q", token)
			}
		}
	}
	for _, relative := range []string{
		"contracts/governance/architecture-review.schema.json",
		"templates/architecture-gap-audit.md",
	} {
		if _, err := r.readRegular(relative, maxPolicyBytes); err != nil {
			found.add(relative, "%v", err)
		}
	}
	info, err := os.Stat(filepath.Join(r.root, "scripts", "check-architecture.sh"))
	if err == nil && info.Mode().Perm()&0o111 == 0 {
		found.add("scripts/check-architecture.sh", "gate script must be executable")
	}
	ciData, err := r.readRegular(".github/workflows/ci.yml", maxPolicyBytes)
	if err == nil && strings.Contains(string(ciData), "pull_request_target") {
		found.add(".github/workflows/ci.yml", "pull_request_target is forbidden")
	}
}

func (r *repository) validatePolicy(ctx context.Context, configuration *policy, found *problems) {
	if configuration.SchemaVersion != 1 {
		found.add(policyPath, "schema_version must be 1")
	}
	if !modulePathPattern.MatchString(configuration.ModulePath) || configuration.ModulePath != "github.com/torgnexa/torgnexa" {
		found.add(policyPath, "module_path must be the canonical root module")
	}
	checkExactList(policyPath, "frozen_pillars", configuration.FrozenPillars, canonicalPillars, found)
	checkExactList(policyPath, "impact_areas", configuration.ImpactAreas, canonicalImpactAreas, found)
	checkExactList(policyPath, "provider_implementation_roots", configuration.ProviderImplementationRoots, canonicalProviderRoots, found)
	checkExactList(policyPath, "sensitive_paths", configuration.SensitivePaths, canonicalSensitivePaths, found)
	checkExactList(policyPath, "provider_admission.required_tasks", configuration.ProviderAdmission.RequiredTasks, []string{"010", "025", "029", "064"}, found)
	if configuration.ProviderAdmission.Enabled {
		if configuration.ProviderCompositionModule != canonicalProviderCompositionModule {
			found.add(policyPath, "provider_composition_module must be %q while provider admission is enabled", canonicalProviderCompositionModule)
		}
	} else if configuration.ProviderCompositionModule != "" {
		found.add(policyPath, "provider_composition_module must be empty while provider admission is disabled")
	}
	validateImportAllowlist(configuration, found)
	if configuration.ProviderAdmission.Enabled {
		for _, task := range configuration.ProviderAdmission.RequiredTasks {
			if !r.taskCompleted(task) {
				found.add(policyPath, "provider admission cannot open before Task %s is completed", task)
			}
		}
	}

	previous := ""
	seen := make(map[string]struct{}, len(configuration.Modules))
	for _, item := range configuration.Modules {
		if err := ctx.Err(); err != nil {
			return
		}
		if !moduleRootPattern.MatchString(item.Path) || path.Clean(item.Path) != item.Path {
			found.add(policyPath, "invalid module path %q", item.Path)
		}
		if previous != "" && item.Path <= previous {
			found.add(policyPath, "modules must be strictly sorted by path")
		}
		previous = item.Path
		if _, duplicate := seen[item.Path]; duplicate {
			found.add(policyPath, "duplicate module path %q", item.Path)
		}
		seen[item.Path] = struct{}{}
		switch item.Kind {
		case "application", "core_domain", "infrastructure_adapter", "platform_capability", "sdk_port", "shared_types":
		default:
			found.add(policyPath, "module %q has invalid kind %q", item.Path, item.Kind)
		}
	}

	if configuration.ProviderAdmission.Enabled {
		foundComposition := false
		for _, item := range configuration.Modules {
			if item.Path == configuration.ProviderCompositionModule {
				foundComposition = true
				if item.Kind != "infrastructure_adapter" {
					found.add(policyPath, "provider composition module must have kind infrastructure_adapter")
				}
			}
		}
		if !foundComposition {
			found.add(policyPath, "provider composition module must be registered")
		}
	}

	previous = ""
	seen = make(map[string]struct{}, len(configuration.Providers))
	evidenceOwners := make(map[string]string, (len(configuration.Providers)+len(configuration.RetiredProviders))*4)
	if !configuration.ProviderAdmission.Enabled && len(configuration.Providers) != 0 {
		found.add(policyPath, "providers must remain empty while provider admission is disabled")
	}
	if len(configuration.Providers) > maxReviews {
		found.add(policyPath, "provider count exceeds %d", maxReviews)
	}
	for _, item := range configuration.Providers {
		if err := ctx.Err(); err != nil {
			return
		}
		if !providerIDPattern.MatchString(item.ID) {
			found.add(policyPath, "invalid provider id %q", item.ID)
		}
		if previous != "" && item.ID <= previous {
			found.add(policyPath, "providers must be strictly sorted by id")
		}
		previous = item.ID
		if _, duplicate := seen[item.ID]; duplicate {
			found.add(policyPath, "duplicate provider id %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		switch item.Family {
		case "marketplace", "storefront", "classified", "social", "erp", "edo", "government", "payment", "logistics", "pickup", "notification", "fx", "crm", "ai":
		default:
			found.add(policyPath, "provider %q has invalid family %q", item.ID, item.Family)
		}
		if !canonicalProviderImplementation(item.Implementation, item.ID) {
			found.add(policyPath, "provider %q implementation must use a canonical provider path", item.ID)
		} else if !providerImplementationMatchesFamily(item.Implementation, item.Family) {
			found.add(policyPath, "provider %q implementation category does not match family %q", item.ID, item.Family)
		}
		evidence := []struct {
			label     string
			reference string
			expected  string
		}{
			{label: "manifest", reference: item.Manifest, expected: item.Implementation + "/manifest.json"},
			{label: "connector_spec", reference: item.ConnectorSpec, expected: "docs/connectors/" + item.ID + "/spec.md"},
			{label: "capability_audit", reference: item.CapabilityAudit, expected: "docs/connectors/" + item.ID + "/capability-audit.md"},
			{label: "conformance_plan", reference: item.ConformancePlan, expected: "docs/connectors/" + item.ID + "/conformance-plan.md"},
		}
		for _, entry := range evidence {
			label, reference := entry.label, entry.reference
			if err := validateRepositoryPath(reference); err != nil {
				found.add(policyPath, "provider %q %s: %v", item.ID, label, err)
			}
			if reference != entry.expected {
				found.add(policyPath, "provider %q %s must be the canonical path %q", item.ID, label, entry.expected)
			}
			owner := item.ID + "." + label
			if previous, duplicate := evidenceOwners[reference]; duplicate {
				found.add(policyPath, "provider evidence path %q is reused by %s and %s", reference, previous, owner)
			} else {
				evidenceOwners[reference] = owner
			}
		}
		r.validateProviderManifestAndConformance(ctx, item, found)
		if !sort.StringsAreSorted(item.AllowedExternalImports) {
			found.add(policyPath, "provider %q allowed_external_imports must be sorted", item.ID)
		}
		allowedSeen := make(map[string]struct{}, len(item.AllowedExternalImports))
		for _, imported := range item.AllowedExternalImports {
			first := strings.SplitN(imported, "/", 2)[0]
			if path.Clean(imported) != imported || !strings.Contains(first, ".") || strings.HasPrefix(imported, configuration.ModulePath+"/") {
				found.add(policyPath, "provider %q has invalid external import %q", item.ID, imported)
			}
			if _, duplicate := allowedSeen[imported]; duplicate {
				found.add(policyPath, "provider %q has duplicate external import %q", item.ID, imported)
			}
			allowedSeen[imported] = struct{}{}
		}
	}

	if configuration.RetiredProviders == nil {
		found.add(policyPath, "retired_providers must be present as an explicit JSON array")
	}
	if len(configuration.RetiredProviders) > maxReviews {
		found.add(policyPath, "retired provider count exceeds %d", maxReviews)
	}
	previous = ""
	for _, item := range configuration.RetiredProviders {
		if err := ctx.Err(); err != nil {
			return
		}
		if !providerIDPattern.MatchString(item.ID) {
			found.add(policyPath, "invalid retired provider id %q", item.ID)
		}
		if previous != "" && item.ID <= previous {
			found.add(policyPath, "retired_providers must be strictly sorted by id")
		}
		previous = item.ID
		if _, duplicate := seen[item.ID]; duplicate {
			found.add(policyPath, "provider id %q cannot be current and retired or repeated", item.ID)
		}
		seen[item.ID] = struct{}{}

		if !canonicalProviderImplementation(item.Implementation, item.ID) {
			found.add(policyPath, "retired provider %q implementation must use a canonical provider path", item.ID)
		}
		evidence := []struct {
			label     string
			reference string
			expected  string
		}{
			{label: "manifest", reference: item.Manifest, expected: item.Implementation + "/manifest.json"},
			{label: "connector_spec", reference: item.ConnectorSpec, expected: "docs/connectors/" + item.ID + "/spec.md"},
			{label: "capability_audit", reference: item.CapabilityAudit, expected: "docs/connectors/" + item.ID + "/capability-audit.md"},
			{label: "conformance_plan", reference: item.ConformancePlan, expected: "docs/connectors/" + item.ID + "/conformance-plan.md"},
		}
		for _, entry := range evidence {
			if err := validateRepositoryPath(entry.reference); err != nil {
				found.add(policyPath, "retired provider %q %s: %v", item.ID, entry.label, err)
			}
			if entry.reference != entry.expected {
				found.add(policyPath, "retired provider %q %s must be the canonical path %q", item.ID, entry.label, entry.expected)
			}
			owner := "retired " + item.ID + "." + entry.label
			if prior, duplicate := evidenceOwners[entry.reference]; duplicate {
				found.add(policyPath, "provider evidence path %q is reused by %s and %s", entry.reference, prior, owner)
			} else {
				evidenceOwners[entry.reference] = owner
			}
		}
		if !taskPattern.MatchString(item.RetirementTask) || !r.taskExists(item.RetirementTask) {
			found.add(policyPath, "retired provider %q retirement_task %q is invalid or missing", item.ID, item.RetirementTask)
		}
	}
}

func (r *repository) validateProviderManifestAndConformance(ctx context.Context, item provider, found *problems) {
	manifestData, err := r.readRegular(item.Manifest, maxReviewBytes)
	if err != nil {
		found.add(policyPath, "provider %q manifest %q: %v", item.ID, item.Manifest, err)
		return
	}
	var manifest sdk.Manifest
	if err := decodeStrictJSON(ctx, manifestData, &manifest); err != nil {
		found.add(item.Manifest, "invalid Connector SDK manifest: %v", err)
		return
	}
	if err := manifest.Validate(); err != nil {
		found.add(item.Manifest, "invalid Connector SDK manifest")
		return
	}
	if manifest.ID != item.ID || string(manifest.Family) != item.Family {
		found.add(item.Manifest, "manifest identity/family must match provider policy record")
	}

	reference := "docs/connectors/" + item.ID + "/conformance-report.json"
	data, err := r.readRegular(reference, maxReviewBytes)
	if err != nil {
		found.add(policyPath, "provider %q conformance report %q: %v", item.ID, reference, err)
		return
	}
	var report conformance.Report
	if err := decodeStrictJSON(ctx, data, &report); err != nil {
		found.add(reference, "invalid machine-readable conformance report: %v", err)
		return
	}
	if err := report.Validate(); err != nil {
		found.add(reference, "invalid machine-readable conformance report")
		return
	}
	if !report.Passed {
		found.add(reference, "provider conformance report must pass every required check")
	}
	if report.ConnectorID != item.ID || report.ConnectorVersion != manifest.Version || report.SDKVersion != manifest.SDKVersion {
		found.add(reference, "conformance report identity/version must match the admitted manifest")
	}
}

func validateImportAllowlist(configuration *policy, found *problems) {
	if !sort.StringsAreSorted(configuration.CoreExternalImports) {
		found.add(policyPath, "core_external_imports must be sorted")
	}
	seen := make(map[string]struct{}, len(configuration.CoreExternalImports))
	for _, imported := range configuration.CoreExternalImports {
		first := strings.SplitN(imported, "/", 2)[0]
		if path.Clean(imported) != imported || !strings.Contains(first, ".") || strings.HasPrefix(imported, configuration.ModulePath+"/") {
			found.add(policyPath, "invalid Core external import %q", imported)
		}
		if _, duplicate := seen[imported]; duplicate {
			found.add(policyPath, "duplicate Core external import %q", imported)
		}
		seen[imported] = struct{}{}
	}
	if !sort.StringsAreSorted(configuration.CoreSharedImports) {
		found.add(policyPath, "core_shared_imports must be sorted")
	}
	seen = make(map[string]struct{}, len(configuration.CoreSharedImports))
	for _, imported := range configuration.CoreSharedImports {
		if path.Clean(imported) != imported || !strings.HasPrefix(imported, "internal/platform/") || strings.HasPrefix(imported, "internal/platform/postgres/") {
			found.add(policyPath, "invalid Core shared import %q", imported)
		}
		if _, duplicate := seen[imported]; duplicate {
			found.add(policyPath, "duplicate Core shared import %q", imported)
		}
		seen[imported] = struct{}{}
	}
	if !sort.StringsAreSorted(configuration.ProviderAllowedImports) {
		found.add(policyPath, "provider_allowed_internal_imports must be sorted")
	}
	seen = make(map[string]struct{}, len(configuration.ProviderAllowedImports))
	for _, imported := range configuration.ProviderAllowedImports {
		if path.Clean(imported) != imported || !strings.HasPrefix(imported, "internal/platform/") || strings.HasPrefix(imported, "internal/platform/postgres/") {
			found.add(policyPath, "invalid provider internal import %q", imported)
		}
		if _, duplicate := seen[imported]; duplicate {
			found.add(policyPath, "duplicate provider internal import %q", imported)
		}
		seen[imported] = struct{}{}
	}
}

func checkExactList(file, field string, actual, expected []string, found *problems) {
	if len(actual) != len(expected) {
		found.add(file, "%s must contain the canonical %d entries", field, len(expected))
		return
	}
	for index := range expected {
		if actual[index] != expected[index] {
			found.add(file, "%s[%d] must be %q", field, index, expected[index])
		}
	}
}

func (r *repository) loadReviews(ctx context.Context, configuration *policy, found *problems) map[string]review {
	directory := filepath.Join(r.root, filepath.FromSlash(reviewsDir))
	entries, exceeded, err := readBoundedDirectory(directory, maxReviews)
	if err != nil {
		found.add(reviewsDir, "read review directory: %v", err)
		return nil
	}
	if len(entries) == 0 && !exceeded {
		found.add(reviewsDir, "at least one architecture review is required")
	}
	if exceeded {
		found.add(reviewsDir, "review count exceeds %d", maxReviews)
	}
	result := make(map[string]review, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result
		}
		relative := reviewsDir + "/" + entry.Name()
		if entry.Type()&os.ModeSymlink != 0 {
			found.add(relative, "symlinks are forbidden")
			continue
		}
		match := reviewFilePattern.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			found.add(relative, "only NNN-kebab-case.json or NNN[a|b]-kebab-case.json review files are allowed")
			continue
		}
		data, err := r.readRegular(relative, maxReviewBytes)
		if err != nil {
			found.add(relative, "%v", err)
			continue
		}
		var record review
		if err := decodeStrictJSON(ctx, data, &record); err != nil {
			found.add(relative, "%v", err)
			continue
		}
		r.validateReview(ctx, relative, match[1], match[2], &record, configuration, found)
		if previous, duplicate := result[record.ID]; duplicate {
			found.add(relative, "duplicate review id %q also used by task %s", record.ID, previous.Task)
		}
		result[record.ID] = record
	}
	return result
}

func readBoundedDirectory(absolute string, maximum int) ([]os.DirEntry, bool, error) {
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, false, fmt.Errorf("must be a real directory, not a symlink")
	}
	// #nosec G304 -- callers construct absolute beneath the resolved repository root.
	directory, err := os.Open(absolute)
	if err != nil {
		return nil, false, err
	}
	defer directory.Close()
	entries, err := directory.ReadDir(maximum + 1)
	if err != nil && err != io.EOF {
		return nil, false, err
	}
	exceeded := len(entries) > maximum
	if exceeded {
		return nil, true, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, exceeded, nil
}

func (r *repository) validateReview(ctx context.Context, relative, fileTask, fileStage string, record *review, configuration *policy, found *problems) {
	if record.SchemaVersion != 1 {
		found.add(relative, "schema_version must be 1")
	}
	if !taskPattern.MatchString(record.Task) || record.Task != fileTask {
		found.add(relative, "task must match the filename task prefix")
	}
	expectedStage := fileStage
	if record.Stage != expectedStage {
		found.add(relative, "stage must match the optional filename stage suffix")
	}
	expectedID := "ARCH-" + record.Task + strings.ToUpper(expectedStage)
	if !reviewIDPattern.MatchString(record.ID) || record.ID != expectedID {
		found.add(relative, "id must match ARCH-<task><optional stage>")
	}
	if !meaningful(record.Summary, 32) {
		found.add(relative, "summary must be meaningful and contain no placeholder")
	}
	if !r.taskExists(record.Task) {
		found.add(relative, "task %q does not resolve to exactly one issue", record.Task)
	}

	switch record.ChangeClass {
	case "implementation", "new_domain", "new_provider", "pillar_change", "mixed":
	default:
		found.add(relative, "invalid change_class %q", record.ChangeClass)
	}
	if !record.GapAudit.Performed {
		found.add(relative, "gap_audit.performed must be true")
	}
	if record.GapAudit.Prompt != "prompts/05-architecture-gap-audit.txt" {
		found.add(relative, "gap_audit.prompt must reference the canonical prompt")
	}
	switch record.GapAudit.Decision {
	case "within_frozen_architecture", "extend_existing_capability", "route_to_connector_sdk", "architecture_change":
	default:
		found.add(relative, "invalid gap_audit.decision %q", record.GapAudit.Decision)
	}
	expectedDecision := map[string]string{
		"implementation": "within_frozen_architecture",
		"new_domain":     "extend_existing_capability",
		"new_provider":   "route_to_connector_sdk",
		"pillar_change":  "architecture_change",
		"mixed":          "architecture_change",
	}[record.ChangeClass]
	if expectedDecision != "" && record.GapAudit.Decision != expectedDecision {
		found.add(relative, "change_class %q requires gap_audit.decision %q", record.ChangeClass, expectedDecision)
	}

	validateSortedPaths(relative, "scopes", record.Scopes, false, found)
	if len(record.Scopes) == 0 {
		found.add(relative, "at least one changed-path scope is required")
	}
	validatePillars(relative, record.FrozenPillars, found)
	validateImpacts(relative, record.Impacts, found)
	validateSortedPaths(relative, "existing_adrs", record.ExistingADRs, false, found)
	if len(record.ExistingADRs) > 64 {
		found.add(relative, "existing_adrs exceeds 64 paths")
	}
	if len(record.ExistingADRs) == 0 {
		found.add(relative, "at least one existing ADR citation is required")
	}
	for _, reference := range record.ExistingADRs {
		if !adrPathPattern.MatchString(reference) {
			found.add(relative, "existing ADR path %q is invalid", reference)
			continue
		}
		if _, err := r.readRegular(reference, maxReviewBytes); err != nil {
			found.add(relative, "existing ADR %q: %v", reference, err)
		}
	}

	requiresNewADR := record.ChangeClass == "pillar_change" || record.ChangeClass == "mixed"
	if requiresNewADR && record.ADR == nil {
		found.add(relative, "pillar and mixed changes require a new ADR")
	}
	if !requiresNewADR && record.ADR != nil {
		found.add(relative, "only pillar_change and mixed records may declare a decision ADR")
	}
	if record.ADR != nil {
		r.validateDecisionADR(*record.ADR, relative, found)
	}
	if requiresNewADR && len(record.FrozenPillars) == 0 {
		found.add(relative, "pillar and mixed changes must name affected frozen pillars")
	}
	if !requiresNewADR && len(record.FrozenPillars) != 0 {
		found.add(relative, "non-pillar records may not declare affected frozen pillars")
	}

	requiresProvider := record.ChangeClass == "new_provider" || record.ChangeClass == "mixed"
	if requiresProvider && record.Provider == nil {
		found.add(relative, "provider and mixed changes require provider evidence")
	}
	if !requiresProvider && record.Provider != nil {
		found.add(relative, "provider evidence is only valid for provider or mixed changes")
	}
	if record.Provider != nil {
		r.validateProviderReview(relative, record.Provider, configuration, found)
	}

	if !sort.StringsAreSorted(record.FollowUpTasks) {
		found.add(relative, "follow_up_tasks must be sorted")
	}
	if len(record.FollowUpTasks) > 64 {
		found.add(relative, "follow_up_tasks exceeds 64 entries")
	}
	seenTasks := make(map[string]struct{}, len(record.FollowUpTasks))
	for _, task := range record.FollowUpTasks {
		if !taskPattern.MatchString(task) || !r.taskExists(task) {
			found.add(relative, "follow-up task %q is invalid or missing", task)
		}
		if _, duplicate := seenTasks[task]; duplicate {
			found.add(relative, "duplicate follow-up task %q", task)
		}
		seenTasks[task] = struct{}{}
	}
	_ = ctx
}

func validateSortedPaths(file, field string, values []string, allowDirectory bool, found *problems) {
	if len(values) > 256 {
		found.add(file, "%s exceeds 256 paths", field)
	}
	if !sort.StringsAreSorted(values) {
		found.add(file, "%s must be sorted", field)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		candidate := strings.TrimSuffix(value, "/")
		if err := validateRepositoryPath(candidate); err != nil {
			found.add(file, "%s path %q: %v", field, value, err)
		}
		if strings.HasSuffix(value, "/") && !allowDirectory {
			found.add(file, "%s path %q must identify a file", field, value)
		}
		if _, duplicate := seen[value]; duplicate {
			found.add(file, "duplicate %s path %q", field, value)
		}
		seen[value] = struct{}{}
	}
}

func validateRepositoryPath(value string) error {
	if utf8.RuneCountInString(value) > 512 || !repositoryPathPattern.MatchString(value) {
		return fmt.Errorf("must be an ASCII repository path of at most 512 characters")
	}
	_, err := safeRelativePath(value)
	return err
}

func validatePillars(file string, values []string, found *problems) {
	positions := make(map[string]int, len(canonicalPillars))
	for index, pillar := range canonicalPillars {
		positions[pillar] = index
	}
	previous := -1
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		position, ok := positions[value]
		if !ok {
			found.add(file, "unknown frozen pillar %q", value)
			continue
		}
		if position <= previous {
			found.add(file, "frozen_pillars must follow canonical order")
		}
		previous = position
		if _, duplicate := seen[value]; duplicate {
			found.add(file, "duplicate frozen pillar %q", value)
		}
		seen[value] = struct{}{}
	}
}

func validateImpacts(file string, impacts []impact, found *problems) {
	if len(impacts) != len(canonicalImpactAreas) {
		found.add(file, "impacts must contain all %d canonical areas", len(canonicalImpactAreas))
	}
	for index, area := range canonicalImpactAreas {
		if index >= len(impacts) {
			break
		}
		item := impacts[index]
		if item.Area != area {
			found.add(file, "impacts[%d].area must be %q", index, area)
		}
		switch item.Status {
		case "affected", "not_affected", "not_applicable":
		default:
			found.add(file, "impact %q has invalid status %q", area, item.Status)
		}
		if !meaningful(item.Evidence, 24) {
			found.add(file, "impact %q evidence must be meaningful and contain no placeholder", area)
		}
	}
}

func meaningful(value string, minimum int) bool {
	trimmed := strings.TrimSpace(value)
	length := utf8.RuneCountInString(trimmed)
	if length < minimum || length > 2000 {
		return false
	}
	lower := strings.ToLower(trimmed)
	if lower == "n/a" || lower == "na" || lower == "none" || lower == "not applicable" {
		return false
	}
	return !strings.Contains(lower, "todo") && !strings.Contains(lower, "tbd") && !strings.Contains(lower, "fixme")
}

func (r *repository) validateDecisionADR(reference, reviewPath string, found *problems) {
	if !adrPathPattern.MatchString(reference) {
		found.add(reviewPath, "ADR path %q is invalid", reference)
		return
	}
	data, err := r.readRegular(reference, maxReviewBytes)
	if err != nil {
		found.add(reviewPath, "ADR %q: %v", reference, err)
		return
	}
	text := string(data)
	status := markdownADRStatus(text)
	if status != "Accepted" {
		found.add(reference, "decision ADR status must be Accepted before the review is mergeable")
	}
	required := []string{
		"## Context", "## Decision", "## Consequences", "## Alternatives considered",
		"## Compatibility impact", "## Migration and data impact",
		"## Security and privacy impact", "## Operational impact",
	}
	sections := markdownSections(text)
	for _, heading := range required {
		body, exists := sections[heading]
		if !exists {
			found.add(reference, "missing required heading %q", heading)
			continue
		}
		if !meaningful(body, 24) {
			found.add(reference, "required section %q must contain a meaningful non-placeholder rationale", heading)
		}
	}
}

func markdownSections(text string) map[string]string {
	sections := make(map[string]string)
	current := ""
	var body strings.Builder
	flush := func() {
		if current != "" {
			sections[current] = strings.TrimSpace(body.String())
		}
		body.Reset()
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			current = strings.TrimSpace(line)
			continue
		}
		if current != "" {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()
	return sections
}

func markdownADRStatus(text string) string {
	if match := adrInlineStatus.FindStringSubmatch(text); len(match) == 2 {
		return match[1]
	}
	if match := adrSectionStatus.FindStringSubmatch(text); len(match) == 2 {
		return match[1]
	}
	return ""
}

func (r *repository) validateProviderReview(file string, item *providerReview, configuration *policy, found *problems) {
	if !providerIDPattern.MatchString(item.ID) {
		found.add(file, "provider.id is invalid")
	}
	if item.Route != "connector_sdk" {
		found.add(file, "provider.route must be connector_sdk")
	}
	manifestValid := canonicalProviderManifest(item.Manifest, item.ID)
	if !manifestValid {
		found.add(file, "provider manifest must use its canonical connector/plugin evidence path")
	}
	evidence := []struct {
		label     string
		reference string
		expected  string
	}{
		{label: "manifest", reference: item.Manifest},
		{label: "connector_spec", reference: item.ConnectorSpec, expected: "docs/connectors/" + item.ID + "/spec.md"},
		{label: "capability_audit", reference: item.CapabilityAudit, expected: "docs/connectors/" + item.ID + "/capability-audit.md"},
		{label: "conformance_plan", reference: item.ConformancePlan, expected: "docs/connectors/" + item.ID + "/conformance-plan.md"},
	}
	seen := make(map[string]string, len(evidence))
	for _, entry := range evidence {
		label, reference := entry.label, entry.reference
		if err := validateRepositoryPath(reference); err != nil {
			found.add(file, "provider %s %q: %v", label, reference, err)
			continue
		}
		if entry.expected != "" && reference != entry.expected {
			found.add(file, "provider %s must use canonical path %q", label, entry.expected)
		}
		if previous, duplicate := seen[reference]; duplicate {
			found.add(file, "provider evidence path %q is reused by %s and %s", reference, previous, label)
		} else {
			seen[reference] = label
		}
		if _, err := r.readRegular(reference, maxReviewBytes); err != nil {
			found.add(file, "provider %s %q: %v", label, reference, err)
		}
	}

	registered := false
	evidenceMatches := false
	for _, current := range configuration.Providers {
		if current.ID != item.ID {
			continue
		}
		registered = true
		evidenceMatches = item.Manifest == current.Manifest && item.ConnectorSpec == current.ConnectorSpec &&
			item.CapabilityAudit == current.CapabilityAudit && item.ConformancePlan == current.ConformancePlan
		break
	}
	if !registered {
		for _, retired := range configuration.RetiredProviders {
			if retired.ID != item.ID {
				continue
			}
			registered = true
			evidenceMatches = item.Manifest == retired.Manifest && item.ConnectorSpec == retired.ConnectorSpec &&
				item.CapabilityAudit == retired.CapabilityAudit && item.ConformancePlan == retired.ConformancePlan
			break
		}
	}
	if !registered {
		found.add(file, "provider review id %q must name a current or explicitly retired provider", item.ID)
	} else if !evidenceMatches {
		found.add(file, "provider review evidence must exactly match the current or retired policy record for %q", item.ID)
	}
}

func (r *repository) checkProviderLifecycleInventory(ctx context.Context, configuration *policy, reviews map[string]review, found *problems) {
	current := make(map[string]struct{}, len(configuration.Providers))
	for _, item := range configuration.Providers {
		current[item.Implementation] = struct{}{}
	}
	retired := make(map[string]retiredProvider, len(configuration.RetiredProviders))
	for _, item := range configuration.RetiredProviders {
		retired[item.Implementation] = item
	}
	discoveredRetired := make(map[string]struct{}, len(retired))

	for _, root := range canonicalProviderRoots {
		for _, discovered := range r.discoverProviderImplementations(ctx, root, found) {
			implementation := discovered.Path
			if _, exists := current[implementation]; exists {
				continue
			}
			item, exists := retired[implementation]
			if !exists {
				found.add(implementation, "unregistered provider directory is forbidden; only an explicit retired_providers tombstone may remain")
				continue
			}
			discoveredRetired[implementation] = struct{}{}
			r.validateRetiredProviderDirectory(item, found)
		}
	}

	for implementation, item := range retired {
		if _, exists := discoveredRetired[implementation]; !exists {
			found.add(policyPath, "retired provider %q implementation tombstone %q is missing", item.ID, implementation)
		}
		for _, reference := range []string{item.Manifest, item.ConnectorSpec, item.CapabilityAudit, item.ConformancePlan} {
			if _, err := r.readRegular(reference, maxReviewBytes); err != nil {
				found.add(policyPath, "retired provider %q evidence %q: %v", item.ID, reference, err)
			}
		}
		reviewed := false
		for _, record := range reviews {
			if record.Provider != nil && providerReviewMatchesRetired(*record.Provider, item) {
				reviewed = true
				break
			}
		}
		if !reviewed {
			found.add(policyPath, "retired provider %q has no historical architecture review matching its retained evidence", item.ID)
		}
	}
}

func providerReviewMatchesRetired(record providerReview, retired retiredProvider) bool {
	return record.ID == retired.ID && record.Manifest == retired.Manifest &&
		record.ConnectorSpec == retired.ConnectorSpec && record.CapabilityAudit == retired.CapabilityAudit &&
		record.ConformancePlan == retired.ConformancePlan
}

func (r *repository) validateRetiredProviderDirectory(item retiredProvider, found *problems) {
	entries, exceeded, err := readBoundedDirectory(filepath.Join(r.root, filepath.FromSlash(item.Implementation)), 1)
	if err != nil {
		found.add(item.Implementation, "inspect retired provider tombstone: %v", err)
		return
	}
	if exceeded || len(entries) != 1 || entries[0].Name() != "manifest.json" {
		found.add(item.Implementation, "retired provider tombstone must contain only the exact regular non-executable manifest.json")
		return
	}
	entry := entries[0]
	info, err := entry.Info()
	if err != nil {
		found.add(item.Manifest, "inspect retired provider manifest: %v", err)
		return
	}
	if entry.Type()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 != 0 {
		found.add(item.Manifest, "retired provider manifest must be a regular non-executable file")
	}
}

func (r *repository) taskExists(task string) bool {
	if !taskPattern.MatchString(task) {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(r.root, "tasks", "issues", task+"-*.md"))
	if err != nil || len(matches) != 1 {
		return false
	}
	relative, err := filepath.Rel(r.root, matches[0])
	if err != nil {
		return false
	}
	canonical := filepath.ToSlash(relative)
	if !taskIssuePathPattern.MatchString(canonical) {
		return false
	}
	_, err = r.readRegular(canonical, maxReviewBytes)
	return err == nil
}

func (r *repository) taskCompleted(task string) bool {
	if !taskPattern.MatchString(task) {
		return false
	}
	matches, err := filepath.Glob(filepath.Join(r.root, "tasks", "issues", task+"-*.md"))
	if err != nil || len(matches) != 1 {
		return false
	}
	relative, err := filepath.Rel(r.root, matches[0])
	if err != nil {
		return false
	}
	canonical := filepath.ToSlash(relative)
	if !taskIssuePathPattern.MatchString(canonical) {
		return false
	}
	data, err := r.readRegular(canonical, maxReviewBytes)
	if err != nil {
		return false
	}
	return completedTaskStatus(data)
}

func completedTaskStatus(data []byte) bool {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	inStatus := false
	var statusLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "## Status" {
			inStatus = true
			continue
		}
		if !inStatus {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			break
		}
		if trimmed == "" {
			continue
		}
		statusLines = append(statusLines, trimmed)
	}
	if len(statusLines) == 0 {
		return false
	}
	status := strings.ToLower(strings.Join(statusLines, " "))
	first := strings.ToLower(strings.ReplaceAll(statusLines[0], "**", ""))
	firstCanonical := strings.TrimSpace(strings.TrimRight(first, "."))
	completed := firstCanonical == "completed" || strings.HasPrefix(firstCanonical, "completed ") ||
		strings.HasPrefix(firstCanonical, "repository implementation: completed") ||
		strings.HasPrefix(firstCanonical, "repository implementation status: completed")
	blocked := strings.Contains(status, "block") || strings.Contains(status, "pending") ||
		strings.Contains(status, "incomplete") || strings.Contains(status, "in progress")
	return completed && !blocked
}

func (r *repository) checkFreezeDocumentation(found *problems) {
	const relative = "docs/54-architecture-freeze-v1.md"
	data, err := r.readRegular(relative, maxReviewBytes)
	if err != nil {
		found.add(relative, "%v", err)
		return
	}
	text := string(data)
	for _, pillar := range canonicalPillars {
		if !strings.Contains(text, "`"+pillar+"`") {
			found.add(relative, "missing canonical pillar id %q", pillar)
		}
	}
	for _, token := range []string{"architecture/policy.json", "make architecture", "scripts/check-architecture.sh"} {
		if !strings.Contains(text, token) {
			found.add(relative, "missing gate reference %q", token)
		}
	}
}

func safeRelativePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("must be a non-empty repository-relative slash path")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", fmt.Errorf("control characters are forbidden")
		}
	}
	cleaned := path.Clean(value)
	if cleaned != value || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path traversal or non-canonical path is forbidden")
	}
	return cleaned, nil
}

func (r *repository) readRegular(relative string, maximum int64) ([]byte, error) {
	cleaned, err := safeRelativePath(relative)
	if err != nil {
		return nil, err
	}
	current := r.root
	for _, component := range strings.Split(cleaned, "/") {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, fmt.Errorf("lstat: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlinks are forbidden")
		}
	}
	info, err := os.Stat(current)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("must be a regular file")
	}
	if info.Size() > maximum {
		return nil, fmt.Errorf("file exceeds %d bytes", maximum)
	}
	// #nosec G304 -- every path component is canonicalized beneath the resolved root and rejected if symlinked.
	data, err := os.ReadFile(current)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	return data, nil
}
