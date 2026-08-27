package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
	builtins "github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorauth"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/approvalrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectormaprepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/connectorrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/notificationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/reconciliation"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

type reconciliationActionExecutor struct {
	syncRepo      syncengine.Repository
	accounts      *connectorrepo.Repository
	mappings      *connectormaprepo.Repository
	catalog       *catalogrepo.Repository
	approvals     *approvalrepo.Repository
	notifications *notificationrepo.Repository
	secrets       secrets.SecretProvider
	oauthRefresh  connectorauth.RefreshCoordinator
	registry      *runtimeRegistry
}

func newReconciliationActionExecutor(syncRepo syncengine.Repository, accounts *connectorrepo.Repository, mappings *connectormaprepo.Repository, catalogRepository *catalogrepo.Repository, approvals *approvalrepo.Repository, notificationRepository *notificationrepo.Repository, secretSource secrets.SecretProvider, refreshCoordinator connectorauth.RefreshCoordinator, registry *runtimeRegistry) (*reconciliationActionExecutor, error) {
	if syncRepo == nil || accounts == nil || mappings == nil || catalogRepository == nil || approvals == nil || notificationRepository == nil || secretSource == nil || refreshCoordinator == nil || registry == nil {
		return nil, errors.New("worker: reconciliation action dependencies required")
	}
	return &reconciliationActionExecutor{syncRepo: syncRepo, accounts: accounts, mappings: mappings, catalog: catalogRepository, approvals: approvals, notifications: notificationRepository, secrets: secretSource, oauthRefresh: refreshCoordinator, registry: registry}, nil
}

type actionContext struct {
	policy  syncengine.Policy
	account sdk.Account
	runtime sdk.Runtime
}

func (e *reconciliationActionExecutor) resolve(ctx context.Context, scope tenancy.Scope, policyID string) (actionContext, error) {
	p, err := e.syncRepo.Policy(ctx, scope, policyID)
	if err != nil {
		return actionContext{}, err
	}
	a, err := e.accounts.AccountByID(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), p.ConnectorAccountID)
	if err != nil {
		return actionContext{}, err
	}
	runtime, err := connectorruntime.NewForAccount(e.secrets, e.oauthRefresh, scope, a)
	if err != nil {
		return actionContext{}, err
	}
	return actionContext{policy: p, account: a, runtime: runtime}, nil
}

func (e *reconciliationActionExecutor) AutoFix(ctx context.Context, scope tenancy.Scope, req reconciliation.RepairRequest) error {
	if ctx == nil || !scope.Valid() || req.IdempotencyKey == "" {
		return reconciliation.ErrInvalid
	}
	ac, err := e.resolve(ctx, scope, req.Drift.PolicyID)
	if err != nil {
		return err
	}
	if trimEntity(ac.policy.EntityType) != "product" {
		return reconciliation.ErrActionUnavailable
	}
	switch req.Direction {
	case reconciliation.RepairCreateMapping:
		if req.Drift.LocalEntityID == "" || req.Drift.RemoteID == "" {
			return reconciliation.ErrActionUnsafe
		}
		_, err = e.mappings.UpsertMapping(ctx, sdk.MappingUpsert{OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), ConnectorAccountID: ac.account.ID, EntityType: "product", LocalEntityID: req.Drift.LocalEntityID, RemoteID: req.Drift.RemoteID, ExpectedVersion: 0})
		if errors.Is(err, sdk.ErrMappingConflict) {
			existing, lookup := e.mappings.MappingByLocal(ctx, scope.OrganizationID().String(), scope.WorkspaceID().String(), ac.account.ID, "product", req.Drift.LocalEntityID)
			if lookup == nil && existing.RemoteID == req.Drift.RemoteID {
				return nil
			}
		}
		return err
	case reconciliation.RepairRemoteToLocal:
		remote, err := e.findRemoteProduct(ctx, scope, ac, req.Drift.RemoteID)
		if err != nil {
			return err
		}
		coreScope, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		pid, err := catalog.ParseProductID(req.Drift.LocalEntityID)
		if err != nil {
			return err
		}
		current, err := e.catalog.Product(ctx, coreScope, pid)
		if err != nil {
			return err
		}
		mutation := catalog.Mutation{EventID: stableID("evt_rec_", req.IdempotencyKey), OccurredAt: time.Now().UTC(), Source: "worker.reconciliation", CorrelationID: stableID("corr_rec_", req.IdempotencyKey), CausationID: req.Drift.ID}
		switch req.Drift.Kind {
		case reconciliation.DriftContent:
			if current.Title == remote.Title {
				return nil
			}
			_, err = e.catalog.UpdateProduct(ctx, coreScope, catalog.UpdateProduct{ID: pid, ExpectedVersion: current.Version, Title: remote.Title, Description: current.Description}, mutation)
			return err
		case reconciliation.DriftStatusMismatch:
			target, ok := catalogStatus(remote.Status)
			if !ok {
				return reconciliation.ErrActionUnsafe
			}
			if current.Status == target {
				return nil
			}
			_, err = e.catalog.ChangeProductStatus(ctx, coreScope, catalog.ChangeProductStatus{ID: pid, ExpectedVersion: current.Version, Status: target}, mutation)
			return err
		default:
			return reconciliation.ErrActionUnsafe
		}
	case reconciliation.RepairLocalToRemote:
		writer, err := e.registry.productWriter(scope, ac.account, ac.runtime)
		if err != nil {
			return err
		}
		coreScope, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
		if err != nil {
			return err
		}
		pid, err := catalog.ParseProductID(req.Drift.LocalEntityID)
		if err != nil {
			return err
		}
		current, err := e.catalog.Product(ctx, coreScope, pid)
		if err != nil {
			return err
		}
		status := "draft"
		if current.Status == catalog.StatusActive {
			status = "publish"
		}
		if current.Status == catalog.StatusArchived {
			status = "private"
		}
		_, err = writer.UpsertProduct(ctx, ac.account, ac.runtime, sdk.ProductWriteRequest{RemoteID: req.Drift.RemoteID, SellerSKU: current.Code, Title: current.Title, Description: current.Description, StatusRemoteID: status, IdempotencyKey: req.IdempotencyKey})
		return err
	default:
		return reconciliation.ErrActionUnsafe
	}
}

func (e *reconciliationActionExecutor) findRemoteProduct(ctx context.Context, scope tenancy.Scope, ac actionContext, remoteID string) (builtins.Product, error) {
	reader, err := e.registry.productReader(scope, ac.account, ac.runtime)
	if err != nil {
		return builtins.Product{}, err
	}
	cursor := ""
	for pageNo := 0; pageNo < 100; pageNo++ {
		page, err := reader.Read(ctx, sdk.PageRequest{Cursor: cursor, Limit: 1000})
		if err != nil {
			return builtins.Product{}, err
		}
		for _, item := range page.Items {
			if item.ID == remoteID {
				return item, nil
			}
		}
		if page.NextCursor == "" {
			return builtins.Product{}, errors.New("worker: remote product not found")
		}
		if page.NextCursor == cursor {
			return builtins.Product{}, errors.New("worker: remote cursor did not advance")
		}
		cursor = page.NextCursor
	}
	return builtins.Product{}, errors.New("worker: remote product lookup page limit exceeded")
}

func (e *reconciliationActionExecutor) Notify(ctx context.Context, scope tenancy.Scope, req reconciliation.NotifyRequest) error {
	if ctx == nil || !scope.Valid() || req.IdempotencyKey == "" {
		return reconciliation.ErrInvalid
	}
	// Persist a workspace-level operator notification. The reserved recipient is
	// also useful to webhook/projector consumers; per-user fan-out remains a P1 delivery concern.
	now := time.Now().UTC()
	n := notifications.Notification{ID: stableID("ntf_rec_", req.IdempotencyKey), RecipientID: "workspace-operators", DedupeKey: req.IdempotencyKey, Severity: notifications.SeverityWarning, Title: "Reconciliation requires attention", Body: fmt.Sprintf("Drift %s (%s) was detected for policy %s.", req.Drift.ID, req.Drift.Kind, req.Drift.PolicyID), EntityType: "reconciliation_drift", EntityID: req.Drift.ID, OccurrenceCount: 1, FirstOccurredAt: now, LastOccurredAt: now, CreatedAt: now, UpdatedAt: now}
	_, _, err := e.notifications.Upsert(ctx, scope, n)
	return err
}

func (e *reconciliationActionExecutor) RequestApproval(ctx context.Context, scope tenancy.Scope, req reconciliation.ApprovalRequest) error {
	if ctx == nil || !scope.Valid() || req.IdempotencyKey == "" {
		return reconciliation.ErrInvalid
	}
	requestID := stableUUID("approval:" + req.IdempotencyKey)
	if existing, err := e.approvals.Request(ctx, scope, requestID); err == nil && existing.ID == requestID {
		return nil
	}
	now := time.Now().UTC()
	mutation := approval.Mutation{AuditID: stableUUID("audit:" + req.IdempotencyKey), EventID: stableUUID("event:" + req.IdempotencyKey), ActorID: "system:reconciliation", Source: "worker.reconciliation", CorrelationID: stableUUID("correlation:" + req.IdempotencyKey), CausationID: req.Drift.ID, OccurredAt: now}
	_, err := e.approvals.CreateRequest(ctx, scope, "reconciliation.repair", "reconciliation_drift", approval.RequestCommand{RequestID: requestID, ResourceID: req.Drift.ID, Risk: approval.RiskWriteSensitive, Mutation: mutation})
	return err
}

func actionPolicyFor(policy syncengine.Policy, outboundWritable bool) reconciliation.ActionPolicy {
	result := reconciliation.ActionPolicy{MissingMapping: reconciliation.ActionAutoFix, OrphanMapping: reconciliation.ActionNotify, DuplicateMapping: reconciliation.ActionNotify, StaleConnector: reconciliation.ActionNotify}
	switch policy.SourceOfTruth {
	case syncengine.SourceRemote:
		if policy.Direction.AllowsInbound() {
			result.Content = reconciliation.ActionAutoFix
			result.StatusMismatch = reconciliation.ActionAutoFix
		} else {
			result.Content = reconciliation.ActionApproval
			result.StatusMismatch = reconciliation.ActionApproval
		}
	case syncengine.SourceLocal:
		if policy.Direction.AllowsOutbound() && outboundWritable {
			result.Content = reconciliation.ActionAutoFix
			result.StatusMismatch = reconciliation.ActionAutoFix
		} else {
			// Do not create approvals for an operation that the runtime cannot
			// execute. Persist operator-visible evidence and finish the run instead
			// of creating an infinite approval/retry loop.
			result.Content = reconciliation.ActionNotify
			result.StatusMismatch = reconciliation.ActionNotify
		}
	default:
		result.Content = reconciliation.ActionApproval
		result.StatusMismatch = reconciliation.ActionApproval
	}
	return result
}

func catalogStatus(remote string) (catalog.Status, bool) {
	switch strings.ToLower(strings.TrimSpace(remote)) {
	case "active", "publish", "published":
		return catalog.StatusActive, true
	case "draft":
		return catalog.StatusDraft, true
	case "archived", "archive", "private":
		return catalog.StatusArchived, true
	default:
		return "", false
	}
}

func trimEntity(v string) string {
	if len(v) > 1 && v[len(v)-1] == 's' {
		return v[:len(v)-1]
	}
	return v
}
func stableID(prefix, key string) string {
	sum := sha256.Sum256([]byte(key))
	return prefix + hex.EncodeToString(sum[:16])
}

func stableUUID(key string) string {
	sum := sha256.Sum256([]byte(key))
	b := append([]byte(nil), sum[:16]...)
	// RFC 4122-shaped deterministic UUID. The bytes are derived solely from
	// the idempotency key, so retries resolve to exactly the same evidence IDs.
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

var _ reconciliation.ActionExecutor = (*reconciliationActionExecutor)(nil)
