package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/pricing"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

const (
	priceChangeAction       = "pricing.price.updated"
	priceChangeResourceType = "price"
)

type PriceChangeInput struct {
	PriceID         string `json:"price_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Currency        string `json:"currency"`
	MinorUnits      int64  `json:"minor_units"`
	Reason          string `json:"reason,omitempty"`
	IdempotencyKey  string `json:"idempotency_key"`
}

func (i PriceChangeInput) Validate() error {
	if _, err := pricing.ParsePriceID(i.PriceID); err != nil || i.ExpectedVersion < 1 || i.MinorUnits < 0 {
		return ErrInvalid
	}
	currency, err := pricing.NewCurrency(i.Currency)
	if err != nil {
		return ErrInvalid
	}
	if _, err := pricing.NewMoney(i.MinorUnits, currency); err != nil {
		return ErrInvalid
	}
	if !validIdempotencyKey(i.IdempotencyKey) {
		return ErrInvalid
	}
	if _, err := pricing.ParsePriceID(i.IdempotencyKey); err != nil {
		return ErrInvalid
	}
	if i.Reason != "" {
		safe, err := approval.SanitizeComment(i.Reason)
		if err != nil || safe != i.Reason {
			return ErrInvalid
		}
	}
	return nil
}

type MutationStatus string

const (
	MutationCompleted        MutationStatus = "completed"
	MutationQueued           MutationStatus = "queued"
	MutationApprovalRequired MutationStatus = "approval_required"
	MutationDenied           MutationStatus = "denied"
	MutationFailed           MutationStatus = "failed"
)

type MutationResult struct {
	Status            MutationStatus `json:"status"`
	ApprovalRequestID string         `json:"approval_request_id,omitempty"`
	IntentSHA256      string         `json:"intent_sha256"`
	ExpiresAt         *time.Time     `json:"expires_at,omitempty"`
}

type ApprovalRequestStore interface {
	ResolvePolicy(context.Context, tenancy.Scope, string, string, approval.RiskClass) (approval.Policy, error)
	Request(context.Context, tenancy.Scope, string) (approval.Request, error)
	CreateRequest(context.Context, tenancy.Scope, string, string, approval.RequestCommand) (approval.Request, error)
}

type EvidenceIDGenerator interface {
	NewID() (string, error)
}

type Clock interface {
	Now() time.Time
}

type ApprovalPriceChangeRequester struct {
	store ApprovalRequestStore
	ids   EvidenceIDGenerator
	clock Clock
}

func NewApprovalPriceChangeRequester(store ApprovalRequestStore, ids EvidenceIDGenerator, clock Clock) (*ApprovalPriceChangeRequester, error) {
	if store == nil {
		return nil, ErrInvalid
	}
	if ids == nil {
		ids = sortableIDs{}
	}
	if clock == nil {
		clock = wallClock{}
	}
	return &ApprovalPriceChangeRequester{store: store, ids: ids, clock: clock}, nil
}

func (r *ApprovalPriceChangeRequester) RequestPriceChange(ctx context.Context, identity Identity, input PriceChangeInput) (MutationResult, error) {
	if r == nil || r.store == nil || !identity.Valid() || input.Validate() != nil {
		return MutationResult{}, ErrInvalid
	}

	digest, err := priceIntentDigest(input)
	if err != nil {
		return MutationResult{}, ErrInvalid
	}
	result := MutationResult{Status: MutationDenied, IntentSHA256: digest}
	resourceID := input.PriceID + "#sha256=" + digest

	// Replay the exact durable request before resolving today's policy. Existing
	// requests retain their original policy version and remain stable across policy changes.
	if existing, lookupErr := r.store.Request(ctx, identity.Tenant, input.IdempotencyKey); lookupErr == nil {
		return mutationResultForExisting(existing, identity, resourceID, digest)
	} else if !errors.Is(lookupErr, approval.ErrInvalid) {
		return MutationResult{}, lookupErr
	}

	policy, err := r.store.ResolvePolicy(ctx, identity.Tenant, priceChangeAction, priceChangeResourceType, approval.RiskWriteSensitive)
	if err != nil {
		if errors.Is(err, approval.ErrDenied) {
			return result, nil
		}
		return MutationResult{}, err
	}
	if !policy.Matches(priceChangeAction, priceChangeResourceType, approval.RiskWriteSensitive) || approval.GateDecision(approval.RiskWriteSensitive, true) != approval.DecisionRequire {
		return result, nil
	}

	auditID, err := r.ids.NewID()
	if err != nil {
		return MutationResult{}, err
	}
	eventID, err := r.ids.NewID()
	if err != nil {
		return MutationResult{}, err
	}
	now := r.clock.Now().UTC()
	if now.IsZero() {
		return MutationResult{}, ErrInvalid
	}

	correlationID := "mcp-price:" + digest[:32]
	req, err := r.store.CreateRequest(ctx, identity.Tenant, priceChangeAction, priceChangeResourceType, approval.RequestCommand{
		RequestID:  input.IdempotencyKey,
		ResourceID: resourceID,
		Risk:       approval.RiskWriteSensitive,
		Mutation: approval.Mutation{
			AuditID:       auditID,
			EventID:       eventID,
			ActorID:       identity.ActorID,
			Source:        "mcp",
			CorrelationID: correlationID,
			OccurredAt:    now,
		},
	})
	if err != nil {
		if errors.Is(err, approval.ErrDenied) {
			return result, nil
		}
		// A concurrent retry may have committed the same caller-owned request id.
		if existing, lookupErr := r.store.Request(ctx, identity.Tenant, input.IdempotencyKey); lookupErr == nil {
			return mutationResultForExisting(existing, identity, resourceID, digest)
		}
		return MutationResult{}, err
	}
	expires := req.ExpiresAt
	return MutationResult{Status: MutationApprovalRequired, ApprovalRequestID: req.ID, IntentSHA256: digest, ExpiresAt: &expires}, nil
}

func mutationResultForExisting(req approval.Request, identity Identity, resourceID, digest string) (MutationResult, error) {
	if req.ID == "" || req.RequesterID != identity.ActorID || req.Action != priceChangeAction || req.ResourceType != priceChangeResourceType || req.ResourceID != resourceID || req.Risk != approval.RiskWriteSensitive {
		return MutationResult{}, ErrInvalid
	}
	status := MutationApprovalRequired
	switch req.State {
	case approval.StatePending:
		status = MutationApprovalRequired
	case approval.StateApproved, approval.StateExecuting:
		status = MutationQueued
	case approval.StateCompleted:
		status = MutationCompleted
	case approval.StateRejected, approval.StateExpired, approval.StateCancelled:
		status = MutationDenied
	case approval.StateFailed:
		status = MutationFailed
	default:
		return MutationResult{}, ErrInvalid
	}
	expires := req.ExpiresAt
	return MutationResult{Status: status, ApprovalRequestID: req.ID, IntentSHA256: digest, ExpiresAt: &expires}, nil
}

func priceIntentDigest(input PriceChangeInput) (string, error) {
	canonical := struct {
		PriceID         string `json:"price_id"`
		ExpectedVersion int64  `json:"expected_version"`
		Currency        string `json:"currency"`
		MinorUnits      int64  `json:"minor_units"`
		Reason          string `json:"reason,omitempty"`
		IdempotencyKey  string `json:"idempotency_key"`
	}{input.PriceID, input.ExpectedVersion, input.Currency, input.MinorUnits, input.Reason, input.IdempotencyKey}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func validIdempotencyKey(v string) bool {
	if len(v) < 1 || len(v) > 128 || strings.TrimSpace(v) != v {
		return false
	}
	for _, r := range v {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:/-", r) {
			continue
		}
		return false
	}
	return true
}

type sortableIDs struct{}

func (sortableIDs) NewID() (string, error) {
	id, err := tenancy.NewOrganizationID()
	return id.String(), err
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now() }
