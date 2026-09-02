package builtinruntime

import (
	"errors"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/marketplacelisting"
	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
)

func TestListingTaxonomyProfilesAreTypedAndAttached(t *testing.T) {
	for _, connectorID := range []string{"wildberries", "ozon", "yandex-market"} {
		profile, ok := ProfileFor(connectorID)
		if !ok || profile.Validate() != nil {
			t.Fatalf("profile %q is not valid", connectorID)
		}
		taxonomy := AttachListingTaxonomyProfile(marketplacelisting.Taxonomy{ChannelID: connectorID, Fingerprint: "fingerprint"})
		if taxonomy.AdapterProfile == nil || taxonomy.AdapterProfile.ChannelID != connectorID || taxonomy.Fingerprint != "fingerprint" {
			t.Fatalf("profile %q was not attached safely: %#v", connectorID, taxonomy.AdapterProfile)
		}
	}
}

func TestUnknownListingTaxonomyConnectorHasNoInventedProfile(t *testing.T) {
	taxonomy := marketplacelisting.Taxonomy{ChannelID: "custom-connector", Fingerprint: "fingerprint"}
	result := AttachListingTaxonomyProfile(taxonomy)
	if result.AdapterProfile != nil || result.Fingerprint != taxonomy.Fingerprint {
		t.Fatalf("unknown connector received an invented profile: %#v", result.AdapterProfile)
	}
}

func TestListingRemoteWriteFailsClosedUntilQualification(t *testing.T) {
	if err := RemoteOperationAdmission("wildberries", marketplacepublication.OperationCreateProduct); !errors.Is(err, ErrRemoteOperationNotQualified) {
		t.Fatalf("unqualified write error = %v, want qualification gate", err)
	}
	if err := RemoteOperationAdmission("wildberries", marketplacepublication.OperationStatusRead); err != nil {
		t.Fatalf("ready read error = %v", err)
	}
	if err := RemoteOperationAdmission("unknown", marketplacepublication.OperationCreateProduct); !errors.Is(err, ErrRemoteOperationNotQualified) {
		t.Fatalf("unknown connector error = %v, want qualification gate", err)
	}
}
