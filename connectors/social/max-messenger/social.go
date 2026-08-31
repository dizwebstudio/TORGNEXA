package maxconnector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	maxTextRunes   = 4000
	maxAttachments = 12
	maxImageBytes  = 50 << 20
	maxVideoBytes  = 250 << 20
)

type attachment struct {
	Type    string            `json:"type"`
	Payload attachmentPayload `json:"payload"`
}
type attachmentPayload struct {
	Token   string            `json:"token,omitempty"`
	Buttons [][]maxLinkButton `json:"buttons,omitempty"`
}
type maxLinkButton struct {
	Type string `json:"type"`
	Text string `json:"text"`
	URL  string `json:"url"`
}
type newMessageBody struct {
	Text        string       `json:"text,omitempty"`
	Attachments []attachment `json:"attachments,omitempty"`
	Notify      bool         `json:"notify"`
}

type maxMessage struct {
	Recipient struct {
		ChatID   int64  `json:"chat_id"`
		ChatType string `json:"chat_type"`
	} `json:"recipient"`
	Body struct {
		MID string `json:"mid"`
	} `json:"body"`
}

func (connector *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), request) != nil || !maxContentLimits(request) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	var result sdk.SocialPublishResult
	err = connector.useSecret(ctx, runtime, account.SecretReference, validToken, func(token []byte) error {
		attachments := make([]attachment, 0, len(request.Media)+1)
		for _, ref := range request.Media {
			a, e := connector.uploadMedia(ctx, account, token, media, ref)
			if e != nil {
				return e
			}
			attachments = append(attachments, a)
		}
		if len(request.Buttons) > 0 {
			keyboard, e := encodeButtons(request.Buttons)
			if e != nil {
				return e
			}
			attachments = append(attachments, keyboard)
		}
		body, marshalErr := json.Marshal(newMessageBody{Text: request.Text, Attachments: attachments, Notify: true})
		if marshalErr != nil {
			return sdk.ErrInvalidSocialRequest
		}
		raw, callErr := connector.call(ctx, token, "POST", "/messages", []Param{{Name: "chat_id", Value: strconv.FormatInt(configuration.ChatID, 10)}}, body, true)
		if callErr != nil {
			return callErr
		}
		message, parseErr := parseMessage(raw)
		if parseErr != nil || message.Recipient.ChatID != configuration.ChatID || message.Body.MID == "" {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: remotePublicationID(configuration.ChatID, message.Body.MID), Status: sdk.SocialRemotePublished, ObservedAt: connector.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func maxContentLimits(request sdk.SocialPublishRequest) bool {
	if runeLen(request.Text) > maxTextRunes {
		return false
	}
	total := len(request.Media)
	if len(request.Buttons) > 0 {
		total++
	}
	if total > maxAttachments {
		return false
	}
	switch request.Kind {
	case sdk.SocialPostText:
		return request.Text != "" && len(request.Media) == 0
	case sdk.SocialPostMedia:
		return len(request.Media) >= 1 && len(request.Media) <= maxAttachments
	case sdk.SocialPostVideo:
		return len(request.Media) == 1
	default:
		return false
	}
}
func runeLen(v string) int { return len([]rune(v)) }

func encodeButtons(buttons []sdk.SocialButton) (attachment, error) {
	if len(buttons) == 0 {
		return attachment{}, sdk.ErrInvalidSocialRequest
	}
	rows := make([][]maxLinkButton, 0, (len(buttons)+2)/3)
	for i, b := range buttons {
		if b.Validate() != nil {
			return attachment{}, sdk.ErrInvalidSocialRequest
		}
		if i%3 == 0 {
			rows = append(rows, []maxLinkButton{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], maxLinkButton{Type: "link", Text: b.Text, URL: b.URL})
	}
	return attachment{Type: "inline_keyboard", Payload: attachmentPayload{Buttons: rows}}, nil
}

func (connector *Connector) uploadMedia(ctx context.Context, account sdk.Account, token []byte, media sdk.MediaAccessor, ref sdk.SocialMediaRef) (attachment, error) {
	if media == nil {
		return attachment{}, sdk.ErrInvalidSocialRequest
	}
	reader, descriptor, err := media.OpenReleased(ctx, account, ref)
	if err != nil {
		return attachment{}, err
	}
	if reader == nil {
		return attachment{}, sdk.ErrInvalidSocialRequest
	}
	defer reader.Close()
	if descriptor.Validate() != nil || descriptor.SizeBytes < 1 {
		return attachment{}, sdk.ErrInvalidSocialRequest
	}
	typ, ext, maxBytes := "image", ".jpg", int64(maxImageBytes)
	switch ref.Kind {
	case sdk.SocialMediaImage:
		if !supportedImageType(descriptor.MediaType) {
			return attachment{}, sdk.ErrInvalidSocialRequest
		}
		ext = imageExtension(descriptor.MediaType)
	case sdk.SocialMediaVideo:
		typ = "video"
		ext = videoExtension(descriptor.MediaType)
		maxBytes = maxVideoBytes
		if ext == "" {
			return attachment{}, sdk.ErrInvalidSocialRequest
		}
	default:
		return attachment{}, sdk.ErrInvalidSocialRequest
	}
	if descriptor.SizeBytes > maxBytes {
		return attachment{}, sdk.ErrInvalidSocialRequest
	}
	raw, e := connector.call(ctx, token, "POST", "/uploads", []Param{{Name: "type", Value: typ}}, nil, false)
	if e != nil {
		return attachment{}, e
	}
	var init struct {
		URL   string `json:"url"`
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &init) != nil || !validUploadURL(init.URL, typ) {
		return attachment{}, ErrInvalidResponse
	}
	response, e := connector.transport.Upload(ctx, UploadRequest{URL: init.URL, FileName: ref.UploadID + ext, MediaType: descriptor.MediaType, SizeBytes: descriptor.SizeBytes, SHA256: descriptor.SHA256, Body: io.LimitReader(reader, descriptor.SizeBytes), AccessToken: token})
	if e != nil {
		return attachment{}, remoteError(sdk.ErrorInternal, "upload_outcome_unknown", "", 0)
	}
	if remote := normalizeHTTP(response, true); remote != nil {
		return attachment{}, remote
	}
	tokenValue := init.Token
	if tokenValue == "" {
		var uploaded struct {
			Token string `json:"token"`
		}
		if json.Unmarshal(response.Body, &uploaded) != nil {
			return attachment{}, ErrInvalidResponse
		}
		tokenValue = uploaded.Token
	}
	if !validOpaqueToken(tokenValue) {
		return attachment{}, ErrInvalidResponse
	}
	return attachment{Type: typ, Payload: attachmentPayload{Token: tokenValue}}, nil
}

func parseMessage(raw json.RawMessage) (maxMessage, error) {
	var wrapped struct {
		Message maxMessage `json:"message"`
	}
	if json.Unmarshal(raw, &wrapped) == nil && wrapped.Message.Body.MID != "" {
		return wrapped.Message, nil
	}
	var direct maxMessage
	if json.Unmarshal(raw, &direct) == nil && direct.Body.MID != "" {
		return direct, nil
	}
	return maxMessage{}, ErrInvalidResponse
}

func (connector *Connector) ReadSocialPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialPublishResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	chatID, mid, err := parseRemotePublicationID(remoteID)
	if err != nil || chatID != configuration.ChatID {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialPublishResult
	err = connector.useSecret(ctx, runtime, account.SecretReference, validToken, func(token []byte) error {
		raw, e := connector.call(ctx, token, "GET", "/messages/"+mid, nil, nil, false)
		if e != nil {
			var remote *sdk.RemoteError
			if errorsAsNotFound(e, &remote) {
				result = sdk.SocialPublishResult{RemotePublicationID: remoteID, Status: sdk.SocialRemoteFailed, ReasonCode: "remote_missing", ObservedAt: connector.now().UTC()}
				return result.Validate()
			}
			return e
		}
		message, e := parseMessage(raw)
		if e != nil || message.Recipient.ChatID != configuration.ChatID || message.Body.MID != mid {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: remoteID, Status: sdk.SocialRemotePublished, ObservedAt: connector.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func errorsAsNotFound(err error, target **sdk.RemoteError) bool {
	if !errors.As(err, target) {
		return false
	}
	return (*target).Category == sdk.ErrorNotFound
}

func remotePublicationID(chatID int64, mid string) string {
	return fmt.Sprintf("max:%d:%s", chatID, mid)
}
func parseRemotePublicationID(value string) (int64, string, error) {
	if !strings.HasPrefix(value, "max:") {
		return 0, "", sdk.ErrInvalidSocialRequest
	}
	rest := strings.TrimPrefix(value, "max:")
	pos := strings.IndexByte(rest, ':')
	if pos < 1 {
		return 0, "", sdk.ErrInvalidSocialRequest
	}
	chatID, e := strconv.ParseInt(rest[:pos], 10, 64)
	mid := rest[pos+1:]
	if e != nil || chatID == 0 || !validMID(mid) || remotePublicationID(chatID, mid) != value {
		return 0, "", sdk.ErrInvalidSocialRequest
	}
	return chatID, mid, nil
}
func validMID(value string) bool {
	if value == "" || len(value) > 512 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
func validOpaqueToken(v string) bool {
	if v == "" || len(v) > 4096 || strings.TrimSpace(v) != v {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func supportedImageType(v string) bool {
	switch v {
	case "image/jpeg", "image/png", "image/gif", "image/tiff", "image/bmp", "image/heic":
		return true
	}
	return false
}
func imageExtension(v string) string {
	switch v {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/tiff":
		return ".tiff"
	case "image/bmp":
		return ".bmp"
	case "image/heic":
		return ".heic"
	default:
		return ".jpg"
	}
}
func videoExtension(v string) string {
	switch v {
	case "video/mp4":
		return ".mp4"
	case "video/quicktime":
		return ".mov"
	case "video/x-matroska":
		return ".mkv"
	case "video/webm":
		return ".webm"
	default:
		return ""
	}
}
func validUploadURL(value, typ string) bool {
	const prefix = "https://"
	if !strings.HasPrefix(value, prefix) || len(value) > 8192 || strings.ContainsAny(value, "\\#") {
		return false
	}
	rest := value[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return false
	}
	authority := rest[:slash]
	if strings.ContainsAny(authority, "@%[]") || strings.TrimSpace(authority) != authority {
		return false
	}
	host := authority
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		if authority[colon+1:] != "443" || strings.Contains(authority[:colon], ":") {
			return false
		}
		host = authority[:colon]
	}
	host = strings.ToLower(host)
	expected := "iu.oneme.ru"
	if typ == "video" {
		expected = "omub.okcdn.ru"
	}
	return host == expected
}

var _ sdk.SocialPublisher = (*Connector)(nil)
var _ sdk.SocialPublicationStatusReader = (*Connector)(nil)
