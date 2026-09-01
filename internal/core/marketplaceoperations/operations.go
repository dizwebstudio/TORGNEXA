// Package marketplaceoperations contains the provider-neutral readiness model
// for the end-to-end marketplace workflow. It does not call connectors and it
// does not persist a second copy of products, orders, inventory or finance.
package marketplaceoperations

import "sort"

// SupportState is the honest state of one marketplace operation. Qualified is
// intentionally evidence-backed; a declared or enabled capability alone is
// never enough to reach it.
type SupportState string

const (
	StateNotAvailable       SupportState = "not_available"
	StateReadOnly           SupportState = "read_only"
	StatePartiallySupported SupportState = "partially_supported"
	StateQualified          SupportState = "qualified"
	StateBlocked            SupportState = "blocked"
)

// Capability is the redacted capability observation supplied by the host.
// The core only consumes normalized status, never credentials or provider
// payloads.
type Capability struct {
	Name    string
	Status  string
	Enabled bool
}

// AccountInput is the minimum account state needed to produce an operations
// matrix. Values are normalized strings at the application boundary so this
// package remains independent of PostgreSQL, HTTP and connector registries.
type AccountInput struct {
	AccountID       string
	ConnectorID     string
	DisplayName     string
	AccountStatus   string
	HealthStatus    string
	CredentialState string
	RuntimeStatus   string
	Partial         bool
	Capabilities    []Capability
	Qualified       map[string]bool
}

// Operation is one row in the operator-facing marketplace capability matrix.
type Operation struct {
	Key                   string       `json:"key"`
	Label                 string       `json:"label"`
	State                 SupportState `json:"state"`
	RequiredCapabilities  []string     `json:"required_capabilities"`
	AvailableCapabilities []string     `json:"available_capabilities"`
	Permission            string       `json:"permission"`
	Risk                  string       `json:"risk"`
	ApprovalRequired      bool         `json:"approval_required"`
	ReasonCode            string       `json:"reason_code"`
}

// AccountMatrix is the read-only projection rendered by the operations
// center. It is deliberately an operation matrix, not another domain model.
type AccountMatrix struct {
	AccountID     string       `json:"account_id"`
	ConnectorID   string       `json:"connector_id"`
	DisplayName   string       `json:"display_name"`
	AccountStatus string       `json:"account_status"`
	HealthStatus  string       `json:"health_status"`
	OverallState  SupportState `json:"overall_state"`
	Partial       bool         `json:"partial"`
	Operations    []Operation  `json:"operations"`
}

type operationDefinition struct {
	key        string
	label      string
	read       []string
	write      []string
	required   []string
	permission string
	risk       string
	approval   bool
}

var definitions = []operationDefinition{
	{key: "catalog", label: "Каталог и публикация", read: []string{"products.read"}, write: []string{"products.write"}, required: []string{"products.read", "products.write"}, permission: "products.read", risk: "write_sensitive", approval: true},
	{key: "pricing", label: "Цены и скидки", read: []string{"prices.read"}, write: []string{"prices.write"}, required: []string{"prices.read", "prices.write"}, permission: "prices.read", risk: "write_sensitive", approval: true},
	{key: "inventory", label: "Остатки и резервы", read: []string{"inventory.read"}, write: []string{"inventory.write"}, required: []string{"inventory.read", "inventory.write"}, permission: "inventory.read", risk: "write_sensitive", approval: true},
	{key: "orders", label: "Заказы marketplace", read: []string{"orders.read"}, write: []string{"orders.status.write"}, required: []string{"orders.read", "orders.status.write"}, permission: "orders.read", risk: "write_sensitive", approval: true},
	{key: "fulfillment", label: "FBS / DBS и отгрузка", read: []string{"orders.read"}, write: []string{"orders.status.write", "inventory.write"}, required: []string{"orders.read", "orders.status.write", "inventory.write"}, permission: "orders.read", risk: "write_sensitive", approval: true},
	{key: "returns", label: "Возвраты и отмены", read: []string{"returns.read"}, write: []string{"orders.status.write"}, required: []string{"returns.read", "orders.status.write"}, permission: "orders.returns.read", risk: "write_sensitive", approval: true},
	{key: "marking_upd", label: "Маркировка и УПД", required: []string{"marking.codes.request", "marking.aggregation.write", "edo.documents.send"}, permission: "compliance.read", risk: "legally_significant", approval: true},
	{key: "advertising", label: "Реклама и продвижение", read: []string{"ads.read"}, write: []string{"ads.manage"}, required: []string{"ads.read", "ads.manage"}, permission: "ads.read", risk: "write_sensitive", approval: true},
	{key: "settlement_pnl", label: "Settlement и P&L", read: []string{"finance.settlements.read"}, required: []string{"finance.settlements.read"}, permission: "settlements.read", risk: "read", approval: false},
}

// Definitions returns a copy of the stable operation vocabulary.
func Definitions() []string {
	result := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, definition.key)
	}
	return result
}

// Evaluate builds a deterministic, sorted operations matrix from redacted
// host observations. The optional Qualified map is the only way to emit
// qualified; it must come from a separately reviewed qualification store.
func Evaluate(input AccountInput) AccountMatrix {
	available := make(map[string]bool, len(input.Capabilities))
	for _, capability := range input.Capabilities {
		if capability.Enabled || capability.Status == "enabled" {
			available[capability.Name] = true
		}
	}
	blocked := input.AccountStatus == "disabled" || input.AccountStatus == "suspended" || input.AccountStatus == "error" || input.HealthStatus == "unavailable" || input.HealthStatus == "degraded" || input.CredentialState == "invalid" || input.CredentialState == "reauthorization_required"
	operations := make([]Operation, 0, len(definitions))
	for _, definition := range definitions {
		operation := Operation{Key: definition.key, Label: definition.label, RequiredCapabilities: append([]string(nil), definition.required...), Permission: definition.permission, Risk: definition.risk, ApprovalRequired: definition.approval}
		for _, capability := range definition.required {
			if available[capability] {
				operation.AvailableCapabilities = append(operation.AvailableCapabilities, capability)
			}
		}
		operation.State, operation.ReasonCode = stateFor(definition, available, blocked, input.Partial, input.Qualified != nil && input.Qualified[definition.key])
		operations = append(operations, operation)
	}
	return AccountMatrix{AccountID: input.AccountID, ConnectorID: input.ConnectorID, DisplayName: input.DisplayName, AccountStatus: input.AccountStatus, HealthStatus: input.HealthStatus, OverallState: overallState(operations, blocked), Partial: input.Partial, Operations: operations}
}

func stateFor(definition operationDefinition, available map[string]bool, blocked, partial, qualified bool) (SupportState, string) {
	if blocked {
		return StateBlocked, "account_or_credentials_blocked"
	}
	availableCount := 0
	availableReadCount := 0
	availableWriteCount := 0
	readCapabilities := make(map[string]struct{}, len(definition.read))
	for _, capability := range definition.read {
		readCapabilities[capability] = struct{}{}
	}
	for _, capability := range definition.required {
		if available[capability] {
			availableCount++
			if _, isRead := readCapabilities[capability]; isRead {
				availableReadCount++
			} else {
				availableWriteCount++
			}
		}
	}
	if definition.key == "marking_upd" && availableCount == 0 {
		return StateNotAvailable, "requires_separate_government_and_edo_accounts"
	}
	if availableCount == 0 {
		return StateNotAvailable, "capability_not_enabled"
	}
	if qualified && availableCount == len(definition.required) && !partial {
		return StateQualified, "qualification_evidence_current"
	}
	if availableCount < len(definition.required) {
		if len(definition.write) == 0 || (availableReadCount == len(definition.read) && availableWriteCount < len(definition.write)) {
			return StateReadOnly, "write_capability_not_enabled"
		}
		return StatePartiallySupported, "capability_set_incomplete"
	}
	return StatePartiallySupported, "qualification_evidence_required"
}

func overallState(operations []Operation, blocked bool) SupportState {
	if blocked {
		return StateBlocked
	}
	if len(operations) > 0 {
		allQualified := true
		for _, operation := range operations {
			if operation.State != StateQualified {
				allQualified = false
				break
			}
		}
		if allQualified {
			return StateQualified
		}
	}
	for _, operation := range operations {
		if operation.State == StatePartiallySupported || operation.State == StateQualified {
			return StatePartiallySupported
		}
	}
	for _, operation := range operations {
		if operation.State == StateReadOnly {
			return StateReadOnly
		}
	}
	return StateNotAvailable
}

// SortOperations keeps the response stable if the definition table is
// extended by a future release.
func SortOperations(operations []Operation) {
	sort.Slice(operations, func(i, j int) bool { return operations[i].Key < operations[j].Key })
}
