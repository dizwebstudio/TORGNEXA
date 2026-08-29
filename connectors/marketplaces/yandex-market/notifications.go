package yandexmarket

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type notificationEnvelope struct {
	NotificationType string `json:"notificationType"`
	BusinessID       int64  `json:"businessId"`
	CampaignID       int64  `json:"campaignId"`
	OrderID          int64  `json:"orderId"`
	ReturnID         int64  `json:"returnId"`
	FeedbackID       int64  `json:"feedbackId"`
	CommentID        int64  `json:"commentId"`
	ChatID           int64  `json:"chatId"`
	MessageID        int64  `json:"messageId"`
	QuestionID       int64  `json:"questionId"`
	AnswerID         int64  `json:"answerId"`
	RequestID        int64  `json:"requestId"`
	Status           string `json:"status"`
	Substatus        string `json:"substatus"`
	Time             string `json:"time"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
	CancelledAt      string `json:"cancelledAt"`
	RequestedAt      string `json:"requestedAt"`
	StartedAt        string `json:"startedAt"`
	FinishedAt       string `json:"finishedAt"`
}

type Acknowledgement struct {
	Version string    `json:"version"`
	Name    string    `json:"name"`
	Time    time.Time `json:"time"`
}

func (connector *Connector) DecodeMarketplaceNotification(ctx context.Context, account sdk.Account, body []byte) (sdk.MarketplaceNotification, error) {
	if connector == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "notifications.receive") != nil || len(body) == 0 || len(body) > 1<<20 {
		return sdk.MarketplaceNotification{}, sdk.ErrInvalidCommerceRead
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.MarketplaceNotification{}, err
	}
	var parsed notificationEnvelope
	if json.Unmarshal(body, &parsed) != nil || !knownNotificationType(parsed.NotificationType) {
		return sdk.MarketplaceNotification{}, ErrInvalidResponse
	}
	if parsed.BusinessID != 0 && parsed.BusinessID != configuration.BusinessID {
		return sdk.MarketplaceNotification{}, ErrInvalidResponse
	}
	if parsed.CampaignID != 0 && parsed.CampaignID != configuration.CampaignID {
		return sdk.MarketplaceNotification{}, ErrInvalidResponse
	}
	occurredAt, err := notificationTime(parsed)
	if err != nil {
		return sdk.MarketplaceNotification{}, ErrInvalidResponse
	}
	kind, remoteID, err := notificationResource(parsed)
	if err != nil {
		return sdk.MarketplaceNotification{}, ErrInvalidResponse
	}
	businessID, campaignID := "", ""
	if parsed.BusinessID > 0 {
		businessID = strconv.FormatInt(parsed.BusinessID, 10)
	}
	if parsed.CampaignID > 0 {
		campaignID = strconv.FormatInt(parsed.CampaignID, 10)
	}
	dedup := digestStrings(parsed.NotificationType, businessID, campaignID, kind, remoteID, occurredAt.Format(time.RFC3339Nano), parsed.Status, parsed.Substatus)
	projection := sdk.MarketplaceNotification{Type: parsed.NotificationType, BusinessRemoteID: businessID, CampaignRemoteID: campaignID, ResourceKind: kind, ResourceRemoteID: remoteID, OccurredAt: occurredAt, DedupKey: dedup}
	if projection.Validate() != nil {
		return sdk.MarketplaceNotification{}, ErrInvalidResponse
	}
	return projection, nil
}

func (connector *Connector) NotificationAcknowledgement(processedAt time.Time) (Acknowledgement, error) {
	if connector == nil || processedAt.IsZero() {
		return Acknowledgement{}, ErrInvalidResponse
	}
	return Acknowledgement{Version: Manifest().Version, Name: "TORGNEXA", Time: processedAt.UTC()}, nil
}

func knownNotificationType(value string) bool {
	switch value {
	case "PING", "ORDER_CREATED", "ORDER_CANCELLED", "ORDER_STATUS_UPDATED", "ORDER_RETURN_CREATED", "ORDER_CANCELLATION_REQUEST", "ORDER_RETURN_STATUS_UPDATED", "ORDER_UPDATED", "GOODS_FEEDBACK_CREATED", "GOODS_FEEDBACK_COMMENT_CREATED", "CHAT_CREATED", "CHAT_MESSAGE_SENT", "CHAT_ARBITRAGE_STARTED", "CHAT_ARBITRAGE_FINISHED", "QUESTION_CREATED", "QUESTION_ANSWER_CREATED", "QUESTION_COMMENT_CREATED":
		return true
	default:
		return false
	}
}

func notificationTime(item notificationEnvelope) (time.Time, error) {
	values := []string{item.Time, item.CreatedAt, item.UpdatedAt, item.CancelledAt, item.RequestedAt, item.StartedAt, item.FinishedAt}
	var latest time.Time
	for _, value := range values {
		if value == "" {
			continue
		}
		parsed, err := parseUTC(value)
		if err != nil {
			return time.Time{}, err
		}
		if latest.IsZero() || parsed.After(latest) {
			latest = parsed
		}
	}
	if latest.IsZero() {
		return time.Time{}, ErrInvalidResponse
	}
	return latest, nil
}

func notificationResource(item notificationEnvelope) (string, string, error) {
	typeName := item.NotificationType
	if typeName == "PING" {
		return "ping", "", nil
	}
	if strings.HasPrefix(typeName, "ORDER_RETURN_") {
		if item.ReturnID < 1 {
			return "", "", ErrInvalidResponse
		}
		return "return", strconv.FormatInt(item.ReturnID, 10), nil
	}
	if strings.HasPrefix(typeName, "ORDER_") {
		if item.OrderID < 1 {
			return "", "", ErrInvalidResponse
		}
		return "order", strconv.FormatInt(item.OrderID, 10), nil
	}
	if strings.HasPrefix(typeName, "GOODS_FEEDBACK_") {
		id := item.FeedbackID
		if typeName == "GOODS_FEEDBACK_COMMENT_CREATED" && item.CommentID > 0 {
			id = item.CommentID
		}
		if id < 1 {
			return "", "", ErrInvalidResponse
		}
		return "review", strconv.FormatInt(id, 10), nil
	}
	if strings.HasPrefix(typeName, "CHAT_") {
		id := item.ChatID
		if typeName == "CHAT_MESSAGE_SENT" && item.MessageID > 0 {
			id = item.MessageID
		}
		if id < 1 {
			return "", "", ErrInvalidResponse
		}
		return "chat", strconv.FormatInt(id, 10), nil
	}
	if strings.HasPrefix(typeName, "QUESTION_") {
		id := item.QuestionID
		if typeName == "QUESTION_ANSWER_CREATED" && item.AnswerID > 0 {
			id = item.AnswerID
		}
		if typeName == "QUESTION_COMMENT_CREATED" && item.CommentID > 0 {
			id = item.CommentID
		}
		if id < 1 {
			return "", "", ErrInvalidResponse
		}
		return "question", strconv.FormatInt(id, 10), nil
	}
	return "", "", ErrInvalidResponse
}
