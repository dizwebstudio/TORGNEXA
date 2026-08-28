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

// receive always acknowledges with 200, regardless of outcome — a provider
// enumerating account/connector IDs must see the same response whether the
// account exists, the delivery replayed, or verification failed (ADR-0105).
// Every rejection reason is logged internally only.
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
	if err != nil || account.ConnectorID != connectorID || account.Family != sdk.FamilyPayment || account.Status != sdk.AccountActive {
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
	fresh, err := api.repository.RecordWebhookEvidence(r.Context(), scope, evidence)
	if err != nil {
		logger.Error("payment webhook evidence not recorded", "error", err)
		acknowledgeWebhook(w)
		return
	}
	if !fresh {
		// A true provider retry of an already-processed delivery: nothing
		// left to do, and applying it again must not double-count. Still
		// worth a provider-shaped ack (below) so the provider stops retrying.
		acknowledgeVerifiedWebhook(w, verified)
		return
	}

	if err := api.applyVerifiedStatus(r.Context(), scope, account.ID, verified); err != nil {
		logger.Warn("payment webhook status not applied", "error", err)
	}
	acknowledgeVerifiedWebhook(w, verified)
}

// applyVerifiedStatus moves the local payment through the same
// ValidatePaymentTransition/ChangePaymentStatus path every other payment
// mutation uses. The target status comes only from the verified
// EventType ("payment_"+remote status, per paymentstransport.go's
// VerifyWebhook contract) — never from the unverified request body.
func (api paymentWebhookAPI) applyVerifiedStatus(ctx context.Context, scope payments.Scope, connectorAccountID string, verified sdk.PaymentWebhook) error {
	payment, err := api.repository.PaymentByRemoteID(ctx, scope, connectorAccountID, verified.RemotePaymentID)
	if err != nil {
		return err
	}
	remoteStatus := strings.TrimPrefix(verified.EventType, "payment_")
	target := paymentsCanonicalStatus(remoteStatus)
	if target == payment.Status {
		return nil
	}
	if payments.ValidatePaymentTransition(payment.Status, target) != nil {
		return payments.ErrInvalidState
	}
	change := payments.ChangePaymentStatus{ID: payment.ID, ExpectedVersion: payment.Version, Status: target, RemoteStatus: remoteStatus}
	if target == payments.StatusSucceeded {
		at := verified.OccurredAt
		change.SucceededAt = &at
	}
	if target == payments.StatusFailed {
		change.ReasonCode = "provider_declined"
	}
	_, err = api.repository.ChangePaymentStatus(ctx, scope, change, paymentsMutation("system:webhook", verified.DeliveryID))
	return err
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
