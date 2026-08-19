package pluginmarketplace

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type trustStore struct {
	publisher string
	keyID     string
	key       ed25519.PublicKey
}

func (store trustStore) Ed25519PublicKey(_ context.Context, publisher, keyID string) (ed25519.PublicKey, error) {
	if publisher != store.publisher || keyID != store.keyID {
		return nil, pluginsecurity.ErrTrustKey
	}
	return append(ed25519.PublicKey(nil), store.key...), nil
}

func fixture(t *testing.T, version string, caps []sdk.Capability, artifact []byte) (Listing, Consent, trustStore) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(artifact)
	fingerprint := sha256.Sum256(publicKey)
	descriptor := pluginsecurity.Descriptor{
		BoundaryVersion: pluginsecurity.BoundaryVersion,
		ExecutionMode:   pluginsecurity.ExecutionIsolatedProcessV1,
		Trust:           pluginsecurity.TrustVerified,
		Manifest: sdk.Manifest{
			ID: "synthetic-shop", Name: "Synthetic Shop", Family: sdk.FamilyMarketplace, Version: version, SDKVersion: sdk.SDKMajor,
			Capabilities: append([]sdk.Capability(nil), caps...),
			Auth:         []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "marketplace.oauth", Required: true}},
			RateLimit:    sdk.RateLimitPolicy{MaxConcurrency: 4, MinIntervalMS: 100, RequestTimeoutMS: 10000, Retry: sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 200, MaxBackoffMS: 5000}},
		},
		Artifact:  pluginsecurity.ArtifactIdentity{SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(artifact)), PublisherID: "synthetic-publisher", KeyID: "release-key-1", KeyFingerprint: hex.EncodeToString(fingerprint[:]), SignatureBase64: base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))},
		Requested: pluginsecurity.PermissionRequest{Capabilities: append([]sdk.Capability(nil), caps...), SecretClasses: []string{"marketplace.oauth"}, Network: []pluginsecurity.NetworkDestination{{Host: "api.synthetic.example", Port: 443}}},
		Limits:    pluginsecurity.IsolationLimits{MemoryMiB: 128, CPUTimeMS: 5000, WallTimeMS: 10000, MaxOutputBytes: 1 << 20, MaxConcurrentCalls: 4},
	}
	descriptor.Artifact.SignatureBase64 = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, pluginsecurity.SignatureMessage(descriptor)))
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	listing := Listing{
		Descriptor: descriptor, LicenseExpression: "Apache-2.0", SecurityContact: "security@example.invalid",
		Review:      ReviewEvidence{ConformancePassed: true, SupplyChainPassed: true, MalwareScanPassed: true, LicenseReviewed: true, SecurityContactReviewed: true, SBOMVerified: true, ProvenanceVerified: true, ReviewerID: "security-reviewer", ReviewedAt: now},
		PublishedAt: now.Add(time.Minute),
	}
	scope, err := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002")
	if err != nil {
		t.Fatal(err)
	}
	grant := pluginsecurity.PermissionGrant{ExtensionID: descriptor.Manifest.ID, ExtensionVersion: version, ArtifactSHA256: descriptor.Artifact.SHA256, Capabilities: append([]sdk.Capability(nil), caps...), SecretClasses: []string{"marketplace.oauth"}, Network: []pluginsecurity.NetworkDestination{{Host: "api.synthetic.example", Port: 443}}, GrantedAt: now.Add(2 * time.Minute)}
	// grants must be canonical even when manifests/requests are authored in another order.
	if len(grant.Capabilities) == 2 && grant.Capabilities[0] > grant.Capabilities[1] {
		grant.Capabilities[0], grant.Capabilities[1] = grant.Capabilities[1], grant.Capabilities[0]
	}
	consent := Consent{ID: "consent-01", Scope: scope, Grant: grant, ActorID: "admin-user", GrantedAt: grant.GrantedAt}
	return listing, consent, trustStore{publisher: descriptor.Artifact.PublisherID, keyID: descriptor.Artifact.KeyID, key: publicKey}
}

func TestListingViewMakesTrustAndRequestedAuthorityVisible(t *testing.T) {
	listing, _, _ := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, []byte("plugin-v1"))
	view, err := listing.View()
	if err != nil {
		t.Fatal(err)
	}
	if view.Trust != pluginsecurity.TrustVerified || view.PublisherID != "synthetic-publisher" || len(view.Requested.Capabilities) != 1 || view.Requested.Capabilities[0] != "products.read" {
		t.Fatalf("unsafe/incomplete listing view: %#v", view)
	}
}

func TestVerifiedListingRequiresSupplyChainProvenanceEvidence(t *testing.T) {
	listing, _, _ := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, []byte("plugin-v1"))
	listing.Review.ProvenanceVerified = false
	if !errors.Is(listing.Validate(), ErrInvalid) {
		t.Fatalf("verified listing without provenance was accepted")
	}
}

func TestUpdateAlwaysRequiresFreshConsentForNewArtifactAndSurfacesEscalation(t *testing.T) {
	previous, consent, _ := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, []byte("plugin-v1"))
	next, _, _ := fixture(t, "1.1.0", []sdk.Capability{"products.read", "prices.write"}, []byte("plugin-v2"))
	assessment, err := AssessUpdate(previous, consent.Grant, next)
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.RequiresReapproval || len(assessment.AddedCapabilities) != 1 || assessment.AddedCapabilities[0] != "prices.write" {
		t.Fatalf("privilege escalation was not surfaced: %#v", assessment)
	}
	want := map[string]bool{"artifact_changed": true, "capability_escalation": true}
	for _, reason := range assessment.Reasons {
		delete(want, reason)
	}
	if len(want) != 0 {
		t.Fatalf("missing reasons: %#v", want)
	}
}

func TestUpdateWithSameAuthorityStillRequiresExactDigestConsent(t *testing.T) {
	previous, consent, _ := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, []byte("plugin-v1"))
	next, _, _ := fixture(t, "1.0.1", []sdk.Capability{"products.read"}, []byte("plugin-v1.0.1"))
	assessment, err := AssessUpdate(previous, consent.Grant, next)
	if err != nil {
		t.Fatal(err)
	}
	if !assessment.RequiresReapproval || len(assessment.AddedCapabilities) != 0 {
		t.Fatalf("artifact update incorrectly inherited consent: %#v", assessment)
	}
}

func TestAdmitComposesReviewConsentRevocationAndTask025Verification(t *testing.T) {
	artifact := []byte("plugin-v1")
	listing, consent, trust := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, artifact)
	plan, err := Admit(context.Background(), listing, consent, bytes.NewReader(artifact), trust, nil)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if plan.Trust != pluginsecurity.TrustVerified || plan.ArtifactSHA256 != listing.Descriptor.Artifact.SHA256 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if _, err := Admit(context.Background(), listing, consent, bytes.NewReader([]byte("tampered")), trust, nil); !errors.Is(err, pluginsecurity.ErrArtifactDigest) {
		t.Fatalf("tampered artifact result = %v", err)
	}
}

func TestArtifactPublisherAndInstallationRevocationsFailClosed(t *testing.T) {
	artifact := []byte("plugin-v1")
	listing, consent, trust := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, artifact)
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	cases := []Revocation{
		{ID: "rev-artifact", Kind: RevokeArtifact, ExtensionID: listing.Descriptor.Manifest.ID, ArtifactSHA256: listing.Descriptor.Artifact.SHA256, ActorID: "security", Reason: "malware finding", RevokedAt: now},
		{ID: "rev-key", Kind: RevokePublisherKey, PublisherID: listing.Descriptor.Artifact.PublisherID, PublisherKeyID: listing.Descriptor.Artifact.KeyID, ActorID: "security", Reason: "key compromised", RevokedAt: now},
		{ID: "rev-install", Kind: RevokeInstallation, OrganizationID: consent.Scope.OrganizationID().String(), WorkspaceID: consent.Scope.WorkspaceID().String(), ConsentID: consent.ID, ActorID: "tenant-admin", Reason: "integration retired", RevokedAt: now},
	}
	for _, revocation := range cases {
		if _, err := Admit(context.Background(), listing, consent, bytes.NewReader(artifact), trust, RevocationSet{revocation}); !errors.Is(err, ErrRevoked) {
			t.Fatalf("%s: want revoked, got %v", revocation.Kind, err)
		}
	}
}

func TestPrivateListingCannotCrossTenant(t *testing.T) {
	listing, consent, _ := fixture(t, "1.0.0", []sdk.Capability{"products.read"}, []byte("private"))
	listing.Descriptor.Trust = pluginsecurity.TrustPrivate
	listing.PrivateOrganizationID = consent.Scope.OrganizationID().String()
	listing.PrivateWorkspaceID = consent.Scope.WorkspaceID().String()
	// Trust is signed metadata, so the test only checks listing/consent tenancy invariants here.
	if listing.Validate() != nil || consent.Validate(listing) != nil {
		t.Fatal("private listing rejected for owner tenant")
	}
	other, _ := tenancy.ParseScope("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004")
	consent.Scope = other
	if !errors.Is(consent.Validate(listing), ErrInvalid) {
		t.Fatal("private listing crossed tenant boundary")
	}
}

func TestConsentCannotGrantAuthorityMissingFromSignedRequest(t *testing.T) {
	listing, consent, _ := fixture(t, "1.0.0", []sdk.Capability{"products.read", "prices.write"}, []byte("plugin-v1"))
	listing.Descriptor.Requested.Capabilities = []sdk.Capability{"products.read"}
	if !errors.Is(consent.Validate(listing), ErrInvalid) {
		t.Fatal("consent exceeded the signed permission request")
	}
	if _, err := AssessUpdate(listing, consent.Grant, listing); !errors.Is(err, ErrInvalid) {
		t.Fatalf("historical grant exceeding signed request accepted: %v", err)
	}
}

func TestRevocationRequiresCanonicalArtifactAndPublisherIdentifiers(t *testing.T) {
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	invalid := []Revocation{
		{ID: "rev-artifact", Kind: RevokeArtifact, ExtensionID: "Synthetic-Shop", ArtifactSHA256: strings.Repeat("a", 64), ActorID: "security", Reason: "invalid id", RevokedAt: now},
		{ID: "rev-digest", Kind: RevokeArtifact, ExtensionID: "synthetic-shop", ArtifactSHA256: strings.Repeat("A", 64), ActorID: "security", Reason: "noncanonical digest", RevokedAt: now},
		{ID: "rev-key", Kind: RevokePublisherKey, PublisherID: "synthetic-publisher", PublisherKeyID: "bad/key", ActorID: "security", Reason: "invalid key id", RevokedAt: now},
	}
	for _, revocation := range invalid {
		if !errors.Is(revocation.Validate(), ErrInvalid) {
			t.Fatalf("invalid revocation accepted: %#v", revocation)
		}
	}
}
