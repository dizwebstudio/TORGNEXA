package shopware

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReceiveCommerceWebhook is deliberately unimplemented: Shopware's core
// "Webhook" entity (Settings > System > Webhooks) signs deliveries with its
// own dedicated secret (the shopware-shop-signature header, HMAC-SHA256),
// which is a distinct credential from the "Integration" client id/secret
// this connector holds for Admin API access. This connector has no scoped
// way to read that separate webhook secret today, so it fails closed rather
// than accepting an unverifiable delivery or guessing that the Integration
// secret doubles as the webhook one.
func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	return sdk.CommerceWebhookResult{}, unsupportedOperation("webhook_verification_unavailable")
}
