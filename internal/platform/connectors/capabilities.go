package connectors

import "sort"

// Capability is a stable provider-neutral operation identifier.
type Capability string

type CapabilityDefinition struct {
	Name             Capability
	Families         []Family
	Direction        CapabilityDirection
	Risk             CapabilityRisk
	ApprovalRequired bool
}

// CapabilityDirection describes whether TORGNEXA reads remote state or writes
// a remote side effect. It is host-owned metadata and cannot be overridden by
// an account or connector implementation.
type CapabilityDirection string

const (
	CapabilityRead  CapabilityDirection = "read"
	CapabilityWrite CapabilityDirection = "write"
)

// CapabilityRisk is the minimum policy classification for an enabled account
// capability. Every remote write is deliberately write-sensitive in SDK v1.
type CapabilityRisk string

const (
	CapabilityRiskRead           CapabilityRisk = "read"
	CapabilityRiskWriteSensitive CapabilityRisk = "write_sensitive"
)

func (definition CapabilityDefinition) SupportsFamily(family Family) bool {
	for _, allowed := range definition.Families {
		if allowed == family {
			return true
		}
	}
	return false
}

func families(values ...Family) []Family { return values }

func readable(name Capability, allowed []Family) CapabilityDefinition {
	return CapabilityDefinition{Name: name, Families: allowed, Direction: CapabilityRead, Risk: CapabilityRiskRead}
}

func writeSensitive(name Capability, allowed []Family) CapabilityDefinition {
	return CapabilityDefinition{Name: name, Families: allowed, Direction: CapabilityWrite, Risk: CapabilityRiskWriteSensitive, ApprovalRequired: true}
}

var commerceFamilies = families(FamilyMarketplace, FamilyStorefront, FamilyClassified)

var capabilityDefinitions = map[Capability]CapabilityDefinition{
	"classified.listings.read":            readable("classified.listings.read", families(FamilyClassified)),
	"classified.publications.status.read": readable("classified.publications.status.read", families(FamilyClassified)),
	"classified.publications.write":       writeSensitive("classified.publications.write", families(FamilyClassified)),
	"classified.leads.read":               readable("classified.leads.read", families(FamilyClassified)),
	"classified.messages.read":            readable("classified.messages.read", families(FamilyClassified)),
	"classified.messages.reply":           writeSensitive("classified.messages.reply", families(FamilyClassified)),
	"classified.stats.read":               readable("classified.stats.read", families(FamilyClassified)),
	"products.read":                       readable("products.read", commerceFamilies),
	"products.write":                      writeSensitive("products.write", commerceFamilies),
	"prices.read":                         readable("prices.read", commerceFamilies),
	"prices.write":                        writeSensitive("prices.write", commerceFamilies),
	"inventory.read":                      readable("inventory.read", commerceFamilies),
	"inventory.write":                     writeSensitive("inventory.write", commerceFamilies),
	"orders.read":                         readable("orders.read", commerceFamilies),
	"notifications.receive":               readable("notifications.receive", commerceFamilies),
	"orders.status.write":                 writeSensitive("orders.status.write", commerceFamilies),
	"returns.read":                        readable("returns.read", commerceFamilies),
	"reviews.read":                        readable("reviews.read", commerceFamilies),
	"reviews.reply":                       writeSensitive("reviews.reply", commerceFamilies),
	"messages.read":                       readable("messages.read", commerceFamilies),
	"messages.reply":                      writeSensitive("messages.reply", commerceFamilies),
	"ads.read":                            readable("ads.read", commerceFamilies),
	"ads.manage":                          writeSensitive("ads.manage", commerceFamilies),
	"promotions.read":                     readable("promotions.read", commerceFamilies),
	"promotions.manage":                   writeSensitive("promotions.manage", commerceFamilies),
	"finance.settlements.read":            readable("finance.settlements.read", commerceFamilies),

	"social.post.text":      writeSensitive("social.post.text", families(FamilySocial)),
	"social.post.media":     writeSensitive("social.post.media", families(FamilySocial)),
	"social.post.video":     writeSensitive("social.post.video", families(FamilySocial)),
	"social.post.buttons":   writeSensitive("social.post.buttons", families(FamilySocial)),
	"social.post.edit":      writeSensitive("social.post.edit", families(FamilySocial)),
	"social.post.delete":    writeSensitive("social.post.delete", families(FamilySocial)),
	"social.comments.read":  readable("social.comments.read", families(FamilySocial)),
	"social.comments.reply": writeSensitive("social.comments.reply", families(FamilySocial)),
	"social.analytics.read": readable("social.analytics.read", families(FamilySocial)),
	"social.webhooks":       readable("social.webhooks", families(FamilySocial)),

	"erp.catalog.read":   readable("erp.catalog.read", families(FamilyERP)),
	"erp.catalog.write":  writeSensitive("erp.catalog.write", families(FamilyERP)),
	"erp.inventory.read": readable("erp.inventory.read", families(FamilyERP)),
	"erp.orders.read":    readable("erp.orders.read", families(FamilyERP)),
	"erp.orders.write":   writeSensitive("erp.orders.write", families(FamilyERP)),

	"edo.documents.read":         readable("edo.documents.read", families(FamilyEDO)),
	"edo.documents.send":         writeSensitive("edo.documents.send", families(FamilyEDO)),
	"edo.documents.sign_request": writeSensitive("edo.documents.sign_request", families(FamilyEDO)),

	"marking.status.read":           readable("marking.status.read", families(FamilyGovernment)),
	"marking.documents.write":       writeSensitive("marking.documents.write", families(FamilyGovernment)),
	"fiscal.receipts.write":         writeSensitive("fiscal.receipts.write", families(FamilyGovernment)),
	"vetis.documents.read":          readable("vetis.documents.read", families(FamilyGovernment)),
	"vetis.documents.write":         writeSensitive("vetis.documents.write", families(FamilyGovernment)),
	"government.identity.read":      readable("government.identity.read", families(FamilyGovernment)),
	"government.documents.read":     readable("government.documents.read", families(FamilyGovernment)),
	"government.documents.write":    writeSensitive("government.documents.write", families(FamilyGovernment)),
	"government.inventory.read":     readable("government.inventory.read", families(FamilyGovernment)),
	"government.references.read":    readable("government.references.read", families(FamilyGovernment)),
	"government.reconciliation.run": writeSensitive("government.reconciliation.run", families(FamilyGovernment)),
	"compliance.documents.read":     readable("compliance.documents.read", families(FamilyGovernment, FamilyMarketplace, FamilyERP)),
	"compliance.documents.write":    writeSensitive("compliance.documents.write", families(FamilyGovernment, FamilyMarketplace, FamilyERP)),
	"compliance.evaluate":           readable("compliance.evaluate", families(FamilyGovernment, FamilyMarketplace, FamilyERP)),

	"payments.create":      writeSensitive("payments.create", families(FamilyPayment)),
	"payments.refund":      writeSensitive("payments.refund", families(FamilyPayment)),
	"payments.reconcile":   readable("payments.reconcile", families(FamilyPayment)),
	"payments.status.read": readable("payments.status.read", families(FamilyPayment)),
	"payments.webhooks":    readable("payments.webhooks", families(FamilyPayment)),

	"logistics.rates.read":      readable("logistics.rates.read", families(FamilyLogistics)),
	"logistics.shipment.create": writeSensitive("logistics.shipment.create", families(FamilyLogistics)),
	"logistics.shipment.cancel": writeSensitive("logistics.shipment.cancel", families(FamilyLogistics)),
	"logistics.track.read":      readable("logistics.track.read", families(FamilyLogistics)),
	"logistics.webhooks.verify": readable("logistics.webhooks.verify", families(FamilyLogistics)),
	"logistics.return.create":   writeSensitive("logistics.return.create", families(FamilyLogistics)),
	"logistics.label.read":      readable("logistics.label.read", families(FamilyLogistics)),
	"pickup.points.read":        readable("pickup.points.read", families(FamilyPickup, FamilyLogistics)),
	"pickup.capacity.read":      readable("pickup.capacity.read", families(FamilyPickup)),
	"pickup.orders.write":       writeSensitive("pickup.orders.write", families(FamilyPickup)),

	"fx.rates.read":                 readable("fx.rates.read", families(FamilyFX)),
	"notifications.sms.send":        writeSensitive("notifications.sms.send", families(FamilyNotification)),
	"notifications.sms.status.read": readable("notifications.sms.status.read", families(FamilyNotification)),

	"crm.entities.read":     readable("crm.entities.read", families(FamilyCRM)),
	"crm.entities.write":    writeSensitive("crm.entities.write", families(FamilyCRM)),
	"crm.productrows.read":  readable("crm.productrows.read", families(FamilyCRM)),
	"crm.productrows.write": writeSensitive("crm.productrows.write", families(FamilyCRM)),

	"ai.completion.generate": writeSensitive("ai.completion.generate", families(FamilyAI)),
}

func CapabilityDefinitionFor(capability Capability) (CapabilityDefinition, bool) {
	definition, ok := capabilityDefinitions[capability]
	if !ok {
		return CapabilityDefinition{}, false
	}
	definition.Families = append([]Family(nil), definition.Families...)
	return definition, true
}

func KnownCapabilities() []Capability {
	values := make([]Capability, 0, len(capabilityDefinitions))
	for capability := range capabilityDefinitions {
		values = append(values, capability)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}

// RequiredSyncCapabilities maps a provider-neutral entity and connector family
// to the read/write operations required by the sync engine. It never branches
// on a provider identity and returns false for unsupported domain surfaces.
func RequiredSyncCapabilities(family Family, entityType string) (Capability, Capability, bool) {
	switch family {
	case FamilyMarketplace, FamilyStorefront:
		switch entityType {
		case "products", "offers":
			return "products.read", "products.write", true
		case "inventory":
			return "inventory.read", "inventory.write", true
		case "orders":
			return "orders.read", "orders.status.write", true
		}
	case FamilyClassified:
		if entityType == "products" || entityType == "offers" {
			return "classified.listings.read", "classified.publications.write", true
		}
	case FamilyERP:
		switch entityType {
		case "products", "offers":
			return "erp.catalog.read", "erp.catalog.write", true
		case "orders":
			return "erp.orders.read", "erp.orders.write", true
		}
	case FamilyCRM:
		if entityType == "products" || entityType == "offers" {
			return "crm.entities.read", "crm.entities.write", true
		}
	}
	return "", "", false
}
