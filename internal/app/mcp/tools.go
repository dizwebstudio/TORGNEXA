package mcp

import (
	"context"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/legalparty"
	"github.com/torgnexa/torgnexa/internal/core/marketplacegrowth"
	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/platform/agentgovernance"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	connectorSDK "github.com/torgnexa/torgnexa/internal/platform/connectors"
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

// ListingPreviewInput is the provider-neutral, dry-run input accepted by the
// MCP listing tool. It cannot carry credentials or trigger a remote write.
type ListingPreviewInput struct {
	ChannelAccountID string                              `json:"connector_account_id"`
	ChannelID        string                              `json:"connector_id"`
	Taxonomy         marketplacelisting.Taxonomy         `json:"taxonomy"`
	Items            []marketplacelisting.BatchItem      `json:"items"`
	Operations       []marketplacelisting.BatchOperation `json:"operations,omitempty"`
}

// ListingPreviewer is deliberately read-only. Applying a batch is only
// available through the authenticated HTTP approval boundary.
type ListingPreviewer interface {
	PreviewListing(context.Context, Identity, ListingPreviewInput) (marketplacelisting.BatchPreview, error)
}

// GrowthPreviewer exposes the promotion/advertising dry-run to MCP. It never
// applies a remote operation; application remains behind HTTP approval.
type GrowthPreviewer interface {
	PreviewGrowth(context.Context, Identity, marketplacegrowth.PreviewRequest) (marketplacegrowth.Preview, error)
}

// ConnectorReadinessReader exposes the redacted connector catalog to an
// authorized agent. It cannot access tenant credentials or invoke a provider.
type ConnectorReadinessReader interface {
	Readiness(context.Context, Identity) (connectorSDK.ReadinessMatrix, error)
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

func listingPreviewTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionProductsRead, risk: audit.RiskRead, agentRisk: agentgovernance.RiskRead, outputKind: "source_facts", tool: Tool{
		Name: "commerce.marketplace.listing.preview", Title: "Preview marketplace listing batch",
		Description: "Validate marketplace taxonomy, content, media and a mass-edit batch up to 1000 SKU. This tool is a dry-run only; it cannot publish or apply changes.",
		InputSchema: objectSchema(map[string]any{
			"connector_account_id": stringProp("Marketplace account reference", 1, 192),
			"connector_id":         stringProp("Connector reference", 1, 192),
			"taxonomy":             map[string]any{"type": "object"},
			"items":                map[string]any{"type": "array", "minItems": 1, "maxItems": marketplacelisting.MaxBatchItems, "items": map[string]any{"type": "object"}},
			"operations":           map[string]any{"type": "array", "maxItems": 512, "items": map[string]any{"type": "object"}},
		}, []string{"connector_account_id", "connector_id", "taxonomy", "items"}),
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false, "torgnexaRiskClass": "read", "torgnexaApplyBoundary": "http_approval_only"},
	}}
}

func growthPreviewTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionGrowthPreview, risk: audit.RiskRead, agentRisk: agentgovernance.RiskRead, outputKind: "source_facts", tool: Tool{
		Name: "commerce.marketplace.growth.preview", Title: "Preview promotions and advertising changes",
		Description: "Calculate promotion eligibility, effective price, floor, margin and bounded bid or budget changes for up to 1000 SKU. Dry-run only; no remote write.",
		InputSchema: objectSchema(map[string]any{
			"operation":                   map[string]any{"type": "string", "enum": []string{"promotion.apply", "campaign.create", "campaign.launch", "campaign.pause", "campaign.resume", "campaign.stop", "campaign.archive", "campaign.link_products", "bid.update", "budget.update", "kill_switch.enable"}},
			"channel_id":                  stringProp("Provider-neutral channel reference", 1, 192),
			"account_id":                  stringProp("Marketplace account reference", 1, 192),
			"target_id":                   stringProp("Promotion or campaign reference", 1, 192),
			"currency":                    map[string]any{"type": "string", "pattern": "^[A-Z]{3}$"},
			"floor_price_minor":           map[string]any{"type": "integer", "minimum": 1},
			"minimum_margin_basis_points": map[string]any{"type": "integer", "minimum": 0, "maximum": 10000},
			"approval_threshold":          map[string]any{"type": "integer", "minimum": 0, "maximum": marketplacegrowth.MaxPreviewRows},
			"proposed_bid_minor":          map[string]any{"type": "integer", "minimum": 0},
			"maximum_bid_minor":           map[string]any{"type": "integer", "minimum": 0},
			"proposed_budget_minor":       map[string]any{"type": "integer", "minimum": 0},
			"maximum_budget_minor":        map[string]any{"type": "integer", "minimum": 0},
			"bid_unit":                    map[string]any{"type": "string", "enum": []string{"cpc", "cpm", "cpa"}},
			"strategy":                    stringProp("Versioned strategy name", 0, 128),
			"items":                       map[string]any{"type": "array", "minItems": 1, "maxItems": marketplacegrowth.MaxPreviewRows, "items": map[string]any{"type": "object"}},
		}, []string{"operation", "channel_id", "account_id", "target_id", "currency", "floor_price_minor", "items"}),
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false, "torgnexaRiskClass": "read", "torgnexaApplyBoundary": "http_approval_only"},
	}}
}

func connectorReadinessTool(available bool) toolDescriptor {
	return toolDescriptor{available: available, permission: permissionConnectorReadinessRead, risk: audit.RiskRead, agentRisk: agentgovernance.RiskRead, outputKind: "source_facts", tool: Tool{
		Name: "commerce.connectors.readiness.list", Title: "List connector readiness",
		Description: "Read the reviewed, non-secret readiness matrix for all TORGNEXA connectors. The catalog distinguishes health-only, read-only, partial, ready and qualified runtime depth; it never performs remote calls.",
		InputSchema: objectSchema(map[string]any{}, nil),
		Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false, "torgnexaRiskClass": "read"},
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
