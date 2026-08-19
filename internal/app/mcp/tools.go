package mcp

import (
	"time"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
	"github.com/torgnexa/torgnexa/internal/platform/agentgovernance"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/search"
)

type Tool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title,omitempty"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations,omitempty"`
}

type toolDescriptor struct {
	tool             Tool
	permission       string
	risk             audit.Risk
	agentRisk        agentgovernance.Risk
	approvalBoundary bool
	outputKind       string
	available        bool
}

func (d toolDescriptor) authorization() Authorization {
	return Authorization{Permission: d.permission, Tool: d.tool.Name, Risk: d.risk}
}

func productTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionProductsRead, risk: audit.RiskRead, agentRisk: agentgovernance.RiskRead, outputKind: "source_facts", tool: Tool{
		Name: "commerce.products.search", Title: "Search products",
		Description: "Search canonical TORGNEXA products inside the authenticated organization/workspace. Tenant identifiers are never accepted as arguments.",
		InputSchema: objectSchema(map[string]any{
			"query":  stringProp("Search text", 0, search.MaxQueryRunes),
			"status": enumProp("draft", "active", "archived"),
			"limit":  integerProp(1, search.MaxPageSize),
			"cursor": stringProp("Opaque continuation cursor", 0, search.MaxCursorSize),
		}, nil),
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false, "torgnexaRiskClass": "read"},
	}}
}

func orderTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionOrdersRead, risk: audit.RiskRead, agentRisk: agentgovernance.RiskRead, outputKind: "source_facts", tool: Tool{
		Name: "commerce.orders.list", Title: "List orders",
		Description: "List canonical TORGNEXA orders within the authenticated organization/workspace.",
		InputSchema: objectSchema(map[string]any{
			"query":       stringProp("Search text", 0, search.MaxQueryRunes),
			"status":      enumProp("pending", "confirmed", "processing", "fulfilled", "cancelled"),
			"placed_from": map[string]any{"type": "string", "format": "date-time"},
			"placed_to":   map[string]any{"type": "string", "format": "date-time"},
			"limit":       integerProp(1, search.MaxPageSize),
			"cursor":      stringProp("Opaque continuation cursor", 0, search.MaxCursorSize),
		}, nil),
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false, "torgnexaRiskClass": "read"},
	}}
}

func counterpartyTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionCounterpartiesRead, risk: audit.RiskRead, agentRisk: agentgovernance.RiskRead, outputKind: "source_facts", tool: Tool{
		Name: "party.counterparties.search", Title: "Search counterparties",
		Description: "Search canonical legal-party/counterparty master data inside the authenticated organization/workspace.",
		InputSchema: objectSchema(map[string]any{
			"query":           stringProp("Name or code", 0, 256),
			"inn":             stringProp("Russian INN", 0, 12),
			"registration_id": stringProp("Registration identifier", 0, 64),
			"party_type":      enumProp("legal_entity", "individual_entrepreneur", "branch"),
			"limit":           integerProp(1, 100),
		}, nil),
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "openWorldHint": false, "torgnexaRiskClass": "read"},
	}}
}

func priceChangeTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionPriceChangeRequest, risk: audit.RiskWriteSensitive, agentRisk: agentgovernance.RiskSensitiveWrite, approvalBoundary: true, outputKind: "governance_workflow", tool: Tool{
		Name: "commerce.price.change.request", Title: "Request price change",
		Description: "Create a sensitive price-change approval request. This tool never directly changes a price and cannot bypass TORGNEXA approval policy.",
		InputSchema: objectSchema(map[string]any{
			"price_id":         stringProp("Canonical price UUIDv7/ULID", 1, 36),
			"expected_version": integerProp(1, 1<<31-1),
			"currency":         map[string]any{"type": "string", "pattern": "^[A-Z]{3}$"},
			"minor_units":      map[string]any{"type": "integer", "minimum": 0},
			"reason":           stringProp("Short non-secret reason", 0, 256),
			"idempotency_key":  stringProp("Caller-generated canonical UUIDv7/ULID stable retry id", 26, 36),
		}, []string{"price_id", "expected_version", "currency", "minor_units", "idempotency_key"}),
		Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false, "torgnexaRiskClass": "sensitive_write", "torgnexaApprovalRequired": true},
	}}
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}
func stringProp(description string, min, max int) map[string]any {
	p := map[string]any{"type": "string", "description": description, "maxLength": max}
	if min > 0 {
		p["minLength"] = min
	}
	return p
}
func integerProp(min, max int) map[string]any {
	return map[string]any{"type": "integer", "minimum": min, "maximum": max}
}
func enumProp(values ...string) map[string]any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return map[string]any{"type": "string", "enum": out}
}

type productSearchInput struct {
	Query  string `json:"query,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

func (i productSearchInput) limit() int {
	if i.Limit == 0 {
		return 50
	}
	return i.Limit
}
func (i productSearchInput) Validate() error {
	return search.ProductQuery{Text: i.Query, Status: i.Status, Limit: i.limit(), Cursor: i.Cursor}.Validate()
}

type orderListInput struct {
	Query      string     `json:"query,omitempty"`
	Status     string     `json:"status,omitempty"`
	PlacedFrom *time.Time `json:"placed_from,omitempty"`
	PlacedTo   *time.Time `json:"placed_to,omitempty"`
	Limit      int        `json:"limit,omitempty"`
	Cursor     string     `json:"cursor,omitempty"`
}

func (i orderListInput) limit() int {
	if i.Limit == 0 {
		return 50
	}
	return i.Limit
}
func (i orderListInput) Validate() error {
	return search.OrderQuery{Text: i.Query, Status: i.Status, PlacedFrom: i.PlacedFrom, PlacedTo: i.PlacedTo, Limit: i.limit(), Cursor: i.Cursor}.Validate()
}

type counterpartySearchInput struct {
	Query          string `json:"query,omitempty"`
	INN            string `json:"inn,omitempty"`
	RegistrationID string `json:"registration_id,omitempty"`
	PartyType      string `json:"party_type,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

func (i counterpartySearchInput) limit() int {
	if i.Limit == 0 {
		return 50
	}
	return i.Limit
}
func (i counterpartySearchInput) Validate() error {
	q := legalparty.SearchQuery{Text: i.Query, INN: i.INN, RegistrationID: i.RegistrationID, PartyType: legalparty.PartyType(i.PartyType), Limit: i.limit()}
	if err := q.Validate(); err != nil {
		return ErrInvalid
	}
	return nil
}
