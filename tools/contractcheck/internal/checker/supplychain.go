package checker

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/tools/contractcheck/internal/licensepolicy"
	"gopkg.in/yaml.v3"
)

const (
	maxPolicyFileSize = 1 << 20
	policyVersion     = 1
)

var (
	fullCommitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256RE     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	h1SumRE      = regexp.MustCompile(`^h1:[A-Za-z0-9+/]{43}=$`)
	semverRE     = regexp.MustCompile(`^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-[0-9A-Za-z][0-9A-Za-z.-]*)?(?:\+[0-9A-Za-z.-]+)?$`)
	actionNameRE = regexp.MustCompile(`^[a-z0-9_.-]+/[a-z0-9_.-]+(?:/[a-z0-9_.-]+)*$`)
	artifactRE   = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

const (
	releaseInventoryPath = "supply-chain/release-artifacts.json"
	toolVersionsPath     = "supply-chain/tool-versions.json"
	actionPinsPath       = "supply-chain/action-pins.json"
	ciWorkflowPath       = ".github/workflows/ci.yml"
)

const trustedArchitectureRun = `set -euo pipefail
[[ "$BASE_REVISION" =~ ^[0-9a-f]{40}$ ]]
[[ "$HEAD_REVISION" =~ ^[0-9a-f]{40}$ ]]
[[ "$RUNNER_TEMP" == /* && "$GITHUB_WORKSPACE" == /* ]]
[[ "$(git -C "$GITHUB_WORKSPACE" rev-parse --verify HEAD)" == "$HEAD_REVISION" ]]
trusted_base="$RUNNER_TEMP/torgnexa-architecture-base"
trusted_checker="$RUNNER_TEMP/torgnexa-architecturecheck"
trusted_cache="$RUNNER_TEMP/torgnexa-architecture-gocache"
trusted_home="$RUNNER_TEMP/torgnexa-architecture-home"
[[ ! -e "$trusted_base" && ! -e "$trusted_checker" && ! -e "$trusted_cache" && ! -e "$trusted_home" ]]
mkdir -m 0700 -- "$trusted_cache" "$trusted_home"
git -C "$GITHUB_WORKSPACE" cat-file -e "$BASE_REVISION^{commit}"
git -C "$GITHUB_WORKSPACE" -c core.hooksPath=/dev/null worktree add --detach "$trusted_base" "$BASE_REVISION"
[[ "$(git -C "$trusted_base" rev-parse --verify HEAD)" == "$BASE_REVISION" ]]
(
  cd -- "$trusted_base"
  env -i \
    CGO_ENABLED=0 \
    GOCACHE="$trusted_cache" \
    GOENV=off \
    GOTELEMETRY=off \
    GOTOOLCHAIN=local \
    GOWORK=off \
    GOPROXY=off \
    GOSUMDB=off \
    HOME="$trusted_home" \
    LC_ALL=C \
    PATH="$PATH" \
    TMPDIR="$RUNNER_TEMP" \
    TZ=UTC \
    go build -buildvcs=false -mod=readonly -trimpath -o "$trusted_checker" ./tools/architecturecheck
)
[[ -f "$trusted_checker" && ! -L "$trusted_checker" ]]
env -i HOME="$trusted_home" LC_ALL=C PATH=/usr/bin:/bin TZ=UTC \
  "$trusted_checker" --root "$GITHUB_WORKSPACE" --base "$BASE_REVISION" --head "$HEAD_REVISION"`

// CheckSupplyChain validates the repository's offline supply-chain policy.
func CheckSupplyChain(ctx context.Context, root string) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if root == "" {
		return fmt.Errorf("repository root is required")
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	var problems diagnostics
	if !checkSupplyContext(ctx, &problems) {
		return problems.err()
	}

	pins := loadActionPins(ctx, absRoot, &problems)
	workflows := loadWorkflows(ctx, absRoot, &problems)
	checkWorkflows(absRoot, workflows, pins, &problems)
	checkReleaseWorkflow(workflows, &problems)
	checkRepositoryImages(ctx, absRoot, &problems)
	tools := checkToolVersions(ctx, absRoot, &problems)
	checkGoModulePolicy(ctx, absRoot, tools, &problems)
	publicReleaseReady := checkReleaseInventory(ctx, absRoot, &problems)
	checkAuxiliaryPolicyFiles(ctx, absRoot, publicReleaseReady, &problems)
	return problems.err()
}

func checkSupplyContext(ctx context.Context, problems *diagnostics) bool {
	if err := ctx.Err(); err != nil {
		problems.add("supply-chain", "validation interrupted: %v", err)
		return false
	}
	return true
}

type actionPinManifest struct {
	Version int         `json:"version"`
	Actions []actionPin `json:"actions"`
}

type actionPin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

func loadActionPins(ctx context.Context, root string, problems *diagnostics) map[string]actionPin {
	var manifest actionPinManifest
	if !readPolicyJSON(ctx, root, actionPinsPath, &manifest, problems) {
		return nil
	}
	if manifest.Version != policyVersion {
		problems.add(actionPinsPath, "version must be %d", policyVersion)
	}
	pins := make(map[string]actionPin, len(manifest.Actions))
	previous := ""
	for _, pin := range manifest.Actions {
		if pin.Name <= previous && previous != "" {
			problems.add(actionPinsPath, "actions must be strictly sorted by name")
		}
		previous = pin.Name
		if !actionNameRE.MatchString(pin.Name) {
			problems.add(actionPinsPath, "action name %q is invalid", pin.Name)
		}
		if !validPinnedVersion(pin.Version) {
			problems.add(actionPinsPath, "action %q version must be an exact semantic version", pin.Name)
		}
		if !fullCommitRE.MatchString(pin.Commit) {
			problems.add(actionPinsPath, "action %q commit must be 40 lowercase hexadecimal characters", pin.Name)
		}
		if _, duplicate := pins[pin.Name]; duplicate {
			problems.add(actionPinsPath, "duplicate action %q", pin.Name)
		}
		pins[pin.Name] = pin
	}
	if len(pins) == 0 {
		problems.add(actionPinsPath, "at least one action pin is required")
	}
	actionNames := make([]string, 0, len(manifest.Actions))
	for _, pin := range manifest.Actions {
		actionNames = append(actionNames, pin.Name)
	}
	checkExactSortedNames(actionPinsPath, "actions", actionNames, []string{
		"actions/attest",
		"actions/checkout",
		"actions/download-artifact",
		"actions/setup-go",
		"actions/setup-node",
		"actions/upload-artifact",
	}, problems)
	return pins
}

type workflowPolicy struct {
	Rel  string
	Root *yaml.Node
	Jobs map[string]*yaml.Node
}

func loadWorkflows(ctx context.Context, root string, problems *diagnostics) []workflowPolicy {
	const workflowsPath = ".github/workflows"
	directory, err := resolveRealRepositoryPath(root, workflowsPath, true)
	if err != nil {
		problems.add(workflowsPath, "%v", err)
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		problems.add(workflowsPath, "read directory: %v", err)
		return nil
	}
	var workflows []workflowPolicy
	for _, entry := range entries {
		if !checkSupplyContext(ctx, problems) {
			break
		}
		name := entry.Name()
		relative := workflowsPath + "/" + name
		if entry.Type()&os.ModeSymlink != 0 {
			problems.add(relative, "symlinks are forbidden")
			continue
		}
		if entry.IsDir() {
			problems.add(relative, "workflow subdirectories are forbidden")
			continue
		}
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".yml") && !strings.HasSuffix(lower, ".yaml") {
			continue
		}
		data, ok := readRepositoryFile(root, relative, problems)
		if !ok {
			continue
		}
		node, err := parseStrictYAML(data)
		if err != nil {
			problems.add(relative, "invalid workflow YAML: %v", err)
			continue
		}
		rootMap := yamlDocumentMap(node)
		if rootMap == nil {
			problems.add(relative, "workflow root must be a mapping")
			continue
		}
		jobsNode := yamlMapValue(rootMap, "jobs")
		jobs := yamlNamedMappings(relative, "jobs", jobsNode, problems)
		workflows = append(workflows, workflowPolicy{Rel: relative, Root: rootMap, Jobs: jobs})
	}
	sort.Slice(workflows, func(i, j int) bool { return workflows[i].Rel < workflows[j].Rel })
	if len(workflows) == 0 {
		problems.add(workflowsPath, "at least one workflow is required")
	}
	return workflows
}

func checkWorkflows(root string, workflows []workflowPolicy, pins map[string]actionPin, problems *diagnostics) {
	usedPins := make(map[string]struct{})
	checkedLocalActions := make(map[string]struct{})
	for _, workflow := range workflows {
		permissions := yamlMapValue(workflow.Root, "permissions")
		if !isContentsReadPermission(permissions) {
			problems.add(workflow.Rel, "top-level permissions must be exactly contents: read")
		}
		if workflowHasTrigger(workflow.Root, "pull_request_target") {
			problems.add(workflow.Rel, "pull_request_target is forbidden")
		}
		pullRequestWorkflow := workflowHasTrigger(workflow.Root, "pull_request")
		for jobName, job := range workflow.Jobs {
			checkJobPermissions(workflow.Rel, jobName, job, problems)
			if pullRequestWorkflow {
				checkPullRequestPermissions(workflow.Rel, jobName, job, problems)
			}
			checkContinueOnError(workflow.Rel, jobName, job, problems)
			checkJobRunner(workflow.Rel, jobName, job, problems)
			checkWorkflowSteps(root, workflow.Rel, jobName, job, pins, usedPins, checkedLocalActions, problems)
		}
		checkArchitectureWorkflow(workflow, problems)
		walkYAMLMapping(workflow.Root, func(key, value *yaml.Node) {
			switch key.Value {
			case "uses":
				checkActionUse(root, workflow.Rel, value, pins, usedPins, checkedLocalActions, problems)
			case "image":
				checkYAMLImage(workflow.Rel, value, problems)
			case "run":
				if value.Kind == yaml.ScalarNode {
					lower := strings.ToLower(value.Value)
					if strings.Contains(lower, "@latest") {
						problems.add(workflow.Rel, "workflow commands may not use @latest")
					}
					if strings.Contains(lower, "curl ") || strings.Contains(lower, "wget ") {
						problems.add(workflow.Rel, "workflow downloads must be implemented by checksum-verifying repository scripts")
					}
					if strings.Contains(value.Value, "${{") {
						problems.add(workflow.Rel, "workflow expressions must enter run steps through an explicit env mapping")
					}
				}
			}
		})
	}
	for name := range pins {
		if _, used := usedPins[name]; !used {
			problems.add(actionPinsPath, "action pin %q is not used by any workflow", name)
		}
	}
}

func checkArchitectureWorkflow(workflow workflowPolicy, problems *diagnostics) {
	switch workflow.Rel {
	case ciWorkflowPath:
		checkCIArchitectureWorkflow(workflow, problems)
	case ".github/workflows/release.yml":
		job := workflow.Jobs["tests"]
		if job == nil || !jobHasExactRun(job, "./scripts/check-architecture.sh") {
			problems.add(workflow.Rel, "release tests must run the static architecture gate")
		}
	}
}

func checkCIArchitectureWorkflow(workflow workflowPolicy, problems *diagnostics) {
	if !yamlExactNullMap(yamlMapValue(workflow.Root, "on"), "pull_request", "push") {
		problems.add(workflow.Rel, "trusted architecture workflow triggers must be exactly unfiltered push and pull_request")
	}
	jobNames := make([]string, 0, len(workflow.Jobs))
	for name := range workflow.Jobs {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)
	if strings.Join(jobNames, "\x00") != "go\x00javascript" {
		problems.add(workflow.Rel, "trusted CI requires exactly the go and javascript jobs")
	}
	if yamlMapValue(workflow.Root, "env") != nil || yamlMapValue(workflow.Root, "defaults") != nil {
		problems.add(workflow.Rel, "trusted architecture validation forbids top-level env and defaults")
	}
	job := workflow.Jobs["go"]
	if job == nil {
		problems.add(workflow.Rel, "trusted architecture validation requires job go")
		return
	}
	for _, field := range []string{"if", "environment", "secrets", "container", "services", "defaults", "needs", "strategy", "uses"} {
		if yamlMapValue(job, field) != nil {
			problems.add(workflow.Rel, "trusted architecture job go may not define job-level %s", field)
		}
	}
	if yamlTreeContainsCredentialExpression(job) {
		problems.add(workflow.Rel, "trusted architecture job go may not reference secrets or the GitHub token")
	}
	if !yamlExactKeys(job, "env", "permissions", "runs-on", "steps", "timeout-minutes") {
		problems.add(workflow.Rel, "trusted architecture job go contains an unapproved job-level field")
	}
	if yamlScalarValue(job, "runs-on") != "ubuntu-24.04" || yamlScalarValue(job, "timeout-minutes") != "15" || !isContentsReadPermission(yamlMapValue(job, "permissions")) {
		problems.add(workflow.Rel, "trusted architecture job go requires the fixed runner, 15-minute timeout, and exactly contents: read")
	}
	if !yamlExactScalarMap(yamlMapValue(job, "env"), map[string]string{
		"GOTELEMETRY": "off",
		"GOTOOLCHAIN": "local",
		"GOWORK":      "off",
	}) {
		problems.add(workflow.Rel, "trusted architecture job go env must contain only the approved Go safety settings")
	}
	steps := yamlMapValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode || len(steps.Content) < 4 {
		problems.add(workflow.Rel, "trusted architecture job requires checkout, setup-go, trusted diff, then static check steps")
		return
	}

	checkout, setup, trusted, static := steps.Content[0], steps.Content[1], steps.Content[2], steps.Content[3]
	if !yamlExactKeys(checkout, "name", "uses", "with") || yamlScalarValue(checkout, "name") != "Check out the exact revision without persisted credentials" || actionReferenceName(yamlScalarValue(checkout, "uses")) != "actions/checkout" {
		problems.add(workflow.Rel, "trusted architecture step 1 must be the pinned checkout action")
	}
	checkoutWith := yamlMapValue(checkout, "with")
	if !yamlExactScalarMap(checkoutWith, map[string]string{
		"clean":               "true",
		"fetch-depth":         "0",
		"persist-credentials": "false",
		"ref":                 "${{ github.event.pull_request.head.sha || github.sha }}",
	}) {
		problems.add(workflow.Rel, "architecture checkout must cleanly select the exact pull-request head with full history and no persisted credentials")
	}
	if !yamlExactKeys(setup, "name", "uses", "with") || yamlScalarValue(setup, "name") != "Install the approved Go toolchain" || actionReferenceName(yamlScalarValue(setup, "uses")) != "actions/setup-go" || !yamlExactScalarMap(yamlMapValue(setup, "with"), map[string]string{
		"cache":      "false",
		"go-version": "1.26.7",
	}) {
		problems.add(workflow.Rel, "trusted architecture step 2 must install only the approved uncached Go toolchain")
	}
	if !yamlExactKeys(trusted, "env", "if", "name", "run") || yamlScalarValue(trusted, "name") != "Build the trusted base verifier and validate the pull-request diff" {
		problems.add(workflow.Rel, "trusted architecture step 3 has unapproved fields or identity")
	}
	if condition := yamlMapValue(trusted, "if"); condition == nil || condition.Kind != yaml.ScalarNode || condition.Value != "github.event_name == 'pull_request'" {
		problems.add(workflow.Rel, "trusted architecture verifier must run exactly for pull_request events")
	}
	if !yamlExactScalarMap(yamlMapValue(trusted, "env"), map[string]string{
		"BASE_REVISION": "${{ github.event.pull_request.base.sha }}",
		"HEAD_REVISION": "${{ github.event.pull_request.head.sha }}",
	}) {
		problems.add(workflow.Rel, "trusted architecture verifier must bind exact base/head SHA environment values")
	}
	if run := yamlMapValue(trusted, "run"); run == nil || run.Kind != yaml.ScalarNode || strings.TrimSpace(run.Value) != trustedArchitectureRun {
		problems.add(workflow.Rel, "trusted architecture verifier must build and execute the canonical base-revision binary")
	}
	if !yamlExactKeys(static, "name", "run") || yamlScalarValue(static, "name") != "Run repository checks" || strings.TrimSpace(yamlScalarValue(static, "run")) != "./scripts/check.sh" {
		problems.add(workflow.Rel, "trusted architecture step 4 must run the HEAD static repository checks after the diff verifier")
	}
	checkCIJavaScriptJob(workflow.Rel, workflow.Jobs["javascript"], problems)
}

func checkCIJavaScriptJob(relative string, job *yaml.Node, problems *diagnostics) {
	if job == nil {
		problems.add(relative, "trusted CI requires job javascript")
		return
	}
	for _, field := range []string{"if", "environment", "secrets", "container", "services", "defaults", "needs", "strategy", "uses", "env"} {
		if yamlMapValue(job, field) != nil {
			problems.add(relative, "trusted CI job javascript may not define job-level %s", field)
		}
	}
	if yamlTreeContainsCredentialExpression(job) {
		problems.add(relative, "trusted CI job javascript may not reference secrets or the GitHub token")
	}
	if !yamlExactKeys(job, "permissions", "runs-on", "steps", "timeout-minutes") {
		problems.add(relative, "trusted CI job javascript contains an unapproved job-level field")
	}
	if yamlScalarValue(job, "runs-on") != "ubuntu-24.04" || yamlScalarValue(job, "timeout-minutes") != "15" || !isContentsReadPermission(yamlMapValue(job, "permissions")) {
		problems.add(relative, "trusted CI job javascript requires the fixed runner, 15-minute timeout, and exactly contents: read")
	}
	steps := yamlMapValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode || len(steps.Content) != 5 {
		problems.add(relative, "trusted CI job javascript requires exactly five approved steps")
		return
	}
	checkout, setup, policy, install, verify := steps.Content[0], steps.Content[1], steps.Content[2], steps.Content[3], steps.Content[4]
	if !yamlExactKeys(checkout, "name", "uses", "with") || yamlScalarValue(checkout, "name") != "Check out the exact revision without persisted credentials" || actionReferenceName(yamlScalarValue(checkout, "uses")) != "actions/checkout" || !yamlExactScalarMap(yamlMapValue(checkout, "with"), map[string]string{
		"clean":               "true",
		"fetch-depth":         "0",
		"persist-credentials": "false",
		"ref":                 "${{ github.event.pull_request.head.sha || github.sha }}",
	}) {
		problems.add(relative, "trusted CI javascript step 1 must use the exact credential-free checkout")
	}
	if !yamlExactKeys(setup, "name", "uses", "with") || yamlScalarValue(setup, "name") != "Install the approved Node toolchain" || actionReferenceName(yamlScalarValue(setup, "uses")) != "actions/setup-node" || !yamlExactScalarMap(yamlMapValue(setup, "with"), map[string]string{"node-version": "22.16.0"}) {
		problems.add(relative, "trusted CI javascript step 2 must install only Node 22.16.0")
	}
	if !yamlExactKeys(policy, "name", "run") || yamlScalarValue(policy, "name") != "Verify JavaScript dependency policy" || strings.TrimSpace(yamlScalarValue(policy, "run")) != "./scripts/check-js-supply-chain.sh repository" {
		problems.add(relative, "trusted CI javascript step 3 must run the repository JavaScript policy")
	}
	if !yamlExactKeys(install, "name", "run", "working-directory") || yamlScalarValue(install, "name") != "Install the exact n8n build graph without lifecycle scripts" || yamlScalarValue(install, "working-directory") != "integrations/n8n-nodes-torgnexa" || strings.TrimSpace(yamlScalarValue(install, "run")) != "npm ci --ignore-scripts --no-audit --no-fund" {
		problems.add(relative, "trusted CI javascript step 4 must install the locked n8n graph without lifecycle scripts")
	}
	const verifyRun = "npm run build\nnpm run test:offline\nnpm run verify:package\nnpm pack --dry-run --ignore-scripts"
	if !yamlExactKeys(verify, "name", "run", "working-directory") || yamlScalarValue(verify, "name") != "Build, test, and inspect the n8n package" || yamlScalarValue(verify, "working-directory") != "integrations/n8n-nodes-torgnexa" || strings.TrimSpace(yamlScalarValue(verify, "run")) != verifyRun {
		problems.add(relative, "trusted CI javascript step 5 must build, test, and inspect the n8n package")
	}
}

func yamlExactNullMap(mapping *yaml.Node, expected ...string) bool {
	if !yamlExactKeys(mapping, expected...) {
		return false
	}
	for _, key := range expected {
		value := yamlMapValue(mapping, key)
		if value == nil || value.Kind != yaml.ScalarNode || value.Tag != "!!null" {
			return false
		}
	}
	return true
}

func yamlExactKeys(mapping *yaml.Node, expected ...string) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode || len(mapping.Content) != len(expected)*2 {
		return false
	}
	wanted := make(map[string]struct{}, len(expected))
	for _, key := range expected {
		wanted[key] = struct{}{}
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		if key.Kind != yaml.ScalarNode {
			return false
		}
		if _, ok := wanted[key.Value]; !ok {
			return false
		}
	}
	return true
}

func yamlTreeContainsCredentialExpression(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.ScalarNode {
		compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(strings.ToLower(node.Value))
		if strings.Contains(compact, "${{") && (strings.Contains(compact, "secrets.") || strings.Contains(compact, "secrets[") || strings.Contains(compact, "github.token") || strings.Contains(compact, `github["token"]`) || strings.Contains(compact, "github['token']")) {
			return true
		}
	}
	for _, child := range node.Content {
		if yamlTreeContainsCredentialExpression(child) {
			return true
		}
	}
	return false
}

func yamlScalarValue(mapping *yaml.Node, key string) string {
	value := yamlMapValue(mapping, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func yamlExactScalarMap(mapping *yaml.Node, expected map[string]string) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode || len(mapping.Content) != len(expected)*2 {
		return false
	}
	for key, wanted := range expected {
		if yamlScalarValue(mapping, key) != wanted {
			return false
		}
	}
	return true
}

func jobHasExactRun(job *yaml.Node, wanted string) bool {
	steps := yamlMapValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return false
	}
	for _, step := range steps.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}
		run := yamlMapValue(step, "run")
		if run != nil && run.Kind == yaml.ScalarNode && strings.TrimSpace(run.Value) == wanted {
			return true
		}
	}
	return false
}

var allowedPermissionScopes = map[string]struct{}{
	"actions": {}, "artifact-metadata": {}, "attestations": {}, "checks": {}, "contents": {}, "deployments": {},
	"discussions": {}, "id-token": {}, "issues": {}, "models": {}, "packages": {},
	"pages": {}, "pull-requests": {}, "security-events": {}, "statuses": {},
}

var allowedWritesByJob = map[string]map[string]struct{}{
	"security": {"security-events": {}},
	"attest":   {"artifact-metadata": {}, "attestations": {}, "id-token": {}},
	"publish":  {"contents": {}},
}

func checkJobPermissions(relative, jobName string, job *yaml.Node, problems *diagnostics) {
	permissions := yamlMapValue(job, "permissions")
	if permissions == nil || permissions.Kind != yaml.MappingNode {
		problems.add(relative, "job %q must declare an explicit permissions mapping", jobName)
		return
	}
	for i := 0; i < len(permissions.Content); i += 2 {
		key, value := permissions.Content[i], permissions.Content[i+1]
		if _, known := allowedPermissionScopes[key.Value]; !known {
			problems.add(relative, "job %q uses unknown permission %q", jobName, key.Value)
		}
		if value.Kind != yaml.ScalarNode || (value.Value != "read" && value.Value != "write" && value.Value != "none") {
			problems.add(relative, "job %q permission %q must be read, write, or none", jobName, key.Value)
			continue
		}
		if key.Value == "id-token" && value.Value == "read" {
			problems.add(relative, "job %q id-token permission cannot be read", jobName)
		}
		if value.Value == "write" {
			allowed := allowedWritesByJob[jobName]
			if _, ok := allowed[key.Value]; !ok {
				problems.add(relative, "job %q may not request %s: write", jobName, key.Value)
			}
		}
	}
}

func checkPullRequestPermissions(relative, jobName string, job *yaml.Node, problems *diagnostics) {
	if yamlMapValue(job, "environment") != nil {
		problems.add(relative, "pull-request job %q may not use a deployment environment", jobName)
	}
	if yamlMapValue(job, "secrets") != nil {
		problems.add(relative, "pull-request job %q may not receive job-level secrets", jobName)
	}
	if yamlTreeContainsCredentialExpression(job) {
		problems.add(relative, "pull-request job %q may not reference secrets or the GitHub token", jobName)
	}
	permissions := yamlMapValue(job, "permissions")
	if permissions == nil || permissions.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index < len(permissions.Content); index += 2 {
		key, value := permissions.Content[index], permissions.Content[index+1]
		if value.Value != "write" {
			continue
		}
		switch key.Value {
		case "artifact-metadata", "attestations", "contents", "id-token", "packages":
			problems.add(relative, "pull-request job %q may not request %s: write", jobName, key.Value)
		}
	}
}

func checkContinueOnError(relative, jobName string, job *yaml.Node, problems *diagnostics) {
	if value := yamlMapValue(job, "continue-on-error"); value != nil && !yamlFalseValue(value) {
		problems.add(relative, "job %q may not continue on error", jobName)
	}
	steps := yamlMapValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return
	}
	for index, step := range steps.Content {
		if step.Kind == yaml.MappingNode {
			if value := yamlMapValue(step, "continue-on-error"); value != nil && !yamlFalseValue(value) {
				problems.add(relative, "job %q step %d may not continue on error", jobName, index+1)
			}
		}
	}
}

func checkActionUse(root, relative string, value *yaml.Node, pins map[string]actionPin, used, checkedLocalActions map[string]struct{}, problems *diagnostics) {
	if value.Kind != yaml.ScalarNode {
		problems.add(relative, "uses must be a scalar")
		return
	}
	reference := value.Value
	if strings.HasPrefix(reference, "./") {
		checkLocalAction(root, relative, reference, pins, used, checkedLocalActions, problems)
		return
	}
	if strings.HasPrefix(reference, "docker://") {
		if err := validateImmutableImage(strings.TrimPrefix(reference, "docker://")); err != nil {
			problems.add(relative, "docker action %q: %v", reference, err)
		}
		return
	}
	index := strings.LastIndex(reference, "@")
	if index <= 0 || index == len(reference)-1 {
		problems.add(relative, "external action %q must use a registered full commit", reference)
		return
	}
	name, commit := reference[:index], reference[index+1:]
	if !fullCommitRE.MatchString(commit) {
		problems.add(relative, "external action %q must use a 40-character lowercase commit", name)
		return
	}
	pin, registered := pins[name]
	if !registered {
		problems.add(relative, "external action %q is not registered in %s", name, actionPinsPath)
		return
	}
	used[name] = struct{}{}
	if pin.Commit != commit {
		problems.add(relative, "external action %q commit does not match %s", name, actionPinsPath)
	}
}

func isContentsReadPermission(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.MappingNode && len(node.Content) == 2 &&
		node.Content[0].Value == "contents" && node.Content[1].Kind == yaml.ScalarNode && node.Content[1].Value == "read"
}

func checkJobRunner(relative, jobName string, job *yaml.Node, problems *diagnostics) {
	runner := yamlMapValue(job, "runs-on")
	if runner == nil || runner.Kind != yaml.ScalarNode || runner.Value != "ubuntu-24.04" {
		problems.add(relative, "job %q runs-on must be exactly ubuntu-24.04", jobName)
	}
	timeout := yamlMapValue(job, "timeout-minutes")
	minutes := 0
	if timeout != nil && timeout.Kind == yaml.ScalarNode {
		minutes, _ = strconv.Atoi(timeout.Value)
	}
	if minutes < 1 || minutes > 60 {
		problems.add(relative, "job %q timeout-minutes must be between 1 and 60", jobName)
	}
}

func checkWorkflowSteps(root, relative, jobName string, job *yaml.Node, pins map[string]actionPin, used, checkedLocalActions map[string]struct{}, problems *diagnostics) {
	steps := yamlMapValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return
	}
	for index, step := range steps.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}
		uses := yamlMapValue(step, "uses")
		condition := yamlMapValue(step, "if")
		if condition != nil && condition.Kind == yaml.ScalarNode && strings.Contains(strings.ReplaceAll(condition.Value, " ", ""), "always()") {
			if uses == nil || uses.Kind != yaml.ScalarNode || actionReferenceName(uses.Value) != "actions/upload-artifact" {
				problems.add(relative, "job %q step %d may use always() only for upload-artifact evidence", jobName, index+1)
			}
		}
		if uses == nil || uses.Kind != yaml.ScalarNode {
			continue
		}
		name := actionReferenceName(uses.Value)
		with := yamlMapValue(step, "with")
		switch name {
		case "actions/checkout":
			if !yamlFalseValue(yamlMapValue(with, "persist-credentials")) {
				problems.add(relative, "job %q step %d checkout must set persist-credentials: false", jobName, index+1)
			}
			permissions := yamlMapValue(job, "permissions")
			contents := yamlMapValue(permissions, "contents")
			allowedContents := contents != nil && contents.Kind == yaml.ScalarNode && contents.Value == "read"
			if jobName == "publish" && contents != nil && contents.Kind == yaml.ScalarNode && contents.Value == "write" {
				// The protected publication job needs contents:write to stage a draft release.
				// Checkout remains safe because persisted credentials are forbidden above.
				allowedContents = true
			}
			if !allowedContents {
				problems.add(relative, "job %q using checkout must grant contents: read (publish may use contents: write with persist-credentials: false)", jobName)
			}
		case "actions/setup-go":
			version := yamlMapValue(with, "go-version")
			if version == nil || version.Kind != yaml.ScalarNode || version.Value != "1.26.7" {
				problems.add(relative, "job %q step %d setup-go must set go-version: 1.26.7", jobName, index+1)
			}
			if !yamlFalseValue(yamlMapValue(with, "cache")) {
				problems.add(relative, "job %q step %d setup-go must set cache: false", jobName, index+1)
			}
		}
	}
}

func actionReferenceName(reference string) string {
	if index := strings.LastIndex(reference, "@"); index > 0 {
		return reference[:index]
	}
	return reference
}

func checkLocalAction(root, source, reference string, pins map[string]actionPin, used, checked map[string]struct{}, problems *diagnostics) {
	relative := strings.TrimPrefix(reference, "./")
	cleaned, err := safeContractPath(relative)
	if err != nil {
		problems.add(source, "local action %q: %v", reference, err)
		return
	}
	if _, done := checked[cleaned]; done {
		return
	}
	checked[cleaned] = struct{}{}
	path := filepath.Join(root, filepath.FromSlash(cleaned))
	info, err := os.Lstat(path)
	if err != nil {
		problems.add(source, "local action %q does not exist", reference)
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		problems.add(source, "local action %q may not be a symlink", reference)
		return
	}
	manifestRelative := cleaned
	if info.IsDir() {
		manifestRelative = ""
		for _, name := range []string{"action.yml", "action.yaml"} {
			candidate := cleaned + "/" + name
			if _, err := resolveRealRepositoryPath(root, candidate, false); err == nil {
				manifestRelative = candidate
				break
			}
		}
		if manifestRelative == "" {
			problems.add(source, "local action %q has no action.yml or action.yaml", reference)
			return
		}
	} else if !strings.HasSuffix(cleaned, ".yml") && !strings.HasSuffix(cleaned, ".yaml") {
		problems.add(source, "local action file %q must be YAML", reference)
		return
	}
	data, ok := readRepositoryFile(root, manifestRelative, problems)
	if !ok {
		return
	}
	node, err := parseStrictYAML(data)
	if err != nil {
		problems.add(manifestRelative, "invalid local action YAML: %v", err)
		return
	}
	walkYAMLMapping(node, func(key, value *yaml.Node) {
		switch key.Value {
		case "uses":
			checkActionUse(root, manifestRelative, value, pins, used, checked, problems)
		case "run":
			if value.Kind == yaml.ScalarNode && strings.Contains(value.Value, "${{") {
				problems.add(manifestRelative, "expressions must enter run steps through an explicit env mapping")
			}
		}
	})
}

func checkReleaseWorkflow(workflows []workflowPolicy, problems *diagnostics) {
	var candidates []workflowPolicy
	for _, workflow := range workflows {
		if _, ok := workflow.Jobs["publish"]; ok {
			candidates = append(candidates, workflow)
		}
	}
	if len(candidates) != 1 {
		problems.add(".github/workflows", "exactly one release workflow with a publish job is required")
		return
	}
	workflow := candidates[0]
	required := []string{"attest", "build", "contracts", "policy", "publish", "sbom", "security", "tests", "verify"}
	for _, name := range required {
		job, ok := workflow.Jobs[name]
		if !ok {
			problems.add(workflow.Rel, "required release job %q is missing", name)
			continue
		}
		condition := yamlMapValue(job, "if")
		if condition != nil && condition.Kind == yaml.ScalarNode && strings.Contains(strings.ReplaceAll(condition.Value, " ", ""), "always()") {
			problems.add(workflow.Rel, "release gate job %q may not use always()", name)
		}
	}
	checkReleaseEnvironment(workflow.Rel, workflow.Jobs["attest"], "attest", "release-signing", problems)
	checkReleaseEnvironment(workflow.Rel, workflow.Jobs["publish"], "publish", "release-publication", problems)
	checkExactJobPermissions(workflow.Rel, workflow.Jobs["attest"], "attest", map[string]string{
		"artifact-metadata": "write",
		"attestations":      "write",
		"contents":          "read",
		"id-token":          "write",
	}, problems)
	checkExactJobPermissions(workflow.Rel, workflow.Jobs["publish"], "publish", map[string]string{
		"contents": "write",
	}, problems)
	graph := make(map[string][]string, len(workflow.Jobs))
	for name, job := range workflow.Jobs {
		graph[name] = yamlStringList(yamlMapValue(job, "needs"), workflow.Rel, "job "+name+" needs", problems)
		for _, dependency := range graph[name] {
			if _, exists := workflow.Jobs[dependency]; !exists {
				problems.add(workflow.Rel, "job %q needs unknown job %q", name, dependency)
			}
		}
	}
	if hasJobCycle(graph) {
		problems.add(workflow.Rel, "release job graph contains a cycle")
		return
	}
	requireAncestors(workflow.Rel, graph, "build", []string{"tests", "contracts", "policy"}, problems)
	requireAncestors(workflow.Rel, graph, "sbom", []string{"build"}, problems)
	requireAncestors(workflow.Rel, graph, "security", []string{"build"}, problems)
	requireAncestors(workflow.Rel, graph, "attest", []string{"sbom", "security"}, problems)
	requireAncestors(workflow.Rel, graph, "verify", []string{"attest"}, problems)
	requireAncestors(workflow.Rel, graph, "publish", []string{"verify"}, problems)
}

func checkExactJobPermissions(relative string, job *yaml.Node, jobName string, expected map[string]string, problems *diagnostics) {
	if job == nil {
		return
	}
	permissions := yamlMapValue(job, "permissions")
	if permissions == nil || permissions.Kind != yaml.MappingNode {
		return
	}
	actual := make(map[string]string, len(permissions.Content)/2)
	for index := 0; index < len(permissions.Content); index += 2 {
		key, value := permissions.Content[index], permissions.Content[index+1]
		if key.Kind == yaml.ScalarNode && value.Kind == yaml.ScalarNode {
			actual[key.Value] = value.Value
		}
	}
	if len(actual) != len(expected) {
		problems.add(relative, "release job %q permissions must be exactly %v", jobName, expected)
		return
	}
	for scope, level := range expected {
		if actual[scope] != level {
			problems.add(relative, "release job %q permissions must be exactly %v", jobName, expected)
			return
		}
	}
}

func checkReleaseEnvironment(relative string, job *yaml.Node, jobName, expected string, problems *diagnostics) {
	environment := yamlMapValue(job, "environment")
	if environment == nil || environment.Kind != yaml.ScalarNode || environment.Value != expected {
		problems.add(relative, "release job %q environment must be exactly %q", jobName, expected)
	}
}

func requireAncestors(relative string, graph map[string][]string, job string, required []string, problems *diagnostics) {
	if _, exists := graph[job]; !exists {
		return
	}
	ancestors := make(map[string]struct{})
	var visit func(string)
	visit = func(current string) {
		for _, dependency := range graph[current] {
			if _, seen := ancestors[dependency]; seen {
				continue
			}
			ancestors[dependency] = struct{}{}
			visit(dependency)
		}
	}
	visit(job)
	for _, dependency := range required {
		if _, ok := ancestors[dependency]; !ok {
			problems.add(relative, "release job %q must depend on %q", job, dependency)
		}
	}
}

func hasJobCycle(graph map[string][]string) bool {
	state := make(map[string]uint8, len(graph))
	var visit func(string) bool
	visit = func(name string) bool {
		if state[name] == 1 {
			return true
		}
		if state[name] == 2 {
			return false
		}
		state[name] = 1
		for _, dependency := range graph[name] {
			if _, exists := graph[dependency]; exists && visit(dependency) {
				return true
			}
		}
		state[name] = 2
		return false
	}
	for name := range graph {
		if visit(name) {
			return true
		}
	}
	return false
}

func checkRepositoryImages(ctx context.Context, root string, problems *diagnostics) {
	composeFiles, dockerfiles := discoverRepositoryFiles(ctx, root, problems)
	remaining := maxYAMLTotalNodes
	for _, relative := range composeFiles {
		data, ok := readRepositoryFile(root, relative, problems)
		if !ok {
			continue
		}
		node, err := parseComposeYAMLWithBudget(ctx, data, &remaining)
		if err != nil {
			problems.add(relative, "invalid Compose YAML: %v", err)
			continue
		}
		walkYAMLMapping(node, func(key, value *yaml.Node) {
			if key.Value == "image" {
				checkYAMLImage(relative, value, problems)
			}
		})
	}
	for _, relative := range dockerfiles {
		checkDockerfileImages(root, relative, problems)
	}
}

func checkYAMLImage(relative string, value *yaml.Node, problems *diagnostics) {
	if value.Kind != yaml.ScalarNode {
		problems.add(relative, "image reference must be a scalar")
		return
	}
	if err := validateImmutableImage(value.Value); err != nil {
		problems.add(relative, "image %q: %v", value.Value, err)
	}
}

func validateImmutableImage(reference string) error {
	if reference == "scratch" {
		return nil
	}
	if strings.ContainsAny(reference, " \t\r\n$") {
		return fmt.Errorf("reference must be static and contain no whitespace")
	}
	parts := strings.Split(reference, "@")
	if len(parts) != 2 || !strings.HasPrefix(parts[1], "sha256:") || !sha256RE.MatchString(strings.TrimPrefix(parts[1], "sha256:")) {
		return fmt.Errorf("reference must include exactly one sha256 digest")
	}
	name := parts[0]
	lastSlash := strings.LastIndex(name, "/")
	lastColon := strings.LastIndex(name, ":")
	if lastColon <= lastSlash || lastColon == len(name)-1 || strings.EqualFold(name[lastColon+1:], "latest") {
		return fmt.Errorf("reference must retain a non-latest human-readable tag")
	}
	repository := name[:lastColon]
	if repository == "docker.io/library/postgres" {
		repository = "postgres"
	}
	approved := map[string]struct{}{
		"apache/kafka":                 {},
		"clickhouse/clickhouse-server": {},
		"dxflrs/garage":                {},
		"golang":                       {},
		"node":                         {},
		"postgres":                     {},
		"quay.io/keycloak/keycloak":    {},
		"valkey/valkey":                {},
	}
	if _, ok := approved[repository]; !ok {
		return fmt.Errorf("repository %q is not approved", repository)
	}
	return nil
}

func checkDockerfileImages(root, relative string, problems *diagnostics) {
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		return
	}
	aliases := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.EqualFold(fields[0], "FROM") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			problems.add(relative, "line %d: multiline FROM is forbidden", lineNumber)
			continue
		}
		index := 1
		for index < len(fields) && strings.HasPrefix(fields[index], "--") {
			index++
		}
		if index >= len(fields) {
			problems.add(relative, "line %d: FROM image is missing", lineNumber)
			continue
		}
		image := fields[index]
		if _, stage := aliases[strings.ToLower(image)]; !stage {
			if err := validateImmutableImage(image); err != nil {
				problems.add(relative, "line %d FROM %q: %v", lineNumber, image, err)
			}
		}
		if index+2 < len(fields) && strings.EqualFold(fields[index+1], "AS") {
			aliases[strings.ToLower(fields[index+2])] = struct{}{}
		}
	}
	if err := scanner.Err(); err != nil {
		problems.add(relative, "scan Dockerfile: %v", err)
	}
}

func checkGoModulePolicy(ctx context.Context, root string, tools toolVersionManifest, problems *diagnostics) {
	modules, goWorkFiles := discoverGoFiles(ctx, root, problems)
	expected := []string{
		"go.mod",
		"sdk/examples/go/go.mod",
		"sdk/go/go.mod",
		"tools/contractcheck/go.mod",
		"tools/sdkgen/go.mod",
		"tools/securitytools/go.mod",
	}
	if strings.Join(modules, "\x00") != strings.Join(expected, "\x00") {
		problems.add("go.mod", "module inventory must be exactly %v; found %v", expected, modules)
	}
	for _, relative := range goWorkFiles {
		problems.add(relative, "go.work files are forbidden; validation must use GOWORK=off")
	}
	for _, relative := range modules {
		checkGoMod(root, relative, tools, problems)
	}
	checkUnsupportedPackageEcosystems(ctx, root, problems)
}

type goModulePolicy struct {
	Module            string
	GoVersion         string
	Toolchain         string
	LocalReplacements map[string]string
}

var registeredGoModules = map[string]goModulePolicy{
	"go.mod": {
		Module:    "github.com/torgnexa/torgnexa",
		GoVersion: "1.26.0",
		Toolchain: "go1.26.7",
	},
	"sdk/examples/go/go.mod": {
		Module:    "github.com/torgnexa/torgnexa-sdk-examples-go",
		GoVersion: "1.23.0",
		LocalReplacements: map[string]string{
			"github.com/torgnexa/torgnexa-sdk-go": "../../go",
		},
	},
	"sdk/go/go.mod": {
		Module:    "github.com/torgnexa/torgnexa-sdk-go",
		GoVersion: "1.23.0",
	},
	"tools/contractcheck/go.mod": {
		Module:    "github.com/torgnexa/torgnexa/tools/contractcheck",
		GoVersion: "1.26.0",
		Toolchain: "go1.26.7",
	},
	"tools/sdkgen/go.mod": {
		Module:    "github.com/torgnexa/torgnexa/tools/sdkgen",
		GoVersion: "1.23.0",
	},
	"tools/securitytools/go.mod": {
		Module:    "github.com/torgnexa/torgnexa/tools/securitytools",
		GoVersion: "1.26.0",
		Toolchain: "go1.26.7",
	},
}

func checkGoMod(root, relative string, toolsManifest toolVersionManifest, problems *diagnostics) {
	policy, registered := registeredGoModules[relative]
	if !registered {
		return
	}
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		return
	}
	var modulePath, goVersion, toolchain string
	var requirements []goRequirement
	replacements := make(map[string]string)
	inRequireBlock := false
	inToolBlock := false
	var tools []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if index := strings.Index(line, " //"); index >= 0 {
			line = strings.TrimSpace(line[:index])
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if inRequireBlock {
			if fields[0] == ")" {
				inRequireBlock = false
				continue
			}
			if len(fields) != 2 {
				problems.add(relative, "malformed require entry %q", line)
				continue
			}
			requirements = append(requirements, goRequirement{fields[0], fields[1]})
			continue
		}
		if inToolBlock {
			if fields[0] == ")" {
				inToolBlock = false
				continue
			}
			if len(fields) != 1 {
				problems.add(relative, "malformed tool entry %q", line)
				continue
			}
			tools = append(tools, fields[0])
			continue
		}
		switch fields[0] {
		case "module":
			if len(fields) != 2 || modulePath != "" {
				problems.add(relative, "module directive must appear exactly once")
			} else {
				modulePath = fields[1]
			}
		case "go":
			if len(fields) != 2 || goVersion != "" {
				problems.add(relative, "go directive must appear exactly once")
			} else {
				goVersion = fields[1]
			}
		case "toolchain":
			if len(fields) != 2 || toolchain != "" {
				problems.add(relative, "toolchain directive must appear exactly once")
			} else {
				toolchain = fields[1]
			}
		case "require":
			if len(fields) == 2 && fields[1] == "(" {
				inRequireBlock = true
			} else if len(fields) == 3 {
				requirements = append(requirements, goRequirement{fields[1], fields[2]})
			} else {
				problems.add(relative, "malformed require directive %q", line)
			}
		case "tool":
			if len(fields) == 2 && fields[1] == "(" {
				inToolBlock = true
			} else if len(fields) == 2 {
				tools = append(tools, fields[1])
			} else {
				problems.add(relative, "malformed tool directive %q", line)
			}
		case "replace":
			if len(fields) != 4 || fields[2] != "=>" {
				problems.add(relative, "malformed replace directive %q", line)
				continue
			}
			if _, duplicate := replacements[fields[1]]; duplicate {
				problems.add(relative, "duplicate replacement %q", fields[1])
			}
			replacements[fields[1]] = fields[3]
		case "exclude", "retract":
			problems.add(relative, "%s directives are forbidden by supply-chain policy", fields[0])
		}
	}
	if err := scanner.Err(); err != nil {
		problems.add(relative, "scan go.mod: %v", err)
	}
	if inRequireBlock {
		problems.add(relative, "unterminated require block")
	}
	if inToolBlock {
		problems.add(relative, "unterminated tool block")
	}
	if goVersion != policy.GoVersion {
		problems.add(relative, "go directive must be exactly %s", policy.GoVersion)
	}
	if toolchain != policy.Toolchain {
		if policy.Toolchain == "" {
			problems.add(relative, "toolchain directive must be omitted for this compatibility module")
		} else {
			problems.add(relative, "toolchain directive must be exactly %s", policy.Toolchain)
		}
	}
	if modulePath != policy.Module {
		problems.add(relative, "module path must be %q", policy.Module)
	}
	if !equalStringMaps(replacements, policy.LocalReplacements) {
		if len(policy.LocalReplacements) == 0 {
			problems.add(relative, "replace directives are forbidden by supply-chain policy")
		} else {
			problems.add(relative, "replace directives must exactly match the registered local SDK mapping")
		}
	}
	seen := make(map[string]struct{}, len(requirements))
	for _, requirement := range requirements {
		if _, duplicate := seen[requirement.module]; duplicate {
			problems.add(relative, "duplicate requirement %q", requirement.module)
		}
		seen[requirement.module] = struct{}{}
		if !validPinnedVersion(requirement.version) {
			problems.add(relative, "requirement %q must use an exact semantic or pseudo-version", requirement.module)
		}
	}
	for module := range replacements {
		if _, required := seen[module]; !required {
			problems.add(relative, "local replacement %q must bind a pinned requirement", module)
		}
	}
	externalRequirements := make([]goRequirement, 0, len(requirements))
	for _, requirement := range requirements {
		if _, local := replacements[requirement.module]; !local {
			externalRequirements = append(externalRequirements, requirement)
		}
	}
	if len(externalRequirements) > 0 {
		sums := checkGoSums(root, filepath.ToSlash(filepath.Join(filepath.Dir(relative), "go.sum")), externalRequirements, problems)
		checkGoToolBindings(relative, requirements, tools, sums, toolsManifest, problems)
	} else if len(tools) > 0 {
		problems.add(relative, "tool directives require pinned module requirements")
	}
	if relative != "tools/securitytools/go.mod" && len(tools) > 0 {
		problems.add(relative, "tool directives are allowed only in tools/securitytools/go.mod")
	}
}

type goRequirement struct{ module, version string }

func checkGoSums(root, relative string, requirements []goRequirement, problems *diagnostics) map[string]string {
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		return nil
	}
	entries := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 3 || !h1SumRE.MatchString(fields[2]) {
			problems.add(relative, "malformed checksum entry")
			continue
		}
		entries[fields[0]+" "+fields[1]] = fields[2]
	}
	if err := scanner.Err(); err != nil {
		problems.add(relative, "scan go.sum: %v", err)
	}
	for _, requirement := range requirements {
		key := requirement.module + " " + requirement.version
		if _, exists := entries[key]; !exists {
			problems.add(relative, "missing module checksum for %s", key)
		}
		if _, exists := entries[key+"/go.mod"]; !exists {
			problems.add(relative, "missing go.mod checksum for %s", key)
		}
	}
	return entries
}

func checkGoToolBindings(relative string, requirements []goRequirement, tools []string, sums map[string]string, manifest toolVersionManifest, problems *diagnostics) {
	if relative != "tools/securitytools/go.mod" {
		return
	}
	expectedTools := make([]string, 0, len(manifest.GoTools))
	requirementByModule := make(map[string]string, len(requirements))
	for _, requirement := range requirements {
		requirementByModule[requirement.module] = requirement.version
	}
	toolModules := map[string]string{
		"github.com/securego/gosec/v2/cmd/gosec": "github.com/securego/gosec/v2",
		"golang.org/x/vuln/cmd/govulncheck":      "golang.org/x/vuln",
	}
	for _, tool := range manifest.GoTools {
		expectedTools = append(expectedTools, tool.Module)
		requirementModule := toolModules[tool.Module]
		if requirementByModule[requirementModule] != tool.Version {
			problems.add(relative, "tool %q requirement must match %s", tool.Name, toolVersionsPath)
		}
		if sums[requirementModule+" "+tool.Version] != tool.Sum {
			problems.add(relative, "tool %q module checksum must match %s", tool.Name, toolVersionsPath)
		}
	}
	sort.Strings(expectedTools)
	sort.Strings(tools)
	if strings.Join(tools, "\x00") != strings.Join(expectedTools, "\x00") {
		problems.add(relative, "tool directives must exactly match %s", toolVersionsPath)
	}
}

type toolVersionManifest struct {
	Version      int             `json:"version"`
	GoTools      []goToolVersion `json:"go_tools"`
	ArchiveTools []archiveTool   `json:"archive_tools"`
	BinaryTools  []binaryTool    `json:"binary_tools"`
}

type goToolVersion struct {
	Name    string `json:"name"`
	Module  string `json:"module"`
	Version string `json:"version"`
	Sum     string `json:"sum"`
}

type archiveTool struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Source        string `json:"source"`
	ArchiveSHA256 string `json:"archive_sha256"`
	BinarySHA256  string `json:"binary_sha256"`
}

type binaryTool struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Source       string `json:"source"`
	BinarySHA256 string `json:"binary_sha256"`
}

func checkToolVersions(ctx context.Context, root string, problems *diagnostics) toolVersionManifest {
	var manifest toolVersionManifest
	if !readPolicyJSON(ctx, root, toolVersionsPath, &manifest, problems) {
		return manifest
	}
	if manifest.Version != policyVersion {
		problems.add(toolVersionsPath, "version must be %d", policyVersion)
	}
	checkGoTools(manifest.GoTools, problems)
	checkArchiveTools(manifest.ArchiveTools, problems)
	checkBinaryTools(manifest.BinaryTools, problems)
	return manifest
}

func checkGoTools(tools []goToolVersion, problems *diagnostics) {
	expected := map[string]string{
		"gosec":       "github.com/securego/gosec/v2/cmd/gosec",
		"govulncheck": "golang.org/x/vuln/cmd/govulncheck",
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		if module, known := expected[tool.Name]; !known || tool.Module != module {
			problems.add(toolVersionsPath, "Go tool %q has an unexpected module", tool.Name)
		}
		if !validPinnedVersion(tool.Version) {
			problems.add(toolVersionsPath, "Go tool %q version must be exact", tool.Name)
		}
		if !h1SumRE.MatchString(tool.Sum) {
			problems.add(toolVersionsPath, "Go tool %q sum must be an h1 checksum", tool.Name)
		}
	}
	checkExactSortedNames(toolVersionsPath, "go_tools", names, []string{"gosec", "govulncheck"}, problems)
}

func checkArchiveTools(tools []archiveTool, problems *diagnostics) {
	prefixes := map[string]string{
		"syft":  "https://github.com/anchore/syft/releases/download/",
		"trivy": "https://github.com/aquasecurity/trivy/releases/download/",
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		checkDownloadIdentity(tool.Name, tool.Version, tool.Source, prefixes[tool.Name], problems)
		if !sha256RE.MatchString(tool.ArchiveSHA256) || !sha256RE.MatchString(tool.BinarySHA256) {
			problems.add(toolVersionsPath, "archive tool %q requires lowercase archive and binary SHA-256 values", tool.Name)
		}
	}
	checkExactSortedNames(toolVersionsPath, "archive_tools", names, []string{"syft", "trivy"}, problems)
}

func checkBinaryTools(tools []binaryTool, problems *diagnostics) {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
		prefix := ""
		if tool.Name == "cosign" {
			prefix = "https://github.com/sigstore/cosign/releases/download/"
		}
		checkDownloadIdentity(tool.Name, tool.Version, tool.Source, prefix, problems)
		if !sha256RE.MatchString(tool.BinarySHA256) {
			problems.add(toolVersionsPath, "binary tool %q requires a lowercase binary SHA-256", tool.Name)
		}
	}
	checkExactSortedNames(toolVersionsPath, "binary_tools", names, []string{"cosign"}, problems)
}

func checkDownloadIdentity(name, version, source, prefix string, problems *diagnostics) {
	if !validPinnedVersion(version) {
		problems.add(toolVersionsPath, "tool %q version must be exact", name)
	}
	parsed, err := url.Parse(source)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		problems.add(toolVersionsPath, "tool %q source must be a fixed HTTPS URL without credentials, query, or fragment", name)
		return
	}
	if prefix == "" || !strings.HasPrefix(source, prefix) || !strings.Contains(parsed.Path, "/"+version+"/") {
		problems.add(toolVersionsPath, "tool %q source does not match its approved upstream/version", name)
	}
}

func validPinnedVersion(value string) bool {
	if !semverRE.MatchString(value) {
		return false
	}
	version := strings.TrimPrefix(value, "v")
	coreAndPrerelease := version
	if index := strings.IndexByte(version, '+'); index >= 0 {
		if !validVersionIdentifiers(version[index+1:], false) {
			return false
		}
		coreAndPrerelease = version[:index]
	}
	if index := strings.IndexByte(coreAndPrerelease, '-'); index >= 0 {
		return validVersionIdentifiers(coreAndPrerelease[index+1:], true)
	}
	return true
}

func validVersionIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for _, character := range identifier {
			if (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') && character != '-' {
				return false
			}
			if character < '0' || character > '9' {
				numeric = false
			}
		}
		if rejectNumericLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func checkExactSortedNames(relative, field string, actual, expected []string, problems *diagnostics) {
	for index := 1; index < len(actual); index++ {
		if actual[index] <= actual[index-1] {
			problems.add(relative, "%s must be strictly sorted and unique", field)
			break
		}
	}
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		problems.add(relative, "%s must contain exactly %v", field, expected)
	}
}

type releaseArtifactInventory struct {
	Version            int            `json:"version"`
	PublicReleaseReady bool           `json:"public_release_ready"`
	Binaries           []binaryEntry  `json:"binaries"`
	SourceOnlyCommands []string       `json:"source_only_commands"`
	DevelopmentRuntime []runtimeEntry `json:"development_runtime"`
}

type binaryEntry struct {
	Name      string   `json:"name"`
	Package   string   `json:"package"`
	Platforms []string `json:"platforms"`
}

type runtimeEntry struct {
	Name      string   `json:"name"`
	Image     string   `json:"image"`
	Platforms []string `json:"platforms"`
}

func checkReleaseInventory(ctx context.Context, root string, problems *diagnostics) bool {
	var inventory releaseArtifactInventory
	if !readPolicyJSON(ctx, root, releaseInventoryPath, &inventory, problems) {
		return false
	}
	if inventory.Version != policyVersion {
		problems.add(releaseInventoryPath, "version must be %d", policyVersion)
	}
	commands := discoverCommands(root, problems)
	actualBinaries := make([]string, 0, len(inventory.Binaries))
	previous := ""
	for _, binary := range inventory.Binaries {
		if binary.Name <= previous && previous != "" {
			problems.add(releaseInventoryPath, "binaries must be strictly sorted by name")
		}
		previous = binary.Name
		if !artifactRE.MatchString(binary.Name) || binary.Package != "./cmd/"+binary.Name {
			problems.add(releaseInventoryPath, "binary %q must use package ./cmd/<name>", binary.Name)
		}
		checkPlatforms(releaseInventoryPath, "binary "+binary.Name, binary.Platforms, problems)
		actualBinaries = append(actualBinaries, binary.Name)
	}
	accountedCommands := append([]string(nil), actualBinaries...)
	previous = ""
	for _, command := range inventory.SourceOnlyCommands {
		if command <= previous && previous != "" {
			problems.add(releaseInventoryPath, "source_only_commands must be strictly sorted and unique")
		}
		previous = command
		if !artifactRE.MatchString(command) {
			problems.add(releaseInventoryPath, "source-only command %q is invalid", command)
		}
		accountedCommands = append(accountedCommands, command)
	}
	sort.Strings(accountedCommands)
	for index := 1; index < len(accountedCommands); index++ {
		if accountedCommands[index] == accountedCommands[index-1] {
			problems.add(releaseInventoryPath, "command %q cannot be both release and source-only", accountedCommands[index])
		}
	}
	if strings.Join(accountedCommands, "\x00") != strings.Join(commands, "\x00") {
		problems.add(releaseInventoryPath, "binary inventory must exactly match cmd packages %v", commands)
	}

	composeImages := loadComposeServiceImages(ctx, root, "docker-compose.yml", problems)
	actualRuntime := make(map[string]string, len(inventory.DevelopmentRuntime))
	previous = ""
	for _, runtime := range inventory.DevelopmentRuntime {
		if runtime.Name <= previous && previous != "" {
			problems.add(releaseInventoryPath, "development_runtime must be strictly sorted by name")
		}
		previous = runtime.Name
		if !artifactRE.MatchString(runtime.Name) {
			problems.add(releaseInventoryPath, "runtime name %q is invalid", runtime.Name)
		}
		if err := validateImmutableImage(runtime.Image); err != nil {
			problems.add(releaseInventoryPath, "runtime %q image: %v", runtime.Name, err)
		}
		if strings.Join(runtime.Platforms, "\x00") != "linux/amd64\x00linux/arm64" {
			problems.add(releaseInventoryPath, "runtime %q platforms must be exactly [linux/amd64 linux/arm64]", runtime.Name)
		}
		if _, duplicate := actualRuntime[runtime.Name]; duplicate {
			problems.add(releaseInventoryPath, "duplicate runtime %q", runtime.Name)
		}
		actualRuntime[runtime.Name] = runtime.Image
	}
	if !equalStringMaps(actualRuntime, composeImages) {
		problems.add(releaseInventoryPath, "development_runtime must exactly match docker-compose.yml service images")
	}
	if inventory.PublicReleaseReady {
		if _, err := resolveRealRepositoryPath(root, "LICENSE", false); err != nil {
			problems.add(releaseInventoryPath, "public_release_ready requires a real top-level LICENSE file")
		}
	}
	return inventory.PublicReleaseReady
}

func checkPlatforms(relative, field string, platforms []string, problems *diagnostics) {
	if len(platforms) == 0 {
		problems.add(relative, "%s platforms must not be empty", field)
		return
	}
	previous := ""
	for _, platform := range platforms {
		if platform != "linux/amd64" && platform != "linux/arm64" {
			problems.add(relative, "%s platform %q is not allowed", field, platform)
		}
		if previous != "" && platform <= previous {
			problems.add(relative, "%s platforms must be strictly sorted and unique", field)
		}
		previous = platform
	}
}

func discoverCommands(root string, problems *diagnostics) []string {
	directory, err := resolveRealRepositoryPath(root, "cmd", true)
	if err != nil {
		problems.add("cmd", "%v", err)
		return nil
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		problems.add("cmd", "read directory: %v", err)
		return nil
	}
	var commands []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			problems.add("cmd/"+entry.Name(), "symlinks are forbidden")
			continue
		}
		if !entry.IsDir() {
			continue
		}
		children, err := os.ReadDir(filepath.Join(directory, entry.Name()))
		if err != nil {
			problems.add("cmd/"+entry.Name(), "read directory: %v", err)
			continue
		}
		for _, child := range children {
			if !child.IsDir() && strings.HasSuffix(child.Name(), ".go") && !strings.HasSuffix(child.Name(), "_test.go") {
				commands = append(commands, entry.Name())
				break
			}
		}
	}
	sort.Strings(commands)
	return commands
}

func loadComposeServiceImages(ctx context.Context, root, relative string, problems *diagnostics) map[string]string {
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		return nil
	}
	remaining := maxYAMLNodes
	node, err := parseComposeYAMLWithBudget(ctx, data, &remaining)
	if err != nil {
		problems.add(relative, "invalid Compose YAML: %v", err)
		return nil
	}
	rootMap := yamlDocumentMap(node)
	services := yamlMapValue(rootMap, "services")
	serviceMaps := yamlNamedMappings(relative, "services", services, problems)
	images := make(map[string]string)
	for name, service := range serviceMaps {
		image := yamlMapValue(service, "image")
		if image == nil {
			continue
		}
		if image.Kind != yaml.ScalarNode {
			problems.add(relative, "service %q image must be a scalar", name)
			continue
		}
		images[name] = image.Value
	}
	return images
}

type riskExceptionManifest struct {
	Version          int             `json:"version"`
	ApprovalEnforced bool            `json:"approval_enforced"`
	Exceptions       []riskException `json:"exceptions"`
}

type riskException struct {
	ID                   string   `json:"id"`
	Category             string   `json:"category"`
	Severity             string   `json:"severity"`
	Target               string   `json:"target"`
	Scope                string   `json:"scope"`
	Release              string   `json:"release"`
	FindingID            string   `json:"finding_id"`
	Justification        string   `json:"justification"`
	CompensatingControls []string `json:"compensating_controls"`
	Ticket               string   `json:"ticket"`
	Owner                string   `json:"owner"`
	ApprovedBy           string   `json:"approved_by"`
	ApprovedAt           string   `json:"approved_at"`
	ExpiresAt            string   `json:"expires_at"`
}

func checkAuxiliaryPolicyFiles(ctx context.Context, root string, publicReleaseReady bool, problems *diagnostics) {
	checkLicensePolicy(ctx, root, problems)
	checkRiskExceptions(ctx, root, publicReleaseReady, problems)
}

func checkLicensePolicy(ctx context.Context, root string, problems *diagnostics) {
	const relative = "supply-chain/license-policy.json"
	if !checkSupplyContext(ctx, problems) {
		return
	}
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		return
	}
	if err := licensepolicy.ValidatePolicy(data); err != nil {
		problems.add(relative, "%v", err)
	}
}

func checkRiskExceptions(ctx context.Context, root string, _ bool, problems *diagnostics) {
	const relative = "supply-chain/risk-exceptions.json"
	var manifest riskExceptionManifest
	if !readPolicyJSON(ctx, root, relative, &manifest, problems) {
		return
	}
	if manifest.Version != policyVersion {
		problems.add(relative, "version must be %d", policyVersion)
	}
	if len(manifest.Exceptions) > 0 && !manifest.ApprovalEnforced {
		problems.add(relative, "exceptions require approval_enforced: true")
	}
	if manifest.ApprovalEnforced {
		checkRiskCODEOWNERS(root, problems)
	}
	allowedCategories := map[string]struct{}{"container": {}, "license": {}, "misconfiguration": {}, "sast": {}, "vulnerability": {}}
	previous := ""
	for _, exception := range manifest.Exceptions {
		if !artifactRE.MatchString(exception.ID) {
			problems.add(relative, "exception id %q is invalid", exception.ID)
		}
		if previous != "" && exception.ID <= previous {
			problems.add(relative, "exceptions must be strictly sorted and unique by id")
		}
		previous = exception.ID
		if _, allowed := allowedCategories[exception.Category]; !allowed {
			problems.add(relative, "exception %q category is invalid", exception.ID)
		}
		if exception.Severity != "high" && exception.Severity != "critical" {
			problems.add(relative, "exception %q severity must be high or critical", exception.ID)
		}
		if strings.TrimSpace(exception.Target) == "" || strings.TrimSpace(exception.FindingID) == "" || strings.TrimSpace(exception.Justification) == "" || strings.TrimSpace(exception.Ticket) == "" || strings.TrimSpace(exception.Owner) == "" || strings.TrimSpace(exception.ApprovedBy) == "" {
			problems.add(relative, "exception %q has empty required fields", exception.ID)
		}
		if strings.ContainsAny(exception.Target+exception.FindingID+exception.Ticket, "*?[]{}") || strings.Contains(exception.Target+exception.FindingID+exception.Ticket, "${{") {
			problems.add(relative, "exception %q fields must not contain wildcards or expressions", exception.ID)
		}
		if !validPinnedVersion(exception.Release) {
			problems.add(relative, "exception %q release must be an exact v-prefixed semantic version", exception.ID)
		}
		parts := strings.SplitN(exception.Scope, "@", 2)
		if len(parts) != 2 || parts[0] != exception.Target || parts[1] == "" || strings.EqualFold(parts[1], "latest") || strings.ContainsAny(exception.Scope, "*?[]{}") {
			problems.add(relative, "exception %q scope must bind target to an exact version or digest", exception.ID)
		}
		if exception.Owner == exception.ApprovedBy {
			problems.add(relative, "exception %q owner and approver must differ", exception.ID)
		}
		if len(exception.CompensatingControls) == 0 {
			problems.add(relative, "exception %q requires compensating controls", exception.ID)
		}
		approvedAt, approvedErr := time.Parse(time.RFC3339, exception.ApprovedAt)
		expiresAt, expiryErr := time.Parse(time.RFC3339, exception.ExpiresAt)
		now := time.Now().UTC()
		if approvedErr != nil || approvedAt.After(now) {
			problems.add(relative, "exception %q must have a non-future RFC3339 approval time", exception.ID)
		}
		if expiryErr != nil || !expiresAt.After(now) {
			problems.add(relative, "exception %q must have a future RFC3339 expiry", exception.ID)
		} else if approvedErr == nil && (expiresAt.Sub(approvedAt) <= 0 || expiresAt.Sub(approvedAt) > 90*24*time.Hour) {
			problems.add(relative, "exception %q approval window must be positive and no longer than 90 days", exception.ID)
		}
	}
}

func checkRiskCODEOWNERS(root string, problems *diagnostics) {
	const relative = ".github/CODEOWNERS"
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		problems.add("supply-chain/risk-exceptions.json", "approval_enforced requires .github/CODEOWNERS")
		return
	}
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "/supply-chain/risk-exceptions.json" {
			valid := true
			for _, owner := range fields[1:] {
				if !strings.HasPrefix(owner, "@") || len(owner) < 3 {
					valid = false
				}
			}
			if valid {
				found = true
			}
		}
	}
	if !found {
		problems.add("supply-chain/risk-exceptions.json", "approval_enforced requires exact CODEOWNERS coverage")
	}
}

func readPolicyJSON(ctx context.Context, root, relative string, target any, problems *diagnostics) bool {
	data, ok := readRepositoryFile(root, relative, problems)
	if !ok {
		return false
	}
	remaining := maxJSONDocumentNodes
	value, err := parseStrictJSONWithBudget(ctx, data, &remaining)
	if err != nil {
		problems.add(relative, "invalid JSON: %v", err)
		return false
	}
	if err := decodeKnownFields(value, target); err != nil {
		problems.add(relative, "decode: %v", err)
		return false
	}
	return true
}

func readRepositoryFile(root, relative string, problems *diagnostics) ([]byte, bool) {
	path, err := resolveRealRepositoryPath(root, relative, false)
	if err != nil {
		problems.add(relative, "%v", err)
		return nil, false
	}
	// #nosec G304 -- path was canonicalized beneath root and every component was checked for symlinks.
	file, err := os.Open(path)
	if err != nil {
		problems.add(relative, "open: %v", err)
		return nil, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		problems.add(relative, "inspect: %v", err)
		return nil, false
	}
	if !info.Mode().IsRegular() || info.Size() > maxPolicyFileSize {
		problems.add(relative, "must be a regular file no larger than %d bytes", maxPolicyFileSize)
		return nil, false
	}
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		problems.add(relative, "read: %v", err)
		return nil, false
	}
	return data, true
}

func resolveRealRepositoryPath(root, relative string, wantDirectory bool) (string, error) {
	cleaned, err := safeContractPath(relative)
	if err != nil {
		return "", err
	}
	current := root
	parts := strings.Split(cleaned, "/")
	for index, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlinks are forbidden")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", fmt.Errorf("path component %q is not a directory", part)
		}
		if index == len(parts)-1 {
			if wantDirectory && !info.IsDir() {
				return "", fmt.Errorf("must be a directory")
			}
			if !wantDirectory && !info.Mode().IsRegular() {
				return "", fmt.Errorf("must be a regular file")
			}
		}
	}
	return current, nil
}

func discoverRepositoryFiles(ctx context.Context, root string, problems *diagnostics) (composeFiles, dockerfiles []string) {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if !checkSupplyContext(ctx, problems) {
			return filepath.SkipAll
		}
		if walkErr != nil {
			problems.add(toSlashRelative(root, path), "walk: %v", walkErr)
			return nil
		}
		relative := toSlashRelative(root, path)
		if entry.IsDir() && (relative == ".git" || relative == "vendor" || relative == "dist") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		base := strings.ToLower(entry.Name())
		if isComposeFilename(base) {
			if entry.Type()&os.ModeSymlink != 0 {
				problems.add(relative, "symlinks are forbidden")
			} else {
				composeFiles = append(composeFiles, relative)
			}
		}
		if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
			if entry.Type()&os.ModeSymlink != 0 {
				problems.add(relative, "symlinks are forbidden")
			} else {
				dockerfiles = append(dockerfiles, relative)
			}
		}
		return nil
	})
	if err != nil {
		problems.add("supply-chain", "walk repository images: %v", err)
	}
	sort.Strings(composeFiles)
	sort.Strings(dockerfiles)
	return composeFiles, dockerfiles
}

func discoverGoFiles(ctx context.Context, root string, problems *diagnostics) (modules, workspaces []string) {
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if !checkSupplyContext(ctx, problems) {
			return filepath.SkipAll
		}
		if walkErr != nil {
			problems.add(toSlashRelative(root, path), "walk: %v", walkErr)
			return nil
		}
		relative := toSlashRelative(root, path)
		if entry.IsDir() && (relative == ".git" || relative == "vendor" || relative == "dist") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() != "go.mod" && entry.Name() != "go.work" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			problems.add(relative, "symlinks are forbidden")
			return nil
		}
		if entry.Name() == "go.mod" {
			modules = append(modules, relative)
		} else {
			workspaces = append(workspaces, relative)
		}
		return nil
	})
	if err != nil {
		problems.add("go.mod", "walk repository: %v", err)
	}
	sort.Strings(modules)
	sort.Strings(workspaces)
	return modules, workspaces
}

func checkUnsupportedPackageEcosystems(ctx context.Context, root string, problems *diagnostics) {
	registered := map[string]struct{}{
		"frontend/package-lock.json":                        {},
		"frontend/package.json":                             {},
		"integrations/n8n-nodes-torgnexa/package-lock.json": {},
		"integrations/n8n-nodes-torgnexa/package.json":      {},
		"sdk/python/pyproject.toml":                         {},
		"sdk/typescript/package.json":                       {},
	}
	found := make(map[string]struct{}, len(registered))
	exactNames := map[string]struct{}{
		"cargo.lock": {}, "cargo.toml": {}, "composer.json": {}, "composer.lock": {},
		"gemfile": {}, "gemfile.lock": {}, "gradlew": {}, "npm-shrinkwrap.json": {},
		"package-lock.json": {}, "package.json": {}, "pipfile": {}, "pipfile.lock": {},
		"pnpm-lock.yaml": {}, "poetry.lock": {}, "pom.xml": {}, "pyproject.toml": {},
		"requirements.txt": {}, "yarn.lock": {},
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if !checkSupplyContext(ctx, problems) {
			return filepath.SkipAll
		}
		if walkErr != nil {
			problems.add(toSlashRelative(root, path), "walk: %v", walkErr)
			return nil
		}
		relative := toSlashRelative(root, path)
		if entry.IsDir() && (relative == ".git" || relative == "vendor" || relative == "dist" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		name := strings.ToLower(entry.Name())
		_, exact := exactNames[name]
		gradle := strings.HasPrefix(name, "build.gradle") || strings.HasPrefix(name, "settings.gradle")
		requirements := strings.HasPrefix(name, "requirements-") && strings.HasSuffix(name, ".txt")
		if exact || gradle || requirements {
			if _, allowed := registered[relative]; allowed {
				found[relative] = struct{}{}
			} else {
				problems.add(relative, "package ecosystem is not registered in supply-chain policy")
			}
		}
		return nil
	})
	if err != nil {
		problems.add("supply-chain", "walk package ecosystems: %v", err)
	}
	for relative := range registered {
		if _, exists := found[relative]; !exists {
			problems.add(relative, "registered package ecosystem file is missing")
		}
	}
}

func isComposeFilename(name string) bool {
	return name == "compose.yml" || name == "compose.yaml" || name == "docker-compose.yml" || name == "docker-compose.yaml" ||
		(strings.HasPrefix(name, "compose.") && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml"))) ||
		(strings.HasPrefix(name, "docker-compose.") && (strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")))
}

func yamlDocumentMap(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	return node
}

func yamlMapValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func workflowHasTrigger(root *yaml.Node, trigger string) bool {
	on := yamlMapValue(root, "on")
	if on == nil {
		return false
	}
	if on.Kind == yaml.ScalarNode {
		return on.Value == trigger
	}
	if on.Kind == yaml.SequenceNode {
		for _, child := range on.Content {
			if child.Kind == yaml.ScalarNode && child.Value == trigger {
				return true
			}
		}
		return false
	}
	return yamlMapValue(on, trigger) != nil
}

func yamlNamedMappings(relative, field string, node *yaml.Node, problems *diagnostics) map[string]*yaml.Node {
	result := make(map[string]*yaml.Node)
	if node == nil || node.Kind != yaml.MappingNode {
		problems.add(relative, "%s must be a mapping", field)
		return result
	}
	for index := 0; index < len(node.Content); index += 2 {
		name, value := node.Content[index].Value, node.Content[index+1]
		if value.Kind != yaml.MappingNode {
			problems.add(relative, "%s %q must be a mapping", field, name)
			continue
		}
		result[name] = value
	}
	return result
}

func yamlStringList(node *yaml.Node, relative, field string, problems *diagnostics) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	if node.Kind != yaml.SequenceNode {
		problems.add(relative, "%s must be a string or sequence", field)
		return nil
	}
	result := make([]string, 0, len(node.Content))
	seen := make(map[string]struct{})
	for _, child := range node.Content {
		if child.Kind != yaml.ScalarNode || child.Value == "" {
			problems.add(relative, "%s entries must be non-empty strings", field)
			continue
		}
		if _, duplicate := seen[child.Value]; duplicate {
			problems.add(relative, "%s contains duplicate %q", field, child.Value)
		}
		seen[child.Value] = struct{}{}
		result = append(result, child.Value)
	}
	return result
}

func yamlFalseValue(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!bool" && node.Value == "false"
}

func walkYAMLMapping(node *yaml.Node, visit func(key, value *yaml.Node)) {
	if node == nil {
		return
	}
	if node.Kind == yaml.MappingNode {
		for index := 0; index < len(node.Content); index += 2 {
			key, value := node.Content[index], node.Content[index+1]
			visit(key, value)
		}
	}
	for _, child := range node.Content {
		walkYAMLMapping(child, visit)
	}
}

func equalStringMaps(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
