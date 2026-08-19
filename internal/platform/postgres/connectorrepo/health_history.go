package connectorrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const MaxHealthHistory = 100

type HealthSnapshot struct {
	AccountID          string     `json:"account_id"`
	Status             string     `json:"status"`
	Category           string     `json:"category"`
	ReasonCode         string     `json:"reason_code,omitempty"`
	RateLimitRemaining *int64     `json:"rate_limit_remaining,omitempty"`
	RateLimitResetAt   *time.Time `json:"rate_limit_reset_at,omitempty"`
	CheckedAt          time.Time  `json:"checked_at"`
}

func (repository *Repository) HealthHistory(ctx context.Context, scope tenancy.Scope, accountID string, limit int) ([]HealthSnapshot, error) {
	if err := validateRepositoryCall(ctx, repository); err != nil {
		return nil, err
	}
	if !scope.Valid() || accountID == "" || len(accountID) > 128 || limit < 1 || limit > MaxHealthHistory {
		return nil, sdk.ErrInvalidAccount
	}
	out := make([]HealthSnapshot, 0, limit)
	err := repository.withTx(ctx, scope, true, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT status,category,reason_code,rate_limit_remaining,rate_limit_reset_at,checked_at FROM connector_health_history WHERE organization_id=$1 AND workspace_id=$2 AND connector_account_id=$3 ORDER BY checked_at DESC,sequence_id DESC LIMIT $4`, scope.OrganizationID().String(), scope.WorkspaceID().String(), accountID, limit)
		if err != nil {
			return fmt.Errorf("connector health history: list: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var item HealthSnapshot
			var reason sql.NullString
			var remaining sql.NullInt64
			var reset sql.NullTime
			item.AccountID = accountID
			if err := rows.Scan(&item.Status, &item.Category, &reason, &remaining, &reset, &item.CheckedAt); err != nil {
				return err
			}
			if reason.Valid {
				item.ReasonCode = reason.String
			}
			if remaining.Valid {
				v := remaining.Int64
				item.RateLimitRemaining = &v
			}
			if reset.Valid {
				v := reset.Time.UTC()
				item.RateLimitResetAt = &v
			}
			item.CheckedAt = item.CheckedAt.UTC()
			out = append(out, item)
		}
		return rows.Err()
	})
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func healthCategory(h sdk.Health) string {
	switch h.ReasonCode {
	case "credentials_missing", "credentials_invalid", "credentials_unavailable", "remote_check_not_configured":
		return "configuration_error"
	case "auth_rejected", "oauth_exchange_failed":
		return "authentication_error"
	case "rate_limited":
		return "rate_limited"
	case "remote_unavailable", "provider_unavailable", "connector_health_failed", "upstream_timeout":
		return "remote_unavailable"
	}
	switch h.Status {
	case sdk.HealthHealthy:
		return "healthy"
	case sdk.HealthDegraded:
		return "degraded"
	default:
		return "remote_unavailable"
	}
}
