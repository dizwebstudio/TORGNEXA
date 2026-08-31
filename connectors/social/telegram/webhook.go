package telegram

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
	ErrWebhookUnauthorized = errors.New("telegram: webhook unauthorized")
	ErrWebhookInvalid      = errors.New("telegram: invalid webhook")
)

// VerificationHeader identifies the callback-scoped header consumed by the
// host before the provider verifies the payload.
func (connector *Connector) VerificationHeader() string { return "X-Telegram-Bot-Api-Secret-Token" }

// SubscribeSocialWebhook registers the host endpoint with Telegram while
// narrowing delivery to the channel update types handled by this connector.
func (connector *Connector) SubscribeSocialWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
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
	return connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		return runtime.Secrets().UseSecret(ctx, configuration.WebhookSecretReference, func(secret []byte) error {
			if !validWebhookSecret(secret) {
				return ErrInvalidCredentials
			}
			raw, callErr := connector.call(ctx, token, "setWebhook", []Param{
				{Name: "url", Value: endpoint},
				{Name: "secret_token", Value: string(secret)},
				{Name: "allowed_updates", Value: `["channel_post","edited_channel_post"]`},
			}, nil, true)
			if callErr != nil {
				return callErr
			}
			var accepted bool
			if json.Unmarshal(raw, &accepted) != nil || !accepted {
				return ErrInvalidResponse
			}
			return nil
		})
	})
}

// UnsubscribeSocialWebhook removes the configured Telegram webhook only when
// Telegram confirms that it points at the exact endpoint supplied by the
// caller. This prevents one account from deleting a deployment-owned webhook.
func (connector *Connector) UnsubscribeSocialWebhook(ctx context.Context, account sdk.Account, runtime sdk.Runtime, endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "social.webhooks") != nil || !validWebhookEndpoint(endpoint) {
		return sdk.ErrInvalidSocialWebhook
	}
	var endpointMismatch = errors.New("telegram: webhook endpoint mismatch")
	return connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, err := connector.call(ctx, token, "getWebhookInfo", nil, nil, false)
		if err != nil {
			return err
		}
		var info struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(raw, &info) != nil {
			return ErrInvalidResponse
		}
		if strings.TrimSpace(info.URL) != "" && strings.TrimSpace(info.URL) != endpoint {
			return endpointMismatch
		}
		raw, err = connector.call(ctx, token, "deleteWebhook", nil, nil, true)
		if err != nil {
			return err
		}
		var accepted bool
		if json.Unmarshal(raw, &accepted) != nil || !accepted {
			return ErrInvalidResponse
		}
		return nil
	})
}

// ReceiveSocialWebhook verifies a Telegram Bot API channel update and hands
// only a bounded, content-addressed event to the host-owned deduplicator.
// Subscription lifecycle calls remain outside this operation: Telegram's
// webhook URL and secret are configured by the deployment owner.
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
	err = runtime.Secrets().UseSecret(ctx, configuration.WebhookSecretReference, func(expected []byte) error {
		if !validWebhookSecret(expected) || len(expected) != len(request.VerificationToken) || subtle.ConstantTimeCompare(expected, request.VerificationToken) != 1 {
			return ErrWebhookUnauthorized
		}
		canonical, update, eventType, message, parseErr := canonicalTelegramUpdate(request.Body)
		if parseErr != nil {
			return parseErr
		}
		if message.Chat.ID != configuration.ChatID || message.Chat.Type != "channel" || message.MessageID < 1 || update.UpdateID < 0 {
			return ErrWebhookInvalid
		}
		seconds := message.Date
		if eventType == "edited_channel_post" && message.EditDate > 0 {
			seconds = message.EditDate
		}
		occurred := time.Unix(seconds, 0).UTC()
		if seconds < 1 || occurred.Year() < 2020 || occurred.After(request.ReceivedAt.Add(10*time.Minute)) {
			return ErrWebhookInvalid
		}
		digest := sha256.Sum256(canonical)
		fingerprint := hex.EncodeToString(digest[:])
		deliveryID := "sha256:" + fingerprint
		claim := sdk.SocialWebhookClaim{
			DeliveryID:          deliveryID,
			EventType:           "telegram." + eventType,
			RemoteChannelID:     strconv.FormatInt(message.Chat.ID, 10),
			RemoteObjectID:      strconv.FormatInt(message.MessageID, 10),
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
		result = sdk.SocialWebhookResult{DeliveryID: deliveryID, EventType: "telegram." + eventType, RemoteChannelID: strconv.FormatInt(message.Chat.ID, 10), RemoteObjectID: strconv.FormatInt(message.MessageID, 10), OccurredAt: occurred, Duplicate: duplicate, CanonicalPayload: canonical}
		return result.Validate()
	})
	return result, err
}

type telegramUpdate struct {
	UpdateID          int64                   `json:"update_id"`
	ChannelPost       *telegramWebhookMessage `json:"channel_post"`
	EditedChannelPost *telegramWebhookMessage `json:"edited_channel_post"`
}

type telegramWebhookMessage struct {
	MessageID int64 `json:"message_id"`
	Date      int64 `json:"date"`
	EditDate  int64 `json:"edit_date"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
}

func canonicalTelegramUpdate(body []byte) (json.RawMessage, telegramUpdate, string, telegramWebhookMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var generic any
	if dec.Decode(&generic) != nil {
		return nil, telegramUpdate{}, "", telegramWebhookMessage{}, ErrWebhookInvalid
	}
	canonical, err := json.Marshal(generic)
	if err != nil {
		return nil, telegramUpdate{}, "", telegramWebhookMessage{}, ErrWebhookInvalid
	}
	var update telegramUpdate
	if json.Unmarshal(canonical, &update) != nil || (update.ChannelPost == nil && update.EditedChannelPost == nil) || (update.ChannelPost != nil && update.EditedChannelPost != nil) {
		return nil, telegramUpdate{}, "", telegramWebhookMessage{}, ErrWebhookInvalid
	}
	if update.ChannelPost != nil {
		return canonical, update, "channel_post", *update.ChannelPost, nil
	}
	return canonical, update, "edited_channel_post", *update.EditedChannelPost, nil
}

func validWebhookSecret(value []byte) bool {
	if len(value) < 16 || len(value) > 256 {
		return false
	}
	for _, b := range value {
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-') {
			return false
		}
	}
	return true
}

func validWebhookEndpoint(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < len("https://a/b") || len(value) > 2048 {
		return false
	}
	if !strings.HasPrefix(value, "https://") || strings.ContainsAny(value, "\\?#") {
		return false
	}
	rest := value[len("https://"):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash == len(rest)-1 {
		return false
	}
	authority := rest[:slash]
	if strings.TrimSpace(authority) != authority || strings.ContainsAny(authority, "@[]") || strings.Contains(authority, "..") {
		return false
	}
	host, port := authority, ""
	if colon := strings.IndexByte(authority, ':'); colon >= 0 {
		if strings.LastIndexByte(authority, ':') != colon {
			return false
		}
		host, port = authority[:colon], authority[colon+1:]
		if port != "443" && port != "80" && port != "88" && port != "8443" {
			return false
		}
	}
	if host == "" || strings.Contains(host, ".") && strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, character := range []byte(host) {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '.' || character == '-') {
			return false
		}
	}
	return true
}

var _ sdk.SocialWebhookReceiver = (*Connector)(nil)
var _ sdk.SocialWebhookController = (*Connector)(nil)
