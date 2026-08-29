// Package architecture validates the frozen architecture boundary and its
// repository review evidence. It is build-time tooling and has no runtime
// authority over business requests.
package architecture

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	policyPath = "architecture/policy.json"
	reviewsDir = "architecture/reviews"

	maxPolicyBytes = 512 << 10
	maxReviewBytes = 256 << 10
	maxReviews     = 1000
	maxJSONDepth   = 64
	maxJSONNodes   = 100000
	maxGoFiles     = 10000
	maxGoFileBytes = 4 << 20
	maxTreeEntries = 100000
)

var canonicalPillars = []string{
	"universal-commerce-content-fulfillment-models",
	"pim-mdm-financial-stock-ledgers",
	"connector-plugin-runtime-capabilities",
	"event-platform-outbox-inbox",
	"bidirectional-sync-reconciliation",
	"workflow-approval-audit-lineage",
	"privacy-secrets-security-governance",
	"reporting-settlements-growth-supply-planning",
	"developer-surfaces",
	"russia-compliance-generic-ports",
	"legal-party-product-compliance-fx",
	"enterprise-iam-siem-cloud-upload-edge",
}

var canonicalImpactAreas = []string{
	"tenancy",
	"privacy_data_governance",
	"api_compatibility",
	"event_compatibility",
	"plugin_sdk_compatibility",
	"database_migration",
	"security",
	"approvals_risk",
	"audit_lineage",
	"reconciliation_idempotency",
	"webhooks_egress",
	"slo_observability",
	"operations_rollback",
	"testing_conformance",
}

var canonicalProviderRoots = []string{"connectors", "plugins"}

const canonicalProviderCompositionModule = "internal/platform/builtinruntime"

var canonicalSensitivePaths = []string{
	"architecture/policy.json",
	"cmd/",
	"docs/01-architecture.md",
	"docs/03-module-boundaries.md",
	"docs/54-architecture-freeze-v1.md",
	"internal/core/",
	"internal/platform/",
}

type policy struct {
	SchemaVersion               int               `json:"schema_version"`
	ModulePath                  string            `json:"module_path"`
	FrozenPillars               []string          `json:"frozen_pillars"`
	ImpactAreas                 []string          `json:"impact_areas"`
	ProviderImplementationRoots []string          `json:"provider_implementation_roots"`
	ProviderAdmission           admission         `json:"provider_admission"`
	ProviderCompositionModule   string            `json:"provider_composition_module"`
	CoreExternalImports         []string          `json:"core_external_imports"`
	CoreSharedImports           []string          `json:"core_shared_imports"`
	ProviderAllowedImports      []string          `json:"provider_allowed_internal_imports"`
	SensitivePaths              []string          `json:"sensitive_paths"`
	Modules                     []module          `json:"modules"`
	Providers                   []provider        `json:"providers"`
	RetiredProviders            []retiredProvider `json:"retired_providers"`
}

type admission struct {
	Enabled       bool     `json:"enabled"`
	RequiredTasks []string `json:"required_tasks"`
}

type module struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type provider struct {
	ID                     string   `json:"id"`
	Family                 string   `json:"family"`
	Implementation         string   `json:"implementation"`
	Manifest               string   `json:"manifest"`
	ConnectorSpec          string   `json:"connector_spec"`
	CapabilityAudit        string   `json:"capability_audit"`
	ConformancePlan        string   `json:"conformance_plan"`
	AllowedExternalImports []string `json:"allowed_external_imports"`
}

type retiredProvider struct {
	ID              string `json:"id"`
	Implementation  string `json:"implementation"`
	Manifest        string `json:"manifest"`
	ConnectorSpec   string `json:"connector_spec"`
	CapabilityAudit string `json:"capability_audit"`
	ConformancePlan string `json:"conformance_plan"`
	RetirementTask  string `json:"retirement_task"`
}

type review struct {
	SchemaVersion int             `json:"schema_version"`
	ID            string          `json:"id"`
	Task          string          `json:"task"`
	Stage         string          `json:"stage,omitempty"`
	ChangeClass   string          `json:"change_class"`
	Summary       string          `json:"summary"`
	Scopes        []string        `json:"scopes"`
	FrozenPillars []string        `json:"frozen_pillars"`
	ADR           *string         `json:"adr"`
	ExistingADRs  []string        `json:"existing_adrs"`
	GapAudit      gapAudit        `json:"gap_audit"`
	Impacts       []impact        `json:"impacts"`
	Provider      *providerReview `json:"provider"`
	FollowUpTasks []string        `json:"follow_up_tasks"`
}

type gapAudit struct {
	Performed bool   `json:"performed"`
	Prompt    string `json:"prompt"`
	Decision  string `json:"decision"`
}

type impact struct {
	Area     string `json:"area"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

type providerReview struct {
	ID              string `json:"id"`
	Route           string `json:"route"`
	Manifest        string `json:"manifest"`
	ConnectorSpec   string `json:"connector_spec"`
	CapabilityAudit string `json:"capability_audit"`
	ConformancePlan string `json:"conformance_plan"`
}

// Report summarizes a successful repository architecture validation.
type Report struct {
	Modules   int
	Providers int
	Reviews   int
	Changes   int
}

type problems struct {
	items   problemMaxHeap
	seen    map[string]struct{}
	omitted bool
}

func (p *problems) add(path, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if path != "" {
		message = path + ": " + message
	}
	message = sanitizeProblem(message)
	if p.seen == nil {
		p.seen = make(map[string]struct{}, maxDiagnostics)
	}
	if _, duplicate := p.seen[message]; duplicate {
		return
	}
	if len(p.items) < maxDiagnostics {
		p.seen[message] = struct{}{}
		heap.Push(&p.items, message)
		return
	}
	p.omitted = true
	if message >= p.items[0] {
		return
	}
	largest := heap.Pop(&p.items).(string)
	delete(p.seen, largest)
	p.seen[message] = struct{}{}
	heap.Push(&p.items, message)
}

const (
	maxDiagnostics      = 1000
	maxDiagnosticLength = 1024
)

func sanitizeProblem(value string) string {
	var result strings.Builder
	result.Grow(min(len(value), maxDiagnosticLength))
	for _, r := range value {
		if result.Len() >= maxDiagnosticLength {
			result.WriteString("...")
			break
		}
		if unicode.IsControl(r) {
			r = ' '
		}
		size := utf8.RuneLen(r)
		if size < 0 {
			r = utf8.RuneError
			size = utf8.RuneLen(r)
		}
		if result.Len()+size > maxDiagnosticLength {
			result.WriteString("...")
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}

type problemMaxHeap []string

func (h problemMaxHeap) Len() int           { return len(h) }
func (h problemMaxHeap) Less(i, j int) bool { return h[i] > h[j] }
func (h problemMaxHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *problemMaxHeap) Push(value any) {
	*h = append(*h, value.(string))
}

func (h *problemMaxHeap) Pop() any {
	old := *h
	last := len(old) - 1
	value := old[last]
	old[last] = ""
	*h = old[:last]
	return value
}

func (p *problems) err() error {
	if len(p.items) == 0 {
		return nil
	}
	items := append([]string(nil), p.items...)
	sort.Strings(items)
	message := strings.Join(items, "\n")
	if p.omitted {
		message += "\n... additional deterministic diagnostics omitted"
	}
	return errors.New(message)
}
