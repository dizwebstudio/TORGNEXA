package woocommerce

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func verifyWebhook(body []byte, signature, secret string) bool {
	decoded, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || len(decoded) != sha256.Size {
		return false
	}
	key := []byte(secret)
	defer clear(key)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	return subtle.ConstantTimeCompare(decoded, expected) == 1
}

func canonicalWebhookDelivery(signature string, body []byte) string {
	digest := sha256.Sum256(append(append([]byte(signature), 0), body...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	if _, err := connector.configuration(ctx, account); err != nil {
		return sdk.CommerceWebhookResult{}, err
	}
	var output sdk.CommerceWebhookResult
	err := connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		if !verifyWebhook(request.Body, request.Signature, credential.WebhookSecret) {
			remote, _ := sdk.NewRemoteError(sdk.ErrorUnauthorized, "webhook_signature_invalid", "", 0)
			return remote
		}
		parts := strings.Split(request.ExpectedTopic, ".")
		if len(parts) != 2 {
			return sdk.ErrInvalidCommerceWebhook
		}
		var envelope struct {
			ID              int64  `json:"id"`
			DateModifiedGMT string `json:"date_modified_gmt"`
			DateCreatedGMT  string `json:"date_created_gmt"`
		}
		if json.Unmarshal(request.Body, &envelope) != nil || envelope.ID < 1 {
			return ErrInvalidResponse
		}
		occurred := request.ReceivedAt
		if envelope.DateModifiedGMT != "" {
			if parsed, e := parseWooTime(envelope.DateModifiedGMT); e == nil {
				occurred = parsed
			}
		} else if envelope.DateCreatedGMT != "" {
			if parsed, e := parseWooTime(envelope.DateCreatedGMT); e == nil {
				occurred = parsed
			}
		}
		deliveryID := canonicalWebhookDelivery(request.Signature, request.Body)
		canonical, marshalErr := json.Marshal(struct {
			ID              int64  `json:"id"`
			DateModifiedGMT string `json:"date_modified_gmt,omitempty"`
			DateCreatedGMT  string `json:"date_created_gmt,omitempty"`
		}{ID: envelope.ID, DateModifiedGMT: envelope.DateModifiedGMT, DateCreatedGMT: envelope.DateCreatedGMT})
		if marshalErr != nil {
			return ErrInvalidResponse
		}
		claim := sdk.CommerceWebhookClaim{DeliveryID: deliveryID, EventType: request.ExpectedTopic, ResourceKind: parts[0], ResourceRemoteID: intString(envelope.ID), OccurredAt: occurred.UTC(), CanonicalPayload: canonical}
		duplicate, e := dedup.ClaimCommerceWebhook(ctx, account, claim)
		if e != nil {
			return e
		}
		output = sdk.CommerceWebhookResult{DeliveryID: claim.DeliveryID, EventType: claim.EventType, ResourceKind: claim.ResourceKind, ResourceRemoteID: claim.ResourceRemoteID, OccurredAt: claim.OccurredAt, Duplicate: duplicate, CanonicalPayload: claim.CanonicalPayload}
		return output.Validate()
	})
	return output, err
}
