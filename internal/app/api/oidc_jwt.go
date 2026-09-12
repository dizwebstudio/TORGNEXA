package api

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

const (
	maxJWKSResponse       = 256 << 10
	defaultJWKSTTL        = 5 * time.Minute
	minimumJWKSTTL        = 30 * time.Second
	maximumJWKSTTL        = time.Hour
	maximumJWKSStale      = 15 * time.Minute
	minimumJWKSRefetchGap = time.Second
	jwtClockSkew          = 30 * time.Second
)

var (
	errOIDCJWTInvalid      = errors.New("oidc jwt invalid")
	errOIDCJWKSUnavailable = errors.New("oidc jwks unavailable")
)

type oidcAudience []string

func (audience *oidcAudience) UnmarshalJSON(raw []byte) error {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if !validOIDCIdentifier(one) {
			return errOIDCJWTInvalid
		}
		*audience = []string{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil || len(many) == 0 || len(many) > 16 {
		return errOIDCJWTInvalid
	}
	for _, value := range many {
		if !validOIDCIdentifier(value) {
			return errOIDCJWTInvalid
		}
	}
	*audience = many
	return nil
}

func (audience oidcAudience) contains(expected string) bool {
	for _, value := range audience {
		if value == expected {
			return true
		}
	}
	return false
}

type oidcJWTHeader struct {
	Algorithm string   `json:"alg"`
	KeyID     string   `json:"kid"`
	Type      string   `json:"typ"`
	Critical  []string `json:"crit"`
}

type oidcJWKDocument struct {
	Keys []oidcJWK `json:"keys"`
}

type oidcJWK struct {
	KeyType   string `json:"kty"`
	KeyID     string `json:"kid"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

type oidcSigningKey struct {
	key *rsa.PublicKey
}

type issuerJWKSCache struct {
	client     *http.Client
	url        string
	host       string
	now        func() time.Time
	metrics    *oidcHotPathRecorder
	mu         sync.Mutex
	keys       map[string]oidcSigningKey
	expiresAt  time.Time
	staleUntil time.Time
	refreshed  time.Time
	lastErr    error
	refreshing chan struct{}
}

func newIssuerJWKSCache(client *http.Client, endpoint, host string, metrics *oidcHotPathRecorder) *issuerJWKSCache {
	return &issuerJWKSCache{client: client, url: endpoint, host: host, now: time.Now, metrics: metrics}
}

func (cache *issuerJWKSCache) signingKey(ctx context.Context, keyID string, force bool) (oidcSigningKey, error) {
	if cache == nil || cache.client == nil || !validOIDCIdentifier(keyID) {
		return oidcSigningKey{}, errOIDCJWTInvalid
	}
	for {
		now := cache.now().UTC()
		cache.mu.Lock()
		key, found := cache.keys[keyID]
		fresh := found && now.Before(cache.expiresAt)
		if fresh && !force {
			cache.mu.Unlock()
			cache.metrics.recordJWKSCacheHit(ctx)
			return key, nil
		}
		if cache.refreshing != nil {
			waiting := cache.refreshing
			cache.mu.Unlock()
			select {
			case <-waiting:
				force = false
				continue
			case <-ctx.Done():
				return oidcSigningKey{}, errOIDCJWKSUnavailable
			}
		}
		if !cache.refreshed.IsZero() && now.Sub(cache.refreshed) < minimumJWKSRefetchGap {
			lastErr := cache.lastErr
			cache.mu.Unlock()
			if found && now.Before(cache.staleUntil) {
				return key, nil
			}
			if lastErr != nil {
				return oidcSigningKey{}, errOIDCJWKSUnavailable
			}
			return oidcSigningKey{}, errOIDCJWTInvalid
		}
		cache.refreshing = make(chan struct{})
		waiting := cache.refreshing
		cache.mu.Unlock()

		keys, ttl, err := cache.fetch(ctx)
		now = cache.now().UTC()
		cache.mu.Lock()
		cache.refreshed = now
		cache.lastErr = err
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			cache.refreshed = time.Time{}
			cache.lastErr = nil
		}
		if err == nil {
			cache.keys = keys
			cache.expiresAt = now.Add(ttl)
			cache.staleUntil = cache.expiresAt.Add(maximumJWKSStale)
		}
		key, found = cache.keys[keyID]
		staleUsable := found && now.Before(cache.staleUntil)
		close(waiting)
		cache.refreshing = nil
		cache.mu.Unlock()
		if err != nil {
			if staleUsable {
				cache.metrics.recordJWKSStaleUse(ctx)
				return key, nil
			}
			return oidcSigningKey{}, errOIDCJWKSUnavailable
		}
		if !found {
			return oidcSigningKey{}, errOIDCJWTInvalid
		}
		return key, nil
	}
}

func (cache *issuerJWKSCache) fetch(ctx context.Context) (map[string]oidcSigningKey, time.Duration, error) {
	cache.metrics.recordJWKSHTTPCall(ctx)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, cache.url, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Host = cache.host
	response, err := cache.client.Do(request) // #nosec G704 -- URL and transport hosts are issuer-bound at startup.
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxJWKSResponse))
		return nil, 0, errOIDCJWKSUnavailable
	}
	var document oidcJWKDocument
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxJWKSResponse))
	if err := decoder.Decode(&document); err != nil || len(document.Keys) == 0 || len(document.Keys) > 32 {
		return nil, 0, errOIDCJWKSUnavailable
	}
	keys := make(map[string]oidcSigningKey, len(document.Keys))
	for _, value := range document.Keys {
		key, err := parseOIDCRSAKey(value)
		if err != nil {
			continue
		}
		if _, duplicate := keys[value.KeyID]; duplicate {
			return nil, 0, errOIDCJWKSUnavailable
		}
		keys[value.KeyID] = key
	}
	if len(keys) == 0 {
		return nil, 0, errOIDCJWKSUnavailable
	}
	return keys, jwksTTL(response.Header.Get("Cache-Control")), nil
}

func parseOIDCRSAKey(value oidcJWK) (oidcSigningKey, error) {
	if value.KeyType != "RSA" || !validOIDCIdentifier(value.KeyID) || (value.Use != "" && value.Use != "sig") || (value.Algorithm != "" && value.Algorithm != "RS256") {
		return oidcSigningKey{}, errOIDCJWTInvalid
	}
	modulus, err := base64.RawURLEncoding.DecodeString(value.Modulus)
	if err != nil || len(modulus) < 256 || len(modulus) > 1024 {
		return oidcSigningKey{}, errOIDCJWTInvalid
	}
	exponent, err := base64.RawURLEncoding.DecodeString(value.Exponent)
	if err != nil || (string(exponent) != "\x01\x00\x01" && string(exponent) != "\x03") {
		return oidcSigningKey{}, errOIDCJWTInvalid
	}
	exponentValue := 65537
	if len(exponent) == 1 {
		exponentValue = 3
	}
	publicKey := &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: exponentValue}
	if publicKey.N.BitLen() < 2048 || publicKey.N.BitLen() > 8192 {
		return oidcSigningKey{}, errOIDCJWTInvalid
	}
	return oidcSigningKey{key: publicKey}, nil
}

func jwksTTL(cacheControl string) time.Duration {
	ttl := defaultJWKSTTL
	for _, directive := range strings.Split(cacheControl, ",") {
		name, raw, ok := strings.Cut(strings.TrimSpace(directive), "=")
		if !ok || strings.ToLower(name) != "max-age" {
			continue
		}
		seconds, err := strconv.ParseInt(strings.Trim(raw, "\""), 10, 32)
		if err == nil {
			switch {
			case seconds < int64(minimumJWKSTTL/time.Second):
				return minimumJWKSTTL
			case seconds > int64(maximumJWKSTTL/time.Second):
				return maximumJWKSTTL
			default:
				ttl = time.Duration(seconds) * time.Second
			}
		}
		break
	}
	if ttl < minimumJWKSTTL {
		return minimumJWKSTTL
	}
	if ttl > maximumJWKSTTL {
		return maximumJWKSTTL
	}
	return ttl
}

type oidcJWTVerifier struct {
	config config.OIDC
	cache  *issuerJWKSCache
	now    func() time.Time
}

func (verifier *oidcJWTVerifier) verify(ctx context.Context, token string) (oidcClaims, error) {
	parts := strings.Split(token, ".")
	if verifier == nil || verifier.cache == nil || len(parts) != 3 {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(headerRaw) > 4096 {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	var header oidcJWTHeader
	if err := json.Unmarshal(headerRaw, &header); err != nil || header.Algorithm != "RS256" || !validOIDCIdentifier(header.KeyID) || (header.Type != "" && header.Type != "JWT") || len(header.Critical) != 0 {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) == 0 || len(signature) > 1024 {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	key, err := verifier.cache.signingKey(ctx, header.KeyID, false)
	if err != nil {
		return oidcClaims{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key.key, crypto.SHA256, digest[:], signature); err != nil {
		key, refreshErr := verifier.cache.signingKey(ctx, header.KeyID, true)
		if refreshErr != nil || rsa.VerifyPKCS1v15(key.key, crypto.SHA256, digest[:], signature) != nil {
			return oidcClaims{}, errOIDCJWTInvalid
		}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(payload) > maxUserInfoResponse {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	var claims oidcClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	now := verifier.now().UTC()
	if claims.Issuer != verifier.config.Issuer || claims.Authorized != verifier.config.ClientID || !claims.Audience.contains(verifier.config.Audience) ||
		!validOIDCIdentifier(claims.Subject) || (claims.SessionID != "" && !validOIDCIdentifier(claims.SessionID)) || claims.ExpiresAt <= now.Unix() ||
		claims.ExpiresAt <= claims.IssuedAt || claims.NotBefore > now.Add(jwtClockSkew).Unix() || claims.IssuedAt <= 0 || claims.IssuedAt > now.Add(jwtClockSkew).Unix() ||
		(claims.AuthTime > 0 && (claims.AuthTime > now.Add(jwtClockSkew).Unix() || claims.ExpiresAt <= claims.AuthTime)) {
		return oidcClaims{}, errOIDCJWTInvalid
	}
	return claims, nil
}

func validOIDCIdentifier(value string) bool {
	if value == "" || len(value) > 255 || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
