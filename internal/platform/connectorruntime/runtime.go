// Package connectorruntime adapts host platform capabilities to the narrow
// Connector SDK Runtime surface. Provider implementations import only the SDK
// package and cannot access the concrete secret repository.
package connectorruntime

import (
	"context"
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

var ErrInvalidRuntime = errors.New("connector runtime: invalid runtime")

type Runtime struct {
	secrets *secretAccessor
}

var _ sdk.Runtime = (*Runtime)(nil)

func New(secretSource secrets.SecretProvider, scope tenancy.Scope) (*Runtime, error) {
	if secretSource == nil || !scope.Valid() {
		return nil, ErrInvalidRuntime
	}
	return &Runtime{secrets: &secretAccessor{source: secretSource, scope: scope}}, nil
}

// NewForAccount projects an OAuth account's encrypted credential bundle into a
// callback-scoped current access token. Non-OAuth accounts retain the ordinary
// opaque secret behavior.
func NewForAccount(secretSource secrets.SecretProvider, coordinator connectorauth.RefreshCoordinator, scope tenancy.Scope, account sdk.Account) (*Runtime, error) {
	runtime, err := New(secretSource, scope)
	if err != nil {
		return nil, err
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		return nil, ErrInvalidRuntime
	}
	configuration, err := connectorauth.OAuthConfiguration(manifest)
	if err != nil {
		return runtime, nil
	}
	if account.SecretReference == "" {
		return nil, ErrInvalidRuntime
	}
	manager, err := connectorauth.NewTokenManager(secretSource, coordinator)
	if err != nil {
		return nil, ErrInvalidRuntime
	}
	runtime.secrets.oauth = &oauthBinding{account: account, manager: manager, prepare: configuration.GrantType == "authorization_code"}
	return runtime, nil
}

func (runtime *Runtime) Secrets() sdk.SecretAccessor {
	if runtime == nil {
		return nil
	}
	return runtime.secrets
}

type secretAccessor struct {
	source secrets.SecretProvider
	scope  tenancy.Scope
	oauth  *oauthBinding
}

type oauthBinding struct {
	account sdk.Account
	manager *connectorauth.TokenManager
	prepare bool
}

var _ sdk.SecretAccessor = (*secretAccessor)(nil)

func (accessor *secretAccessor) UseSecret(ctx context.Context, reference sdk.SecretReference, consumer func([]byte) error) error {
	if accessor == nil || accessor.source == nil || !accessor.scope.Valid() || ctx == nil || consumer == nil {
		return ErrInvalidRuntime
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parsed, err := secrets.ParseReference(string(reference))
	if err != nil {
		return sdk.ErrSecretReference
	}
	if accessor.oauth != nil && reference == accessor.oauth.account.SecretReference {
		return accessor.oauth.manager.UseAccessToken(ctx, accessor.scope, accessor.oauth.account, consumer)
	}
	return accessor.source.Use(ctx, accessor.scope, parsed, consumer)
}

// PrepareOAuth refreshes an expiring account token before a health probe. It is
// a no-op for non-OAuth runtimes.
func (runtime *Runtime) PrepareOAuth(ctx context.Context) error {
	if runtime == nil || runtime.secrets == nil {
		return ErrInvalidRuntime
	}
	if runtime.secrets.oauth == nil || !runtime.secrets.oauth.prepare {
		return nil
	}
	return runtime.secrets.oauth.manager.Prepare(ctx, runtime.secrets.scope, runtime.secrets.oauth.account)
}
