// Package pluginmarketplace implements governance for distributing and installing
// isolated third-party TORGNEXA Connector SDK plugins. It composes the Task-025
// signed artifact boundary, Task-064 conformance evidence and Task-065 supply-chain
// evidence without adding another execution path.
package pluginmarketplace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

var (
	marketplaceSafeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	marketplaceDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	marketplaceKeyIDPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
)

var (
	ErrInvalid         = errors.New("plugin marketplace: invalid governance record")
	ErrUnreviewed      = errors.New("plugin marketplace: review gates incomplete")
	ErrConsentRequired = errors.New("plugin marketplace: explicit consent required")
	ErrRevoked         = errors.New("plugin marketplace: plugin or installation revoked")
)

type ReviewEvidence struct {
	ConformancePassed       bool      `json:"conformance_passed"`
	SupplyChainPassed       bool      `json:"supply_chain_passed"`
	MalwareScanPassed       bool      `json:"malware_scan_passed"`
	LicenseReviewed         bool      `json:"license_reviewed"`
	SecurityContactReviewed bool      `json:"security_contact_reviewed"`
	SBOMVerified            bool      `json:"sbom_verified"`
	ProvenanceVerified      bool      `json:"provenance_verified"`
	ReviewerID              string    `json:"reviewer_id"`
	ReviewedAt              time.Time `json:"reviewed_at"`
}

func (review ReviewEvidence) Validate(trust pluginsecurity.TrustLevel) error {
	if !trust.Valid() || !review.ConformancePassed || !review.SupplyChainPassed || !review.MalwareScanPassed ||
		!review.LicenseReviewed || !review.SecurityContactReviewed || strings.TrimSpace(review.ReviewerID) == "" ||
		review.ReviewerID != strings.TrimSpace(review.ReviewerID) || review.ReviewedAt.IsZero() || review.ReviewedAt.Location() != time.UTC {
		return ErrUnreviewed
	}
	// Official and verified marketplace artifacts must have the same subject-bound
	// SBOM/provenance evidence required by Task 065. Community/private artifacts
	// still pass supply-chain/malware review but may not have trusted builder identity.
	if (trust == pluginsecurity.TrustOfficial || trust == pluginsecurity.TrustVerified) && (!review.SBOMVerified || !review.ProvenanceVerified) {
		return ErrUnreviewed
	}
	return nil
}

type Listing struct {
	Descriptor            pluginsecurity.Descriptor `json:"security_descriptor"`
	LicenseExpression     string                    `json:"license_expression"`
	SecurityContact       string                    `json:"security_contact"`
	Review                ReviewEvidence            `json:"review"`
	PrivateOrganizationID string                    `json:"private_organization_id,omitempty"`
	PrivateWorkspaceID    string                    `json:"private_workspace_id,omitempty"`
	PublishedAt           time.Time                 `json:"published_at"`
}

func (listing Listing) Validate() error {
	if listing.Descriptor.Validate() != nil || !validMetadataText(listing.LicenseExpression, 1, 256) ||
		!validMetadataText(listing.SecurityContact, 3, 320) || listing.PublishedAt.IsZero() || listing.PublishedAt.Location() != time.UTC ||
		listing.Review.Validate(listing.Descriptor.Trust) != nil || listing.PublishedAt.Before(listing.Review.ReviewedAt) {
		return ErrInvalid
	}
	if listing.Descriptor.Trust == pluginsecurity.TrustPrivate {
		scope, err := tenancy.ParseScope(listing.PrivateOrganizationID, listing.PrivateWorkspaceID)
		if err != nil || !scope.Valid() {
			return ErrInvalid
		}
	} else if listing.PrivateOrganizationID != "" || listing.PrivateWorkspaceID != "" {
		return ErrInvalid
	}
	return nil
}

func validMetadataText(value string, min, max int) bool {
	return value == strings.TrimSpace(value) && len(value) >= min && len(value) <= max && !strings.ContainsAny(value, "\r\n\x00")
}

// ListingView is safe marketplace metadata intended for installation/review UI.
// It exposes trust and requested authority but never publisher private keys,
// credentials or plugin runtime state.
type ListingView struct {
	ID                string                           `json:"id"`
	Name              string                           `json:"name"`
	Family            sdk.Family                       `json:"family"`
	Version           string                           `json:"version"`
	SDKVersion        int                              `json:"sdk_version"`
	ArtifactSHA256    string                           `json:"artifact_sha256"`
	PublisherID       string                           `json:"publisher_id"`
	PublisherKeyID    string                           `json:"publisher_key_id"`
	Trust             pluginsecurity.TrustLevel        `json:"trust"`
	LicenseExpression string                           `json:"license_expression"`
	SecurityContact   string                           `json:"security_contact"`
	Requested         pluginsecurity.PermissionRequest `json:"requested_permissions"`
	Limits            pluginsecurity.IsolationLimits   `json:"isolation_limits"`
	PublishedAt       time.Time                        `json:"published_at"`
}

func (listing Listing) View() (ListingView, error) {
	if err := listing.Validate(); err != nil {
		return ListingView{}, err
	}
	request := cloneRequest(listing.Descriptor.Requested)
	return ListingView{
		ID: listing.Descriptor.Manifest.ID, Name: listing.Descriptor.Manifest.Name, Family: listing.Descriptor.Manifest.Family,
		Version: listing.Descriptor.Manifest.Version, SDKVersion: listing.Descriptor.Manifest.SDKVersion,
		ArtifactSHA256: listing.Descriptor.Artifact.SHA256, PublisherID: listing.Descriptor.Artifact.PublisherID,
		PublisherKeyID: listing.Descriptor.Artifact.KeyID, Trust: listing.Descriptor.Trust,
		LicenseExpression: listing.LicenseExpression, SecurityContact: listing.SecurityContact, Requested: request,
		Limits: listing.Descriptor.Limits, PublishedAt: listing.PublishedAt,
	}, nil
}

func cloneRequest(request pluginsecurity.PermissionRequest) pluginsecurity.PermissionRequest {
	return pluginsecurity.PermissionRequest{
		Capabilities:  append([]sdk.Capability(nil), request.Capabilities...),
		SecretClasses: append([]string(nil), request.SecretClasses...),
		Network:       append([]pluginsecurity.NetworkDestination(nil), request.Network...),
	}
}

type Consent struct {
	ID        string                         `json:"id"`
	Scope     tenancy.Scope                  `json:"-"`
	Grant     pluginsecurity.PermissionGrant `json:"grant"`
	ActorID   string                         `json:"actor_id"`
	GrantedAt time.Time                      `json:"granted_at"`
}

func (consent Consent) Validate(listing Listing) error {
	if !consent.Scope.Valid() || !validMetadataText(consent.ID, 1, 160) || !validMetadataText(consent.ActorID, 1, 256) ||
		consent.GrantedAt.IsZero() || consent.GrantedAt.Location() != time.UTC || consent.Grant.Validate() != nil || listing.Validate() != nil ||
		!consent.Grant.GrantedAt.Equal(consent.GrantedAt) || consent.Grant.ExtensionID != listing.Descriptor.Manifest.ID ||
		consent.Grant.ExtensionVersion != listing.Descriptor.Manifest.Version || consent.Grant.ArtifactSHA256 != listing.Descriptor.Artifact.SHA256 ||
		!grantWithinRequest(consent.Grant, listing.Descriptor.Requested) {
		return ErrInvalid
	}
	if listing.Descriptor.Trust == pluginsecurity.TrustPrivate &&
		(consent.Scope.OrganizationID().String() != listing.PrivateOrganizationID || consent.Scope.WorkspaceID().String() != listing.PrivateWorkspaceID) {
		return ErrInvalid
	}
	return nil
}

func grantWithinRequest(grant pluginsecurity.PermissionGrant, request pluginsecurity.PermissionRequest) bool {
	capabilities := make(map[sdk.Capability]struct{}, len(request.Capabilities))
	for _, capability := range request.Capabilities {
		capabilities[capability] = struct{}{}
	}
	for _, capability := range grant.Capabilities {
		if _, ok := capabilities[capability]; !ok {
			return false
		}
	}

	secrets := make(map[string]struct{}, len(request.SecretClasses))
	for _, class := range request.SecretClasses {
		secrets[class] = struct{}{}
	}
	for _, class := range grant.SecretClasses {
		if _, ok := secrets[class]; !ok {
			return false
		}
	}

	network := make(map[string]struct{}, len(request.Network))
	for _, destination := range request.Network {
		network[networkKey(destination)] = struct{}{}
	}
	for _, destination := range grant.Network {
		if _, ok := network[networkKey(destination)]; !ok {
			return false
		}
	}
	return true
}

func networkKey(value pluginsecurity.NetworkDestination) string {
	return fmt.Sprintf("%s:%d", value.Host, value.Port)
}

type UpdateAssessment struct {
	RequiresReapproval bool                                `json:"requires_reapproval"`
	Reasons            []string                            `json:"reasons"`
	AddedCapabilities  []sdk.Capability                    `json:"added_capabilities,omitempty"`
	AddedSecretClasses []string                            `json:"added_secret_classes,omitempty"`
	AddedNetwork       []pluginsecurity.NetworkDestination `json:"added_network,omitempty"`
	RaisedLimits       []string                            `json:"raised_limits,omitempty"`
}

// AssessUpdate compares the previously consented authority with a candidate
// listing. Any different artifact requires fresh exact-digest consent (Task 025),
// while authority growth is separately surfaced as explicit privilege escalation.
func AssessUpdate(previous Listing, previousGrant pluginsecurity.PermissionGrant, next Listing) (UpdateAssessment, error) {
	if previous.Validate() != nil || next.Validate() != nil || previousGrant.Validate() != nil ||
		previousGrant.ExtensionID != previous.Descriptor.Manifest.ID || previousGrant.ExtensionVersion != previous.Descriptor.Manifest.Version ||
		previousGrant.ArtifactSHA256 != previous.Descriptor.Artifact.SHA256 || !grantWithinRequest(previousGrant, previous.Descriptor.Requested) ||
		previous.Descriptor.Manifest.ID != next.Descriptor.Manifest.ID {
		return UpdateAssessment{}, ErrInvalid
	}
	out := UpdateAssessment{}
	if previous.Descriptor.Artifact.SHA256 != next.Descriptor.Artifact.SHA256 || previous.Descriptor.Manifest.Version != next.Descriptor.Manifest.Version {
		out.Reasons = append(out.Reasons, "artifact_changed")
	}
	if previous.Descriptor.Artifact.PublisherID != next.Descriptor.Artifact.PublisherID || previous.Descriptor.Artifact.KeyID != next.Descriptor.Artifact.KeyID {
		out.Reasons = append(out.Reasons, "publisher_identity_changed")
	}
	if trustRank(next.Descriptor.Trust) < trustRank(previous.Descriptor.Trust) {
		out.Reasons = append(out.Reasons, "trust_downgrade")
	}
	out.AddedCapabilities = missingCapabilities(previousGrant.Capabilities, next.Descriptor.Requested.Capabilities)
	out.AddedSecretClasses = missingStrings(previousGrant.SecretClasses, next.Descriptor.Requested.SecretClasses)
	out.AddedNetwork = missingNetwork(previousGrant.Network, next.Descriptor.Requested.Network)
	out.RaisedLimits = raisedLimits(previous.Descriptor.Limits, next.Descriptor.Limits)
	if len(out.AddedCapabilities) > 0 {
		out.Reasons = append(out.Reasons, "capability_escalation")
	}
	if len(out.AddedSecretClasses) > 0 {
		out.Reasons = append(out.Reasons, "secret_scope_escalation")
	}
	if len(out.AddedNetwork) > 0 {
		out.Reasons = append(out.Reasons, "network_scope_escalation")
	}
	if len(out.RaisedLimits) > 0 {
		out.Reasons = append(out.Reasons, "resource_limit_escalation")
	}
	out.Reasons = uniqueSorted(out.Reasons)
	out.RequiresReapproval = len(out.Reasons) > 0
	return out, nil
}

func trustRank(trust pluginsecurity.TrustLevel) int {
	switch trust {
	case pluginsecurity.TrustOfficial:
		return 4
	case pluginsecurity.TrustVerified:
		return 3
	case pluginsecurity.TrustCommunity:
		return 2
	case pluginsecurity.TrustPrivate:
		return 1
	default:
		return 0
	}
}

func missingCapabilities(old, next []sdk.Capability) []sdk.Capability {
	set := map[sdk.Capability]struct{}{}
	for _, value := range old {
		set[value] = struct{}{}
	}
	out := []sdk.Capability{}
	for _, value := range next {
		if _, ok := set[value]; !ok {
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func missingStrings(old, next []string) []string {
	set := map[string]struct{}{}
	for _, value := range old {
		set[value] = struct{}{}
	}
	out := []string{}
	for _, value := range next {
		if _, ok := set[value]; !ok {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func missingNetwork(old, next []pluginsecurity.NetworkDestination) []pluginsecurity.NetworkDestination {
	key := networkKey
	set := map[string]struct{}{}
	for _, value := range old {
		set[key(value)] = struct{}{}
	}
	out := []pluginsecurity.NetworkDestination{}
	for _, value := range next {
		if _, ok := set[key(value)]; !ok {
			out = append(out, value)
		}
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out
}

func raisedLimits(old, next pluginsecurity.IsolationLimits) []string {
	out := []string{}
	if next.MemoryMiB > old.MemoryMiB {
		out = append(out, "memory_mib")
	}
	if next.CPUTimeMS > old.CPUTimeMS {
		out = append(out, "cpu_time_ms")
	}
	if next.WallTimeMS > old.WallTimeMS {
		out = append(out, "wall_time_ms")
	}
	if next.MaxOutputBytes > old.MaxOutputBytes {
		out = append(out, "max_output_bytes")
	}
	if next.MaxConcurrentCalls > old.MaxConcurrentCalls {
		out = append(out, "max_concurrent_calls")
	}
	return out
}

func uniqueSorted(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

type RevocationKind string

const (
	RevokeArtifact     RevocationKind = "artifact"
	RevokePublisherKey RevocationKind = "publisher_key"
	RevokeInstallation RevocationKind = "installation"
)

type Revocation struct {
	ID             string         `json:"id"`
	Kind           RevocationKind `json:"kind"`
	ExtensionID    string         `json:"extension_id,omitempty"`
	ArtifactSHA256 string         `json:"artifact_sha256,omitempty"`
	PublisherID    string         `json:"publisher_id,omitempty"`
	PublisherKeyID string         `json:"publisher_key_id,omitempty"`
	OrganizationID string         `json:"organization_id,omitempty"`
	WorkspaceID    string         `json:"workspace_id,omitempty"`
	ConsentID      string         `json:"consent_id,omitempty"`
	ActorID        string         `json:"actor_id"`
	Reason         string         `json:"reason"`
	RevokedAt      time.Time      `json:"revoked_at"`
}

func (revocation Revocation) Validate() error {
	if !validMetadataText(revocation.ID, 1, 160) || !validMetadataText(revocation.ActorID, 1, 256) ||
		!validMetadataText(revocation.Reason, 1, 512) || revocation.RevokedAt.IsZero() || revocation.RevokedAt.Location() != time.UTC {
		return ErrInvalid
	}
	switch revocation.Kind {
	case RevokeArtifact:
		if !marketplaceSafeIDPattern.MatchString(revocation.ExtensionID) || !marketplaceDigestPattern.MatchString(revocation.ArtifactSHA256) || revocation.PublisherID != "" || revocation.PublisherKeyID != "" || revocation.ConsentID != "" || revocation.OrganizationID != "" || revocation.WorkspaceID != "" {
			return ErrInvalid
		}
	case RevokePublisherKey:
		if !marketplaceSafeIDPattern.MatchString(revocation.PublisherID) || !marketplaceKeyIDPattern.MatchString(revocation.PublisherKeyID) || revocation.ExtensionID != "" || revocation.ArtifactSHA256 != "" || revocation.ConsentID != "" || revocation.OrganizationID != "" || revocation.WorkspaceID != "" {
			return ErrInvalid
		}
	case RevokeInstallation:
		if revocation.ConsentID == "" || revocation.ExtensionID != "" || revocation.ArtifactSHA256 != "" || revocation.PublisherID != "" || revocation.PublisherKeyID != "" {
			return ErrInvalid
		}
		if _, err := tenancy.ParseScope(revocation.OrganizationID, revocation.WorkspaceID); err != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type RevocationSet []Revocation

func (set RevocationSet) Check(listing Listing, consent Consent) error {
	if listing.Validate() != nil || consent.Validate(listing) != nil {
		return ErrInvalid
	}
	for _, revocation := range set {
		if revocation.Validate() != nil {
			return ErrInvalid
		}
		switch revocation.Kind {
		case RevokeArtifact:
			if revocation.ExtensionID == listing.Descriptor.Manifest.ID && revocation.ArtifactSHA256 == listing.Descriptor.Artifact.SHA256 {
				return ErrRevoked
			}
		case RevokePublisherKey:
			if revocation.PublisherID == listing.Descriptor.Artifact.PublisherID && revocation.PublisherKeyID == listing.Descriptor.Artifact.KeyID {
				return ErrRevoked
			}
		case RevokeInstallation:
			if revocation.ConsentID == consent.ID && revocation.OrganizationID == consent.Scope.OrganizationID().String() && revocation.WorkspaceID == consent.Scope.WorkspaceID().String() {
				return ErrRevoked
			}
		}
	}
	return nil
}

// Admit validates marketplace review, tenant consent, revocations and finally
// reuses Task-025 digest/signature/grant verification to produce the inert plan
// consumed by Task-029. Governance never executes plugin code itself.
func Admit(ctx context.Context, listing Listing, consent Consent, artifact io.Reader, trustStore pluginsecurity.TrustStore, revocations RevocationSet) (pluginsecurity.AdmissionPlan, error) {
	if ctx == nil || artifact == nil || trustStore == nil || listing.Validate() != nil || consent.Validate(listing) != nil {
		return pluginsecurity.AdmissionPlan{}, ErrInvalid
	}
	if err := revocations.Check(listing, consent); err != nil {
		return pluginsecurity.AdmissionPlan{}, err
	}
	plan, err := pluginsecurity.Prepare(ctx, listing.Descriptor, consent.Grant, artifact, trustStore)
	if err != nil {
		return pluginsecurity.AdmissionPlan{}, err
	}
	return plan, nil
}
