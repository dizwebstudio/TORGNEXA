// Package entitlementrepo implements tenant-scoped PostgreSQL entitlement and quota storage.
package entitlementrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/entitlements"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/lineage"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/auditrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/lineagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
)

const applyScope = `SELECT set_config('app.organization_id',$1,true),set_config('app.workspace_id',$2,true)`

type Repository struct{ database *sql.DB }

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, errors.New("entitlement repository: database required")
	}
	return &Repository{database: db}, nil
}

func (r *Repository) InstallRule(ctx context.Context, scope tenancy.Scope, rule entitlements.Rule, m entitlements.Mutation) error {
	if r == nil || r.database == nil || ctx == nil || !scope.Valid() || rule.Validate() != nil || m.Validate() != nil {
		return entitlements.ErrInvalid
	}
	if rule.OrganizationID != scope.OrganizationID().String() || rule.WorkspaceID != scope.WorkspaceID().String() {
		return entitlements.ErrInvalid
	}
	return r.withTx(ctx, scope, func(tx *sql.Tx) error {
		var existingID sql.NullString
		var maxVersion sql.NullInt64
		err := tx.QueryRowContext(ctx, `SELECT min(id),max(version) FROM entitlement_rules WHERE organization_id=$1 AND workspace_id=$2 AND feature_key=$3`, rule.OrganizationID, rule.WorkspaceID, rule.Feature.String()).Scan(&existingID, &maxVersion)
		if err != nil {
			return fmt.Errorf("entitlement repository: rule version: %w", err)
		}
		expected := int64(1)
		if maxVersion.Valid {
			expected = maxVersion.Int64 + 1
			if existingID.Valid && existingID.String != rule.ID {
				return entitlements.ErrConflict
			}
		}
		if rule.Version != expected {
			return entitlements.ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO entitlement_rules(id,organization_id,workspace_id,feature_key,enabled,source,version,effective_from,effective_until,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, rule.ID, rule.OrganizationID, rule.WorkspaceID, rule.Feature.String(), rule.Enabled, rule.Source, rule.Version, rule.EffectiveFrom, rule.EffectiveUntil, rule.CreatedAt)
		if err != nil {
			return fmt.Errorf("entitlement repository: insert rule: %w", err)
		}
		if err = appendEvidence(ctx, tx, scope, m, "entitlement.rule.installed", "entitlement_rule", rule.ID, audit.Summary{"feature": rule.Feature.String(), "enabled": rule.Enabled, "source": rule.Source, "version": rule.Version}, "governance.entitlement.rule_changed.v1", map[string]any{"rule_id": rule.ID, "feature": rule.Feature.String(), "enabled": rule.Enabled, "source": rule.Source, "version": rule.Version}, lineage.Ref{System: "torgnexa", EntityType: "entitlement_rule", EntityID: rule.ID, Version: lineage.VersionNumber(rule.Version)}); err != nil {
			return err
		}
		return nil
	})
}

func (r *Repository) InstallQuotaPolicy(ctx context.Context, scope tenancy.Scope, p entitlements.QuotaPolicy, m entitlements.Mutation) error {
	if r == nil || r.database == nil || ctx == nil || !scope.Valid() || p.Validate() != nil || m.Validate() != nil {
		return entitlements.ErrInvalid
	}
	if p.OrganizationID != scope.OrganizationID().String() || p.WorkspaceID != scope.WorkspaceID().String() {
		return entitlements.ErrInvalid
	}
	return r.withTx(ctx, scope, func(tx *sql.Tx) error {
		var existingID sql.NullString
		var maxVersion sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT min(id),max(version) FROM entitlement_quota_policies WHERE organization_id=$1 AND workspace_id=$2 AND metric_key=$3`, p.OrganizationID, p.WorkspaceID, p.Metric.String()).Scan(&existingID, &maxVersion); err != nil {
			return fmt.Errorf("entitlement repository: quota version: %w", err)
		}
		expected := int64(1)
		if maxVersion.Valid {
			expected = maxVersion.Int64 + 1
			if existingID.Valid && existingID.String != p.ID {
				return entitlements.ErrConflict
			}
		}
		if p.Version != expected {
			return entitlements.ErrConflict
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO entitlement_quota_policies(id,organization_id,workspace_id,metric_key,limit_value,window_kind,source,version,effective_from,effective_until,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, p.ID, p.OrganizationID, p.WorkspaceID, p.Metric.String(), p.Limit, string(p.Window), p.Source, p.Version, p.EffectiveFrom, p.EffectiveUntil, p.CreatedAt)
		if err != nil {
			return fmt.Errorf("entitlement repository: insert quota policy: %w", err)
		}
		return appendEvidence(ctx, tx, scope, m, "entitlement.quota_policy.installed", "entitlement_quota_policy", p.ID, audit.Summary{"metric": p.Metric.String(), "limit": p.Limit, "window": string(p.Window), "source": p.Source, "version": p.Version}, "governance.entitlement.quota_policy_changed.v1", map[string]any{"policy_id": p.ID, "metric": p.Metric.String(), "limit": p.Limit, "window": string(p.Window), "source": p.Source, "version": p.Version}, lineage.Ref{System: "torgnexa", EntityType: "entitlement_quota_policy", EntityID: p.ID, Version: lineage.VersionNumber(p.Version)})
	})
}

func (r *Repository) ResolveRule(ctx context.Context, scope entitlements.Scope, feature entitlements.FeatureKey, at time.Time) (entitlements.Rule, error) {
	ts, err := tenantScope(scope)
	if err != nil || !feature.Valid() || at.IsZero() || !at.Equal(at.UTC()) {
		return entitlements.Rule{}, entitlements.ErrInvalid
	}
	var out entitlements.Rule
	err = r.withReadTx(ctx, ts, func(tx *sql.Tx) error {
		var until sql.NullTime
		row := tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,feature_key,enabled,source,version,effective_from,effective_until,created_at FROM entitlement_rules WHERE organization_id=$1 AND workspace_id=$2 AND feature_key=$3 AND created_at<=$4 AND effective_from<=$4 AND (effective_until IS NULL OR effective_until>$4) ORDER BY version DESC LIMIT 1`, scope.OrganizationID(), scope.WorkspaceID(), feature.String(), at)
		var fk string
		if e := row.Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &fk, &out.Enabled, &out.Source, &out.Version, &out.EffectiveFrom, &until, &out.CreatedAt); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return entitlements.ErrNotFound
			}
			return e
		}
		out.Feature = entitlements.FeatureKey(fk)
		if until.Valid {
			v := until.Time.UTC()
			out.EffectiveUntil = &v
		}
		out.EffectiveFrom = out.EffectiveFrom.UTC()
		out.CreatedAt = out.CreatedAt.UTC()
		return out.Validate()
	})
	return out, err
}

func (r *Repository) ResolveQuotaPolicy(ctx context.Context, scope entitlements.Scope, metric entitlements.MetricKey, at time.Time) (entitlements.QuotaPolicy, error) {
	ts, err := tenantScope(scope)
	if err != nil || !metric.Valid() || at.IsZero() || !at.Equal(at.UTC()) {
		return entitlements.QuotaPolicy{}, entitlements.ErrInvalid
	}
	var out entitlements.QuotaPolicy
	err = r.withReadTx(ctx, ts, func(tx *sql.Tx) error {
		var until sql.NullTime
		var mk, w string
		row := tx.QueryRowContext(ctx, `SELECT id,organization_id,workspace_id,metric_key,limit_value,window_kind,source,version,effective_from,effective_until,created_at FROM entitlement_quota_policies WHERE organization_id=$1 AND workspace_id=$2 AND metric_key=$3 AND created_at<=$4 AND effective_from<=$4 AND (effective_until IS NULL OR effective_until>$4) ORDER BY version DESC LIMIT 1`, scope.OrganizationID(), scope.WorkspaceID(), metric.String(), at)
		if e := row.Scan(&out.ID, &out.OrganizationID, &out.WorkspaceID, &mk, &out.Limit, &w, &out.Source, &out.Version, &out.EffectiveFrom, &until, &out.CreatedAt); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return entitlements.ErrNotFound
			}
			return e
		}
		out.Metric = entitlements.MetricKey(mk)
		out.Window = entitlements.WindowKind(w)
		if until.Valid {
			v := until.Time.UTC()
			out.EffectiveUntil = &v
		}
		out.EffectiveFrom = out.EffectiveFrom.UTC()
		out.CreatedAt = out.CreatedAt.UTC()
		return out.Validate()
	})
	return out, err
}

func (r *Repository) ConsumeQuota(ctx context.Context, scope entitlements.Scope, p entitlements.QuotaPolicy, c entitlements.Consumption) (entitlements.QuotaStatus, error) {
	ts, err := tenantScope(scope)
	if err != nil || p.Validate() != nil || c.Validate() != nil || p.OrganizationID != scope.OrganizationID() || p.WorkspaceID != scope.WorkspaceID() || p.Metric != c.Metric || !p.Effective(c.OccurredAt) {
		return entitlements.QuotaStatus{}, entitlements.ErrInvalid
	}
	start, end, _ := p.Window.Bucket(c.OccurredAt)
	var out entitlements.QuotaStatus
	err = r.withTx(ctx, ts, func(tx *sql.Tx) error {
		// Serialize retries of the same immutable usage id before counter mutation.
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, scope.OrganizationID()+"\x00"+scope.WorkspaceID()+"\x00"+c.ID); e != nil {
			return e
		}
		var metric string
		var amount int64
		var existingStart, existingEnd time.Time
		var corr string
		var policyID string
		var policyVersion int64
		e := tx.QueryRowContext(ctx, `SELECT metric_key,amount,bucket_start,bucket_end,correlation_id,policy_id,policy_version FROM entitlement_quota_usage WHERE organization_id=$1 AND workspace_id=$2 AND id=$3`, scope.OrganizationID(), scope.WorkspaceID(), c.ID).Scan(&metric, &amount, &existingStart, &existingEnd, &corr, &policyID, &policyVersion)
		if e == nil {
			if metric != c.Metric.String() || amount != c.Amount || !existingStart.Equal(start) || !existingEnd.Equal(end) || corr != c.CorrelationID {
				return entitlements.ErrConflict
			}
			return loadStatus(ctx, tx, scope, p, start, end, &out)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO entitlement_quota_counters(organization_id,workspace_id,metric_key,bucket_start,bucket_end,used,limit_snapshot,policy_id,policy_version,updated_at) VALUES($1,$2,$3,$4,$5,0,$6,$7,$8,$9) ON CONFLICT(organization_id,workspace_id,metric_key,bucket_start) DO NOTHING`, scope.OrganizationID(), scope.WorkspaceID(), c.Metric.String(), start, end, p.Limit, p.ID, p.Version, c.OccurredAt)
		if e != nil {
			return e
		}
		var used int64
		if e = tx.QueryRowContext(ctx, `SELECT used FROM entitlement_quota_counters WHERE organization_id=$1 AND workspace_id=$2 AND metric_key=$3 AND bucket_start=$4 FOR UPDATE`, scope.OrganizationID(), scope.WorkspaceID(), c.Metric.String(), start).Scan(&used); e != nil {
			return e
		}
		if c.Amount > p.Limit || used > p.Limit-c.Amount {
			return entitlements.ErrQuotaExceeded
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO entitlement_quota_usage(id,organization_id,workspace_id,metric_key,amount,bucket_start,bucket_end,correlation_id,policy_id,policy_version,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, c.ID, scope.OrganizationID(), scope.WorkspaceID(), c.Metric.String(), c.Amount, start, end, c.CorrelationID, p.ID, p.Version, c.OccurredAt); e != nil {
			return e
		}
		used += c.Amount
		if _, e = tx.ExecContext(ctx, `UPDATE entitlement_quota_counters SET used=$5,limit_snapshot=$6,policy_id=$7,policy_version=$8,bucket_end=$9,updated_at=$10 WHERE organization_id=$1 AND workspace_id=$2 AND metric_key=$3 AND bucket_start=$4`, scope.OrganizationID(), scope.WorkspaceID(), c.Metric.String(), start, used, p.Limit, p.ID, p.Version, end, c.OccurredAt); e != nil {
			return e
		}
		out = entitlements.QuotaStatus{Metric: p.Metric, Limit: p.Limit, Used: used, Remaining: p.Limit - used, WindowStart: start, WindowEnd: end, PolicyID: p.ID, PolicyVersion: p.Version}
		return out.Validate()
	})
	return out, err
}

func (r *Repository) QuotaStatus(ctx context.Context, scope entitlements.Scope, p entitlements.QuotaPolicy, at time.Time) (entitlements.QuotaStatus, error) {
	ts, err := tenantScope(scope)
	if err != nil || p.Validate() != nil || !p.Effective(at) {
		return entitlements.QuotaStatus{}, entitlements.ErrInvalid
	}
	start, end, _ := p.Window.Bucket(at)
	var out entitlements.QuotaStatus
	err = r.withReadTx(ctx, ts, func(tx *sql.Tx) error { return loadStatus(ctx, tx, scope, p, start, end, &out) })
	return out, err
}

func loadStatus(ctx context.Context, tx *sql.Tx, scope entitlements.Scope, p entitlements.QuotaPolicy, start, end time.Time, out *entitlements.QuotaStatus) error {
	var used int64
	e := tx.QueryRowContext(ctx, `SELECT used FROM entitlement_quota_counters WHERE organization_id=$1 AND workspace_id=$2 AND metric_key=$3 AND bucket_start=$4`, scope.OrganizationID(), scope.WorkspaceID(), p.Metric.String(), start).Scan(&used)
	if errors.Is(e, sql.ErrNoRows) {
		used = 0
	} else if e != nil {
		return e
	}
	*out = entitlements.QuotaStatus{Metric: p.Metric, Limit: p.Limit, Used: used, Remaining: p.Limit - used, WindowStart: start, WindowEnd: end, PolicyID: p.ID, PolicyVersion: p.Version}
	if out.Remaining < 0 {
		return entitlements.ErrQuotaExceeded
	}
	return out.Validate()
}

func (r *Repository) withTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, e := r.database.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); e != nil {
		return e
	}
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit()
}
func (r *Repository) withReadTx(ctx context.Context, scope tenancy.Scope, fn func(*sql.Tx) error) error {
	tx, e := r.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, applyScope, scope.OrganizationID().String(), scope.WorkspaceID().String()); e != nil {
		return e
	}
	return fn(tx)
}
func tenantScope(s entitlements.Scope) (tenancy.Scope, error) {
	return tenancy.ParseScope(s.OrganizationID(), s.WorkspaceID())
}

func appendEvidence(ctx context.Context, tx *sql.Tx, scope tenancy.Scope, m entitlements.Mutation, action, resourceType, resourceID string, summary audit.Summary, eventType string, payload map[string]any, output lineage.Ref) error {
	safe, err := audit.SanitizeSummary(summary)
	if err != nil {
		return err
	}
	if err = auditrepo.AppendTransaction(ctx, tx, scope, audit.Record{ID: m.AuditID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), ActorID: m.ActorID, Source: m.Source, Action: action, ResourceType: resourceType, ResourceID: resourceID, CorrelationID: m.CorrelationID, Risk: audit.RiskWriteSensitive, Summary: safe, CreatedAt: m.OccurredAt}); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	typ, err := eventbus.ParseEventType(eventType)
	if err != nil {
		return err
	}
	at, err := domain.NewUTCInstant(m.OccurredAt)
	if err != nil {
		return err
	}
	ev := eventbus.Event{ID: m.EventID, Type: typ, OccurredAt: at, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: resourceType, EntityID: resourceID, Source: m.Source, CorrelationID: m.CorrelationID, CausationID: m.CausationID, ActorID: m.ActorID, TraceID: m.TraceID, Data: data}
	if err = ev.Validate(); err != nil {
		return err
	}
	enq, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	if err = enq.Enqueue(ctx, ev); err != nil {
		return err
	}
	ls, err := lineage.NewScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return err
	}
	lid, err := lineage.DeterministicID(m.EventID)
	if err != nil {
		return err
	}
	output.ObservedAt = &m.OccurredAt
	rec := lineage.Record{ID: lid, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), Source: m.Source, ActorID: m.ActorID, Operation: action, Output: output, Inputs: nil, Transformation: lineage.Transformation{Kind: "policy_mutation", ID: action, Version: "1"}, CorrelationID: m.CorrelationID, CausationID: m.CausationID, AuditID: m.AuditID, EventID: m.EventID, Result: lineage.ResultApplied, OccurredAt: m.OccurredAt}
	return lineagerepo.AppendTransaction(ctx, tx, ls, rec)
}
