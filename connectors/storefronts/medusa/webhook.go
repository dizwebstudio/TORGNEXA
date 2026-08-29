package medusa

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReceiveCommerceWebhook is deliberately unimplemented: Medusa has no
// standardized, discoverable outbound webhook delivery mechanism with a
// signature scheme the way WooCommerce (HMAC-SHA256 + a per-store webhook
// secret) or Shopify (HMAC-SHA256 + the OAuth app's client secret) do.
// Medusa's own docs describe reacting to events via in-process subscribers,
// not signed HTTP callbacks to a third party; a self-hosted merchant could
// wire up custom code to POST events out, but there is no standard contract
// this connector could verify against. Failing closed rather than accepting
// an unverifiable delivery.
func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	return sdk.CommerceWebhookResult{}, unsupportedOperation("webhook_verification_unavailable")
}
