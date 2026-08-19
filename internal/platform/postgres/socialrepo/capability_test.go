package socialrepo

import (
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/social"
	"github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func TestSocialCoreCapabilityVocabularyMatchesConnectorSDKV1(t *testing.T) {
	values := []social.Capability{
		social.CapabilityAnalyticsRead,
		social.CapabilityCommentsRead,
		social.CapabilityCommentsReply,
		social.CapabilityPostDelete,
		social.CapabilityPostEdit,
		social.CapabilityPostMedia,
		social.CapabilityPostText,
		social.CapabilityPostVideo,
	}
	canonical, err := social.CanonicalCapabilities(values)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range canonical {
		definition, ok := connectors.CapabilityDefinitionFor(connectors.Capability(value))
		if !ok {
			t.Fatalf("connector SDK missing %q", value)
		}
		supported := definition.SupportsFamily(connectors.FamilySocial)
		if !supported {
			t.Fatalf("%q is not admitted for social family", value)
		}
	}
}
