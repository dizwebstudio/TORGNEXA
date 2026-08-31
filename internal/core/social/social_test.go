package social

import (
	"errors"
	"testing"
	"time"
)

const (
	tOrg         = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	tWS          = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	tContent     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0201"
	tVariant     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0202"
	tChannel     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0203"
	tPublication = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204"
)

func TestVariantShapeAndMediaReferences(t *testing.T) {
	image := MediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: MediaImage, AltText: "Product"}
	video := MediaRef{UploadID: "upl_fedcba9876543210fedcba9876543210", Kind: MediaVideo}
	cases := []struct {
		name    string
		command CreateVariant
		ok      bool
	}{
		{"text", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatText, Body: "hello"}, true},
		{"image", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatImage, Body: "caption", Media: []MediaRef{image}}, true},
		{"gallery", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatGallery, Media: []MediaRef{image, {UploadID: "upl_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Kind: MediaImage}}}, true},
		{"video", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatVideo, Media: []MediaRef{video}}, true},
		{"article", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatArticle, Title: "Title", Body: "Body"}, true},
		{"text-with-media", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatText, Body: "hello", Media: []MediaRef{image}}, false},
		{"video-with-image", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatVideo, Media: []MediaRef{image}}, false},
		{"raw-url-not-media-ref", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatImage, Media: []MediaRef{{UploadID: "https://example.test/a.png", Kind: MediaImage}}}, false},
		{"duplicate-upload-ref", CreateVariant{ID: VariantID(tVariant), ContentID: ContentID(tContent), Format: FormatGallery, Media: []MediaRef{image, image}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.command.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("invalid variant accepted")
			}
		})
	}
}

func TestCapabilityValidationIsFormatSpecific(t *testing.T) {
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	account := ChannelAccount{ID: ChannelAccountID(tChannel), OrganizationID: tOrg, WorkspaceID: tWS, ConnectorAccountID: "social-account-1", DisplayName: "Channel", Capabilities: []Capability{CapabilityPostText}, Status: ChannelActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	variant := ContentVariant{ID: VariantID(tVariant), OrganizationID: tOrg, WorkspaceID: tWS, ContentID: ContentID(tContent), Format: FormatText, Body: "hello", Version: 1, CreatedAt: now}
	if err := ValidatePublicationPlan(account, variant); err != nil {
		t.Fatal(err)
	}
	variant.Format = FormatVideo
	variant.Body = ""
	variant.Media = []MediaRef{{UploadID: "upl_fedcba9876543210fedcba9876543210", Kind: MediaVideo}}
	if err := ValidatePublicationPlan(account, variant); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("error=%v", err)
	}
	account.Status = ChannelDisabled
	if err := ValidatePublicationPlan(account, variant); !errors.Is(err, ErrChannelUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestHTTPSButtonsRequireTheChannelCapability(t *testing.T) {
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	variant := ContentVariant{ID: VariantID(tVariant), OrganizationID: tOrg, WorkspaceID: tWS, ContentID: ContentID(tContent), Format: FormatText, Body: "hello", Buttons: []Button{{Text: "Open", URL: "https://example.test/product"}}, Version: 1, CreatedAt: now}
	account := ChannelAccount{ID: ChannelAccountID(tChannel), OrganizationID: tOrg, WorkspaceID: tWS, ConnectorAccountID: "social-account-1", DisplayName: "Channel", Capabilities: []Capability{CapabilityPostText}, Status: ChannelActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := ValidatePublicationPlan(account, variant); !errors.Is(err, ErrCapabilityMissing) {
		t.Fatalf("button publication without capability error=%v", err)
	}
	account.Capabilities = []Capability{CapabilityPostButtons, CapabilityPostText}
	if err := ValidatePublicationPlan(account, variant); err != nil {
		t.Fatalf("button publication rejected: %v", err)
	}
	bad := variant
	bad.Buttons = []Button{{Text: "Open", URL: "http://example.test"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("non-HTTPS button accepted")
	}
}

func TestCapabilitiesMustBeCanonicalAndUnique(t *testing.T) {
	if _, err := CanonicalCapabilities([]Capability{CapabilityPostText, CapabilityPostMedia}); err != nil {
		t.Fatal(err)
	}
	if got, err := CanonicalCapabilities([]Capability{CapabilityPostText, CapabilityPostMedia}); err != nil || len(got) != 2 || got[0] != CapabilityPostMedia || got[1] != CapabilityPostText {
		t.Fatalf("canonical capabilities = %#v, %v", got, err)
	}
	if _, err := CanonicalCapabilities([]Capability{CapabilityPostText, CapabilityPostText}); err == nil {
		t.Fatal("duplicate capability accepted")
	}
	account := CreateChannelAccount{ID: ChannelAccountID(tChannel), ConnectorAccountID: "social-account-1", DisplayName: "Channel", Capabilities: []Capability{CapabilityPostText, CapabilityPostMedia}}
	if account.Validate() == nil {
		t.Fatal("non-canonical capability order accepted")
	}
}

func TestScheduleAndPublicationTransitions(t *testing.T) {
	at := time.Date(2026, 8, 12, 9, 30, 0, 0, time.UTC)
	schedule, err := AtSchedule(at)
	if err != nil {
		t.Fatal(err)
	}
	status, err := InitialPublicationStatus(schedule)
	if err != nil || status != PublicationScheduled {
		t.Fatalf("status=%s err=%v", status, err)
	}
	for _, step := range [][2]PublicationStatus{{PublicationScheduled, PublicationReady}, {PublicationReady, PublicationPublishing}, {PublicationPublishing, PublicationFailed}, {PublicationFailed, PublicationReady}, {PublicationReady, PublicationPublishing}, {PublicationPublishing, PublicationPublished}} {
		if err := ValidatePublicationTransition(step[0], step[1]); err != nil {
			t.Fatalf("%s -> %s: %v", step[0], step[1], err)
		}
	}
	if err := ValidatePublicationTransition(PublicationPublished, PublicationReady); err == nil {
		t.Fatal("published publication became mutable")
	}
}

func TestStatusEventRequiresBoundedReasonOnlyForFailure(t *testing.T) {
	e := StatusEvent{EventID: "evt.social.1", PublicationID: PublicationID(tPublication), PublicationVersion: 2, Status: PublicationFailed, Attempt: 1, ReasonCode: "remote_rate_limited", CorrelationID: "request-1", OccurredAt: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.ReasonCode = "HTTP 429 raw body"
	if e.Validate() == nil {
		t.Fatal("raw provider error accepted as reason")
	}
}

func TestMutationRequiresAuditOutboxIdentity(t *testing.T) {
	m := Mutation{EventID: "evt.social.1", AuditID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0999", ActorID: "user-1", Source: "api", CorrelationID: "request-1", OccurredAt: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)}
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	m.AuditID = ""
	if m.Validate() == nil {
		t.Fatal("missing audit id accepted")
	}
}
