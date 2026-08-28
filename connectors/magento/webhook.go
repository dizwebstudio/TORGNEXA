package magento

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReceiveCommerceWebhook is deliberately unimplemented: Magento's open-source
// core has no built-in outbound webhook delivery mechanism or signature
// scheme (Adobe Commerce Cloud's separate Adobe I/O Events product is not
// part of the Admin REST API this connector otherwise targets, and is not
// available to a self-hosted open-source install at all). There is no
// standard contract to verify a delivery against, so this fails closed
// rather than accepting an unverifiable one.
func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	return sdk.CommerceWebhookResult{}, unsupportedOperation("webhook_verification_unavailable")
}
