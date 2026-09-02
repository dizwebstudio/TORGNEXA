package builtinruntime

import (
	"context"
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ErrRemoteOperationNotQualified is returned before a provider call when the
// reviewed connector evidence does not admit the requested operation.
var ErrRemoteOperationNotQualified = errors.New("marketplace taxonomy: remote operation is not qualified")

var listingTaxonomyProfiles = map[string]marketplacelisting.ProviderTaxonomyProfile{
	"wildberries": {
		ChannelID:          "wildberries",
		SchemaVersion:      "wb-content-taxonomy-v1",
		CategoryCodeKind:   "numeric",
		AttributeKeyKind:   "provider_name",
		BulkApplyMode:      "bounded_single_item_calls",
		ReadAfterWriteMode: "catalog_read",
		Qualification:      "qualification_required",
	},
	"ozon": {
		ChannelID:          "ozon",
		SchemaVersion:      "ozon-description-category-v1",
		CategoryCodeKind:   "numeric",
		AttributeKeyKind:   "provider_id",
		BulkApplyMode:      "bounded_single_item_calls",
		ReadAfterWriteMode: "async_status",
		Qualification:      "qualification_required",
	},
	"yandex-market": {
		ChannelID:          "yandex-market",
		SchemaVersion:      "ym-market-category-v1",
		CategoryCodeKind:   "numeric",
		AttributeKeyKind:   "provider_id",
		BulkApplyMode:      "bounded_single_item_calls",
		ReadAfterWriteMode: "catalog_read_or_async_status",
		Qualification:      "qualification_required",
	},
}

// MarketplaceListingTaxonomyReader is the host-facing normalized reader. It
// is deliberately different from the SDK DTO reader so provider packages do
// not import Core models across the connector boundary.
type MarketplaceListingTaxonomyReader interface {
	ReadMarketplaceListingTaxonomy(context.Context, sdk.Account, sdk.Runtime, sdk.MarketplaceListingTaxonomyRequest) (marketplacelisting.Taxonomy, error)
}

type normalizedListingTaxonomyReader struct {
	reader sdk.MarketplaceListingTaxonomyReader
}

func (reader normalizedListingTaxonomyReader) ReadMarketplaceListingTaxonomy(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.MarketplaceListingTaxonomyRequest) (marketplacelisting.Taxonomy, error) {
	if reader.reader == nil {
		return marketplacelisting.Taxonomy{}, ErrRemoteOperationNotQualified
	}
	providerTaxonomy, err := reader.reader.ReadMarketplaceListingTaxonomy(ctx, account, runtime, request)
	if err != nil {
		return marketplacelisting.Taxonomy{}, err
	}
	taxonomy := convertListingTaxonomy(providerTaxonomy)
	if taxonomy.Validate() != nil {
		return marketplacelisting.Taxonomy{}, sdk.ErrInvalidMarketplaceListing
	}
	fingerprint, fingerprintErr := taxonomy.ComputeFingerprint()
	if fingerprintErr != nil {
		return marketplacelisting.Taxonomy{}, sdk.ErrInvalidMarketplaceListing
	}
	taxonomy.Fingerprint = fingerprint
	return taxonomy, nil
}

func convertListingTaxonomy(providerTaxonomy sdk.MarketplaceListingTaxonomy) marketplacelisting.Taxonomy {
	taxonomy := marketplacelisting.Taxonomy{
		ID:           providerTaxonomy.ID,
		ChannelID:    providerTaxonomy.ChannelID,
		Locale:       providerTaxonomy.Locale,
		Jurisdiction: providerTaxonomy.Jurisdiction,
		Version:      providerTaxonomy.Version,
		Source:       providerTaxonomy.Source,
		Fingerprint:  providerTaxonomy.Fingerprint,
		ObservedAt:   providerTaxonomy.ObservedAt,
		FreshUntil:   providerTaxonomy.FreshUntil,
		Categories:   make([]marketplacelisting.Category, 0, len(providerTaxonomy.Categories)),
		Attributes:   make([]marketplacelisting.AttributeDefinition, 0, len(providerTaxonomy.Attributes)),
		MediaSlots:   make([]marketplacelisting.MediaSlot, 0, len(providerTaxonomy.MediaSlots)),
	}
	for _, category := range providerTaxonomy.Categories {
		taxonomy.Categories = append(taxonomy.Categories, marketplacelisting.Category{Code: category.Code, Name: category.Name, ParentCode: category.ParentCode, AttributeCodes: append([]string(nil), category.AttributeCodes...)})
	}
	for _, attribute := range providerTaxonomy.Attributes {
		definition := marketplacelisting.AttributeDefinition{Code: attribute.Code, Name: attribute.Name, ValueType: marketplacelisting.ValueType(attribute.ValueType), Requirement: marketplacelisting.Requirement(attribute.Requirement), Unit: attribute.Unit, Min: attribute.Min, Max: attribute.Max, LocalizedName: cloneStringMap(attribute.LocalizedName)}
		for _, value := range attribute.EnumValues {
			definition.EnumValues = append(definition.EnumValues, marketplacelisting.EnumValue{Code: value.Code, Label: value.Label, Deprecated: value.Deprecated})
		}
		for _, condition := range attribute.Conditions {
			definition.Conditions = append(definition.Conditions, marketplacelisting.Condition{WhenField: condition.WhenField, Equals: condition.Equals})
		}
		taxonomy.Attributes = append(taxonomy.Attributes, definition)
	}
	for _, slot := range providerTaxonomy.MediaSlots {
		taxonomy.MediaSlots = append(taxonomy.MediaSlots, marketplacelisting.MediaSlot{Code: slot.Code, Name: slot.Name, Required: slot.Required, MaxItems: slot.MaxItems, Formats: append([]string(nil), slot.Formats...), MinWidth: slot.MinWidth, MinHeight: slot.MinHeight})
	}
	return taxonomy
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

// ProfileFor returns the immutable adapter profile for a known marketplace.
func ProfileFor(connectorID string) (marketplacelisting.ProviderTaxonomyProfile, bool) {
	profile, ok := listingTaxonomyProfiles[connectorID]
	return profile, ok
}

// AttachListingTaxonomyProfile adds the reviewed provider contract to a
// normalized taxonomy. The profile is metadata only and does not change the
// taxonomy content fingerprint.
func AttachListingTaxonomyProfile(taxonomy marketplacelisting.Taxonomy) marketplacelisting.Taxonomy {
	profile, ok := ProfileFor(taxonomy.ChannelID)
	if !ok {
		return taxonomy
	}
	if readiness, err := sdk.ReadinessProfileFor(taxonomy.ChannelID); err == nil && readiness.Status == sdk.ReadinessQualified && sdk.AllowsRemoteOperation(readiness, "products.write", true) {
		profile.Qualification = "qualified"
	}
	taxonomy.AdapterProfile = &profile
	return taxonomy
}

// RemoteOperationAdmission checks the reviewed connector profile and current
// readiness evidence before a listing operation can create a remote side
// effect. Unknown providers and unqualified writes fail closed.
func RemoteOperationAdmission(connectorID string, operation marketplacepublication.OperationKind) error {
	if !operation.Valid() {
		return ErrRemoteOperationNotQualified
	}
	if _, ok := ProfileFor(connectorID); !ok {
		return ErrRemoteOperationNotQualified
	}
	readiness, err := sdk.ReadinessProfileFor(connectorID)
	if err != nil {
		return ErrRemoteOperationNotQualified
	}
	write := operation != marketplacepublication.OperationStatusRead
	capability := "products.read"
	if write {
		capability = "products.write"
	}
	if !sdk.AllowsRemoteOperation(readiness, capability, write) {
		return ErrRemoteOperationNotQualified
	}
	return nil
}
