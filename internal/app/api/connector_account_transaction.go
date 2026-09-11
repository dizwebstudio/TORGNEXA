package api

import (
	"context"
	"net/http"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

func (api *connectorAccountAPI) mutateAccount(request *http.Request, scope tenancy.Scope, action string, risk audit.Risk, mutate func(context.Context) (sdk.Account, error)) (sdk.Account, error) {
	return auditedSettingsMutation(request.Context(), scope, api.audit, mutate, func(ctx context.Context, account sdk.Account) error {
		return api.capture(request.WithContext(ctx), scope, action, account, risk)
	})
}

func (api *connectorAccountAPI) withSecrets(ctx context.Context, scope tenancy.Scope, operation func(context.Context) error) error {
	provider, ok := api.secrets.(secrets.TransactionalProvider)
	if !ok {
		return secrets.ErrTransactionUnavailable
	}
	return provider.WithinTransaction(ctx, scope, operation)
}

// replaceCredential runs only inside mutateAccount's authoritative transaction.
// No compensating revoke is needed: failed binding, revocation, audit or commit
// rolls back the new encrypted material and preserves the old usable reference.
func (api *connectorAccountAPI) replaceCredential(ctx context.Context, scope tenancy.Scope, account sdk.Account, class secrets.Class, material []byte) (sdk.Account, error) {
	var bound sdk.Account
	err := api.withSecrets(ctx, scope, func(txCtx context.Context) error {
		metadata, err := api.secrets.Create(txCtx, scope, class, material)
		if err != nil {
			return err
		}
		bound, err = api.repository.BindSecret(txCtx, scope.OrganizationID().String(), scope.WorkspaceID().String(), account.ID, sdk.SecretReference(metadata.Reference.String()), account.Version)
		if err != nil {
			return err
		}
		if account.SecretReference != "" {
			old, err := secrets.ParseReference(string(account.SecretReference))
			if err != nil {
				return err
			}
			_, err = api.secrets.Revoke(txCtx, scope, old)
			return err
		}
		return nil
	})
	return bound, err
}
