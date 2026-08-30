package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	corepayments "github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/paymentsrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/workerrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	paymentReconciliationInterval = 5 * time.Minute
	paymentReconciliationWindow   = 48 * time.Hour
	paymentReconciliationBatch    = 1000
)

type paymentScopeLister interface {
	PaymentScopes(context.Context, int) ([]tenancy.Scope, error)
}

type paymentAccountLister interface {
	ListAccounts(context.Context, string, string, string, int) ([]sdk.Account, error)
}

type paymentReconciliationStore interface {
	PaymentByRemoteID(context.Context, corepayments.Scope, string, string) (corepayments.Payment, error)
	RefundByRemoteID(context.Context, corepayments.Scope, string, string) (corepayments.Refund, error)
	ChangePaymentStatus(context.Context, corepayments.Scope, corepayments.ChangePaymentStatus, corepayments.Mutation) (corepayments.Payment, error)
	ChangeRefundStatus(context.Context, corepayments.Scope, corepayments.ChangeRefundStatus, corepayments.Mutation) (corepayments.Refund, error)
}

type paymentGatewayResolver interface {
	paymentGateway(tenancy.Scope, sdk.Account) (builtinruntime.PaymentGateway, error)
}

type paymentReconciliationRunner struct {
	scopes   paymentScopeLister
	accounts paymentAccountLister
	payments paymentReconciliationStore
	secrets  secrets.SecretProvider
	refresh  connectorauth.RefreshCoordinator
	registry paymentGatewayResolver
	interval time.Duration
	window   time.Duration
	now      func() time.Time
}

func runPaymentReconciliation(ctx context.Context, logger *slog.Logger, scopes paymentScopeLister, accounts *connectorrepo.Repository, payments *paymentsrepo.Repository, secretSource secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry, poll time.Duration) error {
	runner := paymentReconciliationRunner{
		scopes: scopes, accounts: accounts, payments: payments, secrets: secretSource,
		refresh: refreshCoordinator, registry: registry, interval: paymentReconciliationInterval,
		window: paymentReconciliationWindow, now: func() time.Time { return time.Now().UTC() },
	}
	if logger == nil {
		return errors.New("worker: payment reconciliation logger required")
	}
	return runner.run(ctx, logger, poll)
}

func (runner paymentReconciliationRunner) run(ctx context.Context, logger *slog.Logger, poll time.Duration) error {
	if ctx == nil || logger == nil || runner.scopes == nil || runner.accounts == nil || runner.payments == nil || runner.secrets == nil || runner.refresh == nil || runner.registry == nil || runner.interval <= 0 || runner.window <= 0 || runner.now == nil {
		return errors.New("worker: invalid payment reconciliation dependencies")
	}
	lastRun := time.Time{}
	return pollLoop(ctx, poll, func() error {
		now := runner.now().UTC()
		if !lastRun.IsZero() && now.Sub(lastRun) < runner.interval {
			return nil
		}
		lastRun = now
		scopes, err := runner.scopes.PaymentScopes(ctx, paymentReconciliationBatch)
		if errors.Is(err, workerrepo.ErrSchemaUnavailable) {
			return nil
		}
		if err != nil {
			return err
		}
		from := now.Add(-runner.window)
		for _, scope := range scopes {
			if err := runner.reconcileScope(ctx, scope, from, now, logger); err != nil {
				logger.Warn("payment reconciliation deferred", "event", "worker.payment_reconciliation_deferred", "organization_id", scope.OrganizationID().String(), "workspace_id", scope.WorkspaceID().String(), "error", err)
			}
		}
		return nil
	})
}

func (runner paymentReconciliationRunner) reconcileScope(ctx context.Context, scope tenancy.Scope, from, to time.Time, logger *slog.Logger) error {
	if !scope.Valid() {
		return errors.New("worker: invalid payment reconciliation scope")
	}
	paymentScope, err := corepayments.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	accounts, err := listActivePaymentAccounts(ctx, runner.accounts, scope)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if err := runner.reconcileAccount(ctx, scope, paymentScope, account, from, to, logger); err != nil {
			logger.Warn("payment account reconciliation failed", "event", "worker.payment_account_reconciliation_failed", "account_id", account.ID, "connector_id", account.ConnectorID, "error", err)
		}
	}
	return nil
}

func listActivePaymentAccounts(ctx context.Context, accounts paymentAccountLister, scope tenancy.Scope) ([]sdk.Account, error) {
	const pageSize = 101
	result := make([]sdk.Account, 0)
	after := ""
	for page := 0; page < 1000; page++ {
		items, err := accounts.ListAccounts(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), after, pageSize)
		if err != nil {
			return nil, err
		}
		for _, account := range items {
			if account.Family == sdk.FamilyPayment && account.Status == sdk.AccountActive {
				result = append(result, account)
			}
		}
		if len(items) < pageSize {
			return result, nil
		}
		next := items[len(items)-1].ID
		if next == "" || next == after {
			return nil, errors.New("worker: payment account pagination did not advance")
		}
		after = next
	}
	return nil, errors.New("worker: payment account pagination exceeded bound")
}

func (runner paymentReconciliationRunner) reconcileAccount(ctx context.Context, tenantScope tenancy.Scope, paymentScope corepayments.Scope, account sdk.Account, from, to time.Time, logger *slog.Logger) error {
	gateway, err := runner.registry.paymentGateway(tenantScope, account)
	if err != nil {
		return err
	}
	runtime, err := connectorruntime.NewForAccount(runner.secrets, runner.refresh, tenantScope, account)
	if err != nil {
		return err
	}
	result, err := gateway.ReconcilePayments(ctx, account, runtime, sdk.PaymentReconcileRequest{From: from, To: to})
	if err != nil {
		return err
	}
	if result.Validate() != nil {
		return errors.New("worker: payment reconciliation response violates sdk contract")
	}
	for _, observation := range result.Items {
		if observation.Kind == "refund" {
			if err := runner.reconcileRefundObservation(ctx, paymentScope, account, observation, logger); err != nil {
				return err
			}
			continue
		}
		if observation.Kind != "sale" {
			logger.Warn("payment reconciliation observation ignored", "event", "worker.payment_reconciliation_observation_ignored", "account_id", account.ID, "remote_id", observation.RemoteID, "kind", observation.Kind)
			continue
		}
		payment, lookupErr := runner.payments.PaymentByRemoteID(ctx, paymentScope, account.ID, observation.RemoteID)
		if errors.Is(lookupErr, corepayments.ErrNotFound) {
			continue
		}
		if lookupErr != nil {
			return lookupErr
		}
		if payment.Amount.MinorUnits() != observation.Amount.MinorUnits || string(payment.Amount.Currency()) != observation.Amount.Currency {
			logger.Warn("payment reconciliation amount mismatch", "event", "worker.payment_reconciliation_amount_mismatch", "account_id", account.ID, "remote_id", observation.RemoteID)
			continue
		}
		target, ok := reconciledPaymentStatus(observation.Status)
		if !ok || target == payment.Status {
			continue
		}
		if corepayments.ValidatePaymentTransition(payment.Status, target) != nil {
			logger.Warn("payment reconciliation transition ignored", "event", "worker.payment_reconciliation_transition_ignored", "account_id", account.ID, "remote_id", observation.RemoteID, "from", payment.Status, "to", target)
			continue
		}
		command := corepayments.ChangePaymentStatus{ID: payment.ID, ExpectedVersion: payment.Version, Status: target, RemoteID: observation.RemoteID, RemoteStatus: observation.Status, CommissionMinorUnits: observation.CommissionMinorUnits}
		if target == corepayments.StatusSucceeded {
			at := observation.OccurredAt
			if at.Before(payment.CreatedAt) {
				at = runner.now().UTC()
			}
			command.SucceededAt = &at
		}
		if target == corepayments.StatusFailed {
			command.ReasonCode = "provider_declined"
		}
		key := account.ID + ":" + payment.ID.String() + ":" + fmt.Sprint(payment.Version) + ":" + observation.Status
		if _, changeErr := runner.payments.ChangePaymentStatus(ctx, paymentScope, command, paymentReconciliationMutation(key, observation.RemoteID)); changeErr != nil && !errors.Is(changeErr, corepayments.ErrConflict) {
			return changeErr
		}
	}
	return nil
}

func (runner paymentReconciliationRunner) reconcileRefundObservation(ctx context.Context, paymentScope corepayments.Scope, account sdk.Account, observation sdk.PaymentSettlement, logger *slog.Logger) error {
	refund, lookupErr := runner.payments.RefundByRemoteID(ctx, paymentScope, account.ID, observation.RemoteID)
	if errors.Is(lookupErr, corepayments.ErrNotFound) {
		return nil
	}
	if lookupErr != nil {
		return lookupErr
	}
	if refund.Amount.MinorUnits() != observation.Amount.MinorUnits || string(refund.Amount.Currency()) != observation.Amount.Currency {
		logger.Warn("payment refund reconciliation amount mismatch", "event", "worker.payment_refund_reconciliation_amount_mismatch", "account_id", account.ID, "remote_id", observation.RemoteID)
		return nil
	}
	target, ok := reconciledRefundStatus(observation.Status)
	if !ok || target == refund.Status {
		return nil
	}
	if corepayments.ValidateRefundTransition(refund.Status, target) != nil {
		logger.Warn("payment refund reconciliation transition ignored", "event", "worker.payment_refund_reconciliation_transition_ignored", "account_id", account.ID, "remote_id", observation.RemoteID, "from", refund.Status, "to", target)
		return nil
	}
	command := corepayments.ChangeRefundStatus{ID: refund.ID, ExpectedVersion: refund.Version, Status: target, RemoteRefundID: observation.RemoteID}
	if _, err := runner.payments.ChangeRefundStatus(ctx, paymentScope, command, paymentReconciliationMutation(account.ID+":"+refund.ID.String()+":"+fmt.Sprint(refund.Version)+":"+string(target), observation.RemoteID)); err != nil && !errors.Is(err, corepayments.ErrConflict) {
		return err
	}
	return nil
}

func reconciledPaymentStatus(remote string) (corepayments.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(remote)) {
	case "created", "pending", "waiting_for_capture", "waiting_for_payment", "in_progress", "processing":
		return corepayments.StatusCreated, true
	case "succeeded", "paid":
		return corepayments.StatusSucceeded, true
	case "canceled", "cancelled":
		return corepayments.StatusCanceled, true
	case "failed", "declined", "rejected":
		return corepayments.StatusFailed, true
	default:
		return "", false
	}
}

func reconciledRefundStatus(remote string) (corepayments.RefundStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(remote)) {
	case "created", "pending", "waiting_for_capture", "waiting_for_payment", "in_progress", "processing":
		return corepayments.RefundAccepted, true
	case "succeeded", "paid":
		return corepayments.RefundSucceeded, true
	case "canceled", "cancelled", "failed", "declined", "rejected":
		return corepayments.RefundFailed, true
	default:
		return "", false
	}
}

func paymentReconciliationMutation(key, causation string) corepayments.Mutation {
	now := time.Now().UTC()
	return corepayments.Mutation{
		EventID: stableUUID("payment-reconciliation:event:" + key), AuditID: stableUUID("payment-reconciliation:audit:" + key),
		ActorID: "system:payment-reconciliation", Source: "worker.payment_reconciliation", CorrelationID: stableUUID("payment-reconciliation:correlation:" + key), CausationID: causation, OccurredAt: now,
	}
}
