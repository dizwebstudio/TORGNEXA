package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/approval"
)

type approvalStoreFake struct {
	policy               approval.Policy
	resolveErr           error
	existing             *approval.Request
	created              int
	action, resourceType string
	cmd                  approval.RequestCommand
}

func (f *approvalStoreFake) ResolvePolicy(context.Context, tenancy.Scope, string, string, approval.RiskClass) (approval.Policy, error) {
	return f.policy, f.resolveErr
}
func (f *approvalStoreFake) Request(context.Context, tenancy.Scope, string) (approval.Request, error) {
	if f.existing == nil {
		return approval.Request{}, approval.ErrInvalid
	}
	return *f.existing, nil
}
func (f *approvalStoreFake) CreateRequest(_ context.Context, _ tenancy.Scope, action, resourceType string, cmd approval.RequestCommand) (approval.Request, error) {
	f.created++
	f.action = action
	f.resourceType = resourceType
	f.cmd = cmd
	return approval.Request{ID: cmd.RequestID, ExpiresAt: time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)}, nil
}

type seqIDs struct {
	ids []string
	i   int
}

func (g *seqIDs) NewID() (string, error) {
	if g.i >= len(g.ids) {
		return "", errors.New("exhausted")
	}
	v := g.ids[g.i]
	g.i++
	return v, nil
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func validPolicy() approval.Policy {
	return approval.Policy{ID: "018f0000-0000-7000-8000-000000000301", OrganizationID: testOrgID, WorkspaceID: testWSID, Name: "mcp-price", Action: priceChangeAction, ResourceType: priceChangeResourceType, MinimumRisk: approval.RiskWriteSensitive, Version: 1, RequestTTL: time.Hour, SeparationOfDuties: true, Stages: []approval.Stage{{Number: 1, Name: "finance", RequiredApprovals: 1, EligibleScopes: []string{"price.approve"}}}, Active: true}
}
func validPriceInput() PriceChangeInput {
	return PriceChangeInput{PriceID: "018f0000-0000-7000-8000-000000000111", ExpectedVersion: 3, Currency: "RUB", MinorUnits: 12345, Reason: "promo adjustment", IdempotencyKey: "018f0000-0000-7000-8000-000000000499"}
}

func TestPriceChangeCreatesApprovalRequestNotMutation(t *testing.T) {
	store := &approvalStoreFake{policy: validPolicy()}
	ids := &seqIDs{ids: []string{"018f0000-0000-7000-8000-000000000402", "018f0000-0000-7000-8000-000000000403"}}
	now := time.Date(2026, 8, 11, 7, 0, 0, 0, time.UTC)
	r, _ := NewApprovalPriceChangeRequester(store, ids, fixedClock{now})
	result, err := r.RequestPriceChange(context.Background(), testIdentity(t, permissionPriceChangeRequest), validPriceInput())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != MutationApprovalRequired || result.ApprovalRequestID == "" || len(result.IntentSHA256) != 64 {
		t.Fatalf("result=%#v", result)
	}
	if store.created != 1 || store.action != priceChangeAction || store.resourceType != priceChangeResourceType {
		t.Fatalf("approval not created correctly: %#v", store)
	}
	if store.cmd.Risk != approval.RiskWriteSensitive || store.cmd.Mutation.ActorID != "user-42" || store.cmd.Mutation.Source != "mcp" || !store.cmd.Mutation.OccurredAt.Equal(now) {
		t.Fatalf("command=%#v", store.cmd)
	}
	if !strings.HasPrefix(store.cmd.ResourceID, validPriceInput().PriceID+"#sha256=") || !strings.HasSuffix(store.cmd.ResourceID, result.IntentSHA256) {
		t.Fatalf("intent not bound to approval resource: %q", store.cmd.ResourceID)
	}
}

func TestPriceChangeReplayReturnsSameApprovalRequest(t *testing.T) {
	input := validPriceInput()
	digest, err := priceIntentDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC)
	existing := approval.Request{
		ID: input.IdempotencyKey, RequesterID: "user-42", Action: priceChangeAction,
		ResourceType: priceChangeResourceType, ResourceID: input.PriceID + "#sha256=" + digest,
		Risk: approval.RiskWriteSensitive, State: approval.StatePending, ExpiresAt: expires,
	}
	store := &approvalStoreFake{existing: &existing, resolveErr: errors.New("policy must not be resolved for replay")}
	r, _ := NewApprovalPriceChangeRequester(store, &seqIDs{}, fixedClock{time.Now()})
	result, err := r.RequestPriceChange(context.Background(), testIdentity(t, permissionPriceChangeRequest), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != MutationApprovalRequired || result.ApprovalRequestID != input.IdempotencyKey || store.created != 0 {
		t.Fatalf("replay result=%#v created=%d", result, store.created)
	}
}

func TestPriceChangeRejectsIdempotencyKeyReuseForDifferentIntent(t *testing.T) {
	original := validPriceInput()
	digest, _ := priceIntentDigest(original)
	existing := approval.Request{
		ID: original.IdempotencyKey, RequesterID: "user-42", Action: priceChangeAction,
		ResourceType: priceChangeResourceType, ResourceID: original.PriceID + "#sha256=" + digest,
		Risk: approval.RiskWriteSensitive, State: approval.StatePending, ExpiresAt: time.Date(2026, 8, 12, 6, 0, 0, 0, time.UTC),
	}
	store := &approvalStoreFake{existing: &existing}
	r, _ := NewApprovalPriceChangeRequester(store, &seqIDs{}, fixedClock{time.Now()})
	changed := original
	changed.MinorUnits++
	_, err := r.RequestPriceChange(context.Background(), testIdentity(t, permissionPriceChangeRequest), changed)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
	if store.created != 0 {
		t.Fatalf("created=%d", store.created)
	}
}

func TestPriceChangeDeniedWithoutPolicy(t *testing.T) {
	store := &approvalStoreFake{resolveErr: approval.ErrDenied}
	r, _ := NewApprovalPriceChangeRequester(store, &seqIDs{}, fixedClock{time.Now()})
	result, err := r.RequestPriceChange(context.Background(), testIdentity(t, permissionPriceChangeRequest), validPriceInput())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != MutationDenied || store.created != 0 || len(result.IntentSHA256) != 64 {
		t.Fatalf("result=%#v created=%d", result, store.created)
	}
}

func TestPriceChangeRejectsSecretReasonAndInvalidMoney(t *testing.T) {
	for _, in := range []PriceChangeInput{
		func() PriceChangeInput { v := validPriceInput(); v.Reason = "Bearer abc.def.ghi"; return v }(),
		func() PriceChangeInput { v := validPriceInput(); v.Currency = "rub"; return v }(),
		func() PriceChangeInput { v := validPriceInput(); v.MinorUnits = -1; return v }(),
	} {
		if in.Validate() == nil {
			t.Fatalf("expected invalid input: %#v", in)
		}
	}
}
