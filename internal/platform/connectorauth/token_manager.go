package connectorauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const OAuthRefreshLeeway = time.Minute

var (
	ErrOAuthReauthorizationRequired = errors.New("connector auth: oauth reauthorization required")
	ErrOAuthRefreshUnavailable      = errors.New("connector auth: oauth refresh unavailable")
)

// RefreshCoordinator serializes refresh-token use across API and worker
// processes. Implementations must release the lock when context is cancelled.
type RefreshCoordinator interface {
	WithRefreshLock(context.Context, tenancy.Scope, secrets.Reference, func(context.Context) error) error
}

// TokenManager resolves only a callback-scoped access token from encrypted
// OAuth material. It never exposes refresh tokens or client credentials to a
// provider adapter.
type TokenManager struct {
	secrets  secrets.SecretProvider
	locks    RefreshCoordinator
	now      func() time.Time
	refresh  func(context.Context, sdk.OAuth2Configuration, TokenBundle, time.Duration, time.Time) ([]byte, error)
	exchange func(context.Context, sdk.OAuth2Configuration, OAuthClient, string, string, string, time.Duration) ([]byte, error)
}

func NewTokenManager(secretSource secrets.SecretProvider, locks RefreshCoordinator) (*TokenManager, error) {
	if secretSource == nil {
		return nil, ErrInvalid
	}
	return &TokenManager{secrets: secretSource, locks: locks, now: func() time.Time { return time.Now().UTC() }, refresh: HTTPRefresh, exchange: HTTPExchange}, nil
}

// Prepare verifies that an account's OAuth material can yield a current access
// token. For an expiring authorization-code grant this may rotate the encrypted
// bundle before the remote account health probe runs.
func (manager *TokenManager) Prepare(ctx context.Context, scope tenancy.Scope, account sdk.Account) error {
	return manager.UseAccessToken(ctx, scope, account, func([]byte) error { return nil })
}

// UseAccessToken invokes consumer with only the current access token and wipes
// the manager-owned byte copy immediately afterwards.
func (manager *TokenManager) UseAccessToken(ctx context.Context, scope tenancy.Scope, account sdk.Account, consumer func([]byte) error) error {
	if manager == nil || manager.secrets == nil || manager.now == nil || manager.refresh == nil || manager.exchange == nil || ctx == nil || !scope.Valid() || consumer == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		return ErrOAuthReauthorizationRequired
	}
	configuration, err := OAuthConfiguration(manifest)
	if err != nil {
		return ErrOAuthReauthorizationRequired
	}
	reference, err := secrets.ParseReference(string(account.SecretReference))
	if err != nil {
		return ErrOAuthReauthorizationRequired
	}
	metadata, err := manager.secrets.Describe(ctx, scope, reference)
	if err != nil || metadata.Status != secrets.StatusActive {
		return ErrOAuthReauthorizationRequired
	}
	timeout := time.Duration(manifest.RateLimit.RequestTimeoutMS) * time.Millisecond
	if timeout > 15*time.Second {
		timeout = 15 * time.Second
	}
	if configuration.GrantType == "client_credentials" {
		if metadata.Class != secrets.ClassOAuthClient {
			return ErrOAuthReauthorizationRequired
		}
		client, readErr := manager.readClient(ctx, scope, reference)
		if readErr != nil {
			return ErrOAuthReauthorizationRequired
		}
		material, exchangeErr := manager.exchange(ctx, configuration, client, "", "", "", timeout)
		defer clear(material)
		if exchangeErr != nil {
			return ErrOAuthRefreshUnavailable
		}
		bundle, parseErr := ParseTokenBundle(material)
		if parseErr != nil {
			return ErrOAuthRefreshUnavailable
		}
		return deliverAccessToken(bundle.AccessToken, consumer)
	}
	if configuration.GrantType != "authorization_code" || metadata.Class != secrets.ClassOAuthRefresh {
		return ErrOAuthReauthorizationRequired
	}
	bundle, err := manager.readBundle(ctx, scope, reference)
	if err != nil {
		return ErrOAuthReauthorizationRequired
	}
	if !bundleNeedsRefresh(bundle, manager.now().UTC()) {
		return deliverAccessToken(bundle.AccessToken, consumer)
	}
	if bundle.RefreshToken == "" || manager.locks == nil {
		return ErrOAuthReauthorizationRequired
	}
	var accessToken string
	err = manager.locks.WithRefreshLock(ctx, scope, reference, func(lockContext context.Context) error {
		latest, readErr := manager.readBundle(lockContext, scope, reference)
		if readErr != nil {
			return ErrOAuthReauthorizationRequired
		}
		if !bundleNeedsRefresh(latest, manager.now().UTC()) {
			accessToken = latest.AccessToken
			return nil
		}
		if latest.RefreshToken == "" {
			return ErrOAuthReauthorizationRequired
		}
		now := manager.now().UTC()
		material, refreshErr := manager.refresh(lockContext, configuration, latest, timeout, now)
		if refreshErr != nil {
			if errors.Is(refreshErr, ErrOAuthRefreshRejected) {
				return ErrOAuthReauthorizationRequired
			}
			return ErrOAuthRefreshUnavailable
		}
		defer clear(material)
		updated, parseErr := ParseTokenBundle(material)
		if parseErr != nil {
			return ErrOAuthRefreshUnavailable
		}
		if _, rotateErr := manager.secrets.Rotate(lockContext, scope, reference, material); rotateErr != nil {
			return ErrOAuthRefreshUnavailable
		}
		accessToken = updated.AccessToken
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrOAuthReauthorizationRequired) {
			return ErrOAuthReauthorizationRequired
		}
		return ErrOAuthRefreshUnavailable
	}
	return deliverAccessToken(accessToken, consumer)
}

func (manager *TokenManager) readBundle(ctx context.Context, scope tenancy.Scope, reference secrets.Reference) (TokenBundle, error) {
	var bundle TokenBundle
	var parseErr error
	err := manager.secrets.Use(ctx, scope, reference, func(material []byte) error {
		bundle, parseErr = ParseTokenBundle(material)
		return nil
	})
	if err != nil || parseErr != nil {
		return TokenBundle{}, ErrInvalid
	}
	return bundle, nil
}

func (manager *TokenManager) readClient(ctx context.Context, scope tenancy.Scope, reference secrets.Reference) (OAuthClient, error) {
	var client OAuthClient
	var parseErr error
	err := manager.secrets.Use(ctx, scope, reference, func(material []byte) error {
		client, parseErr = ParseOAuthClient(material)
		return nil
	})
	if err != nil || parseErr != nil {
		return OAuthClient{}, ErrInvalid
	}
	return client, nil
}

// ParseTokenBundle strictly validates the encrypted OAuth refresh material.
func ParseTokenBundle(material []byte) (TokenBundle, error) {
	decoder := json.NewDecoder(bytes.NewReader(material))
	decoder.DisallowUnknownFields()
	var bundle TokenBundle
	if decoder.Decode(&bundle) != nil || decoder.Decode(&struct{}{}) != io.EOF || !safeSecretPart(bundle.AccessToken, 32768) || !safeSecretPart(bundle.ClientID, 512) || !safeSecretPart(bundle.ClientSecret, 4096) || !safeTokenType(bundle.TokenType) {
		return TokenBundle{}, ErrInvalid
	}
	if bundle.RefreshToken != "" && !safeSecretPart(bundle.RefreshToken, 32768) {
		return TokenBundle{}, ErrInvalid
	}
	if bundle.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, bundle.ExpiresAt)
		if err != nil || expiresAt.Location() != time.UTC {
			return TokenBundle{}, ErrInvalid
		}
	}
	return bundle, nil
}

func bundleNeedsRefresh(bundle TokenBundle, now time.Time) bool {
	if bundle.ExpiresAt == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, bundle.ExpiresAt)
	return err != nil || !expiresAt.After(now.Add(OAuthRefreshLeeway))
}

func deliverAccessToken(value string, consumer func([]byte) error) error {
	token := []byte(value)
	defer clear(token)
	if err := consumer(token); err != nil {
		return secrets.ErrUseFailed
	}
	return nil
}

func safeTokenType(value string) bool {
	return value != "" && len(value) <= 64 && value == strings.TrimSpace(value) && strings.IndexFunc(value, func(character rune) bool { return character < 0x21 || character > 0x7e }) < 0
}
