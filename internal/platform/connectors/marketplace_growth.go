package connectors

import (
	"context"
	"errors"
	"strings"
)

var ErrInvalidMarketplaceGrowthRequest = errors.New("connectors: invalid marketplace growth request")

// PromotionAdvertisingOperation is the typed connector boundary for a
// promotion or advertising write. It is intentionally smaller than a generic
// remote invocation and contains only normalized, non-secret references.
type PromotionAdvertisingOperation struct {
	Operation      string `json:"operation"`
	ChannelID      string `json:"channel_id"`
	AccountID      string `json:"account_id"`
	TargetID       string `json:"target_id"`
	InputDigest    string `json:"input_digest"`
	IdempotencyKey string `json:"idempotency_key"`
	DryRun         bool   `json:"dry_run"`
}

func (o PromotionAdvertisingOperation) Validate() error {
	if !validRemoteReadID(o.Operation) || !validRemoteReadID(o.ChannelID) || !validRemoteReadID(o.AccountID) || !validRemoteReadID(o.TargetID) || len(o.InputDigest) != 64 || o.InputDigest != strings.ToLower(o.InputDigest) || !validRemoteReadID(o.IdempotencyKey) || len(o.IdempotencyKey) > 128 {
		return ErrInvalidMarketplaceGrowthRequest
	}
	return nil
}

// PromotionAdvertisingResult is a sanitized remote outcome. Unknown means
// the request may have been accepted remotely and must be reconciled.
type PromotionAdvertisingResult struct {
	State             string `json:"state"`
	RemoteOperationID string `json:"remote_operation_id,omitempty"`
	ReadAfterWrite    bool   `json:"read_after_write"`
}

// PromotionAdvertisingManager is admitted only after promotions.manage or
// ads.manage qualification for the concrete connector account.
type PromotionAdvertisingManager interface {
	ApplyPromotionAdvertising(context.Context, Account, Runtime, PromotionAdvertisingOperation) (PromotionAdvertisingResult, error)
}
