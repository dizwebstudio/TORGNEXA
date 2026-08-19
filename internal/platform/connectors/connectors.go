// Package connectors defines the provider-neutral Connector SDK boundary.
//
// Provider implementations must depend on this package instead of Core,
// PostgreSQL, concrete secret storage, or provider names embedded in workflows.
package connectors

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const SDKMajor = 1

var (
	ErrInvalidManifest       = errors.New("connectors: invalid manifest")
	ErrUnsupportedSDKVersion = errors.New("connectors: unsupported sdk version")
	ErrUnknownCapability     = errors.New("connectors: unknown capability")
	ErrCapabilityFamily      = errors.New("connectors: capability is not valid for connector family")
	ErrCapabilityMissing     = errors.New("connectors: required capability is missing")
	ErrInvalidHealth         = errors.New("connectors: invalid health result")
)

var (
	manifestIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	versionPattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	safeCodePattern   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

// Family is a provider-neutral connector family. FX and notification are
// explicit families rather than provider-specific exceptions in Core.
type Family string

const (
	FamilyMarketplace  Family = "marketplace"
	FamilyStorefront   Family = "storefront"
	FamilyClassified   Family = "classified"
	FamilySocial       Family = "social"
	FamilyERP          Family = "erp"
	FamilyEDO          Family = "edo"
	FamilyGovernment   Family = "government"
	FamilyPayment      Family = "payment"
	FamilyLogistics    Family = "logistics"
	FamilyPickup       Family = "pickup"
	FamilyFX           Family = "fx"
	FamilyNotification Family = "notification"
	FamilyCRM          Family = "crm"
	FamilyAI           Family = "ai"
)

func (family Family) Valid() bool {
	switch family {
	case FamilyMarketplace, FamilyStorefront, FamilyClassified, FamilySocial, FamilyERP, FamilyEDO,
		FamilyGovernment, FamilyPayment, FamilyLogistics, FamilyPickup, FamilyFX, FamilyNotification, FamilyCRM, FamilyAI:
		return true
	default:
		return false
	}
}

// AuthKind describes the shape of a credential without exposing credential
// material. The actual bytes remain behind a SecretAccessor callback.
type AuthKind string

const (
	AuthNone        AuthKind = "none"
	AuthAPIKey      AuthKind = "api_key"
	AuthBearer      AuthKind = "bearer"
	AuthBasic       AuthKind = "basic"
	AuthOAuth2      AuthKind = "oauth2"
	AuthCertificate AuthKind = "certificate"
)

func (kind AuthKind) Valid() bool {
	switch kind {
	case AuthNone, AuthAPIKey, AuthBearer, AuthBasic, AuthOAuth2, AuthCertificate:
		return true
	default:
		return false
	}
}

// AuthRequirement is non-secret manifest metadata. SecretClass is a generic
// classification understood by the secret layer; it must never contain a key,
// token, password, or provider credential.
type AuthRequirement struct {
	Kind        AuthKind             `json:"kind"`
	SecretClass string               `json:"secret_class,omitempty"`
	Required    bool                 `json:"required"`
	OAuth2      *OAuth2Configuration `json:"oauth2,omitempty"`
}

// OAuth2Configuration is non-secret authorization metadata. Authorization-code
// flows always use PKCE S256; client credentials remain encrypted separately.
type OAuth2Configuration struct {
	GrantType        string   `json:"grant_type"`
	AuthorizationURL string   `json:"authorization_url,omitempty"`
	TokenURL         string   `json:"token_url"`
	Scopes           []string `json:"scopes,omitempty"`
	ClientAuthMethod string   `json:"client_auth_method"`
}

func (configuration OAuth2Configuration) Validate() error {
	if (configuration.GrantType != "authorization_code" && configuration.GrantType != "client_credentials") || configuration.ClientAuthMethod != "client_secret_post" {
		return ErrInvalidManifest
	}
	if !validHTTPSURL(configuration.TokenURL) || len(configuration.Scopes) > 32 {
		return ErrInvalidManifest
	}
	if configuration.GrantType == "authorization_code" && !validHTTPSURL(configuration.AuthorizationURL) {
		return ErrInvalidManifest
	}
	if configuration.GrantType == "client_credentials" && configuration.AuthorizationURL != "" {
		return ErrInvalidManifest
	}
	seen := map[string]struct{}{}
	for _, scope := range configuration.Scopes {
		if scope == "" || len(scope) > 128 || scope != strings.TrimSpace(scope) || strings.ContainsAny(scope, "\x00\r\n\t ") {
			return ErrInvalidManifest
		}
		if _, duplicate := seen[scope]; duplicate {
			return ErrInvalidManifest
		}
		seen[scope] = struct{}{}
	}
	return nil
}

func (requirement AuthRequirement) Validate() error {
	if !requirement.Kind.Valid() {
		return ErrInvalidManifest
	}
	if requirement.Kind == AuthNone {
		if requirement.Required || requirement.SecretClass != "" || requirement.OAuth2 != nil {
			return ErrInvalidManifest
		}
		return nil
	}
	if requirement.Required && !safeCodePattern.MatchString(requirement.SecretClass) {
		return ErrInvalidManifest
	}
	if requirement.SecretClass != "" && !safeCodePattern.MatchString(requirement.SecretClass) {
		return ErrInvalidManifest
	}
	if requirement.Kind == AuthOAuth2 {
		// OAuth2 metadata is additive for SDK-v1 compatibility. Canonical JSON
		// manifests require it through manifest-v2.schema.json; in-memory legacy
		// connector tests remain valid but the host OAuth flow fails closed.
		if requirement.OAuth2 != nil && requirement.OAuth2.Validate() != nil {
			return ErrInvalidManifest
		}
	} else if requirement.OAuth2 != nil {
		return ErrInvalidManifest
	}
	return nil
}

// ConnectionTest is a declarative, credential-aware remote probe. Secret
// placeholders are permitted only in header values and the bounded static body;
// URLs are immutable manifest data and therefore cannot become an SSRF gadget.
type ConnectionTest struct {
	URL             string            `json:"url"`
	Method          string            `json:"method"`
	Headers         map[string]string `json:"headers,omitempty"`
	Body            string            `json:"body,omitempty"`
	SuccessStatuses []int             `json:"success_statuses,omitempty"`
}

func (test ConnectionTest) Validate() error {
	if !validHTTPSURL(test.URL) || (test.Method != "GET" && test.Method != "POST") || len(test.Headers) > 16 || len(test.Body) > 16<<10 || strings.Contains(test.URL, "${") {
		return ErrInvalidManifest
	}
	for name, value := range test.Headers {
		if !safeHeaderName(name) || forbiddenProbeHeader(name) || len(value) > 2048 || strings.ContainsAny(value, "\r\n") || !validCredentialTemplate(value) {
			return ErrInvalidManifest
		}
	}
	if !validCredentialTemplate(test.Body) || len(test.SuccessStatuses) > 16 {
		return ErrInvalidManifest
	}
	seen := map[int]struct{}{}
	for _, status := range test.SuccessStatuses {
		if status < 200 || status > 299 {
			return ErrInvalidManifest
		}
		if _, duplicate := seen[status]; duplicate {
			return ErrInvalidManifest
		}
		seen[status] = struct{}{}
	}
	return nil
}

func forbiddenProbeHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "content-length", "host", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

// RetryPolicy defines SDK-level bounded retry guidance. Connectors still return
// normalized RemoteError values; the host runtime owns scheduling and sleeping.
type RetryPolicy struct {
	MaxAttempts   int `json:"max_attempts"`
	BaseBackoffMS int `json:"base_backoff_ms"`
	MaxBackoffMS  int `json:"max_backoff_ms"`
}

func (policy RetryPolicy) Validate() error {
	if policy.MaxAttempts < 1 || policy.MaxAttempts > 20 || policy.BaseBackoffMS < 1 || policy.MaxBackoffMS < policy.BaseBackoffMS || policy.MaxBackoffMS > 300000 {
		return ErrInvalidManifest
	}
	return nil
}

// RateLimitPolicy is static manifest guidance. Dynamic provider Retry-After is
// represented by RemoteError and takes precedence when present.
type RateLimitPolicy struct {
	MaxConcurrency   int         `json:"max_concurrency"`
	MinIntervalMS    int         `json:"min_interval_ms"`
	RequestTimeoutMS int         `json:"request_timeout_ms"`
	Retry            RetryPolicy `json:"retry"`
}

func (policy RateLimitPolicy) Validate() error {
	if policy.MaxConcurrency < 1 || policy.MaxConcurrency > 1024 || policy.MinIntervalMS < 0 || policy.MinIntervalMS > 60000 || policy.RequestTimeoutMS < 100 || policy.RequestTimeoutMS > 300000 {
		return ErrInvalidManifest
	}
	return policy.Retry.Validate()
}

// Manifest is the stable SDK v1 identity and capability declaration. A
// connector can only be registered after strict validation against the
// canonical capability registry.
type Manifest struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Family         Family            `json:"family"`
	Version        string            `json:"version"`
	SDKVersion     int               `json:"sdk_version"`
	Capabilities   []Capability      `json:"capabilities"`
	Auth           []AuthRequirement `json:"auth,omitempty"`
	RateLimit      RateLimitPolicy   `json:"rate_limit"`
	ConnectionTest *ConnectionTest   `json:"connection_test,omitempty"`
}

func (manifest Manifest) Validate() error {
	if !manifestIDPattern.MatchString(manifest.ID) || !validDisplayName(manifest.Name) || !manifest.Family.Valid() || !versionPattern.MatchString(manifest.Version) {
		return ErrInvalidManifest
	}
	if manifest.SDKVersion != SDKMajor {
		return ErrUnsupportedSDKVersion
	}
	if err := manifest.RateLimit.Validate(); err != nil {
		return err
	}
	if len(manifest.Capabilities) == 0 || len(manifest.Capabilities) > len(capabilityDefinitions) {
		return ErrInvalidManifest
	}
	seen := make(map[Capability]struct{}, len(manifest.Capabilities))
	for _, capability := range manifest.Capabilities {
		definition, ok := capabilityDefinitions[capability]
		if !ok {
			return ErrUnknownCapability
		}
		if _, duplicate := seen[capability]; duplicate {
			return ErrInvalidManifest
		}
		seen[capability] = struct{}{}
		if !definition.SupportsFamily(manifest.Family) {
			return ErrCapabilityFamily
		}
	}
	for _, auth := range manifest.Auth {
		if err := auth.Validate(); err != nil {
			return err
		}
	}
	if len(manifest.Auth) == 0 {
		return ErrInvalidManifest
	}
	if manifest.ConnectionTest != nil {
		if err := manifest.ConnectionTest.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Canonical returns a manifest with stable capability ordering. It does not
// repair invalid input; callers must Validate first.
func (manifest Manifest) Canonical() Manifest {
	copyManifest := manifest
	copyManifest.Capabilities = append([]Capability(nil), manifest.Capabilities...)
	sort.Slice(copyManifest.Capabilities, func(i, j int) bool { return copyManifest.Capabilities[i] < copyManifest.Capabilities[j] })
	copyManifest.Auth = append([]AuthRequirement(nil), manifest.Auth...)
	return copyManifest
}

func (manifest Manifest) Supports(capability Capability) bool {
	for _, item := range manifest.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

func (manifest Manifest) RequiresSecret() bool {
	for _, auth := range manifest.Auth {
		if auth.Required && auth.Kind != AuthNone {
			return true
		}
	}
	return false
}

// HealthStatus is deliberately small and provider-neutral.
type HealthStatus string

const (
	HealthUnknown     HealthStatus = "unknown"
	HealthHealthy     HealthStatus = "healthy"
	HealthDegraded    HealthStatus = "degraded"
	HealthUnavailable HealthStatus = "unavailable"
)

func (status HealthStatus) Valid() bool {
	return status == HealthUnknown || status == HealthHealthy || status == HealthDegraded || status == HealthUnavailable
}

// Health contains only safe normalized metadata. Raw provider responses and
// error strings must never be copied into ReasonCode.
type Health struct {
	Status     HealthStatus `json:"status"`
	ReasonCode string       `json:"reason_code,omitempty"`
	CheckedAt  time.Time    `json:"checked_at"`
}

func (health Health) Validate() error {
	if !health.Status.Valid() {
		return ErrInvalidHealth
	}
	if health.Status == HealthUnknown {
		if !health.CheckedAt.IsZero() || health.ReasonCode != "" {
			return ErrInvalidHealth
		}
		return nil
	}
	if health.CheckedAt.IsZero() || health.CheckedAt.Location() != time.UTC {
		return ErrInvalidHealth
	}
	if health.ReasonCode != "" && !safeCodePattern.MatchString(health.ReasonCode) {
		return ErrInvalidHealth
	}
	if health.Status == HealthHealthy && health.ReasonCode != "" {
		return ErrInvalidHealth
	}
	if (health.Status == HealthDegraded || health.Status == HealthUnavailable) && health.ReasonCode == "" {
		return ErrInvalidHealth
	}
	return nil
}

// SecretAccessor exposes plaintext only inside a host-owned callback. Provider
// code never receives a secret repository or durable secret representation.
type SecretAccessor interface {
	UseSecret(context.Context, SecretReference, func([]byte) error) error
}

// Runtime is the minimum host surface available to a Connector SDK v1
// implementation. Task 025 stabilizes this root surface across the future
// isolated third-party boundary. Adding methods requires a new SDK major.
type Runtime interface {
	Secrets() SecretAccessor
}

// Connector is the frozen SDK v1 root contract. Domain operations are exposed
// by additive capability-specific interfaces in future/reference connector
// tasks rather than by provider names or a universal untyped Invoke method.
// Adding a root method is an SDK-major compatibility change.
type Connector interface {
	Manifest() Manifest
	Health(context.Context, Account, Runtime) (Health, error)
}

// RequireCapability validates both the capability vocabulary and the manifest
// declaration before a workflow attempts an operation.
func RequireCapability(manifest Manifest, capability Capability) error {
	if _, ok := capabilityDefinitions[capability]; !ok {
		return ErrUnknownCapability
	}
	if !manifest.Supports(capability) {
		return ErrCapabilityMissing
	}
	return nil
}

func validDisplayName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) {
		return false
	}
	length := utf8.RuneCountInString(value)
	return length >= 1 && length <= 200
}

func validHTTPSURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == "" && parsed.Hostname() != "" && parsed.Port() == ""
}

func safeHeaderName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	switch strings.ToLower(value) {
	case "host", "connection", "content-length", "proxy-authorization", "proxy-connection", "transfer-encoding", "upgrade":
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-' {
			continue
		}
		return false
	}
	return true
}

var credentialTemplatePattern = regexp.MustCompile(`\$\{(?:secret|basic|[a-z][a-z0-9_]{0,63})\}`)

func validCredentialTemplate(value string) bool {
	if strings.IndexFunc(value, func(r rune) bool { return r == 0 }) >= 0 {
		return false
	}
	without := credentialTemplatePattern.ReplaceAllString(value, "")
	return !strings.Contains(without, "${")
}
