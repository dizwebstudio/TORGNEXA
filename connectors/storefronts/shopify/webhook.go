package shopify

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReceiveCommerceWebhook is deliberately unimplemented in v1: Shopify signs
// every webhook delivery with the OAuth app's client secret
// (X-Shopify-Hmac-Sha256), a single secret shared by every merchant
// installation of the app, not a per-account credential. The host-owned
// OAuth runtime (Task 134) projects UseSecret down to only the current
// per-account access token, so this connector has no scoped way to reach
// that shared app secret today. Rather than skip verification (which would
// let anyone post fake order/product events) or read some other secret
// speculatively, this fails closed until app-level secret scoping exists.
func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	return sdk.CommerceWebhookResult{}, unsupportedOperation("webhook_verification_unavailable")
}
