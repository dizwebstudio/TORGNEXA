package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/marketplacepublication"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/marketplacepublicationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type marketplacePublicationStore interface {
	ClaimQueued(context.Context, tenancy.Scope, time.Time) (marketplacepublication.Operation, error)
	ListPending(context.Context, tenancy.Scope, int) ([]marketplacepublication.Operation, error)
	Snapshot(context.Context, tenancy.Scope, string) (marketplacepublication.Snapshot, error)
	UpdateState(context.Context, tenancy.Scope, marketplacepublication.Operation, int64) error
}

type marketplacePublicationAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

// runMarketplacePublications drains the durable queued snapshot surface. The
// SQL claim is row-locked and versioned; provider calls happen only after the
// operation is SENDING and all credentials remain callback-scoped.
func runMarketplacePublications(ctx context.Context, logger *slog.Logger, dispatch *workerrepo.Repository, store marketplacePublicationStore, accounts marketplacePublicationAccounts, secretSource secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, poll time.Duration, batch int) error {
	if ctx == nil || logger == nil || dispatch == nil || store == nil || accounts == nil || secretSource == nil || registry == nil || registry.builtins == nil || poll <= 0 || batch < 1 {
		return errors.New("worker: invalid marketplace publication dependencies")
	}
	return pollLoop(ctx, poll, func() error {
		scopes, err := dispatch.ActiveScopes(ctx, batch)
		if err != nil {
			return err
		}
		for _, scope := range scopes {
			for i := 0; i < batch; i++ {
				operation, claimErr := store.ClaimQueued(ctx, scope, time.Now().UTC())
				if errors.Is(claimErr, marketplacepublicationrepo.ErrNotFound) {
					break
				}
				if claimErr != nil {
					return claimErr
				}
				if err := processMarketplacePublication(ctx, store, accounts, secretSource, refreshCoordinator, registry, scope, operation); err != nil {
					logger.Warn("marketplace publication deferred", "event", "worker.marketplace_publication_deferred", "operation_id", operation.ID, "error_code", publicationWorkerErrorCode(err))
				}
			}
			pending, pendingErr := store.ListPending(ctx, scope, batch)
			if pendingErr != nil {
				return pendingErr
			}
			for _, operation := range pending {
				if err := reconcileMarketplacePublication(ctx, store, accounts, secretSource, refreshCoordinator, registry, scope, operation); err != nil {
					logger.Warn("marketplace publication reconciliation deferred", "event", "worker.marketplace_publication_reconciliation_deferred", "operation_id", operation.ID, "error_code", publicationWorkerErrorCode(err))
				}
			}
		}
		return nil
	})
}

func processMarketplacePublication(ctx context.Context, store marketplacePublicationStore, accounts marketplacePublicationAccounts, secretSource secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, scope tenancy.Scope, operation marketplacepublication.Operation) error {
	snapshot, err := store.Snapshot(ctx, scope, operation.SnapshotID)
	if err != nil {
		return err
	}
	account, err := accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation.Target.ConnectorAccountID)
	accountTarget := marketplacepublication.Target{OrganizationID: account.OrganizationID, WorkspaceID: account.WorkspaceID, ConnectorAccountID: account.ID, ConnectorID: account.ConnectorID}
	if err != nil || account.Status != sdk.AccountActive || !operation.Target.SameAccount(accountTarget) || !sdk.CapabilityEnabled(mustAccountCapabilities(ctx, accounts, scope, account.ID), "products.write") {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateNeedsAttention, "capability_unavailable")
	}
	runtime, err := connectorruntime.NewForAccount(secretSource, refreshCoordinator, scope, account)
	if err != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateUnknown, "runtime_unavailable")
	}
	if operation.Kind == marketplacepublication.OperationStatusRead {
		return reconcileMarketplacePublicationWithReader(ctx, store, registry, scope, operation, account, runtime, operation.RemoteID, operation.RemoteOperationID)
	}
	writer, err := registry.builtins.ProductPublicationWriter(account, runtime, registry.configLoader(scope))
	if err != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateNeedsAttention, "publication_writer_unavailable")
	}
	request := sdk.ProductPublicationRequest{Operation: operation.Kind, Snapshot: snapshot, RemoteID: operation.RemoteID, IdempotencyKey: operation.IdempotencyKey, DryRun: operation.DryRun, ApprovalRequestID: operation.ApprovalRef, QualityReceiptID: operation.QualityReceiptRef}
	receipt, err := writer.WriteProductPublication(ctx, account, runtime, request)
	if err != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, publicationStateForError(err), publicationWorkerErrorCode(err))
	}
	if receipt.Validate() != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateUnknown, "invalid_remote_receipt")
	}
	next := marketplacepublication.StateAccepted
	switch receipt.Status {
	case sdk.PublicationProcessing:
		next = marketplacepublication.StateProcessing
	case sdk.PublicationPublished:
		next = marketplacepublication.StatePublished
	case sdk.PublicationRejected:
		next = marketplacepublication.StateRejected
	case sdk.PublicationUnknown:
		next = marketplacepublication.StateUnknown
	case sdk.PublicationDryRun, sdk.PublicationAccepted:
		next = marketplacepublication.StateAccepted
	}
	operation.State, operation.RemoteID, operation.RemoteOperationID, operation.ErrorCode, operation.UpdatedAt = next, receipt.RemoteID, receipt.RemoteOperationID, receipt.ErrorCode, receipt.ObservedAt
	return store.UpdateState(ctx, scope, operation, operation.Version)
}

func reconcileMarketplacePublication(ctx context.Context, store marketplacePublicationStore, accounts marketplacePublicationAccounts, secretSource secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, scope tenancy.Scope, operation marketplacepublication.Operation) error {
	account, err := accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), operation.Target.ConnectorAccountID)
	accountTarget := marketplacepublication.Target{OrganizationID: account.OrganizationID, WorkspaceID: account.WorkspaceID, ConnectorAccountID: account.ID, ConnectorID: account.ConnectorID}
	if err != nil || account.Status != sdk.AccountActive || !operation.Target.SameAccount(accountTarget) || !sdk.CapabilityEnabled(mustAccountCapabilities(ctx, accounts, scope, account.ID), "products.read") {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateNeedsAttention, "status_reader_unavailable")
	}
	runtime, err := connectorruntime.NewForAccount(secretSource, refreshCoordinator, scope, account)
	if err != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateUnknown, "runtime_unavailable")
	}
	return reconcileMarketplacePublicationWithReader(ctx, store, registry, scope, operation, account, runtime, operation.RemoteID, operation.RemoteOperationID)
}

func reconcileMarketplacePublicationWithReader(ctx context.Context, store marketplacePublicationStore, registry *runtimeRegistry, scope tenancy.Scope, operation marketplacepublication.Operation, account sdk.Account, runtime sdk.Runtime, remoteID, remoteOperationID string) error {
	reader, err := registry.builtins.ProductPublicationStatusReader(account, runtime, registry.configLoader(scope))
	if err != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateNeedsAttention, "status_reader_unavailable")
	}
	if remoteID == "" && remoteOperationID == "" {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateNeedsAttention, "remote_identity_missing")
	}
	receipt, err := reader.ReadProductPublicationStatus(ctx, account, runtime, sdk.ProductPublicationStatusQuery{RemoteID: remoteID, RemoteOperationID: remoteOperationID, IdempotencyKey: operation.IdempotencyKey})
	if err != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, publicationStateForError(err), publicationWorkerErrorCode(err))
	}
	if receipt.Validate() != nil {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateUnknown, "invalid_remote_receipt")
	}
	next := marketplacepublication.StateUnknown
	switch receipt.Status {
	case sdk.PublicationAccepted:
		next = marketplacepublication.StateAccepted
	case sdk.PublicationProcessing:
		next = marketplacepublication.StateProcessing
	case sdk.PublicationPublished:
		next = marketplacepublication.StatePublished
	case sdk.PublicationRejected:
		next = marketplacepublication.StateRejected
	case sdk.PublicationUnknown:
		next = marketplacepublication.StateUnknown
	case sdk.PublicationDryRun:
		next = marketplacepublication.StateAccepted
	}
	if !marketplacepublication.CanTransition(operation.State, next) {
		return updateMarketplacePublicationFailure(ctx, store, scope, operation, marketplacepublication.StateNeedsAttention, "invalid_status_transition")
	}
	operation.State, operation.ErrorCode, operation.UpdatedAt = next, receipt.ErrorCode, receipt.ObservedAt
	if receipt.RemoteID != "" {
		operation.RemoteID = receipt.RemoteID
	}
	if receipt.RemoteOperationID != "" {
		operation.RemoteOperationID = receipt.RemoteOperationID
	}
	return store.UpdateState(ctx, scope, operation, operation.Version)
}

func updateMarketplacePublicationFailure(ctx context.Context, store marketplacePublicationStore, scope tenancy.Scope, operation marketplacepublication.Operation, state marketplacepublication.State, code string) error {
	operation.State, operation.ErrorCode, operation.UpdatedAt = state, code, time.Now().UTC()
	return store.UpdateState(ctx, scope, operation, operation.Version)
}

func publicationStateForError(err error) marketplacepublication.State {
	var remote *sdk.RemoteError
	if errors.As(err, &remote) {
		if remote.Category == sdk.ErrorUnsupported {
			return marketplacepublication.StateNeedsAttention
		}
		if remote.Retryable() {
			return marketplacepublication.StateUnknown
		}
		return marketplacepublication.StateRejected
	}
	return marketplacepublication.StateUnknown
}

func publicationWorkerErrorCode(err error) string {
	var remote *sdk.RemoteError
	if errors.As(err, &remote) && remote.Code != "" {
		return remote.Code
	}
	return "publication_failed"
}

func mustAccountCapabilities(ctx context.Context, accounts marketplacePublicationAccounts, scope tenancy.Scope, accountID string) []sdk.AccountCapabilitySetting {
	settings, err := accounts.AccountCapabilities(ctx, scope, accountID)
	if err != nil {
		return nil
	}
	return settings
}

var _ marketplacePublicationStore = (*marketplacepublicationrepo.Repository)(nil)
