package architecture

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	maxGitOutput = 8 << 20
	maxChanges   = 10000
)

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

var selfProtectedPaths = []string{
	".github/PULL_REQUEST_TEMPLATE.md",
	".github/workflows/",
	"Makefile",
	"contracts/governance/architecture-review.schema.json",
	"internal/platform/architecture/",
	"prompts/05-architecture-gap-audit.txt",
	"scripts/check-architecture.sh",
	"templates/adr.md",
	"templates/architecture-gap-audit.md",
	"tools/architecturecheck/",
	"tools/contractcheck/internal/checker/supplychain.go",
	"tools/contractcheck/internal/checker/supplychain_test.go",
}

type change struct {
	Status  byte
	OldPath string
	Path    string
}

// CheckDiff validates the checked-out head tree and the complete merge-base to
// head change set. Both revisions must be immutable full Git object IDs.
func CheckDiff(ctx context.Context, root, baseRevision, headRevision string) (Report, error) {
	if !revisionPattern.MatchString(baseRevision) || !revisionPattern.MatchString(headRevision) {
		return Report{}, fmt.Errorf("base and head revisions must be full lowercase 40-hex Git object IDs")
	}
	repository, err := openRepository(root)
	if err != nil {
		return Report{}, err
	}
	if err := repository.requireCompleteCheckout(ctx); err != nil {
		return Report{}, err
	}
	if err := repository.requireCleanHead(ctx, headRevision); err != nil {
		return Report{}, err
	}
	report, err := repository.check(ctx)
	if err != nil {
		return Report{}, err
	}
	if err := repository.requireCleanHead(ctx, headRevision); err != nil {
		return Report{}, err
	}
	mergeBaseData, err := repository.gitOutput(ctx, 128, "merge-base", baseRevision, headRevision)
	if err != nil {
		return Report{}, fmt.Errorf("resolve merge base: %w", err)
	}
	mergeBase := strings.TrimSpace(string(mergeBaseData))
	if !revisionPattern.MatchString(mergeBase) {
		return Report{}, fmt.Errorf("Git returned an invalid merge base")
	}
	if mergeBase != baseRevision {
		return Report{}, fmt.Errorf("pull-request head is stale or diverged: exact base revision must be its merge base")
	}
	diff, err := repository.gitOutput(ctx, maxGitOutput, "diff", "--name-status", "-z", "--find-renames", mergeBase, headRevision, "--")
	if err != nil {
		return Report{}, fmt.Errorf("read architecture diff: %w", err)
	}
	changes, err := parseNameStatus(diff)
	if err != nil {
		return Report{}, err
	}
	if len(changes) > maxChanges {
		return Report{}, fmt.Errorf("change count exceeds %d", maxChanges)
	}
	if err := repository.checkChanges(ctx, mergeBase, changes); err != nil {
		return Report{}, err
	}
	if err := repository.requireCleanHead(ctx, headRevision); err != nil {
		return Report{}, err
	}
	report.Changes = len(changes)
	return report, nil
}

func (r *repository) requireCompleteCheckout(ctx context.Context) error {
	shallow, err := r.gitOutput(ctx, 32, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return fmt.Errorf("inspect repository history: %w", err)
	}
	if strings.TrimSpace(string(shallow)) != "false" {
		return fmt.Errorf("architecture diff requires a complete non-shallow repository")
	}
	for _, key := range []string{"core.sparseCheckout", "core.sparseCheckoutCone"} {
		value, configured, err := r.gitConfigBool(ctx, key)
		if err != nil {
			return fmt.Errorf("inspect Git %s: %w", key, err)
		}
		if configured && value {
			return fmt.Errorf("Git sparse checkout configuration is forbidden for architecture validation")
		}
	}
	tracked, err := r.gitOutput(ctx, maxGitOutput, "ls-files", "-v", "-z")
	if err != nil {
		return fmt.Errorf("inspect tracked checkout inventory: %w", err)
	}
	if err := forEachNULField(tracked, func(field []byte) error {
		if len(field) < 3 || field[1] != ' ' {
			return fmt.Errorf("Git returned an invalid tracked-file inventory")
		}
		if field[0] != 'H' {
			return fmt.Errorf("sparse/skip-worktree, assume-unchanged, or non-materialized tracked files are forbidden for architecture validation")
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *repository) gitConfigBool(ctx context.Context, key string) (bool, bool, error) {
	// #nosec G204 -- key is selected only from the two fixed sparse-checkout keys above; root is resolved and trusted.
	command := exec.CommandContext(ctx, "git", "-C", r.root, "config", "--type=bool", "--get", key)
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "TZ=UTC", "GIT_CONFIG_NOSYSTEM=1", "HOME=/nonexistent"}
	stdout := &boundedBuffer{maximum: 32}
	stderr := &boundedBuffer{maximum: 256}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) && exitError.ExitCode() == 1 && !stdout.exceeded && !stderr.exceeded {
			return false, false, nil
		}
		return false, false, fmt.Errorf("Git config query failed")
	}
	if stdout.exceeded || stderr.exceeded {
		return false, false, fmt.Errorf("Git config output exceeded its safety limit")
	}
	switch strings.TrimSpace(stdout.String()) {
	case "true":
		return true, true, nil
	case "false":
		return false, true, nil
	default:
		return false, false, fmt.Errorf("Git returned an invalid boolean")
	}
}

func forEachNULField(data []byte, visit func([]byte) error) error {
	for len(data) != 0 {
		index := bytes.IndexByte(data, 0)
		if index < 0 {
			return fmt.Errorf("Git returned non-NUL-terminated path data")
		}
		field := data[:index]
		data = data[index+1:]
		if len(field) == 0 {
			continue
		}
		if err := visit(field); err != nil {
			return err
		}
	}
	return nil
}

func (r *repository) requireCleanHead(ctx context.Context, head string) error {
	current, err := r.gitOutput(ctx, 128, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("resolve checked-out HEAD: %w", err)
	}
	if strings.TrimSpace(string(current)) != head {
		return fmt.Errorf("checked-out HEAD does not match the requested head revision")
	}
	for _, arguments := range [][]string{
		{"diff", "--quiet", head, "--"},
		{"diff", "--cached", "--quiet", head, "--"},
	} {
		if _, err := r.gitOutput(ctx, 1024, arguments...); err != nil {
			return fmt.Errorf("working tree or index differs from the requested head revision")
		}
	}
	untracked, err := r.gitOutput(ctx, maxGitOutput, "ls-files", "--others", "-z")
	if err != nil {
		return fmt.Errorf("inspect untracked files: %w", err)
	}
	if len(untracked) != 0 {
		return fmt.Errorf("untracked files make architecture diff evidence incomplete")
	}
	return nil
}

func parseNameStatus(data []byte) ([]change, error) {
	if len(data) == 0 {
		return nil, nil
	}
	changes := make([]change, 0, min(len(data)/32, maxChanges+1))
	next := func() ([]byte, error) {
		index := bytes.IndexByte(data, 0)
		if index < 0 {
			return nil, fmt.Errorf("Git name-status output is not NUL terminated")
		}
		field := data[:index]
		data = data[index+1:]
		return field, nil
	}
	for len(data) != 0 {
		statusField, err := next()
		if err != nil {
			return nil, err
		}
		if len(statusField) == 0 {
			return nil, fmt.Errorf("Git diff contains an empty status")
		}
		status := statusField[0]
		switch status {
		case 'A', 'D', 'M', 'T':
			raw, err := next()
			if err != nil {
				return nil, fmt.Errorf("Git diff is missing a path: %w", err)
			}
			value, err := validateChangedPath(raw)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change{Status: status, Path: value})
		case 'R', 'C':
			oldRaw, err := next()
			if err != nil {
				return nil, fmt.Errorf("Git rename/copy is missing its old path: %w", err)
			}
			newRaw, err := next()
			if err != nil {
				return nil, fmt.Errorf("Git rename/copy is missing its new path: %w", err)
			}
			oldPath, err := validateChangedPath(oldRaw)
			if err != nil {
				return nil, err
			}
			newPath, err := validateChangedPath(newRaw)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change{Status: status, OldPath: oldPath, Path: newPath})
		default:
			return nil, fmt.Errorf("unsupported or unresolved Git diff status %q", status)
		}
		if len(changes) > maxChanges {
			return nil, fmt.Errorf("change count exceeds %d", maxChanges)
		}
	}
	return changes, nil
}

func validateChangedPath(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 4096 || !utf8.Valid(raw) {
		return "", fmt.Errorf("Git diff contains an invalid path")
	}
	value := string(raw)
	cleaned, err := safeRelativePath(value)
	if err != nil {
		return "", fmt.Errorf("Git diff path is unsafe: %w", err)
	}
	return cleaned, nil
}

func (r *repository) checkChanges(ctx context.Context, mergeBase string, changes []change) error {
	var found problems
	configuration, reviews := r.loadCurrentState(ctx, &found)
	tracked, trackedErr := r.trackedPaths(ctx)
	if trackedErr != nil {
		found.add(".", "cannot establish tracked evidence inventory: %v", trackedErr)
	} else {
		for _, record := range reviews {
			if record.Provider == nil {
				continue
			}
			for _, reference := range []string{record.Provider.Manifest, record.Provider.ConnectorSpec, record.Provider.CapabilityAudit, record.Provider.ConformancePlan} {
				if _, exists := tracked[reference]; !exists {
					found.add(reference, "provider review evidence must be an exact tracked HEAD file")
				}
			}
		}
	}
	changedReviews := make([]review, 0)
	addedPaths := make(map[string]struct{})
	changedPaths := make([]string, 0, len(changes)*2)
	casePaths := make(map[string]string)
	referencedADRs := make(map[string]struct{})
	for _, record := range reviews {
		for _, reference := range record.ExistingADRs {
			referencedADRs[reference] = struct{}{}
		}
		if record.ADR != nil {
			referencedADRs[*record.ADR] = struct{}{}
		}
	}

	for _, item := range changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		paths := []string{item.Path}
		if item.OldPath != "" {
			paths = append(paths, item.OldPath)
		}
		for _, changedPath := range paths {
			lower := strings.ToLower(changedPath)
			if previous, collision := casePaths[lower]; collision && previous != changedPath {
				found.add(changedPath, "case-collides with %s", previous)
			}
			casePaths[lower] = changedPath
			changedPaths = append(changedPaths, changedPath)
		}
		if item.Status == 'A' {
			addedPaths[item.Path] = struct{}{}
		}
		if strings.HasPrefix(item.Path, reviewsDir+"/") || strings.HasPrefix(item.OldPath, reviewsDir+"/") {
			if item.Status != 'A' || item.OldPath != "" {
				found.add(item.Path, "architecture review records are append-only")
				continue
			}
			match := reviewFilePattern.FindStringSubmatch(filepath.Base(item.Path))
			if match == nil {
				found.add(item.Path, "new architecture review filename is invalid")
			} else {
				reviewID := "ARCH-" + match[1] + strings.ToUpper(match[2])
				if record, exists := reviews[reviewID]; exists {
					changedReviews = append(changedReviews, record)
				} else {
					found.add(item.Path, "new architecture review did not load as valid evidence")
				}
			}
		}
		for _, changedPath := range paths {
			if !strings.HasPrefix(changedPath, "adr/") || (item.Status == 'A' && changedPath == item.Path) {
				continue
			}
			if _, cited := referencedADRs[changedPath]; cited {
				found.add(changedPath, "accepted ADRs are immutable; ADRs cited by architecture reviews are also immutable; add a superseding ADR")
				continue
			}
			data, err := r.gitFile(ctx, mergeBase, changedPath, maxReviewBytes)
			if err != nil {
				found.add(changedPath, "cannot verify base ADR immutability: %v", err)
			} else if markdownADRStatus(string(data)) == "Accepted" {
				found.add(changedPath, "accepted ADRs are immutable; add a superseding ADR")
			}
		}
	}

	basePolicyData, basePolicyErr := r.gitFile(ctx, mergeBase, policyPath, maxPolicyBytes)
	var basePolicy policy
	basePolicyValid := false
	if basePolicyErr != nil {
		found.add(policyPath, "cannot read merge-base architecture policy: %v", basePolicyErr)
	} else if err := decodeStrictJSON(ctx, basePolicyData, &basePolicy); err != nil {
		found.add(policyPath, "merge-base policy is invalid: %v", err)
	} else {
		basePolicyValid = true
	}
	removedProviderIDs := make(map[string]struct{})
	if basePolicyValid {
		_, _, removed := providerDefinitionChanges(basePolicy.Providers, configuration.Providers)
		for _, providerID := range removed {
			removedProviderIDs[providerID] = struct{}{}
		}
	}

	corePaths := filterPaths(changedPaths, func(value string) bool {
		return isCoreOrPlatformPath(value)
	})
	providerPaths := filterPaths(changedPaths, func(value string) bool {
		return isProviderPath(value) || providerEvidenceOwner(value, configuration) != "" ||
			(basePolicyValid && providerEvidenceOwner(value, basePolicy) != "")
	})
	selfPaths := filterPaths(changedPaths, isSelfProtected)
	frozenPaths := filterPaths(changedPaths, isFrozenDefinitionPath)

	for _, changedPath := range changedPaths {
		if !isSensitive(changedPath, configuration) && (!basePolicyValid || !isSensitive(changedPath, basePolicy)) {
			continue
		}
		switch {
		case isSelfProtected(changedPath) || isFrozenDefinitionPath(changedPath):
			if !reviewCoversPathByClass(changedReviews, changedPath, "pillar_change", "mixed") {
				found.add(changedPath, "architecture gate and frozen-definition paths require exact pillar_change or mixed review coverage")
			}
		case isProviderPath(changedPath):
			providerID := providerIDFromPath(changedPath)
			if providerID == "" {
				if !reviewCoversPathByClass(changedReviews, changedPath, "new_provider", "mixed") {
					found.add(changedPath, "provider-root paths require exact new_provider or mixed review coverage")
				}
			} else if _, removed := removedProviderIDs[providerID]; removed {
				if !reviewCoversPathByClass(changedReviews, changedPath, "pillar_change", "mixed") {
					found.add(changedPath, "provider retirement paths require exact pillar_change or mixed review coverage")
				}
			} else if !reviewCoversProviderPath(changedReviews, changedPath, providerID, "new_provider", "mixed") {
				found.add(changedPath, "provider path requires exact fresh provider evidence for id %q", providerID)
			}
		case providerEvidenceOwner(changedPath, configuration) != "" || (basePolicyValid && providerEvidenceOwner(changedPath, basePolicy) != ""):
			providerID := providerEvidenceOwner(changedPath, configuration)
			if providerID == "" {
				providerID = providerEvidenceOwner(changedPath, basePolicy)
			}
			if !reviewCoversProviderPath(changedReviews, changedPath, providerID, "new_provider", "mixed") {
				found.add(changedPath, "provider evidence change requires exact fresh provider review for id %q", providerID)
			}
		case changedPath == policyPath:
			if !reviewCoversPathByClass(changedReviews, changedPath, "new_domain", "new_provider", "pillar_change", "mixed") {
				found.add(changedPath, "architecture policy requires exact classified review coverage")
			}
		case isCoreOrPlatformPath(changedPath):
			if !reviewCoversPathByClass(changedReviews, changedPath, "implementation", "new_domain", "pillar_change", "mixed") {
				found.add(changedPath, "sensitive change requires a new architecture review with exact compatible class coverage")
			}
		default:
			if !reviewCoversPath(changedReviews, changedPath) {
				found.add(changedPath, "no new architecture review lists this exact sensitive path")
			}
		}
	}
	if len(corePaths) != 0 && len(providerPaths) != 0 {
		providerGroups := make(map[string][]string)
		for _, changedPath := range providerPaths {
			providerID := providerIDFromPath(changedPath)
			if providerID == "" {
				providerID = providerEvidenceOwner(changedPath, configuration)
			}
			if providerID == "" && basePolicyValid {
				providerID = providerEvidenceOwner(changedPath, basePolicy)
			}
			providerGroups[providerID] = append(providerGroups[providerID], changedPath)
		}
		for providerID, paths := range providerGroups {
			required := append(append([]string(nil), corePaths...), paths...)
			if !oneMixedReviewCovers(changedReviews, providerID, required) {
				label := providerID
				if label == "" {
					label = "provider-root"
				}
				found.add("", "one fresh mixed review for %q must cover every provider and Core/Platform path in the combined change", label)
			}
		}
	}
	if (len(selfPaths) != 0 || len(frozenPaths) != 0) && !pathsCoveredByClass(changedReviews, append(selfPaths, frozenPaths...), "pillar_change", "mixed") {
		found.add("", "architecture gate or frozen-definition changes require a pillar_change or mixed review")
	}

	if basePolicyValid {
		addedModules := addedModulePaths(basePolicy.Modules, configuration.Modules)
		for _, modulePath := range addedModules {
			if !reviewCoversPrefixByClass(changedReviews, modulePath+"/", "new_domain", "mixed", "pillar_change") {
				found.add(modulePath, "new module source must be covered by its new_domain, pillar_change, or mixed review")
			}
		}
		addedProviders, changedProviders, removedProviders := providerDefinitionChanges(basePolicy.Providers, configuration.Providers)
		addedRetired, changedRetired, removedRetired := retiredProviderDefinitionChanges(basePolicy.RetiredProviders, configuration.RetiredProviders)
		for _, providerID := range addedProviders {
			if !reviewCoversProviderPath(changedReviews, policyPath, providerID, "new_provider", "mixed") {
				found.add(policyPath, "new provider %q requires matching fresh provider evidence scoped to the policy", providerID)
			}
		}
		for _, providerID := range changedProviders {
			if !reviewCoversProviderPath(changedReviews, policyPath, providerID, "mixed") {
				found.add(policyPath, "existing provider %q policy changes require matching mixed review evidence", providerID)
			}
		}
		if len(changedRetired) != 0 || len(removedRetired) != 0 {
			found.add(policyPath, "retired provider tombstones are immutable; changed=%v removed=%v", changedRetired, removedRetired)
		}
		baseProviders := make(map[string]provider, len(basePolicy.Providers))
		for _, item := range basePolicy.Providers {
			baseProviders[item.ID] = item
		}
		headRetired := make(map[string]retiredProvider, len(configuration.RetiredProviders))
		for _, item := range configuration.RetiredProviders {
			headRetired[item.ID] = item
		}
		removedSet := make(map[string]struct{}, len(removedProviders))
		addedRetiredSet := make(map[string]struct{}, len(addedRetired))
		for _, providerID := range addedRetired {
			addedRetiredSet[providerID] = struct{}{}
		}
		for _, providerID := range removedProviders {
			removedSet[providerID] = struct{}{}
			tombstone, exists := headRetired[providerID]
			if !exists {
				found.add(policyPath, "removed provider %q requires a matching explicit retired_providers tombstone", providerID)
				continue
			}
			if _, newlyAdded := addedRetiredSet[providerID]; !newlyAdded {
				found.add(policyPath, "removed provider %q requires a newly added retired_providers tombstone in the same change", providerID)
			}
			if !retiredProviderMatchesProvider(tombstone, baseProviders[providerID]) {
				found.add(policyPath, "retired provider %q must preserve the merge-base implementation and canonical evidence paths", providerID)
			}
			retirementPaths, deletedGo := providerRetirementPaths(changes, tombstone.Implementation)
			if !deletedGo {
				found.add(tombstone.Implementation, "provider retirement must delete or move every implementation Go source, including at least one merge-base Go file")
			}
			if !retirementReviewCovers(changedReviews, tombstone.RetirementTask, retirementPaths) {
				found.add(policyPath, "provider retirement %q requires one fresh pillar_change or mixed review for Task %s covering the policy and every retired implementation path", providerID, tombstone.RetirementTask)
			}
		}
		for _, providerID := range addedRetired {
			if _, removed := removedSet[providerID]; !removed {
				found.add(policyPath, "new retired provider %q was not an active provider in the merge base and removed by this change", providerID)
			}
		}
		if len(removedProviders) != 0 && !reviewCoversPathByClass(changedReviews, policyPath, "pillar_change", "mixed") {
			found.add(policyPath, "provider removals require pillar_change or mixed review coverage")
		}
		if policyControlsChanged(basePolicy, configuration) && !reviewCoversPathByClass(changedReviews, policyPath, "pillar_change", "mixed") {
			found.add(policyPath, "architecture control, admission, allowlist, removal, or classification changes require a pillar_change or mixed review")
		}
		r.checkAdmissionTaskTransitions(ctx, mergeBase, changes, configuration.ProviderAdmission.RequiredTasks, changedReviews, &found)
		if !basePolicy.ProviderAdmission.Enabled && configuration.ProviderAdmission.Enabled {
			for _, task := range configuration.ProviderAdmission.RequiredTasks {
				if !r.baseTaskCompleted(ctx, mergeBase, task) {
					found.add(policyPath, "provider admission requires Task %s to be completed in the merge base, not in the admission change", task)
				}
			}
		}
	}

	for _, record := range changedReviews {
		if record.ADR == nil {
			continue
		}
		if _, added := addedPaths[*record.ADR]; !added {
			found.add(*record.ADR, "pillar decision ADR must be newly added in this change set")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return found.err()
}

func (r *repository) trackedPaths(ctx context.Context) (map[string]struct{}, error) {
	data, err := r.gitOutput(ctx, maxGitOutput, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	result := make(map[string]struct{})
	err = forEachNULField(data, func(raw []byte) error {
		if len(result) >= maxTreeEntries {
			return fmt.Errorf("tracked-file inventory exceeds %d entries", maxTreeEntries)
		}
		value, err := validateChangedPath(raw)
		if err != nil {
			return err
		}
		result[value] = struct{}{}
		return nil
	})
	return result, err
}

func (r *repository) loadCurrentState(ctx context.Context, found *problems) (policy, map[string]review) {
	data, err := r.readRegular(policyPath, maxPolicyBytes)
	if err != nil {
		found.add(policyPath, "%v", err)
		return policy{}, nil
	}
	var configuration policy
	if err := decodeStrictJSON(ctx, data, &configuration); err != nil {
		found.add(policyPath, "%v", err)
		return policy{}, nil
	}
	return configuration, r.loadReviews(ctx, &configuration, found)
}

func isSensitive(value string, configuration policy) bool {
	for _, rule := range configuration.SensitivePaths {
		if pathRuleMatches(rule, value) {
			return true
		}
	}
	if providerEvidenceOwner(value, configuration) != "" {
		return true
	}
	return isProviderPath(value) || isSelfProtected(value)
}

func isProviderPath(value string) bool {
	return strings.HasPrefix(value, "connectors/") || strings.HasPrefix(value, "plugins/")
}

func providerIDFromPath(value string) string {
	parts := strings.Split(value, "/")
	if len(parts) < 2 || (parts[0] != "connectors" && parts[0] != "plugins") {
		return ""
	}
	if len(parts) >= 4 && providerIDPattern.MatchString(parts[2]) {
		return parts[2]
	}
	if len(parts) == 3 && !strings.Contains(parts[2], ".") && providerIDPattern.MatchString(parts[2]) {
		return parts[2]
	}
	if providerIDPattern.MatchString(parts[1]) {
		return parts[1]
	}
	return ""
}

func providerEvidenceOwner(value string, configuration policy) string {
	for _, item := range configuration.Providers {
		if value == item.Manifest || value == item.ConnectorSpec || value == item.CapabilityAudit || value == item.ConformancePlan {
			return item.ID
		}
	}
	for _, item := range configuration.RetiredProviders {
		if value == item.Manifest || value == item.ConnectorSpec || value == item.CapabilityAudit || value == item.ConformancePlan {
			return item.ID
		}
	}
	return ""
}

func isCoreOrPlatformPath(value string) bool {
	return strings.HasPrefix(value, "internal/core/") || strings.HasPrefix(value, "internal/platform/") ||
		strings.HasPrefix(value, "internal/app/") || strings.HasPrefix(value, "cmd/")
}

func isFrozenDefinitionPath(value string) bool {
	return value == "docs/01-architecture.md" || value == "docs/03-module-boundaries.md" ||
		value == "docs/54-architecture-freeze-v1.md"
}

func isSelfProtected(value string) bool {
	for _, rule := range selfProtectedPaths {
		if pathRuleMatches(rule, value) {
			return true
		}
	}
	return false
}

func pathRuleMatches(rule, value string) bool {
	if strings.HasSuffix(rule, "/") {
		return strings.HasPrefix(value, rule)
	}
	return value == rule
}

func reviewCoversPath(reviews []review, changedPath string) bool {
	for _, record := range reviews {
		for _, scope := range record.Scopes {
			if scope == changedPath {
				return true
			}
		}
	}
	return false
}

func reviewCoversPathByClass(reviews []review, changedPath string, classes ...string) bool {
	for _, record := range reviews {
		if !reviewHasClass(record, classes...) {
			continue
		}
		for _, scope := range record.Scopes {
			if scope == changedPath {
				return true
			}
		}
	}
	return false
}

func reviewCoversProviderPath(reviews []review, changedPath, providerID string, classes ...string) bool {
	for _, record := range reviews {
		if record.Provider == nil || record.Provider.ID != providerID || !reviewHasClass(record, classes...) {
			continue
		}
		for _, scope := range record.Scopes {
			if scope == changedPath {
				return true
			}
		}
	}
	return false
}

func reviewHasClass(record review, classes ...string) bool {
	for _, class := range classes {
		if record.ChangeClass == class {
			return true
		}
	}
	return false
}

func oneMixedReviewCovers(reviews []review, providerID string, requiredPaths []string) bool {
	for _, record := range reviews {
		if record.ChangeClass != "mixed" || record.Provider == nil {
			continue
		}
		if providerID != "" && record.Provider.ID != providerID {
			continue
		}
		scopes := make(map[string]struct{}, len(record.Scopes))
		for _, scope := range record.Scopes {
			scopes[scope] = struct{}{}
		}
		complete := true
		for _, required := range requiredPaths {
			if _, exists := scopes[required]; !exists {
				complete = false
				break
			}
		}
		if complete {
			return true
		}
	}
	return false
}

func pathsCoveredByClass(reviews []review, paths []string, classes ...string) bool {
	for _, changedPath := range paths {
		if !reviewCoversPathByClass(reviews, changedPath, classes...) {
			return false
		}
	}
	return true
}

func reviewCoversPrefix(reviews []review, prefix string) bool {
	for _, record := range reviews {
		for _, scope := range record.Scopes {
			if strings.HasPrefix(scope, prefix) {
				return true
			}
		}
	}
	return false
}

func reviewCoversPrefixByClass(reviews []review, prefix string, classes ...string) bool {
	for _, record := range reviews {
		if !reviewHasClass(record, classes...) {
			continue
		}
		for _, scope := range record.Scopes {
			if strings.HasPrefix(scope, prefix) {
				return true
			}
		}
	}
	return false
}

func hasReviewClass(reviews []review, classes ...string) bool {
	allowed := make(map[string]struct{}, len(classes))
	for _, class := range classes {
		allowed[class] = struct{}{}
	}
	for _, record := range reviews {
		if _, ok := allowed[record.ChangeClass]; ok {
			return true
		}
	}
	return false
}

func filterPaths(values []string, predicate func(string) bool) []string {
	result := make([]string, 0)
	seen := make(map[string]struct{})
	for _, value := range values {
		if !predicate(value) {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func addedModulePaths(before, after []module) []string {
	known := make(map[string]struct{}, len(before))
	for _, item := range before {
		known[item.Path] = struct{}{}
	}
	var result []string
	for _, item := range after {
		if _, exists := known[item.Path]; !exists {
			result = append(result, item.Path)
		}
	}
	sort.Strings(result)
	return result
}

func providerDefinitionChanges(before, after []provider) (added, changed, removed []string) {
	beforeByID := make(map[string]provider, len(before))
	for _, item := range before {
		beforeByID[item.ID] = item
	}
	afterByID := make(map[string]provider, len(after))
	for _, item := range after {
		afterByID[item.ID] = item
		previous, exists := beforeByID[item.ID]
		switch {
		case !exists:
			added = append(added, item.ID)
		case !reflect.DeepEqual(previous, item):
			changed = append(changed, item.ID)
		}
	}
	for _, item := range before {
		if _, exists := afterByID[item.ID]; !exists {
			removed = append(removed, item.ID)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return added, changed, removed
}

func retiredProviderDefinitionChanges(before, after []retiredProvider) (added, changed, removed []string) {
	beforeByID := make(map[string]retiredProvider, len(before))
	for _, item := range before {
		beforeByID[item.ID] = item
	}
	afterByID := make(map[string]retiredProvider, len(after))
	for _, item := range after {
		afterByID[item.ID] = item
		previous, exists := beforeByID[item.ID]
		switch {
		case !exists:
			added = append(added, item.ID)
		case !reflect.DeepEqual(previous, item):
			changed = append(changed, item.ID)
		}
	}
	for _, item := range before {
		if _, exists := afterByID[item.ID]; !exists {
			removed = append(removed, item.ID)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return added, changed, removed
}

func retiredProviderMatchesProvider(retired retiredProvider, current provider) bool {
	return retired.ID == current.ID && retired.Implementation == current.Implementation &&
		retired.Manifest == current.Manifest && retired.ConnectorSpec == current.ConnectorSpec &&
		retired.CapabilityAudit == current.CapabilityAudit && retired.ConformancePlan == current.ConformancePlan
}

func providerRetirementPaths(changes []change, implementation string) ([]string, bool) {
	prefix := implementation + "/"
	paths := make([]string, 0)
	seen := make(map[string]struct{})
	deletedGo := false
	add := func(value string) {
		if !strings.HasPrefix(value, prefix) {
			return
		}
		if _, duplicate := seen[value]; duplicate {
			return
		}
		seen[value] = struct{}{}
		paths = append(paths, value)
	}
	for _, item := range changes {
		add(item.Path)
		add(item.OldPath)
		if (item.Status == 'D' && strings.HasPrefix(item.Path, prefix) && strings.HasSuffix(item.Path, ".go")) ||
			(item.Status == 'R' && strings.HasPrefix(item.OldPath, prefix) && strings.HasSuffix(item.OldPath, ".go")) {
			deletedGo = true
		}
	}
	sort.Strings(paths)
	return paths, deletedGo
}

func retirementReviewCovers(reviews []review, task string, requiredPaths []string) bool {
	if len(requiredPaths) == 0 {
		return false
	}
	for _, record := range reviews {
		if record.Task != task || !reviewHasClass(record, "pillar_change", "mixed") {
			continue
		}
		scopes := make(map[string]struct{}, len(record.Scopes))
		for _, scope := range record.Scopes {
			scopes[scope] = struct{}{}
		}
		if _, exists := scopes[policyPath]; !exists {
			continue
		}
		complete := true
		for _, required := range requiredPaths {
			if _, exists := scopes[required]; !exists {
				complete = false
				break
			}
		}
		if complete {
			return true
		}
	}
	return false
}

func policyControlsChanged(before, after policy) bool {
	beforeModules := make(map[string]string, len(before.Modules))
	for _, item := range before.Modules {
		beforeModules[item.Path] = item.Kind
	}
	afterModules := make(map[string]string, len(after.Modules))
	for _, item := range after.Modules {
		afterModules[item.Path] = item.Kind
	}
	for modulePath, kind := range beforeModules {
		if afterModules[modulePath] != kind {
			return true
		}
	}
	beforeProviders := make(map[string]provider, len(before.Providers))
	for _, item := range before.Providers {
		beforeProviders[item.ID] = item
	}
	afterProviders := make(map[string]provider, len(after.Providers))
	for _, item := range after.Providers {
		afterProviders[item.ID] = item
	}
	for id, previous := range beforeProviders {
		current, exists := afterProviders[id]
		if !exists || !reflect.DeepEqual(previous, current) {
			return true
		}
	}
	before.Modules, before.Providers = nil, nil
	after.Modules, after.Providers = nil, nil
	return !reflect.DeepEqual(before, after)
}

type taskIssueState struct {
	path      string
	completed bool
}

func (r *repository) checkAdmissionTaskTransitions(ctx context.Context, mergeBase string, changes []change, requiredTasks []string, reviews []review, found *problems) {
	changedByTask := make(map[string][]string)
	for _, item := range changes {
		for _, changedPath := range []string{item.OldPath, item.Path} {
			task := admissionTaskForPath(changedPath, requiredTasks)
			if task == "" {
				continue
			}
			changedByTask[task] = append(changedByTask[task], changedPath)
		}
	}
	for task, paths := range changedByTask {
		if err := ctx.Err(); err != nil {
			return
		}
		paths = uniqueSorted(paths)
		base, err := r.gitTaskIssueState(ctx, mergeBase, task)
		if err != nil {
			found.add("tasks/issues", "cannot establish merge-base state for provider-admission prerequisite Task %s: %v", task, err)
			continue
		}
		head, err := r.currentTaskIssueState(task)
		if err != nil {
			found.add("tasks/issues", "provider-admission prerequisite Task %s issue was removed or became ambiguous: %v", task, err)
			continue
		}
		if base.path != head.path {
			found.add(head.path, "provider-admission prerequisite Task %s issue may not be renamed", task)
			continue
		}
		if base.completed {
			found.add(base.path, "completed provider-admission prerequisite Task %s issue is immutable", task)
			continue
		}
		if !head.completed {
			continue
		}
		if !taskCompletionReviewCovers(reviews, task, paths) {
			found.add(head.path, "completion of provider-admission prerequisite Task %s requires one fresh task-bound architecture review covering every changed issue path", task)
		}
	}
}

func admissionTaskForPath(value string, requiredTasks []string) string {
	if value == "" || !taskIssuePathPattern.MatchString(value) {
		return ""
	}
	name := strings.TrimPrefix(value, "tasks/issues/")
	for _, task := range requiredTasks {
		if strings.HasPrefix(name, task+"-") {
			return task
		}
	}
	return ""
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func taskCompletionReviewCovers(reviews []review, task string, requiredPaths []string) bool {
	if len(requiredPaths) == 0 {
		return false
	}
	for _, record := range reviews {
		if record.Task != task || record.ChangeClass == "new_provider" {
			continue
		}
		scopes := make(map[string]struct{}, len(record.Scopes))
		for _, scope := range record.Scopes {
			scopes[scope] = struct{}{}
		}
		complete := true
		for _, required := range requiredPaths {
			if _, exists := scopes[required]; !exists {
				complete = false
				break
			}
		}
		if complete {
			return true
		}
	}
	return false
}

func (r *repository) currentTaskIssueState(task string) (taskIssueState, error) {
	if !taskPattern.MatchString(task) {
		return taskIssueState{}, fmt.Errorf("invalid task id")
	}
	matches, err := filepath.Glob(filepath.Join(r.root, "tasks", "issues", task+"-*.md"))
	if err != nil || len(matches) != 1 {
		return taskIssueState{}, fmt.Errorf("expected exactly one canonical issue")
	}
	relative, err := filepath.Rel(r.root, matches[0])
	if err != nil {
		return taskIssueState{}, fmt.Errorf("resolve issue path")
	}
	canonical := filepath.ToSlash(relative)
	if !taskIssuePathPattern.MatchString(canonical) {
		return taskIssueState{}, fmt.Errorf("issue path is not canonical")
	}
	data, err := r.readRegular(canonical, maxReviewBytes)
	if err != nil {
		return taskIssueState{}, err
	}
	return taskIssueState{path: canonical, completed: completedTaskStatus(data)}, nil
}

func (r *repository) gitTaskIssueState(ctx context.Context, revision, task string) (taskIssueState, error) {
	if !taskPattern.MatchString(task) {
		return taskIssueState{}, fmt.Errorf("invalid task id")
	}
	listing, err := r.gitOutput(ctx, maxGitOutput, "ls-tree", "-r", "--name-only", "-z", revision, "--", "tasks/issues")
	if err != nil {
		return taskIssueState{}, err
	}
	prefix := "tasks/issues/" + task + "-"
	var match string
	err = forEachNULField(listing, func(raw []byte) error {
		candidate, candidateErr := validateChangedPath(raw)
		if candidateErr != nil {
			return candidateErr
		}
		if !taskIssuePathPattern.MatchString(candidate) || !strings.HasPrefix(candidate, prefix) {
			return nil
		}
		if match != "" {
			return fmt.Errorf("multiple canonical issues found")
		}
		match = candidate
		return nil
	})
	if err != nil {
		return taskIssueState{}, err
	}
	if match == "" {
		return taskIssueState{}, fmt.Errorf("canonical issue is missing")
	}
	data, err := r.gitFile(ctx, revision, match, maxReviewBytes)
	if err != nil {
		return taskIssueState{}, err
	}
	return taskIssueState{path: match, completed: completedTaskStatus(data)}, nil
}

func (r *repository) baseTaskCompleted(ctx context.Context, revision, task string) bool {
	state, err := r.gitTaskIssueState(ctx, revision, task)
	return err == nil && state.completed
}

func (r *repository) gitFile(ctx context.Context, revision, relative string, maximum int) ([]byte, error) {
	if _, err := safeRelativePath(relative); err != nil {
		return nil, err
	}
	return r.gitOutput(ctx, maximum, "show", revision+":"+relative)
}

func (r *repository) gitOutput(ctx context.Context, maximum int, arguments ...string) ([]byte, error) {
	// #nosec G204 -- callers construct fixed Git subcommands; every revision and repository path argument is validated first.
	command := exec.CommandContext(ctx, "git", append([]string{"-C", r.root}, arguments...)...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C", "TZ=UTC", "GIT_CONFIG_NOSYSTEM=1", "HOME=/nonexistent"}
	stdout := &boundedBuffer{maximum: maximum}
	stderr := &boundedBuffer{maximum: 4096}
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, fmt.Errorf("Git output exceeded its safety limit")
	}
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = "Git command failed"
		}
		return nil, fmt.Errorf("%s", message)
	}
	return stdout.Bytes(), nil
}

type boundedBuffer struct {
	bytes.Buffer
	maximum  int
	exceeded bool
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	if b.exceeded {
		return len(value), nil
	}
	remaining := b.maximum - b.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(value), nil
	}
	if len(value) > remaining {
		_, _ = b.Buffer.Write(value[:remaining])
		b.exceeded = true
		return len(value), nil
	}
	return b.Buffer.Write(value)
}
