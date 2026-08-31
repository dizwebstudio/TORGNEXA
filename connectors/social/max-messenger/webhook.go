package maxconnector

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrWebhookUnauthorized = errors.New("max: webhook unauthorized")
	ErrWebhookInvalid      = errors.New("max: invalid webhook")
)

var webhookUpdateTypes = []string{"message_created", "message_edited", "message_removed"}

type WebhookController interface {
	SubscribeSocialWebhook(context.Context, sdk.Account, sdk.Runtime, string) error
	UnsubscribeSocialWebhook(context.Context, sdk.Account, sdk.Runtime, string) error
}

func (connector *Connector) SubscribeSocialWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, endpoint string) error {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "social.webhooks") != nil || !validWebhookEndpoint(endpoint) {
		return sdk.ErrInvalidSocialWebhook
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil || configuration.validateWebhook() != nil {
		if err == nil {
			err = ErrInvalidConfiguration
		}
		return err
	}
	return connector.useSecret(ctx, runtime, account.SecretReference, validToken, func(token []byte) error {
		return connector.useSecret(ctx, runtime, configuration.WebhookSecretReference, validWebhookSecret, func(secret []byte) error {
			body, _ := json.Marshal(map[string]any{"url": endpoint, "update_types": webhookUpdateTypes, "secret": string(secret)})
			raw, e := connector.call(ctx, token, "POST", "/subscriptions", nil, body, true)
			if e != nil {
				return e
			}
			var response struct {
				Success bool `json:"success"`
			}
			if json.Unmarshal(raw, &response) != nil || !response.Success {
				return ErrInvalidResponse
			}
			return nil
		})
	})
}

func (connector *Connector) UnsubscribeSocialWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, endpoint string) error {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "social.webhooks") != nil || !validWebhookEndpoint(endpoint) {
		return sdk.ErrInvalidSocialWebhook
	}
	return connector.useSecret(ctx, runtime, account.SecretReference, validToken, func(token []byte) error {
		raw, e := connector.call(ctx, token, "DELETE", "/subscriptions", []Param{{Name: "url", Value: endpoint}}, nil, true)
		if e != nil {
			return e
		}
		var response struct {
			Success bool `json:"success"`
		}
		if json.Unmarshal(raw, &response) != nil || !response.Success {
			return ErrInvalidResponse
		}
		return nil
	})
}

func (connector *Connector) ReceiveSocialWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialWebhookRequest, dedup sdk.SocialWebhookDeduplicator) (sdk.SocialWebhookResult, error) {
	if connector == nil || runtime == nil || runtime.Secrets() == nil || dedup == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "social.webhooks") != nil || request.Validate() != nil {
		return sdk.SocialWebhookResult{}, sdk.ErrInvalidSocialWebhook
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil || configuration.validateWebhook() != nil {
		if err == nil {
			err = ErrInvalidConfiguration
		}
		return sdk.SocialWebhookResult{}, err
	}
	var result sdk.SocialWebhookResult
	err = connector.useSecret(ctx, runtime, configuration.WebhookSecretReference, validWebhookSecret, func(expected []byte) error {
		if len(expected) != len(request.VerificationToken) || subtle.ConstantTimeCompare(expected, request.VerificationToken) != 1 {
			return ErrWebhookUnauthorized
		}
		canonical, update, parseErr := canonicalUpdate(request.Body)
		if parseErr != nil {
			return parseErr
		}
		chatID := update.ChatID
		if chatID == 0 && update.Message != nil {
			chatID = update.Message.Recipient.ChatID
		}
		if chatID != configuration.ChatID {
			return sdk.ErrInvalidSocialWebhook
		}
		mid := update.MessageID
		if mid == "" && update.Message != nil {
			mid = update.Message.Body.MID
		}
		if !allowedWebhookType(update.UpdateType) || !validMID(mid) || update.Timestamp <= 0 {
			return ErrWebhookInvalid
		}
		occurred := time.UnixMilli(update.Timestamp).UTC()
		if occurred.Year() < 2020 || occurred.After(request.ReceivedAt.Add(10*time.Minute)) {
			return ErrWebhookInvalid
		}
		digest := sha256.Sum256(canonical)
		fingerprint := hex.EncodeToString(digest[:])
		deliveryID := "sha256:" + fingerprint
		claim := sdk.SocialWebhookClaim{
			DeliveryID:          deliveryID,
			EventType:           "max." + update.UpdateType,
			RemoteChannelID:     strconv.FormatInt(chatID, 10),
			RemoteObjectID:      mid,
			OccurredAt:          occurred,
			ProviderFingerprint: fingerprint,
			CanonicalPayload:    canonical,
		}
		if err := claim.Validate(); err != nil {
			return err
		}
		duplicate, claimErr := dedup.ClaimSocialWebhook(ctx, account, claim)
		if claimErr != nil {
			return claimErr
		}
		result = sdk.SocialWebhookResult{DeliveryID: deliveryID, EventType: "max." + update.UpdateType, RemoteChannelID: strconv.FormatInt(chatID, 10), RemoteObjectID: mid, OccurredAt: occurred, Duplicate: duplicate, CanonicalPayload: canonical}
		return result.Validate()
	})
	return result, err
}

type maxUpdate struct {
	UpdateType string `json:"update_type"`
	Timestamp  int64  `json:"timestamp"`
	ChatID     int64  `json:"chat_id"`
	MessageID  string `json:"message_id"`
	Message    *struct {
		Recipient struct {
			ChatID int64 `json:"chat_id"`
		} `json:"recipient"`
		Body struct {
			MID string `json:"mid"`
		} `json:"body"`
	} `json:"message"`
}

func canonicalUpdate(body []byte) (json.RawMessage, maxUpdate, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var generic any
	if dec.Decode(&generic) != nil {
		return nil, maxUpdate{}, ErrWebhookInvalid
	}
	canonical, e := json.Marshal(generic)
	if e != nil {
		return nil, maxUpdate{}, ErrWebhookInvalid
	}
	var update maxUpdate
	if json.Unmarshal(canonical, &update) != nil {
		return nil, maxUpdate{}, ErrWebhookInvalid
	}
	return canonical, update, nil
}
func allowedWebhookType(v string) bool {
	for _, allowed := range webhookUpdateTypes {
		if v == allowed {
			return true
		}
	}
	return false
}
func validWebhookEndpoint(value string) bool {
	const prefix = "https://"
	if !strings.HasPrefix(value, prefix) || len(value) > 2048 || strings.ContainsAny(value, "\\#") {
		return false
	}
	rest := value[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return false
	}
	authority := rest[:slash]
	if strings.ContainsAny(authority, "@%[]:") || strings.TrimSpace(authority) != authority || strings.Contains(authority, "..") {
		return false
	}
	return rest[slash:] != ""
}

var _ sdk.SocialWebhookReceiver = (*Connector)(nil)
var _ WebhookController = (*Connector)(nil)
