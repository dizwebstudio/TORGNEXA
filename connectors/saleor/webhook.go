package saleor

import (
	"context"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

// ReceiveCommerceWebhook is deliberately unimplemented. This was
// investigated, not assumed: as of Saleor 3.5 a webhook created without a
// (deprecated) secretKey is signed with a real, verifiable mechanism -- a
// detached JWS using RS256, verifiable against the public key Saleor itself
// publishes at https://<store-host>/.well-known/jwks.json, requiring no
// separate shared secret at all (verified against Saleor's own core source:
// saleor/webhook/transport/__init__.py's signature_for_payload and
// saleor/core/jwt_manager.py's JWTManager.jws_encode). That mechanism does
// not fit through this Connector SDK's generic webhook envelope, though:
// sdk.CommerceWebhookRequest.Validate caps Signature at 256 bytes, and an
// RS256 detached JWS's base64url-encoded signature segment alone already
// exceeds that (a 2048-bit RSA-PKCS1v15 signature is 256 raw bytes, ~342
// base64url characters, before even accounting for the JWS header
// segment). There is consequently no way to carry Saleor's real signature
// through this envelope without silently truncating it, so this fails
// closed rather than accepting an unverifiable delivery.
func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	return sdk.CommerceWebhookResult{}, unsupportedOperation("webhook_signature_envelope_too_small")
}
