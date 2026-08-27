package api

import (
	"context"
	"errors"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

var errSyncCapabilityDenied = errors.New("connector account capability denies synchronization")

type syncPolicyCapabilityGuard interface {
	AuthorizePolicy(context.Context, tenancy.Scope, string, string, syncengine.Direction) error
}

type connectorAccountCapabilityGuard struct {
	repository *connectorrepo.Repository
	runtime    connectorRuntimeAdmission
}

func (guard connectorAccountCapabilityGuard) AuthorizePolicy(ctx context.Context, scope tenancy.Scope, accountID, entityType string, direction syncengine.Direction) error {
	if guard.repository == nil || guard.runtime == nil || !scope.Valid() || !direction.Valid() {
		return errSyncCapabilityDenied
	}
	account, err := guard.repository.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID)
	if err != nil {
		return errSyncCapabilityDenied
	}
	available := guard.runtime.SupportsSync(account.ConnectorID, entityType, string(direction))
	if !available {
		return errSyncCapabilityDenied
	}
	manifest, err := sdk.CatalogManifest(account.ConnectorID)
	if err != nil {
		return errSyncCapabilityDenied
	}
	readCapability, writeCapability, ok := sdk.RequiredSyncCapabilities(account.Family, entityType)
	if !ok {
		return errSyncCapabilityDenied
	}
	settings, err := guard.repository.AccountCapabilities(ctx, scope, accountID)
	if err != nil {
		return err
	}
	if direction.AllowsInbound() && (!manifest.Supports(readCapability) || !sdk.CapabilityEnabled(settings, readCapability)) {
		return errSyncCapabilityDenied
	}
	if direction.AllowsOutbound() && (!manifest.Supports(writeCapability) || !sdk.CapabilityEnabled(settings, writeCapability)) {
		return errSyncCapabilityDenied
	}
	return nil
}
