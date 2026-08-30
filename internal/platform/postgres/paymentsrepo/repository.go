// Package paymentsrepo implements tenant-scoped PostgreSQL persistence for Payments Core.
package paymentsrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true), set_config('app.workspace_id',$2,true)`
const paymentSelect = `SELECT id,organization_id,workspace_id,connector_account_id,external_id,COALESCE(remote_id,''),COALESCE(purpose,''),amount_minor_units,currency,commission_minor_units,status,COALESCE(remote_status,''),COALESCE(reason_code,''),version,created_at,updated_at,expires_at,succeeded_at FROM payments WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`
const refundSelect = `SELECT id,organization_id,workspace_id,payment_id,external_id,COALESCE(remote_refund_id,''),amount_minor_units,currency,status,version,created_at,updated_at FROM payment_refunds WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`

var refRemotePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)

type Repository struct{ db *sql.DB }

var _ payments.Repository = (*Repository)(nil)

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("payments repository: database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Payment(ctx context.Context, scope payments.Scope, id payments.PaymentID) (payments.Payment, error) {
	if err := validate(ctx, r, scope); err != nil {
		return payments.Payment{}, err
	}
	if !id.Valid() {
		return payments.Payment{}, payments.ErrInvalidRecord
	}
	var result payments.Payment
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanPayment(tx.QueryRowContext(ctx, paymentSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) PaymentByExternalID(ctx context.Context, scope payments.Scope, externalID string) (payments.Payment, error) {
	if err := validate(ctx, r, scope); err != nil {
		return payments.Payment{}, err
	}
	var result payments.Payment
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanPayment(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,connector_account_id,external_id,COALESCE(remote_id,''),COALESCE(purpose,''),amount_minor_units,currency,commission_minor_units,status,COALESCE(remote_status,''),COALESCE(reason_code,''),version,created_at,updated_at,expires_at,succeeded_at FROM payments WHERE organization_id=$1 AND workspace_id=$2 AND external_id=$3`, scope.OrganizationID(), scope.WorkspaceID(), externalID))
		return err
	})
	return result, err
}

func (r *Repository) PaymentByRemoteID(ctx context.Context, scope payments.Scope, connectorAccountID, remoteID string) (payments.Payment, error) {
	if err := validate(ctx, r, scope); err != nil {
		return payments.Payment{}, err
	}
	var result payments.Payment
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanPayment(tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,connector_account_id,external_id,COALESCE(remote_id,''),COALESCE(purpose,''),amount_minor_units,currency,commission_minor_units,status,COALESCE(remote_status,''),COALESCE(reason_code,''),version,created_at,updated_at,expires_at,succeeded_at FROM payments WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND remote_id=$4`, scope.OrganizationID(), scope.WorkspaceID(), connectorAccountID, remoteID))
		return err
	})
	return result, err
}

// RefundByRemoteID looks up a refund by connector account and provider refund
// identifier. The account predicate is retained even though payment_refunds
// currently reaches the provider through its parent payment, so a remote ID
// can never be matched across payment rails inside one workspace.
func (r *Repository) RefundByRemoteID(ctx context.Context, scope payments.Scope, connectorAccountID, remoteID string) (payments.Refund, error) {
	if err := validate(ctx, r, scope); err != nil {
		return payments.Refund{}, err
	}
	if !refRemotePattern.MatchString(remoteID) {
		return payments.Refund{}, payments.ErrInvalidRecord
	}
	var result payments.Refund
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanRefund(tx.QueryRowContext(ctx, `SELECT r.id,r.organization_id,r.workspace_id,r.payment_id,r.external_id,COALESCE(r.remote_refund_id,''),r.amount_minor_units,r.currency,r.status,r.version,r.created_at,r.updated_at FROM payment_refunds r JOIN payments p ON p.organization_id=r.organization_id AND p.workspace_id=r.workspace_id AND p.id=r.payment_id WHERE r.organization_id=$1 AND r.workspace_id=$2 AND p.connector_account_id=$3 AND r.remote_refund_id=$4`, scope.OrganizationID(), scope.WorkspaceID(), connectorAccountID, remoteID))
		return err
	})
	return result, err
}

// ListPayments returns recent payments for the tenant finance UI.
func (r *Repository) ListPayments(ctx context.Context, scope payments.Scope, limit int) ([]payments.Payment, error) {
	if err := validate(ctx, r, scope); err != nil || limit < 1 || limit > 200 {
		return nil, payments.ErrInvalidRecord
	}
	result := make([]payments.Payment, 0, limit)
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id,organization_id,workspace_id,connector_account_id,external_id,COALESCE(remote_id,''),COALESCE(purpose,''),amount_minor_units,currency,commission_minor_units,status,COALESCE(remote_status,''),COALESCE(reason_code,''),version,created_at,updated_at,expires_at,succeeded_at FROM payments WHERE organization_id=$1 AND workspace_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, scope.OrganizationID(), scope.WorkspaceID(), limit)
		if err != nil {
			return fmt.Errorf("payments repository: list payments: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			value, scanErr := scanPayment(rows)
			if scanErr != nil {
				return scanErr
			}
			result = append(result, value)
		}
		return rows.Err()
	})
	return result, err
}

func (r *Repository) CreatePayment(ctx context.Context, scope payments.Scope, command payments.CreatePayment, mutation payments.Mutation) (payments.Payment, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return payments.Payment{}, err
	}
	if err := command.Validate(); err != nil {
		return payments.Payment{}, err
	}
	var result payments.Payment
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		var family, status string
		if err := tx.QueryRowContext(ctx, `SELECT family,status FROM connector_accounts WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), command.ConnectorAccountID).Scan(&family, &status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return payments.ErrRailUnavailable
			}
			return fmt.Errorf("payments repository: connector account: %w", err)
		}
		if family != "payment" || status != "active" {
			return payments.ErrRailUnavailable
		}
		var err error
		result, err = scanPayment(tx.QueryRowContext(ctx, `INSERT INTO payments(id,organization_id,workspace_id,connector_account_id,external_id,purpose,amount_minor_units,currency,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,connector_account_id,external_id,COALESCE(remote_id,''),COALESCE(purpose,''),amount_minor_units,currency,commission_minor_units,status,COALESCE(remote_status,''),COALESCE(reason_code,''),version,created_at,updated_at,expires_at,succeeded_at`,
			command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.ConnectorAccountID, command.ExternalID, nullIfEmpty(command.Purpose), command.Amount.MinorUnits(), string(command.Amount.Currency()), command.ExpiresAt))
		if errors.Is(err, payments.ErrNotFound) {
			return payments.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "payments.payment.created", "payment", result.ID.String(), audit.RiskLegallySignificant, audit.Summary{"connector_account_id": result.ConnectorAccountID, "status": string(result.Status), "amount_minor_units": result.Amount.MinorUnits(), "currency": string(result.Amount.Currency()), "version": result.Version}); err != nil {
			return err
		}
		return enqueuePaymentEvent(ctx, tx, scope, mutation, result, "created")
	})
	return result, err
}

func (r *Repository) ChangePaymentStatus(ctx context.Context, scope payments.Scope, command payments.ChangePaymentStatus, mutation payments.Mutation) (payments.Payment, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return payments.Payment{}, err
	}
	if err := command.Validate(); err != nil {
		return payments.Payment{}, err
	}
	var result payments.Payment
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return payments.ErrConflict
		}
		if err := payments.ValidatePaymentTransition(current.Status, command.Status); err != nil {
			return err
		}
		remoteID := current.RemoteID
		if command.RemoteID != "" {
			remoteID = command.RemoteID
		}
		commission := current.CommissionMinorUnits
		if command.CommissionMinorUnits > 0 {
			commission = command.CommissionMinorUnits
		}
		remoteStatus := current.RemoteStatus
		if command.RemoteStatus != "" {
			remoteStatus = command.RemoteStatus
		}
		var reason any
		if command.ReasonCode != "" {
			reason = command.ReasonCode
		}
		var succeededAt any
		if command.SucceededAt != nil {
			succeededAt = *command.SucceededAt
		}
		result, err = scanPayment(tx.QueryRowContext(ctx, `UPDATE payments SET status=$4,remote_id=$5,remote_status=$6,commission_minor_units=$7,reason_code=$8,succeeded_at=$9,version=version+1,updated_at=$10 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$11 RETURNING id,organization_id,workspace_id,connector_account_id,external_id,COALESCE(remote_id,''),COALESCE(purpose,''),amount_minor_units,currency,commission_minor_units,status,COALESCE(remote_status,''),COALESCE(reason_code,''),version,created_at,updated_at,expires_at,succeeded_at`,
			scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), string(command.Status), nullIfEmpty(remoteID), nullIfEmpty(remoteStatus), commission, reason, succeededAt, mutation.OccurredAt, command.ExpectedVersion))
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "payments.payment.status_changed", "payment", result.ID.String(), audit.RiskLegallySignificant, audit.Summary{"from": string(current.Status), "to": string(result.Status), "reason_code": result.ReasonCode, "version": result.Version}); err != nil {
			return err
		}
		return enqueuePaymentEvent(ctx, tx, scope, mutation, result, "status_changed")
	})
	return result, err
}

func (r *Repository) Refund(ctx context.Context, scope payments.Scope, id payments.RefundID) (payments.Refund, error) {
	if err := validate(ctx, r, scope); err != nil {
		return payments.Refund{}, err
	}
	if !id.Valid() {
		return payments.Refund{}, payments.ErrInvalidRecord
	}
	var result payments.Refund
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		var err error
		result, err = scanRefund(tx.QueryRowContext(ctx, refundSelect, scope.OrganizationID(), scope.WorkspaceID(), id.String()))
		return err
	})
	return result, err
}

func (r *Repository) CreateRefund(ctx context.Context, scope payments.Scope, command payments.CreateRefund, mutation payments.Mutation) (payments.Refund, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return payments.Refund{}, err
	}
	if err := command.Validate(); err != nil {
		return payments.Refund{}, err
	}
	var result payments.Refund
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		// Serialize all refund reservations on the parent payment. This makes
		// the amount check safe when two operators retry a refund concurrently.
		payment, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.PaymentID.String()))
		if err != nil {
			return err
		}
		if payment.Status != payments.StatusSucceeded && payment.Status != payments.StatusPartiallyRefunded {
			return payments.ErrInvalidState
		}
		if command.Amount.Currency() != payment.Amount.Currency() {
			return payments.ErrInvalidRecord
		}
		var reservedMinor int64
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor_units),0) FROM payment_refunds WHERE organization_id=$1 AND workspace_id=$2 AND payment_id=$3 AND status IN ('pending','accepted','succeeded','unknown','manual_attention')`, scope.OrganizationID(), scope.WorkspaceID(), command.PaymentID.String()).Scan(&reservedMinor); err != nil {
			return fmt.Errorf("payments repository: sum reserved refunds: %w", err)
		}
		if command.Amount.MinorUnits() > payment.Amount.MinorUnits() || reservedMinor > payment.Amount.MinorUnits()-command.Amount.MinorUnits() {
			return payments.ErrConflict
		}
		result, err = scanRefund(tx.QueryRowContext(ctx, `INSERT INTO payment_refunds(id,organization_id,workspace_id,payment_id,external_id,amount_minor_units,currency) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING id,organization_id,workspace_id,payment_id,external_id,COALESCE(remote_refund_id,''),amount_minor_units,currency,status,version,created_at,updated_at`,
			command.ID.String(), scope.OrganizationID(), scope.WorkspaceID(), command.PaymentID.String(), command.ExternalID, command.Amount.MinorUnits(), string(command.Amount.Currency())))
		if errors.Is(err, payments.ErrNotFound) {
			return payments.ErrConflict
		}
		if err != nil {
			return err
		}
		if err := appendAudit(ctx, tx, scope, mutation, "payments.refund.created", "payment_refund", result.ID.String(), audit.RiskLegallySignificant, audit.Summary{"payment_id": result.PaymentID.String(), "amount_minor_units": result.Amount.MinorUnits(), "currency": string(result.Amount.Currency()), "version": result.Version}); err != nil {
			return err
		}
		return enqueueRefundEvent(ctx, tx, scope, mutation, result)
	})
	return result, err
}

func (r *Repository) ChangeRefundStatus(ctx context.Context, scope payments.Scope, command payments.ChangeRefundStatus, mutation payments.Mutation) (payments.Refund, error) {
	if err := validateMutation(ctx, r, scope, mutation); err != nil {
		return payments.Refund{}, err
	}
	if err := command.Validate(); err != nil {
		return payments.Refund{}, err
	}
	var result payments.Refund
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		current, err := scanRefund(tx.QueryRowContext(ctx, refundSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), command.ID.String()))
		if err != nil {
			return err
		}
		if current.Version != command.ExpectedVersion {
			return payments.ErrConflict
		}
		if err := payments.ValidateRefundTransition(current.Status, command.Status); err != nil {
			return err
		}
		remoteRefundID := current.RemoteRefundID
		if command.RemoteRefundID != "" {
			remoteRefundID = command.RemoteRefundID
		}
		result, err = scanRefund(tx.QueryRowContext(ctx, `UPDATE payment_refunds SET status=$4,remote_refund_id=$5,version=version+1,updated_at=$6 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$7 RETURNING id,organization_id,workspace_id,payment_id,external_id,COALESCE(remote_refund_id,''),amount_minor_units,currency,status,version,created_at,updated_at`,
			scope.OrganizationID(), scope.WorkspaceID(), command.ID.String(), string(command.Status), nullIfEmpty(remoteRefundID), mutation.OccurredAt, command.ExpectedVersion))
		if err != nil {
			return err
		}
		if command.Status == payments.RefundSucceeded {
			if err := reflectRefundOnPayment(ctx, tx, scope, current.PaymentID, mutation); err != nil {
				return err
			}
		}
		if err := appendAudit(ctx, tx, scope, mutation, "payments.refund.status_changed", "payment_refund", result.ID.String(), audit.RiskLegallySignificant, audit.Summary{"from": string(current.Status), "to": string(result.Status), "version": result.Version}); err != nil {
			return err
		}
		return enqueueRefundEvent(ctx, tx, scope, mutation, result)
	})
	return result, err
}

// reflectRefundOnPayment moves the parent payment to refunded/partially_refunded
// once a refund settles, keeping the payment's own status authoritative for readers
// that only look at payments (never at its refund children).
func reflectRefundOnPayment(ctx context.Context, tx *sql.Tx, scope payments.Scope, paymentID payments.PaymentID, mutation payments.Mutation) error {
	payment, err := scanPayment(tx.QueryRowContext(ctx, paymentSelect+` FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), paymentID.String()))
	if err != nil {
		return err
	}
	var refundedMinor, succeededMinor int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount_minor_units),0) FROM payment_refunds WHERE organization_id=$1 AND workspace_id=$2 AND payment_id=$3 AND status='succeeded'`, scope.OrganizationID(), scope.WorkspaceID(), paymentID.String()).Scan(&refundedMinor); err != nil {
		return fmt.Errorf("payments repository: sum refunds: %w", err)
	}
	succeededMinor = payment.Amount.MinorUnits()
	target := payments.StatusPartiallyRefunded
	if refundedMinor >= succeededMinor {
		target = payments.StatusRefunded
	}
	if target == payment.Status {
		return nil
	}
	if err := payments.ValidatePaymentTransition(payment.Status, target); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE payments SET status=$4,version=version+1,updated_at=$5 WHERE organization_id=$1 AND workspace_id=$2 AND id=$3 AND version=$6`,
		scope.OrganizationID(), scope.WorkspaceID(), paymentID.String(), string(target), mutation.OccurredAt, payment.Version); err != nil {
		return fmt.Errorf("payments repository: reflect refund on payment: %w", err)
	}
	return nil
}

func (r *Repository) RecordWebhookEvidence(ctx context.Context, scope payments.Scope, evidence payments.WebhookEvidence) (bool, error) {
	if err := validate(ctx, r, scope); err != nil {
		return false, err
	}
	if err := evidence.Validate(); err != nil {
		return false, payments.ErrInvalidRecord
	}
	var inserted bool
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO payment_webhook_receipts(organization_id,workspace_id,connector_account_id,delivery_id,remote_payment_id,event_type,body_digest,verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
			scope.OrganizationID(), scope.WorkspaceID(), evidence.ConnectorAccountID, evidence.DeliveryID, evidence.RemotePaymentID, evidence.EventType, evidence.BodyDigest, evidence.VerifiedAt)
		if err != nil {
			return fmt.Errorf("payments repository: record webhook evidence: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("payments repository: webhook evidence rows affected: %w", err)
		}
		inserted = affected == 1
		return nil
	})
	return inserted, err
}

func (r *Repository) tx(ctx context.Context, scope payments.Scope, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("payments repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var org, ws string
	if err := tx.QueryRowContext(ctx, applyScope, scope.OrganizationID(), scope.WorkspaceID()).Scan(&org, &ws); err != nil {
		return fmt.Errorf("payments repository: scope: %w", err)
	}
	if org != scope.OrganizationID() || ws != scope.WorkspaceID() {
		return payments.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("payments repository: commit: %w", err)
	}
	return nil
}

func validate(ctx context.Context, r *Repository, scope payments.Scope) error {
	if ctx == nil {
		return errors.New("payments repository: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || r.db == nil {
		return errors.New("payments repository: uninitialized")
	}
	if !scope.Valid() {
		return payments.ErrInvalidScope
	}
	return nil
}
func validateMutation(ctx context.Context, r *Repository, scope payments.Scope, mutation payments.Mutation) error {
	if err := validate(ctx, r, scope); err != nil {
		return err
	}
	return mutation.Validate()
}

func nullIfEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}

type scanner interface{ Scan(...any) error }

func scanPayment(row scanner) (payments.Payment, error) {
	var result payments.Payment
	var id, org, ws, currency, status string
	var minorUnits int64
	var succeededAt sql.NullTime
	if err := row.Scan(&id, &org, &ws, &result.ConnectorAccountID, &result.ExternalID, &result.RemoteID, &result.Purpose, &minorUnits, &currency, &result.CommissionMinorUnits, &status, &result.RemoteStatus, &result.ReasonCode, &result.Version, &result.CreatedAt, &result.UpdatedAt, &result.ExpiresAt, &succeededAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return payments.Payment{}, payments.ErrNotFound
		}
		return payments.Payment{}, fmt.Errorf("payments repository: scan payment: %w", err)
	}
	amount, err := domain.NewMoney(minorUnits, domain.Currency(currency))
	if err != nil {
		return payments.Payment{}, payments.ErrInvalidRecord
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.Status, result.Amount = payments.PaymentID(id), org, ws, payments.Status(status), amount
	result.CreatedAt, result.UpdatedAt, result.ExpiresAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC(), result.ExpiresAt.UTC()
	if succeededAt.Valid {
		at := succeededAt.Time.UTC()
		result.SucceededAt = &at
	}
	if err := result.Validate(); err != nil {
		return payments.Payment{}, err
	}
	return result, nil
}

func scanRefund(row scanner) (payments.Refund, error) {
	var result payments.Refund
	var id, org, ws, paymentID, currency, status string
	var minorUnits int64
	if err := row.Scan(&id, &org, &ws, &paymentID, &result.ExternalID, &result.RemoteRefundID, &minorUnits, &currency, &status, &result.Version, &result.CreatedAt, &result.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return payments.Refund{}, payments.ErrNotFound
		}
		return payments.Refund{}, fmt.Errorf("payments repository: scan refund: %w", err)
	}
	amount, err := domain.NewMoney(minorUnits, domain.Currency(currency))
	if err != nil {
		return payments.Refund{}, payments.ErrInvalidRecord
	}
	result.ID, result.OrganizationID, result.WorkspaceID, result.PaymentID, result.Status, result.Amount = payments.RefundID(id), org, ws, payments.PaymentID(paymentID), payments.RefundStatus(status), amount
	result.CreatedAt, result.UpdatedAt = result.CreatedAt.UTC(), result.UpdatedAt.UTC()
	if err := result.Validate(); err != nil {
		return payments.Refund{}, err
	}
	return result, nil
}

func tenantScope(scope payments.Scope) (tenancy.Scope, error) {
	return tenancy.ParseScope(scope.OrganizationID(), scope.WorkspaceID())
}

func appendAudit(ctx context.Context, tx *sql.Tx, scope payments.Scope, mutation payments.Mutation, action, resourceType, resourceID string, risk audit.Risk, summary audit.Summary) error {
	ts, err := tenantScope(scope)
	if err != nil {
		return err
	}
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	record := audit.Record{ID: mutation.AuditID, OrganizationID: ts.OrganizationID(), WorkspaceID: ts.WorkspaceID(), ActorID: mutation.ActorID, Source: mutation.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: mutation.CorrelationID, Risk: risk, Summary: safe, CreatedAt: mutation.OccurredAt}
	return auditrepo.AppendTransaction(ctx, tx, ts, record)
}

func enqueuePaymentEvent(ctx context.Context, tx *sql.Tx, scope payments.Scope, mutation payments.Mutation, payment payments.Payment, change string) error {
	payload := struct {
		PaymentID          string          `json:"payment_id"`
		ConnectorAccountID string          `json:"connector_account_id"`
		Status             payments.Status `json:"status"`
		ReasonCode         string          `json:"reason_code,omitempty"`
		AmountMinorUnits   int64           `json:"amount_minor_units"`
		Currency           string          `json:"currency"`
		Version            int64           `json:"version"`
		Change             string          `json:"change"`
	}{payment.ID.String(), payment.ConnectorAccountID, payment.Status, payment.ReasonCode, payment.Amount.MinorUnits(), string(payment.Amount.Currency()), payment.Version, change}
	return enqueue(ctx, tx, scope, mutation, "commerce.payments.payment_status_changed.v1", "payment", payment.ID.String(), payload)
}

func enqueueRefundEvent(ctx context.Context, tx *sql.Tx, scope payments.Scope, mutation payments.Mutation, refund payments.Refund) error {
	payload := struct {
		RefundID         string                `json:"refund_id"`
		PaymentID        string                `json:"payment_id"`
		Status           payments.RefundStatus `json:"status"`
		AmountMinorUnits int64                 `json:"amount_minor_units"`
		Currency         string                `json:"currency"`
		Version          int64                 `json:"version"`
	}{refund.ID.String(), refund.PaymentID.String(), refund.Status, refund.Amount.MinorUnits(), string(refund.Amount.Currency()), refund.Version}
	return enqueue(ctx, tx, scope, mutation, "commerce.payments.refund_status_changed.v1", "payment_refund", refund.ID.String(), payload)
}

func enqueue(ctx context.Context, tx *sql.Tx, scope payments.Scope, mutation payments.Mutation, eventType, entityType, entityID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return err
	}
	at, err := domain.NewUTCInstant(mutation.OccurredAt)
	if err != nil {
		return err
	}
	event := eventbus.Event{ID: mutation.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), EntityType: entityType, EntityID: entityID, Source: mutation.Source, CorrelationID: mutation.CorrelationID, CausationID: mutation.CausationID, ActorID: mutation.ActorID, TraceID: mutation.TraceID, Data: data}
	if err := event.Validate(); err != nil {
		return err
	}
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, event)
}
