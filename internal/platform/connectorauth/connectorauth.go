// Package connectorauth implements host-owned, provider-neutral connector OAuth
// and remote connection validation. Provider packages never receive HTTP or
// secret-storage authority from this package.
package connectorauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	CallbackPath       = "/oauth/connectors/callback"
	OAuthSessionTTL    = 10 * time.Minute
	MaxRemoteBodyBytes = 64 << 10
)

var (
	ErrInvalid              = errors.New("connector auth: invalid value")
	ErrCallbackDenied       = errors.New("connector auth: callback denied")
	ErrOAuthUnsupported     = errors.New("connector auth: oauth flow unsupported")
	ErrOAuthRefreshRejected = errors.New("connector auth: oauth refresh rejected")
	ErrRemoteUnsafe         = errors.New("connector auth: remote endpoint denied")
	ErrRemoteUnavailable    = errors.New("connector auth: remote endpoint unavailable")
)

// OAuthClient is the encrypted browser-submitted client registration.
type OAuthClient struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func ParseOAuthClient(material []byte) (OAuthClient, error) {
	var value OAuthClient
	decoder := json.NewDecoder(bytes.NewReader(material))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || decoder.Decode(&struct{}{}) != io.EOF || !safeSecretPart(value.ClientID, 512) || !safeSecretPart(value.ClientSecret, 4096) {
		return OAuthClient{}, ErrInvalid
	}
	return value, nil
}

// TokenBundle is encrypted after a successful callback and is never exposed by API views.
type TokenBundle struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// PendingMaterial is encrypted separately from the session row. The database
// stores only StateDigest, so database disclosure cannot recover a valid state.
type PendingMaterial struct {
	State        string `json:"state"`
	CodeVerifier string `json:"code_verifier"`
}

// CallbackPolicy expands configured application origins to one exact callback path.
type CallbackPolicy struct{ allowed map[string]struct{} }

func NewCallbackPolicy(origins []string) (*CallbackPolicy, error) {
	policy := &CallbackPolicy{allowed: map[string]struct{}{}}
	for _, raw := range origins {
		parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
		if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return nil, ErrInvalid
		}
		if parsed.Scheme == "http" && !loopback(parsed.Hostname()) {
			return nil, ErrInvalid
		}
		policy.allowed[parsed.String()+CallbackPath] = struct{}{}
	}
	return policy, nil
}

func (policy *CallbackPolicy) Validate(raw string) error {
	parsed, err := url.Parse(raw)
	if policy == nil || err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ErrCallbackDenied
	}
	if _, ok := policy.allowed[parsed.String()]; !ok {
		return ErrCallbackDenied
	}
	return nil
}

// NewPKCE creates opaque state and an RFC 7636 S256 verifier/challenge pair.
func NewPKCE() (PendingMaterial, string, error) {
	state, err := randomURLToken(32)
	if err != nil {
		return PendingMaterial{}, "", err
	}
	verifier, err := randomURLToken(48)
	if err != nil {
		return PendingMaterial{}, "", err
	}
	digest := sha256.Sum256([]byte(verifier))
	return PendingMaterial{State: state, CodeVerifier: verifier}, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func StateDigest(state string) (string, error) {
	if len(state) < 32 || len(state) > 256 || strings.ContainsAny(state, "\r\n\t ") {
		return "", ErrInvalid
	}
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:]), nil
}

func AuthorizationURL(configuration sdk.OAuth2Configuration, clientID, callbackURL, state, challenge string) (string, error) {
	if configuration.Validate() != nil || configuration.GrantType != "authorization_code" || !safeSecretPart(clientID, 512) || state == "" || challenge == "" {
		return "", ErrInvalid
	}
	parsed, err := url.Parse(configuration.AuthorizationURL)
	if err != nil {
		return "", ErrInvalid
	}
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", clientID)
	query.Set("redirect_uri", callbackURL)
	query.Set("state", state)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	if len(configuration.Scopes) > 0 {
		query.Set("scope", strings.Join(configuration.Scopes, " "))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// OAuthConfiguration returns the one authorization requirement. Multiple
// OAuth protocols in a single manifest are rejected as ambiguous.
func OAuthConfiguration(manifest sdk.Manifest) (sdk.OAuth2Configuration, error) {
	var found *sdk.OAuth2Configuration
	for _, requirement := range manifest.Auth {
		if requirement.Kind != sdk.AuthOAuth2 {
			continue
		}
		if found != nil || requirement.OAuth2 == nil {
			return sdk.OAuth2Configuration{}, ErrOAuthUnsupported
		}
		copyValue := *requirement.OAuth2
		found = &copyValue
	}
	if found == nil {
		return sdk.OAuth2Configuration{}, ErrOAuthUnsupported
	}
	return *found, nil
}

// HTTPExchange performs a redirect-free, DNS-pinned OAuth token exchange.
func HTTPExchange(ctx context.Context, configuration sdk.OAuth2Configuration, client OAuthClient, code, callbackURL, verifier string, timeout time.Duration) ([]byte, error) {
	if configuration.Validate() != nil || !safeSecretPart(client.ClientID, 512) || !safeSecretPart(client.ClientSecret, 4096) || timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return nil, ErrInvalid
	}
	form := url.Values{"client_id": {client.ClientID}, "client_secret": {client.ClientSecret}, "grant_type": {configuration.GrantType}}
	if len(configuration.Scopes) > 0 {
		form.Set("scope", strings.Join(configuration.Scopes, " "))
	}
	if configuration.GrantType == "authorization_code" {
		if !safeSecretPart(code, 8192) || !safeSecretPart(verifier, 256) {
			return nil, ErrInvalid
		}
		form.Set("code", code)
		form.Set("redirect_uri", callbackURL)
		form.Set("code_verifier", verifier)
	}
	response, err := safeRequest(ctx, http.MethodPost, configuration.TokenURL, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, []byte(form.Encode()), timeout)
	if err != nil || response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, ErrRemoteUnavailable
	}
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(response.Body, &token) != nil || !safeSecretPart(token.AccessToken, 32768) || token.ExpiresIn < 0 || token.ExpiresIn > 10*365*24*60*60 {
		return nil, ErrRemoteUnavailable
	}
	if token.TokenType == "" {
		token.TokenType = "Bearer"
	}
	bundle := TokenBundle{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, TokenType: token.TokenType, ClientID: client.ClientID, ClientSecret: client.ClientSecret}
	if token.ExpiresIn > 0 {
		bundle.ExpiresAt = time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return json.Marshal(bundle)
}

// HTTPRefresh exchanges one encrypted authorization-code refresh token and
// returns a complete replacement bundle. If the provider does not rotate the
// refresh token, the previous value is retained.
func HTTPRefresh(ctx context.Context, configuration sdk.OAuth2Configuration, current TokenBundle, timeout time.Duration, now time.Time) ([]byte, error) {
	if configuration.Validate() != nil || configuration.GrantType != "authorization_code" || !safeSecretPart(current.RefreshToken, 32768) || !safeSecretPart(current.ClientID, 512) || !safeSecretPart(current.ClientSecret, 4096) || timeout < 100*time.Millisecond || timeout > 30*time.Second || now.IsZero() {
		return nil, ErrInvalid
	}
	form := oauthRefreshForm(current)
	response, err := safeRequest(ctx, http.MethodPost, configuration.TokenURL, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"}, []byte(form.Encode()), timeout)
	if err != nil {
		return nil, ErrRemoteUnavailable
	}
	if refreshRejectedStatus(response.StatusCode) {
		return nil, ErrOAuthRefreshRejected
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, ErrRemoteUnavailable
	}
	return refreshedTokenBundle(current, response.Body, now)
}

func refreshRejectedStatus(status int) bool {
	return status == http.StatusBadRequest || status == http.StatusUnauthorized || status == http.StatusForbidden
}

func oauthRefreshForm(current TokenBundle) url.Values {
	return url.Values{
		"client_id":     {current.ClientID},
		"client_secret": {current.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {current.RefreshToken},
	}
}

func refreshedTokenBundle(current TokenBundle, body []byte, now time.Time) ([]byte, error) {
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &token) != nil || !safeSecretPart(token.AccessToken, 32768) || token.ExpiresIn < 0 || token.ExpiresIn > 10*365*24*60*60 {
		return nil, ErrRemoteUnavailable
	}
	if token.RefreshToken == "" {
		token.RefreshToken = current.RefreshToken
	}
	if !safeSecretPart(token.RefreshToken, 32768) {
		return nil, ErrRemoteUnavailable
	}
	if token.TokenType == "" {
		token.TokenType = current.TokenType
	}
	if token.TokenType == "" {
		token.TokenType = "Bearer"
	}
	updated := TokenBundle{AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, TokenType: token.TokenType, ClientID: current.ClientID, ClientSecret: current.ClientSecret}
	if token.ExpiresIn > 0 {
		updated.ExpiresAt = now.UTC().Add(time.Duration(token.ExpiresIn) * time.Second).Format(time.RFC3339)
	}
	return json.Marshal(updated)
}

type remoteResponse struct {
	StatusCode int
	Body       []byte
}

// Check performs the manifest-declared authenticated probe and returns only
// normalized safe health metadata.
func Check(ctx context.Context, manifest sdk.Manifest, material []byte, now time.Time) sdk.Health {
	checkedAt := now.UTC()
	if manifest.ConnectionTest == nil {
		return sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "remote_check_not_configured", CheckedAt: checkedAt}
	}
	fields, err := credentialFields(material)
	if err != nil && manifest.RequiresSecret() {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "credentials_invalid", CheckedAt: checkedAt}
	}
	headers := make(map[string]string, len(manifest.ConnectionTest.Headers))
	for name, template := range manifest.ConnectionTest.Headers {
		value, expandErr := expand(template, fields)
		if expandErr != nil {
			return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "credentials_invalid", CheckedAt: checkedAt}
		}
		headers[name] = value
	}
	body, err := expand(manifest.ConnectionTest.Body, fields)
	if err != nil {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "credentials_invalid", CheckedAt: checkedAt}
	}
	timeout := time.Duration(manifest.RateLimit.RequestTimeoutMS) * time.Millisecond
	if timeout > 15*time.Second {
		timeout = 15 * time.Second
	}
	response, err := safeRequest(ctx, manifest.ConnectionTest.Method, manifest.ConnectionTest.URL, headers, []byte(body), timeout)
	if err != nil {
		return sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "remote_unavailable", CheckedAt: checkedAt}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "auth_rejected", CheckedAt: checkedAt}
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return sdk.Health{Status: sdk.HealthDegraded, ReasonCode: "rate_limited", CheckedAt: checkedAt}
	}
	if successStatus(response.StatusCode, manifest.ConnectionTest.SuccessStatuses) {
		return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: checkedAt}
	}
	return sdk.Health{Status: sdk.HealthUnavailable, ReasonCode: "remote_unavailable", CheckedAt: checkedAt}
}

func safeRequest(ctx context.Context, method, rawURL string, headers map[string]string, body []byte, timeout time.Duration) (remoteResponse, error) {
	if ctx == nil || timeout < 100*time.Millisecond || timeout > 30*time.Second {
		return remoteResponse{}, ErrInvalid
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	parsed, addresses, err := resolvePublic(bounded, rawURL)
	if err != nil {
		return remoteResponse{}, err
	}
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: parsed.Hostname()}, DialContext: pinnedDialer(addresses, "443"), ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(bounded, method, parsed.String(), bytes.NewReader(body))
	if err != nil {
		return remoteResponse{}, ErrInvalid
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return remoteResponse{}, ErrRemoteUnavailable
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxRemoteBodyBytes+1))
	if err != nil || len(payload) > MaxRemoteBodyBytes {
		return remoteResponse{}, ErrRemoteUnavailable
	}
	return remoteResponse{StatusCode: response.StatusCode, Body: payload}, nil
}

func resolvePublic(ctx context.Context, raw string) (*url.URL, []netip.Addr, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" {
		return nil, nil, ErrRemoteUnsafe
	}
	values, err := net.DefaultResolver.LookupNetIP(ctx, "ip", parsed.Hostname())
	if err != nil || len(values) == 0 {
		return nil, nil, ErrRemoteUnavailable
	}
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		value = value.Unmap()
		if !publicAddress(value) {
			return nil, nil, ErrRemoteUnsafe
		}
		result = append(result, value)
	}
	return parsed, result, nil
}

func pinnedDialer(addresses []netip.Addr, port string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		var last error
		for _, address := range addresses {
			connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address.String(), port))
			if err == nil {
				return connection, nil
			}
			last = err
		}
		return nil, last
	}
}

func publicAddress(value netip.Addr) bool {
	if !value.IsValid() || !value.IsGlobalUnicast() || value.IsPrivate() || value.IsLoopback() || value.IsLinkLocalUnicast() || value.IsMulticast() || value.IsUnspecified() {
		return false
	}
	for _, prefix := range []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32")} {
		if prefix.Contains(value) {
			return false
		}
	}
	return true
}

func credentialFields(material []byte) (map[string]string, error) {
	fields := map[string]string{"secret": string(material)}
	if len(material) == 0 {
		return fields, ErrInvalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(material, &object) == nil {
		for key, raw := range object {
			var value string
			if !validFieldName(key) || json.Unmarshal(raw, &value) != nil || !safeSecretPart(value, 32768) {
				continue
			}
			fields[key] = value
		}
	}
	lines := strings.Split(strings.TrimSpace(string(material)), "\n")
	for index, line := range lines {
		fields["line"+strconv.Itoa(index+1)] = strings.TrimSpace(line)
	}
	if username, ok := fields["username"]; ok {
		password, passwordOK := fields["password"]
		if passwordOK {
			fields["basic"] = base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		}
	}
	return fields, nil
}

func expand(template string, fields map[string]string) (string, error) {
	result := template
	for {
		start := strings.Index(result, "${")
		if start < 0 {
			return result, nil
		}
		end := strings.IndexByte(result[start+2:], '}')
		if end < 0 {
			return "", ErrInvalid
		}
		end += start + 2
		name := result[start+2 : end]
		value, ok := fields[name]
		if !ok {
			return "", ErrInvalid
		}
		result = result[:start] + value + result[end+1:]
	}
}

func successStatus(status int, allowed []int) bool {
	if len(allowed) == 0 {
		return status >= 200 && status <= 299
	}
	for _, value := range allowed {
		if status == value {
			return true
		}
	}
	return false
}

func randomURLToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := io.ReadFull(rand.Reader, buffer); err != nil {
		return "", fmt.Errorf("connector auth: random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func safeSecretPart(value string, max int) bool {
	return value != "" && len(value) <= max && strings.IndexFunc(value, func(character rune) bool { return character == 0 || character == '\r' || character == '\n' }) < 0
}

func validFieldName(value string) bool {
	if value == "" || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '_' {
			continue
		}
		return false
	}
	return true
}

func loopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
