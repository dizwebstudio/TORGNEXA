package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/inbox"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/inboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/outboxrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

// commerceWebhooksPathPrefix is public by design. The route resolves the
// tenant/account from the URL, then delegates authenticity and replay handling
// to the qualified connector and the host-owned inbox boundary.
const commerceWebhooksPathPrefix = webhookPathPrefix + "commerce/"

const commerceWebhookEventType = "commerce.storefront.webhook_received.v1"

type commerceWebhookAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
}

type commerceWebhookConfigs interface {
	Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error)
}

type commerceWebhookReceiverResolver interface {
	CommerceWebhookReceiver(sdk.Account, sdk.Runtime, builtinruntime.ConfigLoader) (builtinruntime.CommerceWebhookReceiver, error)
}

type commerceWebhookHeaderResolver func(string, http.Header) (signature, topic string, ok bool)

type commerceWebhookAPI struct {
	accounts commerceWebhookAccounts
	configs  commerceWebhookConfigs
	secrets  secrets.SecretProvider
	registry commerceWebhookReceiverResolver
	dedup    func(tenancy.Scope) sdk.CommerceWebhookDeduplicator
	headers  commerceWebhookHeaderResolver
}

func newCommerceWebhookRoutes(accounts commerceWebhookAccounts, configs commerceWebhookConfigs, secretSource secrets.SecretProvider, registry commerceWebhookReceiverResolver, processor *inboxrepo.Processor) []PublicWebhookRoute {
	if accounts == nil || configs == nil || secretSource == nil || registry == nil || processor == nil {
		return nil
	}
	api := commerceWebhookAPI{accounts: accounts, configs: configs, secrets: secretSource, registry: registry, dedup: func(scope tenancy.Scope) sdk.CommerceWebhookDeduplicator {
		return commerceWebhookDeduplicator{processor: processor, scope: scope}
	}, headers: builtinruntime.CommerceWebhookHeaders}
	return []PublicWebhookRoute{{Method: http.MethodPost, Path: commerceWebhooksPathPrefix, PathPrefix: true, Handler: http.HandlerFunc(api.receive)}}
}

// receive keeps rejections uniform until the qualified connector verifies a
// delivery. Failure at the subsequent inbox/outbox boundary requests redelivery.
func (api commerceWebhookAPI) receive(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default().With("event", "commerce.webhook_received")
	connectorID, organizationID, workspaceID, accountID, ok := parseCommerceWebhookPath(r.URL.Path)
	if !ok {
		logger.Warn("commerce webhook path malformed")
		acknowledgeWebhook(w)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Warn("commerce webhook body unreadable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		logger.Warn("commerce webhook scope invalid")
		acknowledgeWebhook(w)
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), organizationID, workspaceID, accountID)
	if err != nil || account.ConnectorID != connectorID || !commerceWebhookFamily(account.Family) || account.Status != sdk.AccountActive {
		logger.Warn("commerce webhook account unresolved or inactive")
		acknowledgeWebhook(w)
		return
	}
	headerResolver := api.headers
	if headerResolver == nil {
		headerResolver = builtinruntime.CommerceWebhookHeaders
	}
	signature, topic, ok := headerResolver(connectorID, r.Header)
	if !ok {
		logger.Warn("commerce webhook headers missing or unsupported")
		acknowledgeWebhook(w)
		return
	}
	if api.configs == nil {
		acknowledgeWebhook(w)
		return
	}
	references := r.URL.Query()["subscription"]
	if len(references) != 1 {
		acknowledgeWebhook(w)
		return
	}
	rawConfig, _, err := api.configs.Config(r.Context(), scope, account.ID)
	if err != nil {
		acknowledgeWebhook(w)
		return
	}
	expectedTopic, err := sdk.ExpectedCommerceWebhookTopic(rawConfig, references[0])
	if err != nil || topic != expectedTopic {
		acknowledgeWebhook(w)
		return
	}

	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		logger.Warn("commerce webhook runtime unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	receiver, err := api.registry.CommerceWebhookReceiver(account, runtime, func(context.Context, string) (json.RawMessage, error) { return rawConfig, nil })
	if err != nil {
		logger.Warn("commerce webhook receiver unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	request := sdk.CommerceWebhookRequest{
		Signature:     signature,
		HeaderTopic:   topic,
		ExpectedTopic: expectedTopic,
		Body:          body,
		ReceivedAt:    time.Now().UTC(),
	}
	if api.dedup == nil {
		logger.Warn("commerce webhook deduplicator unavailable")
		acknowledgeWebhook(w)
		return
	}
	commit := &commerceWebhookCommit{next: api.dedup(scope)}
	_, err = receiver.ReceiveCommerceWebhook(r.Context(), account, runtime, request, commit)
	if commit.failed {
		logger.Error("verified commerce webhook not committed")
		retryVerifiedWebhook(w)
		return
	}
	if err != nil {
		logger.Warn("commerce webhook verification failed")
	}
	acknowledgeWebhook(w)
}

// Qualified receivers call Claim only after signature and payload verification.
// Track the host-owned commit independently of connector error wrapping.
type commerceWebhookCommit struct {
	next   sdk.CommerceWebhookDeduplicator
	failed bool
}

func (commit *commerceWebhookCommit) ClaimCommerceWebhook(ctx context.Context, account sdk.Account, claim sdk.CommerceWebhookClaim) (bool, error) {
	if commit.next == nil {
		commit.failed = true
		return false, sdk.ErrInvalidCommerceWebhook
	}
	duplicate, err := commit.next.ClaimCommerceWebhook(ctx, account, claim)
	commit.failed = commit.failed || err != nil
	return duplicate, err
}

func (api commerceWebhookAPI) configLoader(scope tenancy.Scope) builtinruntime.ConfigLoader {
	return func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, err := api.configs.Config(ctx, scope, accountID)
		return raw, err
	}
}

func commerceWebhookFamily(family sdk.Family) bool {
	return family == sdk.FamilyMarketplace || family == sdk.FamilyStorefront
}

func normalizeCommerceWebhookTopic(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "."))
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return ""
	}
	validResource := map[string]struct{}{"order": {}, "product": {}, "coupon": {}, "customer": {}}
	validAction := map[string]struct{}{"created": {}, "updated": {}, "deleted": {}}
	if _, ok := validResource[parts[0]]; !ok {
		return ""
	}
	if _, ok := validAction[parts[1]]; !ok {
		return ""
	}
	return parts[0] + "." + parts[1]
}

func parseCommerceWebhookPath(path string) (connectorID, organizationID, workspaceID, accountID string, ok bool) {
	rest := strings.TrimPrefix(path, commerceWebhooksPathPrefix)
	if rest == path {
		return "", "", "", "", false
	}
	segments := strings.Split(rest, "/")
	if len(segments) != 4 {
		return "", "", "", "", false
	}
	for _, segment := range segments {
		if segment == "" {
			return "", "", "", "", false
		}
	}
	return segments[0], segments[1], segments[2], segments[3], true
}

type commerceWebhookDeduplicator struct {
	processor *inboxrepo.Processor
	scope     tenancy.Scope
}

func (dedup commerceWebhookDeduplicator) ClaimCommerceWebhook(ctx context.Context, account sdk.Account, claim sdk.CommerceWebhookClaim) (bool, error) {
	if dedup.processor == nil || !dedup.scope.Valid() || account.Validate() != nil || account.OrganizationID != dedup.scope.OrganizationID().String() || account.WorkspaceID != dedup.scope.WorkspaceID().String() || claim.Validate() != nil {
		return false, sdk.ErrInvalidCommerceWebhook
	}
	instant, err := domain.NewUTCInstant(claim.OccurredAt)
	if err != nil {
		return false, sdk.ErrInvalidCommerceWebhook
	}
	eventType, err := eventbus.ParseEventType(commerceWebhookEventType)
	if err != nil {
		return false, sdk.ErrInvalidCommerceWebhook
	}
	data, err := json.Marshal(struct {
		ConnectorAccountID string          `json:"connector_account_id"`
		EventType          string          `json:"event_type"`
		ResourceKind       string          `json:"resource_kind"`
		ResourceRemoteID   string          `json:"resource_remote_id"`
		CanonicalPayload   json.RawMessage `json:"canonical_payload"`
	}{ConnectorAccountID: account.ID, EventType: claim.EventType, ResourceKind: claim.ResourceKind, ResourceRemoteID: claim.ResourceRemoteID, CanonicalPayload: claim.CanonicalPayload})
	if err != nil {
		return false, err
	}
	eventID := commerceWebhookEventID(account.ID, claim.DeliveryID)
	event := eventbus.Event{ID: eventID, Type: eventType, OccurredAt: instant, OrganizationID: account.OrganizationID, WorkspaceID: account.WorkspaceID, EntityType: "storefront_webhook", EntityID: eventID, Source: "commerce-webhook", CorrelationID: claim.DeliveryID, Data: data}
	if err := event.Validate(); err != nil {
		return false, err
	}
	delivery := eventbus.Delivery{Event: event, Attempt: 1, FirstObservedAt: instant}
	result, err := dedup.processor.ProcessWithFirstObservedTime(ctx, dedup.scope, "commerce.webhook.v1", delivery, func(callCtx context.Context, tx *sql.Tx, item eventbus.Delivery) error {
		return enqueueCommerceWebhook(callCtx, tx, item.Event)
	})
	if err != nil {
		return false, err
	}
	return result == inbox.ResultDuplicate, nil
}

func enqueueCommerceWebhook(ctx context.Context, tx *sql.Tx, event eventbus.Event) error {
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, event)
}

func commerceWebhookEventID(accountID, deliveryID string) string {
	digest := sha256.Sum256(append(append([]byte(accountID), 0), deliveryID...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

var _ sdk.CommerceWebhookDeduplicator = commerceWebhookDeduplicator{}
