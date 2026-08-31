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

type commerceWebhookAPI struct {
	accounts commerceWebhookAccounts
	configs  commerceWebhookConfigs
	secrets  secrets.SecretProvider
	registry commerceWebhookReceiverResolver
	dedup    func(tenancy.Scope) sdk.CommerceWebhookDeduplicator
}

func newCommerceWebhookRoutes(accounts commerceWebhookAccounts, configs commerceWebhookConfigs, secretSource secrets.SecretProvider, registry commerceWebhookReceiverResolver, processor *inboxrepo.Processor) []PublicWebhookRoute {
	if accounts == nil || configs == nil || secretSource == nil || registry == nil || processor == nil {
		return nil
	}
	api := commerceWebhookAPI{accounts: accounts, configs: configs, secrets: secretSource, registry: registry, dedup: func(scope tenancy.Scope) sdk.CommerceWebhookDeduplicator {
		return commerceWebhookDeduplicator{processor: processor, scope: scope}
	}}
	return []PublicWebhookRoute{{Method: http.MethodPost, Path: commerceWebhooksPathPrefix, PathPrefix: true, Handler: http.HandlerFunc(api.receive)}}
}

// receive always acknowledges with 200. Provider retry behavior must not
// reveal whether an account exists or whether a signature matched; only a
// verified delivery reaches the durable inbox/outbox boundary.
func (api commerceWebhookAPI) receive(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default().With("event", "commerce.webhook_received", "path", r.URL.Path)
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
	signature, topic, ok := commerceWebhookHeaders(r, connectorID)
	if !ok {
		logger.Warn("commerce webhook headers missing or unsupported")
		acknowledgeWebhook(w)
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		logger.Warn("commerce webhook runtime unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	receiver, err := api.registry.CommerceWebhookReceiver(account, runtime, api.configLoader(scope))
	if err != nil {
		logger.Warn("commerce webhook receiver unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	request := sdk.CommerceWebhookRequest{
		Signature:     signature,
		HeaderTopic:   topic,
		ExpectedTopic: topic,
		Body:          body,
		ReceivedAt:    time.Now().UTC(),
	}
	if api.dedup == nil {
		logger.Warn("commerce webhook deduplicator unavailable")
		acknowledgeWebhook(w)
		return
	}
	if _, err := receiver.ReceiveCommerceWebhook(r.Context(), account, runtime, request, api.dedup(scope)); err != nil {
		logger.Warn("commerce webhook verification or publication failed", "error", err)
	}
	acknowledgeWebhook(w)
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

func commerceWebhookHeaders(r *http.Request, connectorID string) (signature, topic string, ok bool) {
	var rawTopic string
	switch connectorID {
	case "saleor":
		signature = strings.TrimSpace(r.Header.Get("Saleor-Signature"))
		rawTopic = r.Header.Get("Saleor-Event")
	case "woocommerce":
		signature = strings.TrimSpace(r.Header.Get("X-WC-Webhook-Signature"))
		rawTopic = r.Header.Get("X-WC-Webhook-Topic")
	default:
		return "", "", false
	}
	topic = normalizeCommerceWebhookTopic(rawTopic)
	return signature, topic, signature != "" && topic != ""
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
	result, err := dedup.processor.ProcessWithSQLTransaction(ctx, dedup.scope, "commerce.webhook.v1", delivery, func(callCtx context.Context, tx *sql.Tx, _ eventbus.Delivery) error {
		return enqueueCommerceWebhook(callCtx, tx, event)
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
