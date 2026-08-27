package worker

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	builtins "github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	runtimeconfigstore "github.com/torgnexa/torgnexa/internal/platform/postgres/connectorconfigrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

var ErrConnectorEntityUnsupported = errors.New("worker: connector reconciliation entity unsupported")

type runtimeRegistry struct {
	database *sql.DB
	configs  *runtimeconfigstore.Repository
	builtins *builtins.Registry
}

func newRuntimeRegistry(database *sql.DB) (*runtimeRegistry, error) {
	if database == nil {
		return nil, errors.New("worker: runtime registry database required")
	}
	configs, err := runtimeconfigstore.New(database)
	if err != nil {
		return nil, err
	}
	return &runtimeRegistry{database: database, configs: configs, builtins: builtins.New()}, nil
}

func (registry *runtimeRegistry) Source(ctx context.Context, scope tenancy.Scope, policy syncengine.Policy, account sdk.Account, manifest sdk.Manifest, runtime sdk.Runtime) (reconciliation.Source, error) {
	if registry == nil || registry.database == nil || registry.configs == nil || registry.builtins == nil || !scope.Valid() || policy.Validate() != nil || account.Validate() != nil || manifest.Validate() != nil || runtime == nil {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	entity := strings.TrimSuffix(policy.EntityType, "s")
	if entity != "product" {
		return nil, fmt.Errorf("%w: %s", ErrConnectorEntityUnsupported, policy.EntityType)
	}

	reader, err := registry.productReader(scope, account, runtime)
	if err != nil {
		return nil, err
	}
	return &productReconciliationSource{database: registry.database, account: account, reader: reader, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (registry *runtimeRegistry) configLoader(scope tenancy.Scope) builtins.ConfigLoader {
	return func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, err := registry.configs.Config(ctx, scope, accountID)
		return raw, err
	}
}

func (registry *runtimeRegistry) productReader(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (builtins.ProductReader, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	reader, err := registry.builtins.ProductReader(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return reader, err
}

func (registry *runtimeRegistry) productWriter(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.ProductWriter, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, reconciliation.ErrActionUnavailable
	}
	writer, err := registry.builtins.ProductWriter(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, reconciliation.ErrActionUnavailable
	}
	return writer, err
}

func (registry *runtimeRegistry) supportsProductWrite(account sdk.Account) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsProductWrite(account)
}

func (registry *runtimeRegistry) priceWriter(scope tenancy.Scope, account sdk.Account, runtime sdk.Runtime) (sdk.PriceWriter, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, reconciliation.ErrActionUnavailable
	}
	writer, err := registry.builtins.PriceWriter(account, runtime, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, reconciliation.ErrActionUnavailable
	}
	return writer, err
}

func (registry *runtimeRegistry) supportsPriceWrite(account sdk.Account) bool {
	if registry == nil || registry.builtins == nil {
		return false
	}
	return registry.builtins.SupportsPriceWrite(account)
}

func (registry *runtimeRegistry) socialPublisher(scope tenancy.Scope, account sdk.Account) (sdk.SocialPublisher, error) {
	if registry == nil || registry.builtins == nil || !scope.Valid() {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	publisher, err := registry.builtins.SocialPublisher(account, registry.configLoader(scope))
	if errors.Is(err, builtins.ErrUnavailable) {
		return nil, ErrConnectorSourceBridgeUnavailable
	}
	return publisher, err
}

type productReconciliationSource struct {
	database *sql.DB
	account  sdk.Account
	reader   builtins.ProductReader
	now      func() time.Time
}

func (source *productReconciliationSource) Scan(ctx context.Context, scope tenancy.Scope, req reconciliation.ScanRequest) (reconciliation.ScanPage, error) {
	if source == nil || source.database == nil || source.reader == nil || source.now == nil || !scope.Valid() || req.Validate() != nil {
		return reconciliation.ScanPage{}, reconciliation.ErrInvalid
	}
	page, err := source.reader.Read(ctx, sdk.PageRequest{Cursor: req.Cursor, Limit: req.Limit})
	if err != nil {
		return reconciliation.ScanPage{}, err
	}
	result := reconciliation.ScanPage{NextCursor: page.NextCursor, HasMore: page.NextCursor != "", RemoteObservedAt: source.now().UTC(), Subjects: make([]reconciliation.Subject, 0, len(page.Items))}
	for _, remote := range page.Items {
		subject, err := source.subject(ctx, scope, remote)
		if err != nil {
			return reconciliation.ScanPage{}, err
		}
		result.Subjects = append(result.Subjects, subject)
		if remote.ObservedAt.After(result.RemoteObservedAt) {
			result.RemoteObservedAt = remote.ObservedAt
		}
	}
	return result, nil
}

type localProductSnapshot struct {
	ID, Code, Title, Status string
	Version                 int64
}

func (source *productReconciliationSource) subject(ctx context.Context, scope tenancy.Scope, remote builtins.Product) (reconciliation.Subject, error) {
	mappingLocalID, mapped, err := source.mappingByRemote(ctx, scope, remote.ID)
	if err != nil {
		return reconciliation.Subject{}, err
	}
	var local localProductSnapshot
	localPresent := false
	canAutoMap := false
	if mapped {
		local, localPresent, err = source.productByID(ctx, scope, mappingLocalID)
		if err != nil {
			return reconciliation.Subject{}, err
		}
	} else if remote.Code != "" {
		local, localPresent, err = source.productByCode(ctx, scope, remote.Code)
		if err != nil {
			return reconciliation.Subject{}, err
		}
		canAutoMap = localPresent
	}
	subject := reconciliation.Subject{RemoteID: remote.ID, RemotePresent: true, RemoteFingerprint: productFingerprint(remote.Title), RemoteStatus: remote.Status, RemoteRevision: remote.Revision, ObservedAt: source.now().UTC(), CanAutoMap: canAutoMap}
	if mapped {
		subject.LocalEntityID = mappingLocalID
		subject.MappingLocalCount = 1
		subject.MappingRemoteCount = 1
	}
	if localPresent {
		subject.LocalEntityID = local.ID
		subject.LocalPresent = true
		subject.LocalFingerprint = productFingerprint(local.Title)
		subject.LocalStatus = local.Status
		subject.LocalVersion = local.Version
	}
	return subject, nil
}

func productFingerprint(title string) string {
	data, _ := json.Marshal(struct {
		Title string `json:"title"`
	}{strings.TrimSpace(title)})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (source *productReconciliationSource) mappingByRemote(ctx context.Context, scope tenancy.Scope, remoteID string) (string, bool, error) {
	var id string
	err := source.readTx(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, `SELECT local_entity_id FROM connector_entity_mappings WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 AND entity_type='product' AND remote_id=$4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), source.account.ID, remoteID).Scan(&id)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return id, err == nil, err
}

func (source *productReconciliationSource) productByID(ctx context.Context, scope tenancy.Scope, id string) (localProductSnapshot, bool, error) {
	return source.productLookup(ctx, scope, `id=$3`, id)
}

func (source *productReconciliationSource) productByCode(ctx context.Context, scope tenancy.Scope, code string) (localProductSnapshot, bool, error) {
	return source.productLookup(ctx, scope, `code=$3`, code)
}

func (source *productReconciliationSource) productLookup(ctx context.Context, scope tenancy.Scope, predicate, value string) (localProductSnapshot, bool, error) {
	var product localProductSnapshot
	statement := `SELECT id,code,title,status,version FROM products WHERE organization_id=$1 AND workspace_id=$2 AND ` + predicate + ` LIMIT 1`
	err := source.readTx(ctx, scope, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, statement, scope.OrganizationID().String(), scope.WorkspaceID().String(), value).Scan(&product.ID, &product.Code, &product.Title, &product.Status, &product.Version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return localProductSnapshot{}, false, nil
	}
	return product, err == nil, err
}

func (source *productReconciliationSource) readTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, err := source.database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var org, workspace string
	if err := tx.QueryRowContext(ctx, `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`, scope.OrganizationID().String(), scope.WorkspaceID().String()).Scan(&org, &workspace); err != nil {
		return err
	}
	if org != scope.OrganizationID().String() || workspace != scope.WorkspaceID().String() {
		return tenancy.ErrInvalidScope
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
