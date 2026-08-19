// Package pluginsecurity defines the non-executing security boundary for future
// isolated third-party Connector SDK plugins.
//
// Task 025 intentionally does not launch, dlopen, exec, interpret or otherwise
// execute plugin artifacts. It validates identity, signatures, requested
// permissions, tenant grants and isolation ceilings so Task 029 can later bind
// this contract to an actual sandbox/dry-run runtime.
package pluginsecurity

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	BoundaryVersion  = 1
	maxArtifactBytes = int64(256 << 20)
)

var (
	ErrInvalidDescriptor  = errors.New("plugin security: invalid descriptor")
	ErrInvalidGrant       = errors.New("plugin security: invalid permission grant")
	ErrPermissionEscalate = errors.New("plugin security: permission escalation")
	ErrArtifactDigest     = errors.New("plugin security: artifact digest mismatch")
	ErrArtifactSignature  = errors.New("plugin security: artifact signature verification failed")
	ErrArtifactTooLarge   = errors.New("plugin security: artifact exceeds size limit")
	ErrTrustKey           = errors.New("plugin security: trusted publisher key unavailable")
)

var (
	safeIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
	safeClassPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	digestPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	dnsLabelPattern  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	keyIDPattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)
)

type TrustLevel string

const (
	TrustOfficial  TrustLevel = "official"
	TrustVerified  TrustLevel = "verified"
	TrustCommunity TrustLevel = "community"
	TrustPrivate   TrustLevel = "private"
)

func (level TrustLevel) Valid() bool {
	return level == TrustOfficial || level == TrustVerified || level == TrustCommunity || level == TrustPrivate
}

type ExecutionMode string

const (
	// ExecutionIsolatedProcessV1 is a future contract only. Task 025 does not
	// provide a launcher for it.
	ExecutionIsolatedProcessV1 ExecutionMode = "isolated_process_v1"
)

func (mode ExecutionMode) Valid() bool { return mode == ExecutionIsolatedProcessV1 }

// ArtifactIdentity binds an immutable package digest to publisher signing
// metadata. Signature is Ed25519 over SignatureMessage(descriptor).
type ArtifactIdentity struct {
	SHA256          string `json:"sha256"`
	SizeBytes       int64  `json:"size_bytes"`
	PublisherID     string `json:"publisher_id"`
	KeyID           string `json:"key_id"`
	KeyFingerprint  string `json:"key_fingerprint_sha256"`
	SignatureBase64 string `json:"signature_base64"`
}

func (artifact ArtifactIdentity) Validate() error {
	if !digestPattern.MatchString(artifact.SHA256) || artifact.SizeBytes <= 0 || artifact.SizeBytes > maxArtifactBytes ||
		!safeIDPattern.MatchString(artifact.PublisherID) || !keyIDPattern.MatchString(artifact.KeyID) || !digestPattern.MatchString(artifact.KeyFingerprint) {
		return ErrInvalidDescriptor
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(artifact.SignatureBase64)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrInvalidDescriptor
	}
	return nil
}

// NetworkDestination is an exact TLS egress destination requested by a plugin.
// Wildcards, IP literals and local/single-label names are deliberately rejected.
// Task 029 must additionally enforce DNS resolution/rebinding at the sandbox edge.
type NetworkDestination struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

func (destination NetworkDestination) Validate() error {
	host := destination.Host
	if host == "" || host != strings.ToLower(host) || strings.HasSuffix(host, ".") || strings.ContainsAny(host, "*/:@[]") ||
		destination.Port == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return ErrInvalidDescriptor
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || !strings.Contains(host, ".") {
		return ErrInvalidDescriptor
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !dnsLabelPattern.MatchString(label) {
			return ErrInvalidDescriptor
		}
	}
	return nil
}

func (destination NetworkDestination) key() string {
	return fmt.Sprintf("%s:%d", destination.Host, destination.Port)
}

// PermissionRequest is the complete privilege request of an isolated plugin.
// Capabilities are functional authority; secret classes and network targets are
// runtime authority. There is intentionally no filesystem/env/process grant.
type PermissionRequest struct {
	Capabilities  []sdk.Capability     `json:"capabilities"`
	SecretClasses []string             `json:"secret_classes,omitempty"`
	Network       []NetworkDestination `json:"network,omitempty"`
}

func (request PermissionRequest) Validate(manifest sdk.Manifest) error {
	if err := manifest.Validate(); err != nil {
		return ErrInvalidDescriptor
	}
	if len(request.Capabilities) == 0 || len(request.Capabilities) > len(manifest.Capabilities) || len(request.SecretClasses) > 8 || len(request.Network) > 32 {
		return ErrInvalidDescriptor
	}
	manifestCapabilities := make(map[sdk.Capability]struct{}, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		manifestCapabilities[capability] = struct{}{}
	}
	seenCapabilities := map[sdk.Capability]struct{}{}
	for _, capability := range request.Capabilities {
		if _, duplicate := seenCapabilities[capability]; duplicate {
			return ErrInvalidDescriptor
		}
		seenCapabilities[capability] = struct{}{}
		if _, exists := manifestCapabilities[capability]; !exists {
			return ErrPermissionEscalate
		}
	}

	permittedSecrets := make(map[string]struct{})
	for _, auth := range manifest.Auth {
		if auth.SecretClass != "" {
			permittedSecrets[auth.SecretClass] = struct{}{}
		}
	}
	seenSecrets := map[string]struct{}{}
	for _, class := range request.SecretClasses {
		if !safeClassPattern.MatchString(class) {
			return ErrInvalidDescriptor
		}
		if _, duplicate := seenSecrets[class]; duplicate {
			return ErrInvalidDescriptor
		}
		seenSecrets[class] = struct{}{}
		if _, exists := permittedSecrets[class]; !exists {
			return ErrPermissionEscalate
		}
	}
	for _, auth := range manifest.Auth {
		if auth.Required && auth.Kind != sdk.AuthNone {
			if _, requested := seenSecrets[auth.SecretClass]; !requested {
				return ErrInvalidDescriptor
			}
		}
	}

	seenNetwork := map[string]struct{}{}
	for _, destination := range request.Network {
		if err := destination.Validate(); err != nil {
			return err
		}
		key := destination.key()
		if _, duplicate := seenNetwork[key]; duplicate {
			return ErrInvalidDescriptor
		}
		seenNetwork[key] = struct{}{}
	}
	return nil
}

// IsolationLimits are ceilings that Task 029 must enforce. Task 025 validates
// the contract but does not launch a process or claim enforcement yet.
type IsolationLimits struct {
	MemoryMiB          int `json:"memory_mib"`
	CPUTimeMS          int `json:"cpu_time_ms"`
	WallTimeMS         int `json:"wall_time_ms"`
	MaxOutputBytes     int `json:"max_output_bytes"`
	MaxConcurrentCalls int `json:"max_concurrent_calls"`
}

func (limits IsolationLimits) Validate() error {
	if limits.MemoryMiB < 16 || limits.MemoryMiB > 4096 || limits.CPUTimeMS < 100 || limits.CPUTimeMS > 300000 ||
		limits.WallTimeMS < limits.CPUTimeMS || limits.WallTimeMS > 300000 || limits.MaxOutputBytes < 1024 || limits.MaxOutputBytes > 16<<20 ||
		limits.MaxConcurrentCalls < 1 || limits.MaxConcurrentCalls > 64 {
		return ErrInvalidDescriptor
	}
	return nil
}

// Descriptor is signed package metadata for a future isolated third-party
// plugin. It embeds the already-stabilized Connector SDK manifest.
type Descriptor struct {
	BoundaryVersion int               `json:"boundary_version"`
	ExecutionMode   ExecutionMode     `json:"execution_mode"`
	Trust           TrustLevel        `json:"trust"`
	Manifest        sdk.Manifest      `json:"manifest"`
	Artifact        ArtifactIdentity  `json:"artifact"`
	Requested       PermissionRequest `json:"requested_permissions"`
	Limits          IsolationLimits   `json:"isolation_limits"`
}

func (descriptor Descriptor) Validate() error {
	if descriptor.BoundaryVersion != BoundaryVersion || !descriptor.ExecutionMode.Valid() || !descriptor.Trust.Valid() {
		return ErrInvalidDescriptor
	}
	if err := descriptor.Manifest.Validate(); err != nil {
		return ErrInvalidDescriptor
	}
	if err := descriptor.Artifact.Validate(); err != nil {
		return err
	}
	if err := descriptor.Requested.Validate(descriptor.Manifest); err != nil {
		return err
	}
	if err := descriptor.Limits.Validate(); err != nil {
		return err
	}
	return nil
}

// PermissionGrant is host/user consent. It is bound to an exact immutable
// artifact so an update cannot silently inherit permissions.
type PermissionGrant struct {
	ExtensionID      string               `json:"connector_id"`
	ExtensionVersion string               `json:"connector_version"`
	ArtifactSHA256   string               `json:"artifact_sha256"`
	Capabilities     []sdk.Capability     `json:"capabilities"`
	SecretClasses    []string             `json:"secret_classes,omitempty"`
	Network          []NetworkDestination `json:"network,omitempty"`
	GrantedAt        time.Time            `json:"granted_at"`
}

func (grant PermissionGrant) Validate() error {
	if !safeIDPattern.MatchString(grant.ExtensionID) || !digestPattern.MatchString(grant.ArtifactSHA256) || grant.ExtensionVersion == "" ||
		grant.GrantedAt.IsZero() || grant.GrantedAt.Location() != time.UTC || len(grant.Capabilities) == 0 || len(grant.SecretClasses) > 8 || len(grant.Network) > 32 {
		return ErrInvalidGrant
	}
	if !sortedUniqueCapabilities(grant.Capabilities) || !sortedUniqueStrings(grant.SecretClasses) {
		return ErrInvalidGrant
	}
	previous := ""
	for _, destination := range grant.Network {
		if err := destination.Validate(); err != nil {
			return ErrInvalidGrant
		}
		key := destination.key()
		if previous != "" && key <= previous {
			return ErrInvalidGrant
		}
		previous = key
	}
	return nil
}

func (grant PermissionGrant) validateAgainst(descriptor Descriptor) error {
	if err := grant.Validate(); err != nil {
		return err
	}
	if grant.ExtensionID != descriptor.Manifest.ID || grant.ExtensionVersion != descriptor.Manifest.Version || grant.ArtifactSHA256 != descriptor.Artifact.SHA256 {
		return ErrPermissionEscalate
	}
	requestedCapabilities := make(map[sdk.Capability]struct{}, len(descriptor.Requested.Capabilities))
	for _, capability := range descriptor.Requested.Capabilities {
		requestedCapabilities[capability] = struct{}{}
	}
	for _, capability := range grant.Capabilities {
		if _, ok := requestedCapabilities[capability]; !ok {
			return ErrPermissionEscalate
		}
	}
	requestedSecrets := make(map[string]struct{}, len(descriptor.Requested.SecretClasses))
	for _, class := range descriptor.Requested.SecretClasses {
		requestedSecrets[class] = struct{}{}
	}
	for _, class := range grant.SecretClasses {
		if _, ok := requestedSecrets[class]; !ok {
			return ErrPermissionEscalate
		}
	}
	requestedNetwork := make(map[string]struct{}, len(descriptor.Requested.Network))
	for _, destination := range descriptor.Requested.Network {
		requestedNetwork[destination.key()] = struct{}{}
	}
	for _, destination := range grant.Network {
		if _, ok := requestedNetwork[destination.key()]; !ok {
			return ErrPermissionEscalate
		}
	}
	return nil
}

func sortedUniqueCapabilities(values []sdk.Capability) bool {
	for i, value := range values {
		if _, ok := sdk.CapabilityDefinitionFor(value); !ok {
			return false
		}
		if i > 0 && values[i-1] >= value {
			return false
		}
	}
	return true
}

func sortedUniqueStrings(values []string) bool {
	for i, value := range values {
		if !safeClassPattern.MatchString(value) || (i > 0 && values[i-1] >= value) {
			return false
		}
	}
	return true
}

// TrustStore resolves publisher keys. Concrete marketplace/governance storage
// is intentionally deferred; the verification boundary is stable now.
type TrustStore interface {
	Ed25519PublicKey(context.Context, string, string) (ed25519.PublicKey, error)
}

// AdmissionPlan is inert validated data. It is not executable and deliberately
// contains no function pointer, command, path, environment or raw secret.
type AdmissionPlan struct {
	BoundaryVersion  int             `json:"boundary_version"`
	ExecutionMode    ExecutionMode   `json:"execution_mode"`
	ExtensionID      string          `json:"connector_id"`
	ExtensionVersion string          `json:"connector_version"`
	ArtifactSHA256   string          `json:"artifact_sha256"`
	Trust            TrustLevel      `json:"trust"`
	Granted          PermissionGrant `json:"granted_permissions"`
	Limits           IsolationLimits `json:"isolation_limits"`
}

// Prepare verifies descriptor, exact artifact digest, publisher signature and
// least-privilege grant, then returns an inert future-sandbox plan.
func Prepare(ctx context.Context, descriptor Descriptor, grant PermissionGrant, artifact io.Reader, trustStore TrustStore) (AdmissionPlan, error) {
	if ctx == nil || artifact == nil || trustStore == nil {
		return AdmissionPlan{}, ErrInvalidDescriptor
	}
	if err := ctx.Err(); err != nil {
		return AdmissionPlan{}, err
	}
	if err := descriptor.Validate(); err != nil {
		return AdmissionPlan{}, err
	}
	if err := grant.validateAgainst(descriptor); err != nil {
		return AdmissionPlan{}, err
	}

	digest, size, err := hashArtifact(ctx, artifact)
	if err != nil {
		return AdmissionPlan{}, err
	}
	if size != descriptor.Artifact.SizeBytes || digest != descriptor.Artifact.SHA256 {
		return AdmissionPlan{}, ErrArtifactDigest
	}
	publicKey, err := trustStore.Ed25519PublicKey(ctx, descriptor.Artifact.PublisherID, descriptor.Artifact.KeyID)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return AdmissionPlan{}, ErrTrustKey
	}
	fingerprint := sha256.Sum256(publicKey)
	if hex.EncodeToString(fingerprint[:]) != descriptor.Artifact.KeyFingerprint {
		return AdmissionPlan{}, ErrTrustKey
	}
	signature, _ := base64.StdEncoding.Strict().DecodeString(descriptor.Artifact.SignatureBase64)
	if !ed25519.Verify(publicKey, SignatureMessage(descriptor), signature) {
		return AdmissionPlan{}, ErrArtifactSignature
	}
	return AdmissionPlan{
		BoundaryVersion:  BoundaryVersion,
		ExecutionMode:    descriptor.ExecutionMode,
		ExtensionID:      descriptor.Manifest.ID,
		ExtensionVersion: descriptor.Manifest.Version,
		ArtifactSHA256:   descriptor.Artifact.SHA256,
		Trust:            descriptor.Trust,
		Granted:          grant,
		Limits:           descriptor.Limits,
	}, nil
}

func hashArtifact(ctx context.Context, reader io.Reader) (string, int64, error) {
	hasher := sha256.New()
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return "", 0, err
		}
		n, err := reader.Read(buffer)
		if n > 0 {
			total += int64(n)
			if total > maxArtifactBytes {
				return "", total, ErrArtifactTooLarge
			}
			_, _ = hasher.Write(buffer[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", 0, err
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), total, nil
}

// SignatureMessage is deterministic and intentionally small: changing plugin
// identity/version, artifact digest, trust, permissions or limits invalidates
// the publisher signature. Permission slices are canonicalized first.
func SignatureMessage(descriptor Descriptor) []byte {
	capabilities := append([]sdk.Capability(nil), descriptor.Requested.Capabilities...)
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	secretClasses := append([]string(nil), descriptor.Requested.SecretClasses...)
	sort.Strings(secretClasses)
	network := append([]NetworkDestination(nil), descriptor.Requested.Network...)
	sort.Slice(network, func(i, j int) bool { return network[i].key() < network[j].key() })

	manifest := descriptor.Manifest.Canonical()
	manifestCapabilities := append([]sdk.Capability(nil), manifest.Capabilities...)
	sort.Slice(manifestCapabilities, func(i, j int) bool { return manifestCapabilities[i] < manifestCapabilities[j] })
	auth := append([]sdk.AuthRequirement(nil), manifest.Auth...)
	sort.Slice(auth, func(i, j int) bool {
		left, _ := json.Marshal(auth[i])
		right, _ := json.Marshal(auth[j])
		return string(left) < string(right)
	})
	manifest.Auth = auth
	manifest.Capabilities = manifestCapabilities
	manifestJSON, _ := json.Marshal(manifest)
	manifestDigest := sha256.Sum256(manifestJSON)

	var builder strings.Builder
	fmt.Fprintf(&builder, "torgnexa-plugin-boundary-v1\nid=%s\nversion=%s\nsdk=%d\nfamily=%s\nmanifest_sha256=%s\npublisher=%s\nkey_id=%s\nkey_fingerprint=%s\ntrust=%s\nmode=%s\nsha256=%s\nsize=%d\n",
		descriptor.Manifest.ID, descriptor.Manifest.Version, descriptor.Manifest.SDKVersion, descriptor.Manifest.Family,
		hex.EncodeToString(manifestDigest[:]), descriptor.Artifact.PublisherID, descriptor.Artifact.KeyID,
		descriptor.Artifact.KeyFingerprint, descriptor.Trust, descriptor.ExecutionMode, descriptor.Artifact.SHA256,
		descriptor.Artifact.SizeBytes)
	for _, capability := range capabilities {
		fmt.Fprintf(&builder, "cap=%s\n", capability)
	}
	for _, class := range secretClasses {
		fmt.Fprintf(&builder, "secret=%s\n", class)
	}
	for _, destination := range network {
		fmt.Fprintf(&builder, "net=%s\n", destination.key())
	}
	fmt.Fprintf(&builder, "limits=%d,%d,%d,%d,%d\n", descriptor.Limits.MemoryMiB, descriptor.Limits.CPUTimeMS,
		descriptor.Limits.WallTimeMS, descriptor.Limits.MaxOutputBytes, descriptor.Limits.MaxConcurrentCalls)
	return []byte(builder.String())
}
