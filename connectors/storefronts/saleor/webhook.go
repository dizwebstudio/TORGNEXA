package saleor

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	saleorJWKSPath    = "/.well-known/jwks.json"
	maxSaleorJWKSKeys = 32
	maxSaleorKeyBytes = 1024
)

type saleorJWSHeader struct {
	Alg  string   `json:"alg"`
	Kid  string   `json:"kid"`
	B64  *bool    `json:"b64"`
	Crit []string `json:"crit"`
}

type saleorJWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type saleorJWKS struct {
	Keys []saleorJWK `json:"keys"`
}

type saleorWebhookEnvelope struct {
	Event string `json:"event"`
	Data  struct {
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	} `json:"data"`
}

// parseSaleorJWS parses the compact detached JWS emitted when a Saleor
// webhook has no legacy secretKey. The payload segment must be empty: the
// raw HTTP body is the detached payload and is included verbatim in the
// signature input.
func parseSaleorJWS(value string) (saleorJWSHeader, []byte, string, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] != "" || parts[2] == "" || len(parts[0]) > 4096 || len(parts[2]) > 2048 {
		return saleorJWSHeader{}, nil, "", sdk.ErrInvalidCommerceWebhook
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || len(headerBytes) == 0 || len(headerBytes) > 3072 {
		return saleorJWSHeader{}, nil, "", sdk.ErrInvalidCommerceWebhook
	}
	var header saleorJWSHeader
	if json.Unmarshal(headerBytes, &header) != nil || header.Alg != "RS256" || !validRemoteText(header.Kid, 256) || header.B64 == nil || *header.B64 || len(header.Crit) != 1 || header.Crit[0] != "b64" {
		return saleorJWSHeader{}, nil, "", sdk.ErrInvalidCommerceWebhook
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) < 256 || len(signature) > maxSaleorKeyBytes {
		return saleorJWSHeader{}, nil, "", sdk.ErrInvalidCommerceWebhook
	}
	return header, signature, parts[0], nil
}

func saleorJWKPublicKey(value saleorJWK) (*rsa.PublicKey, error) {
	if value.Kty != "RSA" || value.Use != "sig" || value.Alg != "RS256" || value.N == "" || value.E == "" {
		return nil, errors.New("saleor: unsupported jwk")
	}
	modulusBytes, err := base64.RawURLEncoding.DecodeString(value.N)
	if err != nil || len(modulusBytes) < 256 || len(modulusBytes) > maxSaleorKeyBytes {
		return nil, errors.New("saleor: invalid jwk modulus")
	}
	exponentBytes, err := base64.RawURLEncoding.DecodeString(value.E)
	if err != nil || len(exponentBytes) == 0 || len(exponentBytes) > 4 {
		return nil, errors.New("saleor: invalid jwk exponent")
	}
	exponent := new(big.Int).SetBytes(exponentBytes)
	if !exponent.IsInt64() || exponent.Int64() != 65537 {
		return nil, errors.New("saleor: unsupported jwk exponent")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(modulusBytes), E: 65537}, nil
}

func (connector *Connector) saleorJWKS(ctx context.Context, configuration Configuration, credential credentials) (saleorJWKS, error) {
	if connector == nil || connector.transport == nil {
		return saleorJWKS{}, ErrTransportMissing
	}
	token := []byte(credential.Token)
	defer clear(token)
	response, err := connector.transport.Do(ctx, Request{Method: "GET", Host: configuration.StoreHost, Path: configuration.BasePath + saleorJWKSPath, Bearer: token})
	if err != nil {
		return saleorJWKS{}, normalizedTransportError()
	}
	if len(response.Body) > maxBodyBytes || response.RetryAfterMS < 0 {
		remote, _ := sdk.NewRemoteError(sdk.ErrorInternal, "response_invalid", "", 0)
		return saleorJWKS{}, remote
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return saleorJWKS{}, normalizeTransportStatus(response)
	}
	var keys saleorJWKS
	if json.Unmarshal(response.Body, &keys) != nil || len(keys.Keys) == 0 || len(keys.Keys) > maxSaleorJWKSKeys {
		return saleorJWKS{}, ErrInvalidResponse
	}
	return keys, nil
}

func verifySaleorJWS(ctx context.Context, connector *Connector, configuration Configuration, credential credentials, body []byte, signature string) error {
	header, encodedSignature, protected, err := parseSaleorJWS(signature)
	if err != nil {
		return err
	}
	keys, err := connector.saleorJWKS(ctx, configuration, credential)
	if err != nil {
		return err
	}
	var publicKey *rsa.PublicKey
	for _, candidate := range keys.Keys {
		if candidate.Kid != header.Kid {
			continue
		}
		if publicKey != nil {
			return ErrInvalidResponse
		}
		publicKey, err = saleorJWKPublicKey(candidate)
		if err != nil {
			return ErrInvalidResponse
		}
	}
	if publicKey == nil || len(encodedSignature) != publicKey.Size() {
		return sdk.ErrInvalidCommerceWebhook
	}
	// Saleor's detached JWS uses RFC 7797 b64=false: the signed bytes are
	// protected-header + '.' + the exact UTF-8 body, not a base64 body.
	input := make([]byte, 0, len(protected)+1+len(body))
	input = append(input, protected...)
	input = append(input, '.')
	input = append(input, body...)
	digest := sha256.Sum256(input)
	if rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], encodedSignature) != nil {
		return sdk.ErrInvalidCommerceWebhook
	}
	return nil
}

func saleorWebhookEvent(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "_", "."))
}

func saleorWebhookDeliveryID(signature string, body []byte) string {
	digest := sha256.Sum256(append(append([]byte(signature), 0), body...))
	return "sha256:" + hex.EncodeToString(digest[:])
}

// ReceiveCommerceWebhook verifies Saleor's detached RS256 webhook signature
// against the store-published JWKS and returns a bounded canonical event.
// Saleor webhooks must be configured without the deprecated secretKey; the
// legacy HMAC variant is intentionally not inferred from the app bearer token.
func (connector *Connector) ReceiveCommerceWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.CommerceWebhookRequest, dedup sdk.CommerceWebhookDeduplicator) (sdk.CommerceWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || request.Validate() != nil {
		return sdk.CommerceWebhookResult{}, sdk.ErrInvalidCommerceWebhook
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.CommerceWebhookResult{}, err
	}
	var output sdk.CommerceWebhookResult
	err = connector.withCredentials(ctx, runtime, account.SecretReference, func(credential credentials) error {
		if err := verifySaleorJWS(ctx, connector, configuration, credential, request.Body, request.Signature); err != nil {
			var remote *sdk.RemoteError
			if errors.As(err, &remote) {
				return err
			}
			if !errors.Is(err, sdk.ErrInvalidCommerceWebhook) {
				return err
			}
			remote, _ = sdk.NewRemoteError(sdk.ErrorUnauthorized, "webhook_signature_invalid", "", 0)
			return remote
		}
		var envelope saleorWebhookEnvelope
		if json.Unmarshal(request.Body, &envelope) != nil || envelope.Data.Object.ID == "" || !validRemoteText(envelope.Data.Object.ID, 512) {
			return ErrInvalidResponse
		}
		if envelope.Event != "" && saleorWebhookEvent(envelope.Event) != request.ExpectedTopic {
			return sdk.ErrInvalidCommerceWebhook
		}
		parts := strings.Split(request.ExpectedTopic, ".")
		if len(parts) != 2 {
			return sdk.ErrInvalidCommerceWebhook
		}
		deliveryID := saleorWebhookDeliveryID(request.Signature, request.Body)
		canonical, marshalErr := json.Marshal(struct {
			Event      string `json:"event"`
			ResourceID string `json:"resource_id"`
		}{Event: request.ExpectedTopic, ResourceID: envelope.Data.Object.ID})
		if marshalErr != nil {
			return ErrInvalidResponse
		}
		claim := sdk.CommerceWebhookClaim{DeliveryID: deliveryID, EventType: request.ExpectedTopic, ResourceKind: parts[0], ResourceRemoteID: envelope.Data.Object.ID, OccurredAt: request.ReceivedAt.UTC(), CanonicalPayload: canonical}
		duplicate, claimErr := dedup.ClaimCommerceWebhook(ctx, account, claim)
		if claimErr != nil {
			return claimErr
		}
		output = sdk.CommerceWebhookResult{DeliveryID: claim.DeliveryID, EventType: claim.EventType, ResourceKind: claim.ResourceKind, ResourceRemoteID: claim.ResourceRemoteID, OccurredAt: claim.OccurredAt, Duplicate: duplicate, CanonicalPayload: claim.CanonicalPayload}
		return output.Validate()
	})
	return output, err
}

var _ sdk.CommerceWebhookReceiver = (*Connector)(nil)
