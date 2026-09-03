package architecture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/connectors/conformance"
)

func TestRepositoryPolicyFixturePasses(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	report, err := CheckRepository(context.Background(), root)
	if err != nil {
		t.Fatalf("CheckRepository() error = %v", err)
	}
	if report.Modules != 1 || report.Providers != 0 || report.Reviews != 1 {
		t.Fatalf("CheckRepository() report = %#v", report)
	}
}

func TestRepositoryIgnoresFrontendPackageManagerDependencies(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	writeTestFile(t, root, "frontend/node_modules/typescript/vendor/example.invalid/tool.go", "package tool\n")
	target := filepath.Join(root, "frontend", "node_modules", ".bin", "tsc")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../typescript/bin/tsc", target); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckRepository(context.Background(), root); err != nil {
		t.Fatalf("package-manager dependency tree must not enter first-party inventory: %v", err)
	}
}

func TestRepositoryAllowsProviderNeutralIdentityChecks(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	writeTestFile(t, root, "internal/core/synthetic/model.go", `package synthetic

func sameAccount(accountID, connectorID string) bool {
	return accountID != "" && accountID == connectorID
}
`)
	if _, err := CheckRepository(context.Background(), root); err != nil {
		t.Fatalf("provider-neutral identity validation must remain allowed: %v", err)
	}
}

func TestRepositoryAcceptsSupplementalStageReview(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	writeTestFile(t, root, "tasks/issues/089-fx.md", "# Task 089\n")
	record := validTestReview("089", "internal/core/synthetic/model.go")
	record.Stage = "b"
	record.ID = "ARCH-089B"
	writeTestJSON(t, root, reviewsDir+"/089b-fx-storage.json", record)
	report, err := CheckRepository(context.Background(), root)
	if err != nil {
		t.Fatalf("CheckRepository() error = %v", err)
	}
	if report.Reviews != 2 {
		t.Fatalf("CheckRepository() reviews = %d", report.Reviews)
	}
}

func TestRepositoryRejectsPolicyAndReviewBypasses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
		want   string
	}{
		{
			name: "duplicate policy field",
			mutate: func(t *testing.T, root string) {
				replaceTestFile(t, root, policyPath, `"schema_version": 1`, `"schema_version": 1, "schema_version": 1`)
			},
			want: "duplicate JSON object key",
		},
		{
			name: "retired provider array is explicit",
			mutate: func(t *testing.T, root string) {
				configuration := loadTestPolicy(t, root)
				configuration.RetiredProviders = nil
				writeTestJSON(t, root, policyPath, configuration)
			},
			want: "retired_providers must be present as an explicit JSON array",
		},
		{
			name: "retired provider requires canonical existing task",
			mutate: func(t *testing.T, root string) {
				configuration := loadTestPolicy(t, root)
				configuration.RetiredProviders = []retiredProvider{{
					ID: "ghost", Implementation: "plugins/ghost", Manifest: "plugins/ghost/manifest.json",
					ConnectorSpec: "docs/connectors/ghost/spec.md", CapabilityAudit: "docs/connectors/ghost/capability-audit.md",
					ConformancePlan: "docs/connectors/ghost/conformance-plan.md", RetirementTask: "999",
				}}
				writeTestJSON(t, root, policyPath, configuration)
			},
			want: "retirement_task \"999\" is invalid or missing",
		},
		{
			name: "current and retired provider ids cannot overlap",
			mutate: func(t *testing.T, root string) {
				configuration := loadTestPolicy(t, root)
				configuration.ProviderAdmission.Enabled = true
				configuration.Providers = []provider{{
					ID: "ghost", Family: "marketplace", Implementation: "plugins/ghost", Manifest: "plugins/ghost/manifest.json",
					ConnectorSpec: "docs/connectors/ghost/spec.md", CapabilityAudit: "docs/connectors/ghost/capability-audit.md",
					ConformancePlan: "docs/connectors/ghost/conformance-plan.md", AllowedExternalImports: []string{},
				}}
				configuration.RetiredProviders = []retiredProvider{{
					ID: "ghost", Implementation: "plugins/ghost", Manifest: "plugins/ghost/manifest.json",
					ConnectorSpec: "docs/connectors/ghost/spec.md", CapabilityAudit: "docs/connectors/ghost/capability-audit.md",
					ConformancePlan: "docs/connectors/ghost/conformance-plan.md", RetirementTask: "080",
				}}
				writeTestJSON(t, root, policyPath, configuration)
			},
			want: "cannot be current and retired or repeated",
		},
		{
			name: "retired provider evidence cannot alias",
			mutate: func(t *testing.T, root string) {
				configuration := loadTestPolicy(t, root)
				configuration.RetiredProviders = []retiredProvider{{
					ID: "ghost", Implementation: "plugins/ghost", Manifest: "plugins/ghost/manifest.json",
					ConnectorSpec: "plugins/ghost/manifest.json", CapabilityAudit: "docs/connectors/ghost/capability-audit.md",
					ConformancePlan: "docs/connectors/ghost/conformance-plan.md", RetirementTask: "080",
				}}
				writeTestJSON(t, root, policyPath, configuration)
			},
			want: "provider evidence path \"plugins/ghost/manifest.json\" is reused",
		},
		{
			name: "unknown review field",
			mutate: func(t *testing.T, root string) {
				replaceTestFile(t, root, reviewsDir+"/080-synthetic.json", `"provider": null`, `"provider": null, "unexpected": true`)
			},
			want: "unknown field",
		},
		{
			name: "placeholder evidence",
			mutate: func(t *testing.T, root string) {
				replaceTestFile(t, root, reviewsDir+"/080-synthetic.json", "Synthetic rationale for deterministic architecture policy validation.", "TODO replace this placeholder rationale before review")
			},
			want: "contain no placeholder",
		},
		{
			name: "incomplete impact matrix",
			mutate: func(t *testing.T, root string) {
				record := loadTestReview(t, root, reviewsDir+"/080-synthetic.json")
				record.Impacts = record.Impacts[:len(record.Impacts)-1]
				writeTestJSON(t, root, reviewsDir+"/080-synthetic.json", record)
			},
			want: "impacts must contain all",
		},
		{
			name: "unregistered package",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/platform/unregistered/adapter.go", "package unregistered\n")
			},
			want: "is not registered",
		},
		{
			name: "Go source outside fail-closed roots",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "bridge/ozon/client.go", "package ozon\n")
			},
			want: "outside the fail-closed first-party source-root inventory",
		},
		{
			name: "root vendor source cannot hide runtime code",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "vendor/example.invalid/stealth/provider.go", "package stealth\nconst Provider = \"ozon\"\n")
			},
			want: "vendor directories are forbidden",
		},
		{
			name: "nested vendor source cannot hide runtime code",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/vendor/example.invalid/stealth/provider.go", "package stealth\nconst Provider = \"ozon\"\n")
			},
			want: "vendor directories are forbidden",
		},
		{
			name: "provider implementation disguised as platform module",
			mutate: func(t *testing.T, root string) {
				configuration := loadTestPolicy(t, root)
				configuration.Modules = append(configuration.Modules, module{Path: "internal/platform/ozon", Kind: "platform_capability"})
				writeTestJSON(t, root, policyPath, configuration)
				writeTestFile(t, root, "internal/platform/ozon/client.go", "package ozon\n")
			},
			want: "provider-specific non-provider runtime identifier",
		},
		{
			name: "core imports platform",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nimport _ \"github.com/torgnexa/torgnexa/internal/platform/eventbus\"\n")
			},
			want: "Core may import only",
		},
		{
			name: "cgo escape hatch",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nimport \"C\"\n")
			},
			want: "cgo is forbidden",
		},
		{
			name: "linkname escape hatch",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nimport _ \"unsafe\"\n//go:linkname hidden runtime.hidden\nfunc hidden()\n")
			},
			want: "//go:linkname is forbidden",
		},
		{
			name: "provider identifier in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nconst OzonOrder = \"synthetic\"\n")
			},
			want: "provider-specific Core identifier",
		},
		{
			name: "provider branch in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nfunc route(platform string) bool { if platform == \"vk\" { return true }; return false }\n")
			},
			want: "provider-specific Core branch",
		},
		{
			name: "previously unknown provider branch in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nfunc route(provider string) bool { return provider == \"future-provider\" }\n")
			},
			want: "provider-specific Core branch",
		},
		{
			name: "provider switch case in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nfunc route(provider string) bool { switch provider { case \"ozon\": return true }; return false }\n")
			},
			want: "provider-specific Core branch",
		},
		{
			name: "provider map dispatch in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nvar routes = map[string]int{\"ozon\": 1}\nfunc route(provider string) int { return routes[provider] }\n")
			},
			want: "provider-specific Core string literal",
		},
		{
			name: "provider alias comparison in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nconst remoteKind = \"ozon\"\nfunc route(provider string) bool { return provider == remoteKind }\n")
			},
			want: "provider-specific Core string literal",
		},
		{
			name: "unknown provider alias comparison in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nconst remoteKind = \"future-provider\"\nfunc route(provider string) bool { return provider == remoteKind }\n")
			},
			want: "provider-specific Core branch",
		},
		{
			name: "short provider table dispatch in core",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nvar routes = map[string]int{\"vk\": 1}\nfunc route(provider string) int { return routes[provider] }\n")
			},
			want: "provider-specific table dispatch",
		},
		{
			name: "review path violates schema alphabet",
			mutate: func(t *testing.T, root string) {
				record := loadTestReview(t, root, reviewsDir+"/080-synthetic.json")
				record.Scopes = []string{"internal/core/synthétic/model.go"}
				writeTestJSON(t, root, reviewsDir+"/080-synthetic.json", record)
			},
			want: "ASCII repository path",
		},
		{
			name: "implementation claims frozen pillar",
			mutate: func(t *testing.T, root string) {
				record := loadTestReview(t, root, reviewsDir+"/080-synthetic.json")
				record.FrozenPillars = []string{canonicalPillars[0]}
				writeTestJSON(t, root, reviewsDir+"/080-synthetic.json", record)
			},
			want: "non-pillar records may not declare affected frozen pillars",
		},
		{
			name: "provider review while admission disabled",
			mutate: func(t *testing.T, root string) {
				data := readTestFile(t, root, reviewsDir+"/080-synthetic.json")
				data = strings.Replace(data, `"change_class": "implementation"`, `"change_class": "new_provider"`, 1)
				data = strings.Replace(data, `"provider": null`, `"provider": {"id":"synthetic","route":"connector_sdk","manifest":"docs/manifest.json","connector_spec":"docs/spec.md","capability_audit":"docs/audit.md","conformance_plan":"docs/conformance.md"}`, 1)
				writeTestFile(t, root, "docs/manifest.json", "{}\n")
				writeTestFile(t, root, "docs/spec.md", "Synthetic connector specification evidence.")
				writeTestFile(t, root, "docs/audit.md", "Synthetic provider capability audit evidence.")
				writeTestFile(t, root, "docs/conformance.md", "Synthetic connector conformance plan evidence.")
				writeTestFile(t, root, reviewsDir+"/080-synthetic.json", data)
			},
			want: "current or explicitly retired provider",
		},
		{
			name: "unregistered plugin binary with historical review",
			mutate: func(t *testing.T, root string) {
				writeTestFile(t, root, "tasks/issues/081-synthetic.md", "# Task 081\n")
				writeTestFile(t, root, "plugins/ghost/manifest.json", "{}\n")
				writeTestFile(t, root, "plugins/ghost/plugin.wasm", "synthetic wasm payload\n")
				writeTestFile(t, root, "docs/connectors/ghost/spec.md", "Synthetic connector specification evidence.\n")
				writeTestFile(t, root, "docs/connectors/ghost/capability-audit.md", "Synthetic provider capability audit evidence.\n")
				writeTestFile(t, root, "docs/connectors/ghost/conformance-plan.md", "Synthetic connector conformance plan evidence.\n")
				record := validTestReview("081", "plugins/ghost/plugin.wasm")
				record.ChangeClass = "new_provider"
				record.GapAudit.Decision = "route_to_connector_sdk"
				record.Provider = &providerReview{
					ID: "ghost", Route: "connector_sdk", Manifest: "plugins/ghost/manifest.json",
					ConnectorSpec: "docs/connectors/ghost/spec.md", CapabilityAudit: "docs/connectors/ghost/capability-audit.md",
					ConformancePlan: "docs/connectors/ghost/conformance-plan.md",
				}
				writeTestJSON(t, root, reviewsDir+"/081-ghost-provider.json", record)
			},
			want: "unregistered provider directory is forbidden",
		},
		{
			name: "explicit retired plugin cannot retain executable payload",
			mutate: func(t *testing.T, root string) {
				writeRetiredProviderFixture(t, root, "ghost", "081")
				writeTestFile(t, root, "plugins/ghost/plugin.js", "throw new Error('must never execute');\n")
				// #nosec G302 -- the executable permission is the malicious fixture under test.
				if err := os.Chmod(filepath.Join(root, "plugins/ghost/plugin.js"), 0o700); err != nil {
					t.Fatal(err)
				}
			},
			want: "tombstone must contain only the exact regular non-executable manifest.json",
		},
		{
			name: "pillar ADR missing mandatory impact headings",
			mutate: func(t *testing.T, root string) {
				record := loadTestReview(t, root, reviewsDir+"/080-synthetic.json")
				record.ChangeClass = "pillar_change"
				record.GapAudit.Decision = "architecture_change"
				record.FrozenPillars = []string{canonicalPillars[0]}
				reference := "adr/0035-incomplete.md"
				record.ADR = &reference
				writeTestJSON(t, root, reviewsDir+"/080-synthetic.json", record)
				writeTestFile(t, root, reference, "# ADR 0035\n\nStatus: Accepted\n\n## Context\n\nSynthetic.\n")
			},
			want: "missing required heading",
		},
		{
			name: "pillar ADR has empty mandatory sections",
			mutate: func(t *testing.T, root string) {
				record := loadTestReview(t, root, reviewsDir+"/080-synthetic.json")
				record.ChangeClass = "pillar_change"
				record.GapAudit.Decision = "architecture_change"
				record.FrozenPillars = []string{canonicalPillars[0]}
				reference := "adr/0035-empty.md"
				record.ADR = &reference
				writeTestJSON(t, root, reviewsDir+"/080-synthetic.json", record)
				writeTestFile(t, root, reference, "# ADR 0035\n\nStatus: Accepted\n\n## Context\n\n## Decision\n\n## Consequences\n\n## Alternatives considered\n\n## Compatibility impact\n\n## Migration and data impact\n\n## Security and privacy impact\n\n## Operational impact\n")
			},
			want: "must contain a meaningful non-placeholder rationale",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := writeArchitectureFixture(t)
			test.mutate(t, root)
			_, err := CheckRepository(context.Background(), root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckRepository() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestTask025PluginSecurityBoundaryStaysNonExecuting(t *testing.T) {
	for _, imported := range []string{"os", "io/fs", "path/filepath", "os/exec", "plugin", "runtime/cgo", "syscall", "unsafe"} {
		if !pluginSecurityForbiddenImport(imported) {
			t.Fatalf("Task 025 forbidden import %q was not rejected", imported)
		}
	}
	for _, imported := range []string{"context", "crypto/ed25519", "encoding/json", "net"} {
		if pluginSecurityForbiddenImport(imported) {
			t.Fatalf("Task 025 safe validation import %q was rejected", imported)
		}
	}
}

func TestRepositoryDiagnosticsAreDeterministic(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	replaceTestFile(t, root, reviewsDir+"/080-synthetic.json", "Synthetic rationale for deterministic architecture policy validation.", "TODO unresolved placeholder")
	writeTestFile(t, root, "internal/platform/unregistered/adapter.go", "package unregistered\n")
	_, first := CheckRepository(context.Background(), root)
	_, second := CheckRepository(context.Background(), root)
	if first == nil || second == nil || first.Error() != second.Error() {
		t.Fatalf("diagnostics are not deterministic:\nfirst=%v\nsecond=%v", first, second)
	}
}

func TestProviderAdmissionUsesReviewedSDKBoundary(t *testing.T) {
	t.Parallel()
	root := writeAdmittedProviderFixture(t)
	if _, err := CheckRepository(context.Background(), root); err != nil {
		t.Fatalf("valid provider boundary rejected: %v", err)
	}

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "direct database",
			source: "package synthetic\nimport (\n\t\"database/sql\"\n\tsdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\n)\nvar _ *sql.DB\nvar _ sdk.Connector\n",
			want:   "bypasses the Connector SDK boundary",
		},
		{
			name:   "unapproved external SDK",
			source: "package synthetic\nimport (\n\t_ \"github.com/segmentio/kafka-go\"\n\tsdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\n)\nvar _ sdk.Connector\n",
			want:   "bypasses the Connector SDK boundary",
		},
		{
			name:   "direct process execution",
			source: "package synthetic\nimport (\n\t_ \"os/exec\"\n\tsdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\n)\nvar _ sdk.Connector\n",
			want:   "bypasses the Connector SDK boundary",
		},
		{
			name:   "direct network",
			source: "package synthetic\nimport (\n\t_ \"net/http\"\n\tsdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\n)\nvar _ sdk.Connector\n",
			want:   "bypasses the Connector SDK boundary",
		},
		{
			name:   "direct host filesystem and environment",
			source: "package synthetic\nimport (\n\t_ \"os\"\n\tsdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\n)\nvar _ sdk.Connector\n",
			want:   "bypasses the Connector SDK boundary",
		},
		{
			name:   "blank connector SDK import",
			source: "package synthetic\nimport _ \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\n",
			want:   "has no non-test Go implementation importing the Connector SDK",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := writeAdmittedProviderFixture(t)
			writeTestFile(t, fixture, "connectors/synthetic/connector.go", test.source)
			if _, err := CheckRepository(context.Background(), fixture); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckRepository() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestProviderAdmissionRequiresPassingConformanceReport(t *testing.T) {
	t.Parallel()
	root := writeAdmittedProviderFixture(t)
	if err := os.Remove(filepath.Join(root, "docs/connectors/synthetic/conformance-report.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckRepository(context.Background(), root); err == nil || !strings.Contains(err.Error(), "conformance report") {
		t.Fatalf("missing conformance report error = %v", err)
	}
}

func TestRetiredProviderHistoricalReviewPassesWithAdmissionDisabled(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	writeRetiredProviderFixture(t, root, "ghost", "081")
	if _, err := CheckRepository(context.Background(), root); err != nil {
		t.Fatalf("valid retired provider tombstone rejected: %v", err)
	}
}

func TestRetiredProviderIDRemainsCoreDenylisted(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	writeRetiredProviderFixture(t, root, "ghost", "081")
	writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\nconst Ghost = 1\n")
	if _, err := CheckRepository(context.Background(), root); err == nil || !strings.Contains(err.Error(), "registered provider identifier is forbidden in Core code") {
		t.Fatalf("retired provider identifier error = %v", err)
	}
}

func TestProviderSDKRouteRequiresBuildableProductionFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		filename string
		source   string
	}{
		{name: "ignored filename", filename: "connectors/synthetic/_sdk.go", source: "package synthetic\nimport sdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\nvar _ sdk.Connector\n"},
		{name: "inactive build constraint", filename: "connectors/synthetic/sdk_never.go", source: "//go:build never\n\npackage synthetic\nimport sdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\nvar _ sdk.Connector\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := writeAdmittedProviderFixture(t)
			writeTestFile(t, root, "connectors/synthetic/connector.go", "package synthetic\ntype connector struct{}\n")
			writeTestFile(t, root, test.filename, test.source)
			if _, err := CheckRepository(context.Background(), root); err == nil || !strings.Contains(err.Error(), "has no non-test Go implementation importing the Connector SDK") {
				t.Fatalf("CheckRepository() error = %v", err)
			}
		})
	}
}

func TestProblemsAreBoundedAndSanitizePaths(t *testing.T) {
	t.Parallel()
	var many problems
	for index := 0; index < maxDiagnostics+500; index++ {
		many.add("path", "diagnostic %04d", index)
	}
	if len(many.items) != maxDiagnostics || !many.omitted || !strings.Contains(many.err().Error(), "additional deterministic diagnostics omitted") {
		t.Fatalf("bounded diagnostics state = items=%d omitted=%t error=%v", len(many.items), many.omitted, many.err())
	}
	var hostile problems
	hostile.add("bad\n\x1bpath", "failure %s", fmt.Sprintf("%c", rune(0)))
	message := hostile.err().Error()
	if strings.ContainsAny(message, "\n\x1b\x00") {
		t.Fatalf("diagnostic contains an unsafe control character: %q", message)
	}
}

func TestCompletedTaskStatusRejectsBlockedClaims(t *testing.T) {
	t.Parallel()
	if !completedTaskStatus([]byte("## Status\n\nCompleted on 2026-08-09.\n")) {
		t.Fatal("completed status was rejected")
	}
	for _, value := range []string{
		"## Status\n\nCompleted but blocked by external validation.\n",
		"## Status\n\nCompleted locally; operational acceptance pending.\n",
		"## Status\n\nIn progress.\n",
	} {
		if completedTaskStatus([]byte(value)) {
			t.Fatalf("blocked or incomplete task status passed: %q", value)
		}
	}
}

func TestRepositoryRejectsReviewSymlinkAndCancellation(t *testing.T) {
	t.Parallel()
	root := writeArchitectureFixture(t)
	original := filepath.Join(root, reviewsDir, "080-synthetic.json")
	if err := os.Rename(original, original+".target"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("080-synthetic.json.target", original); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckRepository(context.Background(), root); err == nil || !strings.Contains(err.Error(), "symlinks are forbidden") {
		t.Fatalf("symlink error = %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CheckRepository(canceled, writeArchitectureFixture(t)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled CheckRepository() error = %v", err)
	}
}

func TestStrictJSONRejectsDepthAndNodeBudgets(t *testing.T) {
	t.Parallel()
	deep := strings.Repeat("[", maxJSONDepth+2) + "0" + strings.Repeat("]", maxJSONDepth+2)
	var value any
	if err := decodeStrictJSON(context.Background(), []byte(deep), &value); err == nil || !strings.Contains(err.Error(), "maximum depth") {
		t.Fatalf("depth error = %v", err)
	}
	wide := "[" + strings.Repeat("0,", maxJSONNodes) + "0]"
	if err := decodeStrictJSON(context.Background(), []byte(wide), &value); err == nil || !strings.Contains(err.Error(), "maximum node count") {
		t.Fatalf("node error = %v", err)
	}
}

func writeArchitectureFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	configuration := policy{
		SchemaVersion:               1,
		ModulePath:                  "github.com/torgnexa/torgnexa",
		FrozenPillars:               append([]string(nil), canonicalPillars...),
		ImpactAreas:                 append([]string(nil), canonicalImpactAreas...),
		ProviderImplementationRoots: append([]string(nil), canonicalProviderRoots...),
		ProviderAdmission:           admission{Enabled: false, RequiredTasks: []string{"010", "025", "029", "064"}},
		CoreExternalImports:         []string{},
		CoreSharedImports:           []string{},
		ProviderAllowedImports:      []string{"internal/platform/connectors"},
		SensitivePaths:              append([]string(nil), canonicalSensitivePaths...),
		Modules:                     []module{{Path: "internal/core/synthetic", Kind: "core_domain"}},
		Providers:                   []provider{},
		RetiredProviders:            []retiredProvider{},
	}
	writeTestJSON(t, root, policyPath, configuration)
	writeTestFile(t, root, "internal/core/synthetic/model.go", "package synthetic\ntype Model struct{}\n")
	writeTestFile(t, root, "tasks/issues/080-synthetic.md", "# Task 080\n")
	writeTestFile(t, root, "adr/0001-synthetic.md", "# ADR 0001\n\nStatus: Accepted\n")
	writeTestFile(t, root, "prompts/05-architecture-gap-audit.txt", "privacy/data governance reconciliation/idempotency architecture/reviews/NNN-*.json\n")
	var freeze strings.Builder
	freeze.WriteString("# Architecture freeze\n\narchitecture/policy.json make architecture scripts/check-architecture.sh\n")
	for _, pillar := range canonicalPillars {
		freeze.WriteString("`" + pillar + "`\n")
	}
	writeTestFile(t, root, "docs/54-architecture-freeze-v1.md", freeze.String())
	writeTestFile(t, root, "Makefile", "architecture:\n\t./scripts/check-architecture.sh\ncheck: fmt-check test vet contracts architecture migrations policy\n")
	writeTestFile(t, root, "scripts/check-architecture.sh", "#!/usr/bin/env bash\nset -euo pipefail\ngo run -mod=readonly ./tools/architecturecheck --root . \"$@\"\n")
	// #nosec G302 -- the synthetic gate script must be executable to match the repository contract.
	if err := os.Chmod(filepath.Join(root, "scripts", "check-architecture.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, ".github/workflows/ci.yml", "fetch-depth: 0\ngithub.event.pull_request.base.sha\ngithub.event.pull_request.head.sha\nworktree add --detach \"$trusted_base\" \"$BASE_REVISION\"\n\"$trusted_checker\" --root \"$GITHUB_WORKSPACE\" --base \"$BASE_REVISION\" --head \"$HEAD_REVISION\"\n")
	writeTestFile(t, root, ".github/workflows/release.yml", "Validate the frozen architecture tree\nrun: ./scripts/check-architecture.sh\n")
	writeTestFile(t, root, ".github/PULL_REQUEST_TEMPLATE.md", "architecture/reviews/NNN-*.json\nfrozen-pillar\nProvider changes\n")
	writeTestFile(t, root, "templates/adr.md", "## Compatibility impact\n## Migration and data impact\n## Security and privacy impact\n## Operational impact\n")
	writeTestFile(t, root, "templates/architecture-gap-audit.md", "Synthetic architecture gap audit template.\n")
	writeTestFile(t, root, "contracts/governance/architecture-review.schema.json", "{}\n")
	record := validTestReview("080", "internal/core/synthetic/model.go")
	writeTestJSON(t, root, reviewsDir+"/080-synthetic.json", record)
	return root
}

func writeAdmittedProviderFixture(t *testing.T) string {
	t.Helper()
	root := writeArchitectureFixture(t)
	configuration := loadTestPolicy(t, root)
	configuration.ProviderAdmission.Enabled = true
	configuration.ProviderCompositionModule = canonicalProviderCompositionModule
	configuration.Modules = append(configuration.Modules, module{Path: canonicalProviderCompositionModule, Kind: "infrastructure_adapter"})
	sort.Slice(configuration.Modules, func(i, j int) bool { return configuration.Modules[i].Path < configuration.Modules[j].Path })
	configuration.Providers = []provider{{
		ID: "synthetic", Family: "marketplace", Implementation: "connectors/synthetic",
		Manifest: "connectors/synthetic/manifest.json", ConnectorSpec: "docs/connectors/synthetic/spec.md",
		CapabilityAudit: "docs/connectors/synthetic/capability-audit.md",
		ConformancePlan: "docs/connectors/synthetic/conformance-plan.md", AllowedExternalImports: []string{},
	}}
	writeTestJSON(t, root, policyPath, configuration)
	writeTestFile(t, root, canonicalProviderCompositionModule+"/runtime.go", "package builtinruntime\n")
	writeTestFile(t, root, "internal/core/synthetic/model.go", "package fixturecore\ntype Model struct{}\n")
	for _, task := range configuration.ProviderAdmission.RequiredTasks {
		writeTestFile(t, root, "tasks/issues/"+task+"-synthetic.md", "# Task "+task+"\n\n## Status\n\nCompleted for synthetic architecture tests.\n")
	}
	writeTestFile(t, root, "tasks/issues/081-synthetic.md", "# Task 081\n")
	writeTestFile(t, root, "connectors/synthetic/connector.go", "package synthetic\nimport sdk \"github.com/torgnexa/torgnexa/internal/platform/connectors\"\nvar _ sdk.Connector\n")
	writeTestJSON(t, root, "connectors/synthetic/manifest.json", map[string]any{
		"id": "synthetic", "name": "Synthetic", "family": "marketplace", "version": "1.0.0", "sdk_version": 1,
		"capabilities": []string{"products.read"},
		"auth":         []map[string]any{{"kind": "oauth2", "secret_class": "marketplace.oauth", "required": true}},
		"rate_limit":   map[string]any{"max_concurrency": 1, "min_interval_ms": 0, "request_timeout_ms": 1000, "retry": map[string]any{"max_attempts": 3, "base_backoff_ms": 100, "max_backoff_ms": 1000}},
	})
	writeTestFile(t, root, "docs/connectors/synthetic/spec.md", "Synthetic Connector Spec with bounded deterministic content.\n")
	writeTestFile(t, root, "docs/connectors/synthetic/capability-audit.md", "Synthetic capability audit with bounded deterministic content.\n")
	writeTestFile(t, root, "docs/connectors/synthetic/conformance-plan.md", "Synthetic conformance plan with bounded deterministic content.\n")
	writeTestConformanceReport(t, root, "synthetic", "1.0.0")
	record := validTestReview("081", "connectors/synthetic/connector.go")
	record.ChangeClass = "new_provider"
	record.GapAudit.Decision = "route_to_connector_sdk"
	record.Provider = &providerReview{
		ID: "synthetic", Route: "connector_sdk", Manifest: "connectors/synthetic/manifest.json",
		ConnectorSpec: "docs/connectors/synthetic/spec.md", CapabilityAudit: "docs/connectors/synthetic/capability-audit.md",
		ConformancePlan: "docs/connectors/synthetic/conformance-plan.md",
	}
	writeTestJSON(t, root, reviewsDir+"/081-synthetic-provider.json", record)
	return root
}

func writeRetiredProviderFixture(t *testing.T, root, id, task string) {
	t.Helper()
	configuration := loadTestPolicy(t, root)
	configuration.RetiredProviders = []retiredProvider{{
		ID: id, Implementation: "plugins/" + id, Manifest: "plugins/" + id + "/manifest.json",
		ConnectorSpec: "docs/connectors/" + id + "/spec.md", CapabilityAudit: "docs/connectors/" + id + "/capability-audit.md",
		ConformancePlan: "docs/connectors/" + id + "/conformance-plan.md", RetirementTask: task,
	}}
	writeTestJSON(t, root, policyPath, configuration)
	writeTestFile(t, root, "tasks/issues/"+task+"-synthetic.md", "# Task "+task+"\n")
	writeTestFile(t, root, "plugins/"+id+"/manifest.json", "{}\n")
	writeTestFile(t, root, "docs/connectors/"+id+"/spec.md", "Synthetic Connector Spec with bounded deterministic content.\n")
	writeTestFile(t, root, "docs/connectors/"+id+"/capability-audit.md", "Synthetic capability audit with bounded deterministic content.\n")
	writeTestFile(t, root, "docs/connectors/"+id+"/conformance-plan.md", "Synthetic conformance plan with bounded deterministic content.\n")
	record := validTestReview(task, "plugins/"+id+"/manifest.json")
	record.ChangeClass = "new_provider"
	record.GapAudit.Decision = "route_to_connector_sdk"
	record.Provider = &providerReview{
		ID: id, Route: "connector_sdk", Manifest: "plugins/" + id + "/manifest.json",
		ConnectorSpec: "docs/connectors/" + id + "/spec.md", CapabilityAudit: "docs/connectors/" + id + "/capability-audit.md",
		ConformancePlan: "docs/connectors/" + id + "/conformance-plan.md",
	}
	writeTestJSON(t, root, reviewsDir+"/"+task+"-"+id+"-provider.json", record)
}

func writeTestConformanceReport(t *testing.T, root, connectorID, connectorVersion string) {
	t.Helper()
	checks := make([]conformance.CheckResult, 0, len(conformance.RequiredChecks()))
	for _, id := range conformance.RequiredChecks() {
		checks = append(checks, conformance.CheckResult{ID: id, Status: conformance.StatusPass})
	}
	report := conformance.Report{SuiteVersion: conformance.SuiteVersion, ConnectorID: connectorID, ConnectorVersion: connectorVersion, SDKVersion: 1, Passed: true, Checks: checks, CompletedAt: time.Date(2026, 8, 9, 14, 0, 0, 0, time.UTC)}
	copyReport := report
	copyReport.ReportSHA256 = ""
	data, err := json.Marshal(copyReport)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	report.ReportSHA256 = hex.EncodeToString(digest[:])
	writeTestJSON(t, root, "docs/connectors/"+connectorID+"/conformance-report.json", report)
}

func validTestReview(task, scope string) review {
	impacts := make([]impact, 0, len(canonicalImpactAreas))
	for _, area := range canonicalImpactAreas {
		impacts = append(impacts, impact{
			Area: area, Status: "not_affected",
			Evidence: "Synthetic rationale for deterministic architecture policy validation.",
		})
	}
	return review{
		SchemaVersion: 1,
		ID:            "ARCH-" + task,
		Task:          task,
		ChangeClass:   "implementation",
		Summary:       "Synthetic implementation inside the existing frozen architecture decision.",
		Scopes:        []string{scope},
		FrozenPillars: []string{},
		ADR:           nil,
		ExistingADRs:  []string{"adr/0001-synthetic.md"},
		GapAudit: gapAudit{
			Performed: true,
			Prompt:    "prompts/05-architecture-gap-audit.txt",
			Decision:  "within_frozen_architecture",
		},
		Impacts:       impacts,
		Provider:      nil,
		FollowUpTasks: []string{},
	}
}

func writeTestJSON(t *testing.T, root, relative string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, relative, append(data, '\n'))
}

func writeTestFile(t *testing.T, root, relative string, data any) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var bytes []byte
	switch typed := data.(type) {
	case string:
		bytes = []byte(typed)
	case []byte:
		bytes = typed
	default:
		t.Fatalf("unsupported fixture data %T", data)
	}
	// #nosec G703 -- root is a test-owned TempDir and relative is fixed synthetic fixture data.
	if err := os.WriteFile(path, bytes, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, root, relative string) string {
	t.Helper()
	// #nosec G304 -- root is a test-owned TempDir and relative is fixed synthetic fixture data.
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func replaceTestFile(t *testing.T, root, relative, old, replacement string) {
	t.Helper()
	data := readTestFile(t, root, relative)
	updated := strings.Replace(data, old, replacement, 1)
	if updated == data {
		t.Fatalf("fixture token %q not found", old)
	}
	writeTestFile(t, root, relative, updated)
}

func loadTestReview(t *testing.T, root, relative string) review {
	t.Helper()
	var record review
	if err := json.Unmarshal([]byte(readTestFile(t, root, relative)), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func loadTestPolicy(t *testing.T, root string) policy {
	t.Helper()
	var configuration policy
	if err := json.Unmarshal([]byte(readTestFile(t, root, policyPath)), &configuration); err != nil {
		t.Fatal(err)
	}
	return configuration
}

func runGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	// #nosec G204 -- this test helper receives only synthetic fixture arguments from the test source.
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func TestAllowedGoSourcePathIncludesGeneratedSDKRootsOnly(t *testing.T) {
	for _, path := range []string{"sdk/go/torgnexa/client.gen.go", "sdk/examples/go/main.go", "tools/sdkgen/main.go"} {
		if !allowedGoSourcePath(path) {
			t.Fatalf("expected %s to be allowed", path)
		}
	}
	if allowedGoSourcePath("sdk/private/internal.go") {
		t.Fatal("unexpected arbitrary sdk Go root admission")
	}
}

func TestCompletedTaskStatusRecognizesRepositoryImplementationCompleted(t *testing.T) {
	cases := []string{
		"# Task\n\n## Status\n\nRepository implementation: **Completed** on 2026-08-09.\n\n## Acceptance\n",
		"# Task\n\n## Status\n\nRepository implementation status: Completed.\n\n## Acceptance\n",
	}
	for _, input := range cases {
		if !completedTaskStatus([]byte(input)) {
			t.Fatalf("expected repository completion status to be recognized: %q", input)
		}
	}
	if completedTaskStatus([]byte("# Task\n\n## Status\n\nRepository implementation: Completed but blocked pending qualification.\n")) {
		t.Fatal("blocked completion must remain fail-closed")
	}
}

func TestBuiltInCompositionMayImportOnlyRegisteredProviders(t *testing.T) {
	t.Parallel()
	root := writeAdmittedProviderFixture(t)
	writeTestFile(t, root, canonicalProviderCompositionModule+"/runtime.go", "package builtinruntime\nimport _ \"github.com/torgnexa/torgnexa/connectors/synthetic\"\n")
	if _, err := CheckRepository(context.Background(), root); err != nil {
		t.Fatalf("registered provider composition rejected: %v", err)
	}

	writeTestFile(t, root, canonicalProviderCompositionModule+"/runtime.go", "package builtinruntime\nimport _ \"github.com/torgnexa/torgnexa/connectors/ghost\"\n")
	if _, err := CheckRepository(context.Background(), root); err == nil || !strings.Contains(err.Error(), "provider composition imports an unregistered provider implementation") {
		t.Fatalf("unregistered provider composition error = %v", err)
	}
}
