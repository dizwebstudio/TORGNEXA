// Package marketplacetaxonomy contains the reviewed adapter profiles for
// marketplace listing taxonomy. It does not fetch provider data or hold
// credentials; connectors remain responsible for the remote call.
package marketplacetaxonomy

import (
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ErrRemoteOperationNotQualified is returned before a provider call when the
// reviewed connector evidence does not admit the requested operation.
var ErrRemoteOperationNotQualified = errors.New("marketplace taxonomy: remote operation is not qualified")

var profiles = map[string]marketplacelisting.ProviderTaxonomyProfile{
	"wildberries": {
		ConnectorID:        "wildberries",
		SchemaVersion:      "wb-content-taxonomy-v1",
		CategoryCodeKind:   "numeric",
		AttributeKeyKind:   "provider_name",
		BulkApplyMode:      "bounded_single_item_calls",
		ReadAfterWriteMode: "catalog_read",
		Qualification:      "qualification_required",
	},
	"ozon": {
		ConnectorID:        "ozon",
		SchemaVersion:      "ozon-description-category-v1",
		CategoryCodeKind:   "numeric",
		AttributeKeyKind:   "provider_id",
		BulkApplyMode:      "bounded_single_item_calls",
		ReadAfterWriteMode: "async_status",
		Qualification:      "qualification_required",
	},
	"yandex-market": {
		ConnectorID:        "yandex-market",
		SchemaVersion:      "ym-market-category-v1",
		CategoryCodeKind:   "numeric",
		AttributeKeyKind:   "provider_id",
		BulkApplyMode:      "bounded_single_item_calls",
		ReadAfterWriteMode: "catalog_read_or_async_status",
		Qualification:      "qualification_required",
	},
}

// ProfileFor returns the immutable adapter profile for a known marketplace.
func ProfileFor(connectorID string) (marketplacelisting.ProviderTaxonomyProfile, bool) {
	profile, ok := profiles[connectorID]
	return profile, ok
}

// AttachProfile adds the reviewed provider contract to a normalized taxonomy.
// The profile describes adapter semantics and deliberately does not change the
// taxonomy content fingerprint, preserving compatibility with stored evidence.
func AttachProfile(taxonomy marketplacelisting.Taxonomy) marketplacelisting.Taxonomy {
	profile, ok := ProfileFor(taxonomy.ChannelID)
	if !ok {
		return taxonomy
	}
	if readiness, err := sdk.ReadinessProfileFor(taxonomy.ChannelID); err == nil && readiness.Status == sdk.ReadinessQualified && sdk.AllowsRemoteOperation(readiness, "products.write", true) {
		profile.Qualification = "qualified"
	}
	taxonomy.ProviderProfile = &profile
	return taxonomy
}

// RemoteOperationAdmission checks the provider-specific profile together with
// the repository readiness evidence. It deliberately uses operation semantics
// rather than provider IDs, so a new connector cannot accidentally inherit a
// write path merely by being added to a manifest.
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
