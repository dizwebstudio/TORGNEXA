package api

import (
	"context"
	"crypto/sha256"
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
	verifier     *oidcJWTVerifier
	profiles     *oidcProfileCache
	metrics      *oidcHotPathRecorder
}

type oidcEgressBoundary struct {
	issuerHost   string
	userinfoHost string
	jwksHost     string
}

type oidcClaims struct {
	Issuer      string       `json:"iss"`
	Subject     string       `json:"sub"`
	Authorized  string       `json:"azp"`
	Audience    oidcAudience `json:"aud"`
	ExpiresAt   int64        `json:"exp"`
	NotBefore   int64        `json:"nbf"`
	IssuedAt    int64        `json:"iat"`
	AuthTime    int64        `json:"auth_time"`
	SessionID   string       `json:"sid"`
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
			host = strings.ToLower(strings.Trim(host, "[]"))
			if splitErr != nil || (host != boundary.userinfoHost && host != boundary.jwksHost) {
				return nil, fmt.Errorf("oidc identity egress denied")
			}
			return (&net.Dialer{Timeout: cfg.OIDC.RequestTimeout}).DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	client := &http.Client{
		Transport:     transport,
		Timeout:       cfg.OIDC.RequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	metrics := &oidcHotPathRecorder{}
	authenticator := &oidcAuthenticator{cfg: cfg.OIDC, userinfoHost: boundary.issuerHost, environment: cfg.Environment, sessions: sessions, client: client, metrics: metrics}
	authenticator.verifier = &oidcJWTVerifier{config: cfg.OIDC, cache: newIssuerJWKSCache(client, cfg.OIDC.JWKSURL, boundary.issuerHost, metrics), now: time.Now}
	authenticator.profiles = newOIDCProfileCache(metrics)
	if memberships == nil {
		return nil, nil, nil, ErrSecurityCompositionInvalid
	}
	membership := newMembershipCache(memberships, metrics)
	resolver := claimTenantResolver{environment: cfg.Environment, organizationID: cfg.OIDC.DevelopmentOrganization, workspaceID: cfg.OIDC.DevelopmentWorkspace, memberships: memberships, cache: membership}
	return authenticator, resolver, roleAuthorizer{memberships: memberships, cache: membership}, nil
}

func validateOIDCConfig(cfg config.Config) (oidcEgressBoundary, error) {
	if cfg.OIDC.Issuer == "" || cfg.OIDC.JWKSURL == "" || cfg.OIDC.UserInfoURL == "" || cfg.OIDC.ClientID == "" || cfg.OIDC.Audience == "" || cfg.OIDC.RequestTimeout <= 0 {
		return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
	}
	parsedURLs := make([]*url.URL, 0, 3)
	for _, raw := range []string{cfg.OIDC.Issuer, cfg.OIDC.UserInfoURL, cfg.OIDC.JWKSURL} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
		}
		if parsed.Scheme != "https" && (cfg.Environment != config.EnvironmentDevelopment || parsed.Scheme != "http") {
			return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
		}
		parsedURLs = append(parsedURLs, parsed)
	}
	issuerHost := strings.ToLower(parsedURLs[0].Hostname())
	userinfoHost := strings.ToLower(parsedURLs[1].Hostname())
	jwksHost := strings.ToLower(parsedURLs[2].Hostname())
	if cfg.Environment != config.EnvironmentDevelopment &&
		(!sameOIDCOrigin(parsedURLs[0], parsedURLs[1]) || !sameOIDCOrigin(parsedURLs[0], parsedURLs[2])) {
		return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
	}
	if cfg.Environment == config.EnvironmentDevelopment &&
		((userinfoHost != issuerHost && !allowedDevelopmentOIDCHost(userinfoHost)) ||
			(jwksHost != issuerHost && !allowedDevelopmentOIDCHost(jwksHost))) {
		return oidcEgressBoundary{}, ErrSecurityCompositionInvalid
	}
	return oidcEgressBoundary{issuerHost: parsedURLs[0].Host, userinfoHost: userinfoHost, jwksHost: jwksHost}, nil
}

func sameOIDCOrigin(first, second *url.URL) bool {
	return strings.EqualFold(first.Scheme, second.Scheme) &&
		strings.EqualFold(first.Hostname(), second.Hostname()) &&
		effectiveOIDCPort(first) == effectiveOIDCPort(second)
}

func effectiveOIDCPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func allowedDevelopmentOIDCHost(host string) bool {
	if host == "keycloak" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (authenticator *oidcAuthenticator) Authenticate(ctx context.Context, request *http.Request) (Principal, error) {
	if authenticator == nil || authenticator.client == nil || authenticator.verifier == nil || request == nil {
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
	claims, err := authenticator.verifier.verify(ctx, token)
	if errors.Is(err, errOIDCJWKSUnavailable) {
		return Principal{}, ErrAuthenticationUnavailable
	}
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	subjectRef := identityReference(claims.Issuer, claims.Subject)
	info, _, err := authenticator.profiles.resolve(ctx, subjectRef, func() (userInfoClaims, bool, error) {
		return authenticator.fetchUserInfo(ctx, token, claims.Subject)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Principal{}, ErrAuthenticationUnavailable
		}
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
	observation := securitysettings.Observation{EventID: newApprovalID(), SessionRef: sessionRef, SubjectRef: subjectRef, ClientKind: oidcClientKind(request.UserAgent()), AuthenticatedAt: authenticatedAt, ExpiresAt: time.Unix(claims.ExpiresAt, 0).UTC(), ObservedAt: time.Now().UTC()}
	written := true
	if detailed, ok := authenticator.sessions.(securitysettings.DetailedStore); ok {
		result, observeErr := detailed.ObserveDetailed(ctx, scope, observation)
		err = observeErr
		written = result.Created || result.TimestampsUpdated
	} else {
		err = authenticator.sessions.Observe(ctx, scope, observation)
	}
	authenticator.metrics.recordSessionDBCall(ctx, written && err == nil, !written && err == nil)
	if err != nil {
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

// OIDCHotPathMetrics returns the bounded process-local authentication snapshot.
func (authenticator *oidcAuthenticator) OIDCHotPathMetrics() OIDCHotPathMetrics {
	if authenticator == nil {
		return OIDCHotPathMetrics{}
	}
	return authenticator.metrics.snapshot()
}

func (authenticator *oidcAuthenticator) beginOIDCHotPath(ctx context.Context) (context.Context, func(bool, bool)) {
	return authenticator.metrics.begin(ctx)
}

func (authenticator *oidcAuthenticator) fetchUserInfo(ctx context.Context, token, subject string) (userInfoClaims, bool, error) {
	authenticator.metrics.recordUserInfoHTTPCall(ctx)
	// #nosec G704 -- UserInfoURL is accepted only by validateOIDCConfig and the transport enforces the exact allowlisted host.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, authenticator.cfg.UserInfoURL, nil)
	if err != nil {
		return userInfoClaims{}, false, nil
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Host = authenticator.userinfoHost
	// #nosec G704 -- redirects are disabled and the client transport has an exact-host egress boundary.
	response, err := authenticator.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return userInfoClaims{}, false, ctx.Err()
		}
		return userInfoClaims{}, false, nil
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxUserInfoResponse))
		return userInfoClaims{}, false, nil
	}
	var info userInfoClaims
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxUserInfoResponse))
	if err := decoder.Decode(&info); err != nil || info.Subject == "" || info.Subject != subject {
		return userInfoClaims{}, false, nil
	}
	return info, true, nil
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

type claimTenantResolver struct {
	environment    config.Environment
	organizationID string
	workspaceID    string
	memberships    workspaceMembershipStore
	cache          *membershipCache
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
	var member tenancyrepo.Member
	if resolver.cache != nil && !membershipCacheBypassed(ctx) {
		member, err = resolver.cache.resolve(ctx, scope, identity)
	} else {
		if resolver.cache != nil {
			resolver.cache.metrics.recordMembershipDBCall(ctx)
		}
		member, err = resolver.memberships.ResolveActiveMember(ctx, scope, identity)
	}
	if err != nil {
		if resolver.environment != config.EnvironmentDevelopment || !principalHasRole(principal, "admin") {
			return tenancy.Scope{}, ErrUnauthorized
		}
		email := principal.Email
		if email == "" {
			email = "dev-" + principal.SubjectRef[:16] + "@local.invalid"
		}
		member, err = resolver.memberships.BootstrapDevelopmentAdministrator(ctx, scope, principal.SubjectRef, email)
		if err != nil {
			return tenancy.Scope{}, ErrUnauthorized
		}
		if resolver.cache != nil {
			resolver.cache.put(scope, principal.SubjectRef, member)
		}
	}
	storeResolvedMembership(ctx, scope, principal.SubjectRef, member)
	return scope, nil
}

type roleAuthorizer struct {
	memberships workspaceMembershipStore
	cache       *membershipCache
}

func (authorizer roleAuthorizer) Authorize(ctx context.Context, principal Principal, scope tenancy.Scope, permission string) error {
	if authorizer.memberships == nil {
		return ErrUnauthorized
	}
	member, ok := loadResolvedMembership(ctx, scope, principal.SubjectRef)
	if !ok {
		var err error
		if authorizer.cache != nil {
			member, err = authorizer.cache.resolve(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: principal.SubjectRef})
		} else {
			member, err = authorizer.memberships.ResolveActiveMember(ctx, scope, tenancyrepo.MemberIdentity{SubjectRef: principal.SubjectRef})
		}
		if err != nil {
			return ErrUnauthorized
		}
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
