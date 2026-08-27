// Package socialdispatchrepo persists provider-neutral remote publication
// receipts outside canonical Social Core state.
package socialdispatchrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var ErrNotFound = errors.New("social dispatch repository: receipt not found")

type Receipt struct {
	PublicationID       string
	ConnectorAccountID  string
	RemotePublicationID string
	ObservedAt          time.Time
}

type Repository struct{ db *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("social dispatch repository: database required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Receipt(ctx context.Context, scope tenancy.Scope, publicationID string) (Receipt, error) {
	var result Receipt
	err := r.tx(ctx, scope, true, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT publication_id,connector_account_id,remote_publication_id,observed_at FROM social_publication_receipts WHERE organization_id=$1 AND workspace_id=$2 AND publication_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), publicationID).Scan(&result.PublicationID, &result.ConnectorAccountID, &result.RemotePublicationID, &result.ObservedAt)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, ErrNotFound
	}
	result.ObservedAt = result.ObservedAt.UTC()
	return result, err
}

// Record inserts an immutable receipt. An exact replay returns the persisted
// value; a conflicting remote result fails closed.
func (r *Repository) Record(ctx context.Context, scope tenancy.Scope, receipt Receipt) (Receipt, error) {
	if receiptHasEmptyIdentity(receipt) || receipt.ObservedAt.IsZero() || receipt.ObservedAt.Location() != time.UTC {
		return Receipt{}, errors.New("social dispatch repository: invalid receipt")
	}
	var result Receipt
	err := r.tx(ctx, scope, false, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `INSERT INTO social_publication_receipts(organization_id,workspace_id,publication_id,connector_account_id,remote_publication_id,observed_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT (organization_id,workspace_id,publication_id) DO NOTHING RETURNING publication_id,connector_account_id,remote_publication_id,observed_at`, scope.OrganizationID().String(), scope.WorkspaceID().String(), receipt.PublicationID, receipt.ConnectorAccountID, receipt.RemotePublicationID, receipt.ObservedAt).Scan(&result.PublicationID, &result.ConnectorAccountID, &result.RemotePublicationID, &result.ObservedAt)
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT publication_id,connector_account_id,remote_publication_id,observed_at FROM social_publication_receipts WHERE organization_id=$1 AND workspace_id=$2 AND publication_id=$3`, scope.OrganizationID().String(), scope.WorkspaceID().String(), receipt.PublicationID).Scan(&result.PublicationID, &result.ConnectorAccountID, &result.RemotePublicationID, &result.ObservedAt)
		if err == nil && !sameReceiptIdentity(result, receipt) {
			return errors.New("social dispatch repository: receipt conflict")
		}
		return err
	})
	result.ObservedAt = result.ObservedAt.UTC()
	return result, err
}

func receiptHasEmptyIdentity(receipt Receipt) bool {
	identity := receiptIdentity(receipt)
	return identity.Publication == "" || identity.Account == "" || identity.Remote == ""
}

func sameReceiptIdentity(left, right Receipt) bool {
	return reflect.DeepEqual(receiptIdentity(left), receiptIdentity(right))
}

type dispatchIdentity struct {
	Publication string
	Account     string
	Remote      string
}

func receiptIdentity(receipt Receipt) dispatchIdentity {
	return dispatchIdentity{Publication: receipt.PublicationID, Account: receipt.ConnectorAccountID, Remote: receipt.RemotePublicationID}
}

func (r *Repository) tx(ctx context.Context, scope tenancy.Scope, readOnly bool, operation func(*sql.Tx) error) error {
	if ctx == nil || r == nil || r.db == nil || !scope.Valid() || operation == nil {
		return errors.New("social dispatch repository: invalid call")
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: readOnly})
	if err != nil {
		return fmt.Errorf("social dispatch repository: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	var organizationID, workspaceID string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&organizationID, &workspaceID); err != nil {
		return fmt.Errorf("social dispatch repository: scope: %w", err)
	}
	if organizationID != scope.OrganizationID().String() || workspaceID != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := operation(tx); err != nil {
		return err
	}
	return tx.Commit()
}
