package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/core/userprofile"
	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/tenancyrepo"
	"github.com/torgnexa/torgnexa/internal/platform/securitysettings"
)

const maxUserInfoResponse = 32 << 10

type oidcAuthenticator struct {
	cfg          config.OIDC
	client       *http.Client
	userinfoHost string
	environment  config.Environment
	sessions     securitysettings.Store
}

type oidcEgressBoundary struct {
	issuerHost   string
	userinfoHost string
}

type oidcClaims struct {
	Issuer      string `json:"iss"`
	Subject     string `json:"sub"`
	Authorized  string `json:"azp"`
	ExpiresAt   int64  `json:"exp"`
	IssuedAt    int64  `json:"iat"`
	AuthTime    int64  `json:"auth_time"`
	SessionID   string `json:"sid"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
	OrganizationID string `json:"organization_id"`
	WorkspaceID    string `json:"workspace_id"`
	Username       string `json:"preferred_username"`
	Email          string `json:"email"`
	GivenName      string `json:"given_name"`
	FamilyName     string `json:"family_name"`
	PictureURL     string `json:"picture"`
	Birthdate      string `json:"birthdate"`
	JobTitle       string `json:"job_title"`
	Position       string `json:"position"`
	Title          string `json:"title"`
	Department     string `json:"department"`
	PhoneNumber    string `json:"phone_number"`
}

type userInfoClaims struct {
	Subject       string `json:"sub"`
	Username      string `json:"preferred_username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	PictureURL    string `json:"picture"`
	Birthdate     string `json:"birthdate"`
	JobTitle      string `json:"job_title"`
	Position      string `json:"position"`
	Title         string `json:"title"`
	Department    string `json:"department"`
	PhoneNumber   string `json:"phone_number"`
}

type workspaceMembershipStore interface {
	ResolveActiveMember(context.Context, tenancy.Scope, tenancyrepo.MemberIdentity) (tenancyrepo.Member, error)
	BootstrapDevelopmentAdministrator(context.Context, tenancy.Scope, string, string) (tenancyrepo.Member, error)
}

func newOIDCSecurity(cfg config.Config, sessions securitysettings.Store, memberships workspaceMembershipStore) (Authenticator, TenantResolver, Authorizer, error) {
	boundary, err := validateOIDCConfig(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, splitErr := net.SplitHostPort(address)
			if splitErr != nil || !strings.EqualFold(strings.Trim(host, "[]"), boundary.userinfoHost) {
				return nil, fmt.Errorf("oidc userinfo egress denied")
			}
			return (&net.Dialer{Timeout: cfg.OIDC.RequestTimeout}).DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	authenticator := &oidcAuthenticator{cfg: cfg.OIDC, userinfoHost: boundary.issuerHost, environment: cfg.Environment, sessions: sessions, client: &http.Client{
		Transport:     transport,
		Timeout:       cfg.OIDC.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if memberships == nil {
		return nil, nil, nil, ErrSecurityCompositionInvalid
	}
	resolver := claimTenantResolver{environment: cfg.Environment, organizationID: cfg.OIDC.DevelopmentOrganization, workspaceID: cfg.OIDC.DevelopmentWorkspace, memberships: memberships}
	return authenticator, resolver, roleAuthorizer{memberships: memberships}, nil
}

func validateOIDCConfig(cfg config.Config) (oidcEgressBoundary, error) {
	if cfg.OIDC.Issuer == "" || cfg.OIDC.UserInfoURL == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.RequestTimeout <= 0 {
		return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
	}
	parsedURLs := make([]*url.URL, 0, 2)
	for _, raw := range []string{cfg.OIDC.Issuer, cfg.OIDC.UserInfoURL} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
		}
		if parsed.Scheme != "https" && cfg.Environment != config.EnvironmentDevelopment {
			return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
		}
		parsedURLs = append(parsedURLs, parsed)
	}
	issuerHost := strings.ToLower(parsedURLs[0].Hostname())
	userinfoHost := strings.ToLower(parsedURLs[1].Hostname())
	if userinfoHost != issuerHost && !(cfg.Environment == config.EnvironmentDevelopment && allowedDevelopmentOIDCHost(userinfoHost)) {
		return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
	}
	return oidcEgressBoundary{issuerHost: parsedURLs[0].Host, userinfoHost: userinfoHost}, nil
}

func allowedDevelopmentOIDCHost(host string) bool {
	if host == "keycloak" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (authenticator *oidcAuthenticator) Authenticate(ctx context.Context, request *http.Request) (Principal, error) {
	if authenticator == nil || authenticator.client == nil || request == nil {
		return Principal{}, ErrUnauthenticated
	}
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || strings.Count(header, " ") != 1 {
		return Principal{}, ErrUnauthenticated
	}
	token := strings.TrimPrefix(header, "Bearer ")
	if len(token) < 32 || len(token) > 16_384 || strings.ContainsAny(token, "\r\n\t ") {
		return Principal{}, ErrUnauthenticated
	}
	// #nosec G704 -- UserInfoURL is accepted only by validateOIDCConfig and the transport enforces the exact allowlisted host.
	userinfoRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, authenticator.cfg.UserInfoURL, nil)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	userinfoRequest.Header.Set("Authorization", "Bearer "+token)
	// Community deployments use an internal backchannel address while Keycloak
	// validates requests against its public issuer hostname.
	userinfoRequest.Host = authenticator.userinfoHost
	// #nosec G704 -- redirects are disabled and the client transport has an exact-host egress boundary.
	response, err := authenticator.client.Do(userinfoRequest)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxUserInfoResponse))
		return Principal{}, ErrUnauthenticated
	}
	var info userInfoClaims
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxUserInfoResponse))
	if err := decoder.Decode(&info); err != nil || info.Subject == "" {
		return Principal{}, ErrUnauthenticated
	}
	claims, err := decodeValidatedTokenClaims(token)
	if err != nil || claims.Subject != info.Subject || claims.Issuer != authenticator.cfg.Issuer || claims.Authorized != authenticator.cfg.ClientID || claims.ExpiresAt <= time.Now().Unix() {
		return Principal{}, ErrUnauthenticated
	}
	roles := make([]string, 0, len(claims.RealmAccess.Roles))
	for _, role := range claims.RealmAccess.Roles {
		if role == "admin" || role == "manager" || role == "operator" || role == "viewer" {
			roles = append(roles, role)
		}
	}
	sessionSeed := strings.TrimSpace(claims.SessionID)
	if sessionSeed == "" && claims.IssuedAt > 0 {
		sessionSeed = fmt.Sprintf("issued:%d", claims.IssuedAt)
	}
	if sessionSeed == "" {
		return Principal{}, ErrUnauthenticated
	}
	sessionRef := identityReference(claims.Issuer, claims.Subject+"\x00"+sessionSeed)
	subjectRef := identityReference(claims.Issuer, claims.Subject)
	organizationID, workspaceID := claims.OrganizationID, claims.WorkspaceID
	if authenticator.environment == config.EnvironmentDevelopment && organizationID == "" && workspaceID == "" {
		organizationID, workspaceID = authenticator.cfg.DevelopmentOrganization, authenticator.cfg.DevelopmentWorkspace
	}
	scope, scopeErr := tenancy.ParseScope(organizationID, workspaceID)
	if scopeErr != nil {
		return Principal{}, ErrUnauthenticated
	}
	authenticatedAt := time.Unix(claims.AuthTime, 0).UTC()
	if claims.AuthTime <= 0 {
		authenticatedAt = time.Unix(claims.IssuedAt, 0).UTC()
	}
	if claims.IssuedAt <= 0 || authenticatedAt.IsZero() {
		return Principal{}, ErrUnauthenticated
	}
	if authenticator.sessions == nil {
		return Principal{}, ErrAuthenticationUnavailable
	}
	if err := authenticator.sessions.Observe(ctx, scope, securitysettings.Observation{EventID: newApprovalID(), SessionRef: sessionRef, SubjectRef: subjectRef, ClientKind: oidcClientKind(request.UserAgent()), AuthenticatedAt: authenticatedAt, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(), ObservedAt: time.Now().UTC()}); err != nil {
		if errors.Is(err, securitysettings.ErrSessionRevoked) || errors.Is(err, securitysettings.ErrInvalid) {
			return Principal{}, ErrUnauthenticated
		}
		// Do not expose a DB error or challenge a valid credential when the
		// authoritative session check could not complete. Access stays denied.
		return Principal{}, ErrAuthenticationUnavailable
	}
	profile := profileFromOIDCClaims(claims, info, subjectRef)
	return Principal{Issuer: claims.Issuer, Subject: claims.Subject, SessionRef: sessionRef, SubjectRef: subjectRef, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(), Email: profile.Email, VerifiedEmail: verifiedUserInfoEmail(info), Profile: profile, Roles: roles, OrganizationID: claims.OrganizationID, WorkspaceID: claims.WorkspaceID}, nil
}

// Verification belongs to the email in this authenticated UserInfo response.
// Never combine its flag with profile/token fallbacks or trust a decoded token
// claim as independent invitation evidence. Missing/null/false means no proof.
func verifiedUserInfoEmail(info userInfoClaims) string {
	if !info.EmailVerified {
		return ""
	}
	email := profileEmailClaim(info.Email, "")
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return ""
	}
	return email
}

func profileFromOIDCClaims(claims oidcClaims, info userInfoClaims, subjectRef string) userprofile.Identity {
	profile := userprofile.Identity{
		SubjectRef:  subjectRef,
		Username:    firstProfileClaim(info.Username, claims.Username, 128),
		Email:       profileEmailClaim(info.Email, claims.Email),
		GivenName:   firstProfileClaim(info.GivenName, claims.GivenName, 160),
		FamilyName:  firstProfileClaim(info.FamilyName, claims.FamilyName, 160),
		PictureURL:  profilePictureClaim(info.PictureURL, claims.PictureURL),
		Birthdate:   profileBirthdateClaim(info.Birthdate, claims.Birthdate),
		JobTitle:    profileJobTitle(claims, info),
		Department:  firstProfileClaim(info.Department, claims.Department, 160),
		PhoneNumber: firstProfileClaim(info.PhoneNumber, claims.PhoneNumber, 64),
	}
	if !profile.Valid() {
		// An invalid optional claim must not invalidate an otherwise valid OIDC
		// session. Each optional value is independently bounded above.
		profile = userprofile.Identity{
			SubjectRef:  subjectRef,
			Username:    safeProfileClaim(profile.Username, 128),
			Email:       profileEmailClaim(profile.Email, ""),
			GivenName:   safeProfileClaim(profile.GivenName, 160),
			FamilyName:  safeProfileClaim(profile.FamilyName, 160),
			PictureURL:  profilePictureClaim(profile.PictureURL, ""),
			Birthdate:   profileBirthdateClaim(profile.Birthdate, ""),
			JobTitle:    safeProfileClaim(profile.JobTitle, 160),
			Department:  safeProfileClaim(profile.Department, 160),
			PhoneNumber: safeProfileClaim(profile.PhoneNumber, 64),
		}
	}
	return profile
}

func firstProfileClaim(primary, fallback string, maximum int) string {
	if value := safeProfileClaim(primary, maximum); value != "" {
		return value
	}
	return safeProfileClaim(fallback, maximum)
}

func profileEmailClaim(primary, fallback string) string {
	for _, value := range []string{primary, fallback} {
		candidate := safeProfileClaim(value, 254)
		if candidate != "" && strings.Contains(candidate, "@") {
			return strings.ToLower(candidate)
		}
	}
	return ""
}

func profileJobTitle(claims oidcClaims, info userInfoClaims) string {
	for _, value := range []string{info.JobTitle, info.Position, info.Title, claims.JobTitle, claims.Position, claims.Title} {
		if candidate := safeProfileClaim(value, 160); candidate != "" {
			return candidate
		}
	}
	return ""
}

func safeProfileClaim(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return ""
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return ""
		}
	}
	return value
}

func profilePictureClaim(primary, fallback string) string {
	value := firstProfileClaim(primary, fallback, 2048)
	if strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "/") {
		return value
	}
	return ""
}

func profileBirthdateClaim(primary, fallback string) string {
	value := firstProfileClaim(primary, fallback, 32)
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return ""
	}
	return value
}

func identityReference(issuer, value string) string {
	digest := sha256.Sum256([]byte(issuer + "\x00" + value))
	return hex.EncodeToString(digest[:])
}

func oidcClientKind(userAgent string) string {
	value := strings.ToLower(userAgent)
	switch {
	case strings.Contains(value, "android") || strings.Contains(value, "iphone") || strings.Contains(value, "ipad"):
		return "mobile"
	case strings.Contains(value, "mozilla/"):
		return "browser"
	case strings.Contains(value, "curl/") || strings.Contains(value, "postman") || strings.Contains(value, "httpie"):
		return "api"
	default:
		return "unknown"
	}
}

func decodeValidatedTokenClaims(token string) (oidcClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return oidcClaims{}, ErrUnauthenticated
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) > maxUserInfoResponse {
		return oidcClaims{}, ErrUnauthenticated
	}
	var claims oidcClaims
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	if err := decoder.Decode(&claims); err != nil {
		return oidcClaims{}, ErrUnauthenticated
	}
	return claims, nil
}

type claimTenantResolver struct {
	environment    config.Environment
	organizationID string
	workspaceID    string
	memberships    workspaceMembershipStore
}

func (resolver claimTenantResolver) ResolveTenant(ctx context.Context, principal Principal, _ *http.Request) (tenancy.Scope, error) {
	organizationID, workspaceID := principal.OrganizationID, principal.WorkspaceID
	if resolver.environment == config.EnvironmentDevelopment && organizationID == "" && workspaceID == "" {
		organizationID, workspaceID = resolver.organizationID, resolver.workspaceID
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		return tenancy.Scope{}, ErrUnauthorized
	}
	if resolver.memberships == nil || principal.SubjectRef == "" {
		return tenancy.Scope{}, ErrUnauthorized
	}
	identity := tenancyrepo.MemberIdentity{SubjectRef: principal.SubjectRef, VerifiedEmail: principal.VerifiedEmail}
	if _, err := resolver.memberships.ResolveActiveMember(ctx, scope, identity); err != nil {
		if resolver.environment != config.EnvironmentDevelopment || !principalHasRole(principal, "admin") {
			return tenancy.Scope{}, ErrUnauthorized
		}
		email := principal.Email
		if email == "" {
			email = "dev-" + principal.SubjectRef[:16] + "@local.invalid"
		}
		if _, bootstrapErr := resolver.memberships.BootstrapDevelopmentAdministrator(ctx, scope, principal.SubjectRef, email); bootstrapErr != nil {
			return tenancy.Scope{}, ErrUnauthorized
		}
	}
	return scope, nil
}

type roleAuthorizer struct{ memberships workspaceMembershipStore }

func (authorizer roleAuthorizer) Authorize(ctx context.Context, principal Principal, scope tenancy.Scope, permission string) error {
	if authorizer.memberships == nil {
		return ErrUnauthorized
	}
	member, err := authorizer.memberships.ResolveActiveMember(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: principal.SubjectRef})
	if err != nil {
		return ErrUnauthorized
	}
	role := member.Role
	readPermission := permission == "connectors.accounts.read" || permission == "integrations.center.read" || permission == "products.read" || permission == "orders.read" || permission == "orders.returns.read" || permission == "stock.read" || permission == "wms.read" || permission == "compliance.read" || permission == "notifications.read" || permission == "reports.read" || permission == "finance.reports.read" || permission == "finance.reports.detail.read" || permission == "ads.read" || permission == "promotions.read" || permission == "audit.read" || permission == "sync.read" || permission == "approvals.read" || permission == "workflows.read" || permission == "settings.workspace.read" || permission == "settings.profile.read" || permission == "lineage.read" || permission == "counterparties.read" || permission == "entitlements.read" || permission == "webhooks.read" || permission == "settlements.read" || permission == "fx.read" || permission == "cloud.subscription.read" || permission == "plugins.read" || permission == "operations.realtime.read" || permission == "settings.ai_providers.read" || permission == "settings.mcp_accounts.read" || permission == "settings.ai_governance.read" || permission == "assistant.read" || permission == "customer_service.read" || permission == "procurement.suppliers.read" || permission == "procurement.offers.read" || permission == "procurement.purchase_orders.read" || permission == "procurement.reconciliation.read" || permission == "ecosystem.read"
	operatorPermission := permission == "orders.demo.write" || permission == "orders.status.write" || permission == "orders.returns.write" || permission == "payments.refunds.write" || permission == "social.post.edit" || permission == "social.post.delete" || permission == "ai.analyze" || permission == "connectors.replay.run" || permission == "profitability.scenarios.write" || permission == "finance.reports.write" || permission == "ads.manage" || permission == "promotions.manage" || permission == "workflows.write" || permission == "workflows.run" || permission == "assistant.ask" || permission == "assistant.preview" || permission == "assistant.feedback" || permission == "customer_service.write" || permission == "customer_service.reply" || permission == "customer_service.assign" || permission == "procurement.suppliers.write" || permission == "procurement.offers.write" || permission == "procurement.price_lists.write" || permission == "procurement.purchase_orders.write" || permission == "marketplace.operations.write" || permission == "wms.write" || permission == "counterparties.write" || permission == "ecosystem.onboarding.write" || permission == "ecosystem.partners.write"
	selfServicePermission := permission == "settings.profile.write"
	if role == "admin" || (operatorPermission && (role == "manager" || role == "operator")) || (selfServicePermission && (role == "manager" || role == "operator" || role == "viewer")) || (readPermission && (role == "manager" || role == "operator" || role == "viewer")) {
		return nil
	}
	return ErrUnauthorized
}

func principalHasRole(principal Principal, expected string) bool {
	for _, role := range principal.Roles {
		if role == expected {
			return true
		}
	}
	return false
}
