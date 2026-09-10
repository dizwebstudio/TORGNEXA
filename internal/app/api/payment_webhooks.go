package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/payments"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	"github.com/torgnexa/torgnexa/internal/platform/connectorruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

// paymentWebhooksPathPrefix is registered as a PublicWebhookRoute (ADR-0105 /
// Task 136), so it is unauthenticated by construction: this handler alone is
// responsible for proving the caller is who it claims to be, by re-verifying
// the delivery against the provider's own API (never by trusting anything in
// the request itself).
const paymentWebhooksPathPrefix = webhookPathPrefix + "payments/"

// paymentGatewayResolver is the narrow slice of *builtinruntime.Registry this
// handler needs, so tests can substitute a fake gateway without constructing
// a real registry/transport stack.
type paymentGatewayResolver interface {
	PaymentGateway(sdk.Account, builtinruntime.ConfigLoader) (builtinruntime.PaymentGateway, error)
}

type paymentWebhookAPI struct {
	repository paymentsAPIRepository
	accounts   paymentsConnectorAccounts
	configs    paymentsRuntimeConfig
	secrets    secrets.SecretProvider
	registry   paymentGatewayResolver
}

func newPaymentWebhookRoutes(repository paymentsAPIRepository, accounts paymentsConnectorAccounts, configs paymentsRuntimeConfig, secretSource secrets.SecretProvider, registry *builtinruntime.Registry) []PublicWebhookRoute {
	if repository == nil || accounts == nil || registry == nil {
		return nil
	}
	api := paymentWebhookAPI{repository: repository, accounts: accounts, configs: configs, secrets: secretSource, registry: registry}
	return []PublicWebhookRoute{
		{Method: http.MethodPost, Path: paymentWebhooksPathPrefix, PathPrefix: true, Handler: http.HandlerFunc(api.receive)},
	}
}

// receive keeps pre-verification rejections uniform. A verified delivery is
// acknowledged only after its receipt and business effects durably commit.
func (api paymentWebhookAPI) receive(w http.ResponseWriter, r *http.Request) {
	logger := slog.Default().With("event", "payments.webhook_received", "path", r.URL.Path)
	connectorID, organizationID, workspaceID, accountID, ok := parsePaymentWebhookPath(r.URL.Path)
	if !ok {
		logger.Warn("payment webhook path malformed")
		acknowledgeWebhook(w)
		return
	}
	logger = logger.With("connector_id", connectorID, "account_id", accountID)

	// The body is already capped by http.MaxBytesReader inside
	// serveWebhookRoute (Task 136); this is just the read.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Warn("payment webhook body unreadable", "error", err)
		acknowledgeWebhook(w)
		return
	}

	tenantScope, err := tenancy.ParseScope(organizationID, workspaceID)
	if err != nil {
		logger.Warn("payment webhook scope invalid")
		acknowledgeWebhook(w)
		return
	}
	scope, err := payments.ParseScope(organizationID, workspaceID)
	if err != nil {
		logger.Warn("payment webhook scope invalid")
		acknowledgeWebhook(w)
		return
	}
	account, err := api.accounts.AccountByID(r.Context(), organizationID, workspaceID, accountID)
	accountKey := account.ConnectorID
	pathKey := connectorID
	accountIdentityMatches := err == nil && accountKey == pathKey
	if !accountIdentityMatches || account.Family != sdk.FamilyPayment || account.Status != sdk.AccountActive {
		logger.Warn("payment webhook account unresolved or inactive")
		acknowledgeWebhook(w)
		return
	}

	gateway, err := api.registry.PaymentGateway(account, api.configLoader(tenantScope))
	if err != nil {
		logger.Warn("payment webhook rail unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	runtime, err := connectorruntime.New(api.secrets, tenantScope)
	if err != nil {
		logger.Warn("payment webhook runtime unavailable", "error", err)
		acknowledgeWebhook(w)
		return
	}
	// proof carries whatever the transport for this connector expects as a
	// secondary signal (a signature header, when the provider sends one);
	// today's transports (yookassa/sbp) do not require it to be meaningful,
	// since ADR-0105 makes the callback re-fetch the load-bearing check.
	proof := []byte(r.Header.Get("X-Webhook-Signature"))
	if len(proof) == 0 {
		proof = []byte("no-signature")
	}
	verified, err := gateway.VerifyPaymentWebhook(r.Context(), account, runtime, body, proof)
	if err != nil {
		logger.Warn("payment webhook verification failed", "error", err)
		acknowledgeWebhook(w)
		return
	}

	evidence := payments.WebhookEvidence{DeliveryID: verified.DeliveryID, ConnectorAccountID: account.ID, RemotePaymentID: verified.RemotePaymentID, EventType: verified.EventType, BodyDigest: verified.BodyDigest, VerifiedAt: verified.OccurredAt}
	remoteStatus := strings.TrimPrefix(verified.EventType, "payment_")
	observation := payments.VerifiedWebhook{Evidence: evidence, Status: paymentsCanonicalStatus(remoteStatus), RemoteStatus: remoteStatus}
	if _, err := api.repository.ApplyVerifiedWebhook(r.Context(), scope, observation, paymentsMutation("system:webhook", verified.DeliveryID)); err != nil {
		logger.Error("verified payment webhook not committed")
		retryVerifiedWebhook(w)
		return
	}
	acknowledgeVerifiedWebhook(w, verified)
}

// retryVerifiedWebhook is used only after independent provider verification.
// No internal error or provider acknowledgement token is exposed on failure.
func retryVerifiedWebhook(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "5")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("{}"))
}

func (api paymentWebhookAPI) configLoader(tenantScope tenancy.Scope) builtinruntime.ConfigLoader {
	if api.configs == nil {
		return nil
	}
	return func(ctx context.Context, accountID string) (json.RawMessage, error) {
		raw, _, err := api.configs.Config(ctx, tenantScope, accountID)
		return raw, err
	}
}

// parsePaymentWebhookPath extracts {connector_id}/{organization_id}/{workspace_id}/{account_id}
// from the trailing path segments after paymentWebhooksPathPrefix.
func parsePaymentWebhookPath(path string) (connectorID, organizationID, workspaceID, accountID string, ok bool) {
	rest := strings.TrimPrefix(path, paymentWebhooksPathPrefix)
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

func acknowledgeWebhook(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

// acknowledgeVerifiedWebhook is only ever reached after
// gateway.VerifyPaymentWebhook has already succeeded — i.e. the delivery's
// own cryptographic signature or re-fetch already proved it is genuine.
// That gate is what keeps a provider-specific ack format safe under
// ADR-0105: an attacker without the provider's signing secret can never
// reach this function, so a differentiated response here leaks nothing to
// enumeration attempts, unlike the uniform acknowledgeWebhook used for every
// pre-verification rejection. verified.Ack carries whatever exact body the
// provider's own transport (which alone knows its identity) decided the
// callback contract requires — this stays provider-agnostic on purpose, so
// admitting another connector with its own ack quirk needs no change here.
func acknowledgeVerifiedWebhook(w http.ResponseWriter, verified sdk.PaymentWebhook) {
	if verified.Ack != "" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(verified.Ack))
		return
	}
	acknowledgeWebhook(w)
}
