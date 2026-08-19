// Package connectorruntime adapts host platform capabilities to the narrow
// Connector SDK Runtime surface. Provider implementations import only the SDK
// package and cannot access the concrete secret repository.
package connectorruntime

import (
	"context"
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
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

func (runtime *Runtime) Secrets() sdk.SecretAccessor {
	if runtime == nil {
		return nil
	}
	return runtime.secrets
}

type secretAccessor struct {
	source secrets.SecretProvider
	scope  tenancy.Scope
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
	return accessor.source.Use(ctx, accessor.scope, parsed, consumer)
}
