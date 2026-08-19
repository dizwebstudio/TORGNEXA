package connectors

import (
	"errors"
	"testing"
	"time"
)

func socialManifest(caps ...Capability) Manifest {
	return Manifest{ID: "social-fixture", Name: "Social Fixture", Family: FamilySocial, Version: "1.0.0", SDKVersion: SDKMajor, Capabilities: caps, Auth: []AuthRequirement{{Kind: AuthBearer, SecretClass: "social-token", Required: true}}, RateLimit: RateLimitPolicy{MaxConcurrency: 2, MinIntervalMS: 100, RequestTimeoutMS: 10000, Retry: RetryPolicy{MaxAttempts: 3, BaseBackoffMS: 100, MaxBackoffMS: 1000}}}
}

func TestValidateSocialPublishRequiresExactCapability(t *testing.T) {
	request := SocialPublishRequest{PublicationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204", Kind: SocialPostVideo, Media: []SocialMediaRef{{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: SocialMediaVideo}}}
	manifest := socialManifest("social.post.text")
	if err := ValidateSocialPublish(manifest, request); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("error=%v", err)
	}
	manifest = socialManifest("social.post.video")
	if err := ValidateSocialPublish(manifest, request); err != nil {
		t.Fatal(err)
	}
}

func TestSocialPublishRequestRejectsURLsAndMixedVideo(t *testing.T) {
	bad := []SocialPublishRequest{
		{PublicationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204", Kind: SocialPostMedia, Media: []SocialMediaRef{{UploadID: "https://example.test/image.jpg", Kind: SocialMediaImage}}},
		{PublicationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204", Kind: SocialPostVideo, Media: []SocialMediaRef{{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: SocialMediaImage}}},
	}
	for _, request := range bad {
		if request.Validate() == nil {
			t.Fatalf("invalid request accepted: %#v", request)
		}
	}
}

func TestSocialPublishResultUsesSafeNormalizedReason(t *testing.T) {
	result := SocialPublishResult{RemotePublicationID: "remote-1", Status: SocialRemoteFailed, ReasonCode: "remote_rate_limited", ObservedAt: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	result.ReasonCode = "HTTP 429: Authorization Bearer secret"
	if result.Validate() == nil {
		t.Fatal("raw provider failure accepted")
	}
}

func TestSocialButtonsRequireHTTPSAndCapability(t *testing.T) {
	request := SocialPublishRequest{
		PublicationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204",
		Kind:          SocialPostText,
		Text:          "open",
		Buttons:       []SocialButton{{Text: "Open", URL: "https://example.test/path"}},
	}
	if err := request.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSocialPublish(socialManifest("social.post.text"), request); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("button capability error=%v", err)
	}
	if err := ValidateSocialPublish(socialManifest("social.post.text", "social.post.buttons"), request); err != nil {
		t.Fatal(err)
	}
	request.Buttons[0].URL = "http://example.test"
	if request.Validate() == nil {
		t.Fatal("non-HTTPS button accepted")
	}
}

func TestSocialEditAndDeleteAreAdditiveCapabilitySurfaces(t *testing.T) {
	edit := SocialEditRequest{RemotePublicationID: "tg:-1001234567890:101", Kind: SocialPostText, Text: "edited"}
	if err := edit.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSocialEdit(socialManifest("social.post.text"), edit); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("edit capability error=%v", err)
	}
	if err := ValidateSocialEdit(socialManifest("social.post.text", "social.post.edit"), edit); err != nil {
		t.Fatal(err)
	}
	result := SocialDeleteResult{RemotePublicationID: "tg:-1001234567890:101", Deleted: true, ObservedAt: time.Date(2026, 8, 11, 10, 30, 0, 0, time.UTC)}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
}
