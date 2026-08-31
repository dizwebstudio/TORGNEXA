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

// socialWebhooksPathPrefix is public by design. The URL carries the tenant
// binding because provider callbacks do not have an authenticated principal;
// the connector still verifies the provider secret before this route commits
// anything to the host-owned Inbox/outbox boundary.
const socialWebhooksPathPrefix = webhookPathPrefix + "social/"

const socialWebhookEventType = "commerce.social.webhook_received.v1"

type socialWebhookAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
	AccountCapabilities(context.Context, tenancy.Scope, string) ([]sdk.AccountCapabilitySetting, error)
}

type socialWebhookConfigs interface {
	Config(context.Context, tenancy.Scope, string) (json.RawMessage, int64, error)
}

type socialWebhookReceiverResolver interface {
	SocialWebhookReceiver(sdk.Account, builtinruntime.ConfigLoader) (sdk.SocialWebhookReceiver, error)
}

type socialWebhookAPI struct {
	accounts  socialWebhookAccounts
	configs   socialWebhookConfigs
	secrets   secrets.SecretProvider
	registry  socialWebhookReceiverResolver
	processor *inboxrepo.Processor
}

func newSocialWebhookRoutes(accounts socialWebhookAccounts, configs socialWebhookConfigs, secretSource secrets.SecretProvider, registry socialWebhookReceiverResolver, processor *inboxrepo.Processor) []PublicWebhookRoute {
	if accounts == nil || configs == nil || secretSource == nil || registry == nil || processor == nil {
		return nil
	}
	api := socialWebhookAPI{accounts: accounts, configs: configs, secrets: secretSource, registry: registry, processor: processor}
	return []PublicWebhookRoute{{Method: http.MethodPost, Path: socialWebhooksPathPrefix, PathPrefix: true, Handler: http.HandlerFunc(api.receive)}}
}

// receive always acknowledges with 200. MAX retries are therefore independent
// of account enumeration and verification outcomes; only a verified event can
// reach the durable Inbox/outbox transaction.
func (api socialWebhookAPI) receive(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default().With("event", "social.webhook_received", "path", r.URL.Path)
	connectorID, organizationID, workspaceID, accountID, ok := parseSocialWebhookPath(r.URL.Path)
	if !ok {
		logger.Warn("social webhook path malformed")
		acknowledgeWebhook(w)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Warn("social webhook body unreadable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	scope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		logger.Warn("social webhook scope invalid")
		acknowledgeWebhook(w)
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), organizationID, workspaceID, accountID)
	if err != nil || account.ConnectorID != connectorID || account.Family != sdk.FamilySocial || account.Status != sdk.AccountActive {
		logger.Warn("social webhook account unresolved or inactive")
		acknowledgeWebhook(w)
		return
	}
	capability := sdk.Capability("social.webhooks")
	settings, err := api.accounts.AccountCapabilities(r.Context(), scope, account.ID)
	if err != nil || !sdk.CapabilityEnabled(settings, capability) {
		logger.Warn("social webhook capability unavailable")
		acknowledgeWebhook(w)
		return
	}
	verificationToken, ok := socialWebhookVerificationToken(r, connectorID)
	if !ok {
		logger.Warn("social webhook verification header missing or unsupported")
		acknowledgeWebhook(w)
		return
	}
	runtime, err := connectorruntime.New(api.secrets, scope)
	if err != nil {
		logger.Warn("social webhook runtime unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	receiver, err := api.registry.SocialWebhookReceiver(account, api.configLoader(scope))
	if err != nil {
		logger.Warn("social webhook receiver unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	request := sdk.SocialWebhookRequest{
		VerificationToken: verificationToken,
		Body:              body,
		ReceivedAt:        time.Now().UTC(),
	}
	if api.processor == nil {
		logger.Warn("social webhook deduplicator unavailable")
		acknowledgeWebhook(w)
		return
	}
	dedup := socialWebhookDeduplicator{processor: api.processor, scope: scope}
	if _, err := receiver.ReceiveSocialWebhook(r.Context(), account, runtime, request, dedup); err != nil {
		logger.Warn("social webhook verification or publication failed", "error", err)
	}
	acknowledgeWebhook(w)
}

func (api socialWebhookAPI) configLoader(scope tenancy.Scope) builtinruntime.ConfigLoader {
	return func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, err := api.configs.Config(ctx, scope, accountID)
		return raw, err
	}
}

func socialWebhookVerificationToken(r *http.Request, connectorID string) ([]byte, bool) {
	if connectorID != "max-messenger" {
		return nil, false
	}
	value := strings.TrimSpace(r.Header.Get("X-Max-Bot-Api-Secret"))
	return []byte(value), value != ""
}

func parseSocialWebhookPath(path string) (connectorID, organizationID, workspaceID, accountID string, ok bool) {
	rest := strings.TrimPrefix(path, socialWebhooksPathPrefix)
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

type socialWebhookDeduplicator struct {
	processor *inboxrepo.Processor
	scope     tenancy.Scope
}

func (dedup socialWebhookDeduplicator) ClaimSocialWebhook(ctx context.Context, account sdk.Account, claim sdk.SocialWebhookClaim) (bool, error) {
	if dedup.processor == nil || !dedup.scope.Valid() || account.Validate() != nil || account.OrganizationID != dedup.scope.OrganizationID().String() || account.WorkspaceID != dedup.scope.WorkspaceID().String() || claim.Validate() != nil {
		return false, sdk.ErrInvalidSocialWebhook
	}
	instant, err := domain.NewUTCInstant(claim.OccurredAt)
	if err != nil {
		return false, sdk.ErrInvalidSocialWebhook
	}
	eventType, err := eventbus.ParseEventType(socialWebhookEventType)
	if err != nil {
		return false, sdk.ErrInvalidSocialWebhook
	}
	data, err := json.Marshal(struct {
		ConnectorAccountID  string `json:"connector_account_id"`
		DeliveryID          string `json:"delivery_id"`
		EventType           string `json:"event_type"`
		RemoteChannelID     string `json:"remote_channel_id"`
		RemoteObjectID      string `json:"remote_object_id,omitempty"`
		ProviderFingerprint string `json:"provider_fingerprint"`
	}{
		ConnectorAccountID:  account.ID,
		DeliveryID:          claim.DeliveryID,
		EventType:           claim.EventType,
		RemoteChannelID:     claim.RemoteChannelID,
		RemoteObjectID:      claim.RemoteObjectID,
		ProviderFingerprint: claim.ProviderFingerprint,
	})
	if err != nil {
		return false, err
	}
	eventID := socialWebhookEventID(account.ID, claim.DeliveryID)
	event := eventbus.Event{
		ID:             eventID,
		Type:           eventType,
		OccurredAt:     instant,
		OrganizationID: account.OrganizationID,
		WorkspaceID:    account.WorkspaceID,
		EntityType:     "social_webhook",
		EntityID:       eventID,
		Source:         "social-webhook",
		CorrelationID:  claim.DeliveryID,
		Data:           data,
	}
	if err := event.Validate(); err != nil {
		return false, err
	}
	delivery := eventbus.Delivery{Event: event, Attempt: 1, FirstObservedAt: instant}
	result, err := dedup.processor.ProcessWithSQLTransaction(ctx, dedup.scope, "social.webhook.v1", delivery, enqueueSocialWebhook)
	if err != nil {
		return false, err
	}
	return result == inbox.ResultDuplicate, nil
}

func enqueueSocialWebhook(ctx context.Context, tx *sql.Tx, delivery eventbus.Delivery) error {
	enqueuer, err := outboxrepo.NewTransactionEnqueuer(tx)
	if err != nil {
		return err
	}
	return enqueuer.Enqueue(ctx, delivery.Event)
}

func socialWebhookEventID(accountID, deliveryID string) string {
	digest := sha256.Sum256(append(append([]byte(accountID), 0), deliveryID...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

var _ socialWebhookReceiverResolver = (*builtinruntime.Registry)(nil)
var _ sdk.SocialWebhookDeduplicator = socialWebhookDeduplicator{}
