package securitysettings

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var (
	ErrIdentityInvalid      = errors.New("identity provider settings: invalid value")
	ErrIdentityNotFound     = errors.New("identity provider settings: not found")
	ErrIdentityConflict     = errors.New("identity provider settings: conflict")
	ErrIdentityNotValidated = errors.New("identity provider settings: current revision is not validated")
	ErrIdentityUnsafeURL    = errors.New("identity provider settings: URL is not allowed")
)

// ProviderDraft is a provider-neutral OIDC configuration. ID is a
// tenant-owned label (for example corporate or vk), never a Core dispatch key.
type ProviderDraft struct {
	ID              string
	Protocol        string
	DisplayName     string
	IssuerURL       string
	ClientID        string
	CallbackURL     string
	SecretReference string
	ExpectedVersion uint64
	CorrelationID   string
	CreatedAt       time.Time
}

// ProviderRevision is an immutable configuration snapshot.
type ProviderRevision struct {
	ID              string
	Protocol        string
	DisplayName     string
	IssuerURL       string
	ClientID        string
	CallbackURL     string
	SecretReference string
	Revision        uint64
	CreatedAt       time.Time
}

// ProviderValidation is append-only discovery evidence for one revision.
type ProviderValidation struct {
	ID               string
	IdentityID       string
	Revision         uint64
	Status           string
	ReasonCode       string
	MetadataDigest   string
	Issuer           string
	AuthorizationURL string
	TokenURL         string
	JWKSURL          string
	CheckedAt        time.Time
	ExpectedVersion  uint64
	CorrelationID    string
}

// ProviderConfiguration combines the mutable pointer with immutable revision
// and validation evidence. Client secret material is intentionally absent.
type ProviderConfiguration struct {
	ProviderRevision
	Version           uint64
	ActiveRevision    uint64
	Enabled           bool
	ValidationStatus  string
	ValidationReason  string
	ValidatedAt       *time.Time
	UpdatedAt         time.Time
	LastCorrelationID string
	Replayed          bool
}

// IdentityProviderStore owns tenant-scoped version pointers and immutable
// revisions. Rollback changes a pointer; it never rewrites history.
type IdentityProviderStore interface {
	ListProviders(context.Context, tenancy.Scope, int) ([]ProviderConfiguration, error)
	Provider(context.Context, tenancy.Scope, string) (ProviderConfiguration, error)
	SaveProvider(context.Context, tenancy.Scope, ProviderDraft) (ProviderConfiguration, error)
	RecordProviderValidation(context.Context, tenancy.Scope, ProviderValidation) (ProviderConfiguration, error)
	ActivateProvider(context.Context, tenancy.Scope, string, uint64, string, time.Time) (ProviderConfiguration, error)
	RollbackProvider(context.Context, tenancy.Scope, string, uint64, uint64, string, string, time.Time) (ProviderConfiguration, error)
	DisableProvider(context.Context, tenancy.Scope, string, uint64, string, time.Time) (ProviderConfiguration, error)
}

// ProviderResolver is injectable so URL policy tests never depend on public DNS.
type ProviderResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type systemProviderResolver struct{}

func (systemProviderResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

// ProviderURLPolicy is a deployment-owned allowlist for managed IdP egress and
// exact browser callbacks. Empty allowlists deny every managed configuration.
type ProviderURLPolicy struct {
	issuerHosts map[string]struct{}
	callbacks   map[string]struct{}
	resolver    ProviderResolver
}

// NewProviderURLPolicy creates a default-deny URL policy. Callback origins are
// expanded only to their exact /oidc/callback URL.
func NewProviderURLPolicy(issuerHosts, callbackOrigins []string, resolver ProviderResolver) (*ProviderURLPolicy, error) {
	if resolver == nil {
		resolver = systemProviderResolver{}
	}
	policy := &ProviderURLPolicy{issuerHosts: map[string]struct{}{}, callbacks: map[string]struct{}{}, resolver: resolver}
	for _, raw := range issuerHosts {
		host := strings.ToLower(strings.TrimSpace(raw))
		if host == "" || strings.ContainsAny(host, "/:@?#") || net.ParseIP(host) != nil {
			return nil, ErrIdentityInvalid
		}
		policy.issuerHosts[host] = struct{}{}
	}
	for _, raw := range callbackOrigins {
		origin, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
		if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" || (origin.Scheme != "https" && origin.Scheme != "http") {
			return nil, ErrIdentityInvalid
		}
		if origin.Scheme == "http" && !loopbackCallbackHost(origin.Hostname()) {
			return nil, ErrIdentityInvalid
		}
		policy.callbacks[origin.String()+"/oidc/callback"] = struct{}{}
	}
	return policy, nil
}

type providerEndpoint struct {
	URL *url.URL
	IPs []net.IP
}

// ValidateDraft checks bounded fields, the deployment allowlist and current DNS
// answers before any configuration or secret reference is persisted.
func (p *ProviderURLPolicy) ValidateDraft(ctx context.Context, draft ProviderDraft) error {
	if ctx == nil || !validIdentityID(draft.ID) || draft.Protocol != "oidc" || !validIdentityText(draft.DisplayName, 160) || !validIdentityText(draft.ClientID, 256) || draft.ExpectedVersion > 1<<62 || !validCorrelation(draft.CorrelationID) || draft.CreatedAt.IsZero() {
		return ErrIdentityInvalid
	}
	if _, err := p.resolve(ctx, draft.IssuerURL); err != nil {
		return err
	}
	callback, err := url.Parse(draft.CallbackURL)
	if err != nil || callback.User != nil || callback.RawQuery != "" || callback.Fragment != "" {
		return ErrIdentityUnsafeURL
	}
	if _, ok := p.callbacks[callback.String()]; !ok {
		return ErrIdentityUnsafeURL
	}
	return nil
}

func (p *ProviderURLPolicy) resolve(ctx context.Context, raw string) (providerEndpoint, error) {
	if p == nil || p.resolver == nil || ctx == nil || raw != strings.TrimSpace(raw) || len(raw) > 2048 || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return providerEndpoint{}, ErrIdentityUnsafeURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return providerEndpoint{}, ErrIdentityUnsafeURL
	}
	host := strings.ToLower(parsed.Hostname())
	if _, ok := p.issuerHosts[host]; !ok {
		return providerEndpoint{}, ErrIdentityUnsafeURL
	}
	addresses, err := p.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return providerEndpoint{}, ErrIdentityUnsafeURL
	}
	ips := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if !publicIdentityEndpointIP(address.IP) {
			return providerEndpoint{}, ErrIdentityUnsafeURL
		}
		ips = append(ips, address.IP)
	}
	return providerEndpoint{URL: parsed, IPs: ips}, nil
}

// ProviderValidator validates OIDC discovery metadata before activation.
type ProviderValidator interface {
	Validate(context.Context, ProviderRevision) (ProviderValidation, error)
}

// OIDCDiscoveryValidator performs a redirect-free, size-bounded and DNS-pinned
// discovery request. Metadata endpoints must also belong to the issuer allowlist.
type OIDCDiscoveryValidator struct {
	policy  *ProviderURLPolicy
	timeout time.Duration
}

func NewOIDCDiscoveryValidator(policy *ProviderURLPolicy, timeout time.Duration) (*OIDCDiscoveryValidator, error) {
	if policy == nil || timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return nil, ErrIdentityInvalid
	}
	return &OIDCDiscoveryValidator{policy: policy, timeout: timeout}, nil
}

func (v *OIDCDiscoveryValidator) Validate(ctx context.Context, revision ProviderRevision) (ProviderValidation, error) {
	if ctx == nil || v == nil || v.policy == nil || revision.Protocol != "oidc" || revision.Revision == 0 {
		return ProviderValidation{}, ErrIdentityInvalid
	}
	endpoint, err := v.policy.resolve(ctx, revision.IssuerURL)
	if err != nil {
		return ProviderValidation{}, err
	}
	discovery := *endpoint.URL
	discovery.Path = strings.TrimRight(discovery.Path, "/") + "/.well-known/openid-configuration"
	transport := &http.Transport{
		Proxy:             nil,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.URL.Hostname()},
		DialContext:       pinnedDialer(endpoint.IPs, "443"),
		ForceAttemptHTTP2: true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: v.timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, discovery.String(), nil)
	if err != nil {
		return ProviderValidation{}, ErrIdentityInvalid
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return ProviderValidation{}, errors.New("identity provider settings: discovery unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return ProviderValidation{}, errors.New("identity provider settings: discovery unavailable")
	}
	var metadata struct {
		Issuer           string `json:"issuer"`
		AuthorizationURL string `json:"authorization_endpoint"`
		TokenURL         string `json:"token_endpoint"`
		JWKSURL          string `json:"jwks_uri"`
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if readErr != nil || len(payload) > 64<<10 || json.Unmarshal(payload, &metadata) != nil || strings.TrimRight(metadata.Issuer, "/") != strings.TrimRight(revision.IssuerURL, "/") {
		return ProviderValidation{}, errors.New("identity provider settings: invalid discovery metadata")
	}
	for _, raw := range []string{metadata.AuthorizationURL, metadata.TokenURL, metadata.JWKSURL} {
		if _, err := v.policy.resolve(ctx, raw); err != nil {
			return ProviderValidation{}, ErrIdentityUnsafeURL
		}
	}
	canonical, _ := json.Marshal(metadata)
	digest := sha256.Sum256(canonical)
	return ProviderValidation{Status: "valid", ReasonCode: "validated", MetadataDigest: hex.EncodeToString(digest[:]), Issuer: metadata.Issuer, AuthorizationURL: metadata.AuthorizationURL, TokenURL: metadata.TokenURL, JWKSURL: metadata.JWKSURL}, nil
}

func pinnedDialer(ips []net.IP, port string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var last error
		for _, ip := range ips {
			connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return connection, nil
			}
			last = err
		}
		return nil, last
	}
}

func publicIdentityEndpointIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	for _, network := range identityBlockedCIDRs {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

var identityBlockedCIDRs = mustIdentityCIDRs([]string{
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
	"2001:db8::/32",
})

func mustIdentityCIDRs(values []string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic("invalid identity endpoint CIDR policy")
		}
		result = append(result, network)
	}
	return result
}

func loopbackCallbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validIdentityID(value string) bool {
	if value == "" || len(value) > 64 || value != strings.ToLower(strings.TrimSpace(value)) {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || (index > 0 && (character == '.' || character == '_' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

func validIdentityText(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validCorrelation(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 128 && strings.IndexFunc(value, unicode.IsControl) < 0
}
