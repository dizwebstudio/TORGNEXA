package pluginsecurity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testTrustStore struct {
	publisher string
	keyID     string
	key       ed25519.PublicKey
}

func (store testTrustStore) Ed25519PublicKey(_ context.Context, publisher, keyID string) (ed25519.PublicKey, error) {
	if publisher != store.publisher || keyID != store.keyID {
		return nil, ErrTrustKey
	}
	return append(ed25519.PublicKey(nil), store.key...), nil
}

func testDescriptor(t *testing.T, artifact []byte) (Descriptor, PermissionGrant, testTrustStore) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(artifact)
	fingerprint := sha256.Sum256(publicKey)
	descriptor := Descriptor{
		BoundaryVersion: BoundaryVersion,
		ExecutionMode:   ExecutionIsolatedProcessV1,
		Trust:           TrustVerified,
		Manifest: sdk.Manifest{
			ID:         "synthetic-shop",
			Name:       "Synthetic Shop",
			Family:     sdk.FamilyMarketplace,
			Version:    "1.2.3",
			SDKVersion: sdk.SDKMajor,
			Capabilities: []sdk.Capability{
				"products.read",
				"prices.write",
			},
			Auth: []sdk.AuthRequirement{{Kind: sdk.AuthBearer, SecretClass: "marketplace.oauth", Required: true}},
			RateLimit: sdk.RateLimitPolicy{
				MaxConcurrency:   4,
				MinIntervalMS:    100,
				RequestTimeoutMS: 10000,
				Retry:            sdk.RetryPolicy{MaxAttempts: 4, BaseBackoffMS: 200, MaxBackoffMS: 5000},
			},
		},
		Artifact: ArtifactIdentity{
			SHA256:          hex.EncodeToString(digest[:]),
			SizeBytes:       int64(len(artifact)),
			PublisherID:     "synthetic-publisher",
			KeyID:           "release-key-1",
			KeyFingerprint:  hex.EncodeToString(fingerprint[:]),
			SignatureBase64: base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
		},
		Requested: PermissionRequest{
			Capabilities:  []sdk.Capability{"products.read", "prices.write"},
			SecretClasses: []string{"marketplace.oauth"},
			Network:       []NetworkDestination{{Host: "api.synthetic.example", Port: 443}},
		},
		Limits: IsolationLimits{MemoryMiB: 128, CPUTimeMS: 5000, WallTimeMS: 10000, MaxOutputBytes: 1 << 20, MaxConcurrentCalls: 4},
	}
	descriptor.Artifact.SignatureBase64 = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, SignatureMessage(descriptor)))
	grant := PermissionGrant{
		ExtensionID:      descriptor.Manifest.ID,
		ExtensionVersion: descriptor.Manifest.Version,
		ArtifactSHA256:   descriptor.Artifact.SHA256,
		Capabilities:     []sdk.Capability{"prices.write", "products.read"},
		SecretClasses:    []string{"marketplace.oauth"},
		Network:          []NetworkDestination{{Host: "api.synthetic.example", Port: 443}},
		GrantedAt:        time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC),
	}
	// Grants are canonical/sorted even if the request is not.
	grant.Capabilities = []sdk.Capability{"prices.write", "products.read"}
	return descriptor, grant, testTrustStore{publisher: descriptor.Artifact.PublisherID, keyID: descriptor.Artifact.KeyID, key: publicKey}
}

func TestPrepareProducesInertLeastPrivilegePlan(t *testing.T) {
	artifact := []byte("synthetic signed plugin artifact")
	descriptor, grant, trust := testDescriptor(t, artifact)
	plan, err := Prepare(context.Background(), descriptor, grant, bytes.NewReader(artifact), trust)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if plan.ExecutionMode != ExecutionIsolatedProcessV1 || plan.ExtensionID != descriptor.Manifest.ID || plan.ArtifactSHA256 != descriptor.Artifact.SHA256 {
		t.Fatalf("unexpected plan: %#v", plan)
	}
	if len(plan.Granted.Capabilities) != 2 || plan.Granted.SecretClasses[0] != "marketplace.oauth" {
		t.Fatalf("grant lost from plan: %#v", plan.Granted)
	}
}

func TestPrepareRejectsArtifactTamper(t *testing.T) {
	artifact := []byte("synthetic signed plugin artifact")
	descriptor, grant, trust := testDescriptor(t, artifact)
	_, err := Prepare(context.Background(), descriptor, grant, bytes.NewReader([]byte("tampered artifact")), trust)
	if !errors.Is(err, ErrArtifactDigest) {
		t.Fatalf("want digest failure, got %v", err)
	}
}

func TestPrepareRejectsSignedMetadataTamper(t *testing.T) {
	artifact := []byte("synthetic signed plugin artifact")
	descriptor, grant, trust := testDescriptor(t, artifact)
	descriptor.Manifest.Name = "Changed After Signing"
	_, err := Prepare(context.Background(), descriptor, grant, bytes.NewReader(artifact), trust)
	if !errors.Is(err, ErrArtifactSignature) {
		t.Fatalf("want signature failure, got %v", err)
	}
}

func TestPermissionRequestRejectsEscalation(t *testing.T) {
	descriptor, _, _ := testDescriptor(t, []byte("artifact"))
	descriptor.Requested.Capabilities = []sdk.Capability{"products.read", "orders.read"}
	if !errors.Is(descriptor.Requested.Validate(descriptor.Manifest), ErrPermissionEscalate) {
		t.Fatal("capability outside manifest must be rejected")
	}

	descriptor, _, _ = testDescriptor(t, []byte("artifact"))
	descriptor.Requested.SecretClasses = append(descriptor.Requested.SecretClasses, "unrequested.secret")
	if !errors.Is(descriptor.Requested.Validate(descriptor.Manifest), ErrPermissionEscalate) {
		t.Fatal("secret class outside manifest auth must be rejected")
	}
}

func TestGrantCannotExceedSignedRequest(t *testing.T) {
	artifact := []byte("artifact")
	descriptor, grant, trust := testDescriptor(t, artifact)
	grant.Network = []NetworkDestination{{Host: "extra.synthetic.example", Port: 443}}
	_, err := Prepare(context.Background(), descriptor, grant, bytes.NewReader(artifact), trust)
	if !errors.Is(err, ErrPermissionEscalate) {
		t.Fatalf("want permission escalation, got %v", err)
	}
}

func TestNetworkDestinationIsExactPublicDNSOnly(t *testing.T) {
	invalid := []NetworkDestination{
		{Host: "localhost", Port: 443},
		{Host: "127.0.0.1", Port: 443},
		{Host: "10.0.0.1", Port: 443},
		{Host: "*.example.com", Port: 443},
		{Host: "api", Port: 443},
		{Host: "API.example.com", Port: 443},
		{Host: "api.example.com", Port: 0},
	}
	for _, value := range invalid {
		if err := value.Validate(); err == nil {
			t.Fatalf("expected invalid destination: %#v", value)
		}
	}
	if err := (NetworkDestination{Host: "api.example.com", Port: 443}).Validate(); err != nil {
		t.Fatalf("valid destination rejected: %v", err)
	}
}

func TestDescriptorRequiresManifestSecretClass(t *testing.T) {
	descriptor, _, _ := testDescriptor(t, []byte("artifact"))
	descriptor.Requested.SecretClasses = nil
	if err := descriptor.Validate(); !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("required manifest secret must be requested, got %v", err)
	}
}

func TestPermissionGrantRequiresCanonicalOrdering(t *testing.T) {
	_, grant, _ := testDescriptor(t, []byte("artifact"))
	grant.Capabilities = []sdk.Capability{"products.read", "prices.write"}
	if err := grant.Validate(); err == nil {
		t.Fatal("non-canonical capability ordering must be rejected")
	}
}
