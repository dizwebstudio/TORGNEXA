package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/logistics"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

// logisticsWebhooksPathPrefix is public by design. The handler below owns
// tenant/account resolution and the connector performs provider-side
// re-verification before any evidence is persisted.
const logisticsWebhooksPathPrefix = webhookPathPrefix + "logistics/"

type logisticsWebhookRepository interface {
	ShipmentByRemoteID(context.Context, tenancy.Scope, string, string) (logistics.Shipment, error)
	RecordWebhookEvidence(context.Context, tenancy.Scope, logistics.WebhookEvidence) (bool, error)
}

type logisticsWebhookAccounts interface {
	AccountByID(context.Context, string, string, string) (sdk.Account, error)
}

type logisticsWebhookResolver interface {
	LogisticsWebhook(context.Context, sdk.Account, sdk.Runtime, []byte, []byte) (sdk.LogisticsWebhook, error)
}

type logisticsWebhookAPI struct {
	repository logisticsWebhookRepository
	accounts   logisticsWebhookAccounts
	secrets    secrets.SecretProvider
	registry   logisticsWebhookResolver
}

func newLogisticsWebhookRoutes(repository logisticsWebhookRepository, accounts logisticsWebhookAccounts, secretSource secrets.SecretProvider, registry logisticsWebhookResolver) []PublicWebhookRoute {
	if repository == nil || accounts == nil || secretSource == nil || registry == nil {
		return nil
	}
	api := logisticsWebhookAPI{repository: repository, accounts: accounts, secrets: secretSource, registry: registry}
	return []PublicWebhookRoute{{Method: http.MethodPost, Path: logisticsWebhooksPathPrefix, PathPrefix: true, Handler: http.HandlerFunc(api.receive)}}
}

// receive always acknowledges with 200. This keeps provider retry behavior
// independent from account enumeration and verification outcomes, matching
// the public webhook boundary contract.
func (api logisticsWebhookAPI) receive(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default().With("event", "logistics.webhook_received", "path", r.URL.Path)
	connectorID, organizationID, workspaceID, accountID, ok := parseLogisticsWebhookPath(r.URL.Path)
	if !ok {
		logger.Warn("logistics webhook path malformed")
		acknowledgeWebhook(w)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Warn("logistics webhook body unreadable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	tenantScope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		logger.Warn("logistics webhook scope invalid")
		acknowledgeWebhook(w)
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), organizationID, workspaceID, accountID)
	if err != nil || account.ConnectorID != connectorID || account.Family != sdk.FamilyLogistics || account.Status != sdk.AccountActive {
		logger.Warn("logistics webhook account unresolved or inactive")
		acknowledgeWebhook(w)
		return
	}
	runtime, err := connectorruntime.New(api.secrets, tenantScope)
	if err != nil {
		logger.Warn("logistics webhook runtime unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	proof := []byte(strings.TrimSpace(r.Header.Get("X-Webhook-Signature")))
	verified, err := api.registry.LogisticsWebhook(r.Context(), account, runtime, body, proof)
	if err != nil {
		logger.Warn("logistics webhook provider verification failed", "error", err)
		acknowledgeWebhook(w)
		return
	}
	shipment, err := api.repository.ShipmentByRemoteID(r.Context(), tenantScope, account.ID, verified.RemoteID)
	if err != nil {
		logger.Warn("logistics webhook shipment unresolved", "error", err)
		acknowledgeWebhook(w)
		return
	}
	digest := sha256.Sum256(body)
	_, err = api.repository.RecordWebhookEvidence(r.Context(), tenantScope, logistics.WebhookEvidence{
		DeliveryID:         verified.DeliveryID,
		ConnectorAccountID: account.ID,
		ShipmentID:         shipment.ID,
		RemoteID:           verified.RemoteID,
		EventType:          "ORDER_STATUS",
		RemoteStatus:       verified.Status,
		BodyDigest:         hex.EncodeToString(digest[:]),
		VerifiedAt:         verified.OccurredAt.UTC(),
	})
	if err != nil {
		logger.Warn("logistics webhook evidence not recorded", "error", err)
	}
	acknowledgeWebhook(w)
}

// parseLogisticsWebhookPath extracts connector/org/workspace/account IDs from
// /api/v1/webhooks/logistics/{connector_id}/{organization_id}/{workspace_id}/{account_id}.
func parseLogisticsWebhookPath(path string) (connectorID, organizationID, workspaceID, accountID string, ok bool) {
	rest := strings.TrimPrefix(path, logisticsWebhooksPathPrefix)
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

var _ logisticsWebhookResolver = (*builtinruntime.Registry)(nil)
