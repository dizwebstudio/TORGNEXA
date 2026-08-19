package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	maxTextRunes    = 4096
	maxCaptionRunes = 1024
	maxAlbumItems   = 10
	maxPhotoBytes   = 10 << 20
	maxVideoBytes   = 50 << 20
)

type telegramMessage struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}
type inputMedia struct {
	Type    string `json:"type"`
	Media   string `json:"media"`
	Caption string `json:"caption,omitempty"`
}
type inlineButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}
type inlineKeyboard struct {
	InlineKeyboard [][]inlineButton `json:"inline_keyboard"`
}

func (connector *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), request) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if !telegramContentLimits(request.Kind, request.Text, len(request.Media), len(request.Buttons)) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	var result sdk.SocialPublishResult
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		params := []Param{{Name: "chat_id", Value: strconv.FormatInt(configuration.ChatID, 10)}}
		markup, markupErr := encodeButtons(request.Buttons)
		if markupErr != nil {
			return markupErr
		}
		if markup != "" {
			params = append(params, Param{Name: "reply_markup", Value: markup})
		}
		method := "sendMessage"
		var files []FilePart
		var raw json.RawMessage
		var callErr error
		switch request.Kind {
		case sdk.SocialPostText:
			params = append(params, Param{Name: "text", Value: request.Text})
			raw, callErr = connector.call(ctx, token, method, params, nil, true)
		case sdk.SocialPostMedia:
			if media == nil {
				return sdk.ErrInvalidSocialRequest
			}
			if len(request.Media) == 1 {
				method = "sendPhoto"
				part, err := openPart(ctx, account, media, request.Media[0], "photo", 0)
				if err != nil {
					return err
				}
				defer part.close()
				files = []FilePart{part.file}
				params = append(params, Param{Name: "photo", Value: "attach://photo0"})
				if request.Text != "" {
					params = append(params, Param{Name: "caption", Value: request.Text})
				}
				raw, callErr = connector.call(ctx, token, method, params, files, true)
			} else {
				if len(request.Buttons) > 0 {
					return sdk.ErrInvalidSocialRequest
				}
				method = "sendMediaGroup"
				items := make([]inputMedia, 0, len(request.Media))
				closers := make([]io.Closer, 0, len(request.Media))
				defer func() {
					for _, c := range closers {
						_ = c.Close()
					}
				}()
				for i, ref := range request.Media {
					part, err := openPart(ctx, account, media, ref, "photo", i)
					if err != nil {
						return err
					}
					closers = append(closers, part.closer)
					files = append(files, part.file)
					item := inputMedia{Type: "photo", Media: "attach://" + part.file.FieldName}
					if i == 0 {
						item.Caption = request.Text
					}
					items = append(items, item)
				}
				encoded, _ := json.Marshal(items)
				params = []Param{{Name: "chat_id", Value: strconv.FormatInt(configuration.ChatID, 10)}, {Name: "media", Value: string(encoded)}}
				raw, callErr = connector.call(ctx, token, method, params, files, true)
			}
		case sdk.SocialPostVideo:
			if media == nil {
				return sdk.ErrInvalidSocialRequest
			}
			method = "sendVideo"
			part, err := openPart(ctx, account, media, request.Media[0], "video", 0)
			if err != nil {
				return err
			}
			defer part.close()
			files = []FilePart{part.file}
			params = append(params, Param{Name: "video", Value: "attach://video0"})
			if request.Text != "" {
				params = append(params, Param{Name: "caption", Value: request.Text})
			}
			raw, callErr = connector.call(ctx, token, method, params, files, true)
		default:
			return sdk.ErrInvalidSocialRequest
		}
		if callErr != nil {
			return callErr
		}
		ids, err := parseMessages(raw, configuration.ChatID)
		if err != nil {
			return err
		}
		result = sdk.SocialPublishResult{RemotePublicationID: remotePublicationID(configuration.ChatID, ids), Status: sdk.SocialRemotePublished, ObservedAt: connector.now().UTC()}
		return result.Validate()
	})
	return result, err
}

type openedPart struct {
	file   FilePart
	closer io.Closer
}

func (part openedPart) close() {
	if part.closer != nil {
		_ = part.closer.Close()
	}
}

func openPart(ctx context.Context, account sdk.Account, media sdk.MediaAccessor, ref sdk.SocialMediaRef, prefix string, index int) (openedPart, error) {
	reader, descriptor, err := media.OpenReleased(ctx, account, ref)
	if err != nil {
		return openedPart{}, err
	}
	if reader == nil {
		return openedPart{}, sdk.ErrInvalidSocialRequest
	}
	if descriptor.Validate() != nil || descriptor.SizeBytes < 1 {
		reader.Close()
		return openedPart{}, sdk.ErrInvalidSocialRequest
	}
	max := int64(maxPhotoBytes)
	allowed := ref.Kind == sdk.SocialMediaImage && supportedPhotoType(descriptor.MediaType)
	ext := photoExtension(descriptor.MediaType)
	if ref.Kind == sdk.SocialMediaVideo {
		max = maxVideoBytes
		allowed = descriptor.MediaType == "video/mp4"
		ext = ".mp4"
	}
	if !allowed || descriptor.SizeBytes > max {
		reader.Close()
		return openedPart{}, sdk.ErrInvalidSocialRequest
	}
	name := fmt.Sprintf("%s%d", prefix, index)
	return openedPart{file: FilePart{FieldName: name, FileName: ref.UploadID + ext, MediaType: descriptor.MediaType, SizeBytes: descriptor.SizeBytes, SHA256: descriptor.SHA256, Body: io.LimitReader(reader, descriptor.SizeBytes)}, closer: reader}, nil
}

func (connector *Connector) EditSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialEditRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialEdit(Manifest(), request) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if !telegramContentLimits(request.Kind, request.Text, len(request.Media), len(request.Buttons)) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	chatID, ids, err := parseRemotePublicationID(request.RemotePublicationID)
	if err != nil || chatID != configuration.ChatID || len(ids) != 1 {
		return sdk.SocialPublishResult{}, unsupported("album_edit_unsupported")
	}
	var result sdk.SocialPublishResult
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		params := []Param{{Name: "chat_id", Value: strconv.FormatInt(chatID, 10)}, {Name: "message_id", Value: strconv.FormatInt(ids[0], 10)}}
		markup, e := encodeButtons(request.Buttons)
		if e != nil {
			return e
		}
		if markup != "" {
			params = append(params, Param{Name: "reply_markup", Value: markup})
		}
		var raw json.RawMessage
		var callErr error
		if request.Kind == sdk.SocialPostText {
			params = append(params, Param{Name: "text", Value: request.Text})
			raw, callErr = connector.call(ctx, token, "editMessageText", params, nil, true)
		} else {
			if media == nil || len(request.Media) != 1 {
				return sdk.ErrInvalidSocialRequest
			}
			prefix := "photo"
			mediaType := "photo"
			if request.Kind == sdk.SocialPostVideo {
				prefix = "video"
				mediaType = "video"
			}
			part, openErr := openPart(ctx, account, media, request.Media[0], prefix, 0)
			if openErr != nil {
				return openErr
			}
			defer part.close()
			item := inputMedia{Type: mediaType, Media: "attach://" + part.file.FieldName, Caption: request.Text}
			encoded, _ := json.Marshal(item)
			params = append(params, Param{Name: "media", Value: string(encoded)})
			raw, callErr = connector.call(ctx, token, "editMessageMedia", params, []FilePart{part.file}, true)
		}
		if callErr != nil {
			return callErr
		}
		parsed, parseErr := parseMessages(raw, chatID)
		if parseErr != nil || len(parsed) != 1 || parsed[0] != ids[0] {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: request.RemotePublicationID, Status: sdk.SocialRemotePublished, ObservedAt: connector.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func (connector *Connector) DeleteSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialDeleteResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "social.post.delete") != nil {
		return sdk.SocialDeleteResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialDeleteResult{}, err
	}
	chatID, ids, err := parseRemotePublicationID(remoteID)
	if err != nil || chatID != configuration.ChatID {
		return sdk.SocialDeleteResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialDeleteResult
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		params := []Param{{Name: "chat_id", Value: strconv.FormatInt(chatID, 10)}}
		method := "deleteMessage"
		if len(ids) == 1 {
			params = append(params, Param{Name: "message_id", Value: strconv.FormatInt(ids[0], 10)})
		} else {
			method = "deleteMessages"
			encoded, _ := json.Marshal(ids)
			params = append(params, Param{Name: "message_ids", Value: string(encoded)})
		}
		raw, e := connector.call(ctx, token, method, params, nil, true)
		if e != nil {
			return e
		}
		var ok bool
		if json.Unmarshal(raw, &ok) != nil || !ok {
			return ErrInvalidResponse
		}
		result = sdk.SocialDeleteResult{RemotePublicationID: remoteID, Deleted: true, ObservedAt: connector.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func telegramContentLimits(kind sdk.SocialPostKind, text string, mediaCount, buttonCount int) bool {
	switch kind {
	case sdk.SocialPostText:
		return runeLen(text) > 0 && runeLen(text) <= maxTextRunes && mediaCount == 0
	case sdk.SocialPostMedia:
		return runeLen(text) <= maxCaptionRunes && mediaCount >= 1 && mediaCount <= maxAlbumItems && !(mediaCount > 1 && buttonCount > 0)
	case sdk.SocialPostVideo:
		return runeLen(text) <= maxCaptionRunes && mediaCount == 1
	default:
		return false
	}
}
func runeLen(s string) int { return len([]rune(s)) }
func encodeButtons(buttons []sdk.SocialButton) (string, error) {
	if len(buttons) == 0 {
		return "", nil
	}
	rows := make([][]inlineButton, 0, (len(buttons)+1)/2)
	for i, b := range buttons {
		if b.Validate() != nil {
			return "", sdk.ErrInvalidSocialRequest
		}
		if i%2 == 0 {
			rows = append(rows, []inlineButton{})
		}
		rows[len(rows)-1] = append(rows[len(rows)-1], inlineButton{Text: b.Text, URL: b.URL})
	}
	raw, err := json.Marshal(inlineKeyboard{InlineKeyboard: rows})
	if err != nil {
		return "", sdk.ErrInvalidSocialRequest
	}
	return string(raw), nil
}
func parseMessages(raw json.RawMessage, chatID int64) ([]int64, error) {
	var one telegramMessage
	if json.Unmarshal(raw, &one) == nil && one.MessageID > 0 {
		if one.Chat.ID != chatID {
			return nil, ErrInvalidResponse
		}
		return []int64{one.MessageID}, nil
	}
	var many []telegramMessage
	if json.Unmarshal(raw, &many) != nil || len(many) < 2 || len(many) > maxAlbumItems {
		return nil, ErrInvalidResponse
	}
	ids := make([]int64, 0, len(many))
	for _, m := range many {
		if m.MessageID < 1 || m.Chat.ID != chatID {
			return nil, ErrInvalidResponse
		}
		ids = append(ids, m.MessageID)
	}
	return ids, nil
}
func remotePublicationID(chatID int64, ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return fmt.Sprintf("tg:%d:%s", chatID, strings.Join(parts, ","))
}
func parseRemotePublicationID(value string) (int64, []int64, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[0] != "tg" {
		return 0, nil, sdk.ErrInvalidSocialRequest
	}
	chatID, e := strconv.ParseInt(parts[1], 10, 64)
	if e != nil || chatID >= 0 {
		return 0, nil, sdk.ErrInvalidSocialRequest
	}
	rawIDs := strings.Split(parts[2], ",")
	if len(rawIDs) < 1 || len(rawIDs) > maxAlbumItems {
		return 0, nil, sdk.ErrInvalidSocialRequest
	}
	ids := make([]int64, len(rawIDs))
	for i, s := range rawIDs {
		id, e := strconv.ParseInt(s, 10, 64)
		if e != nil || id < 1 {
			return 0, nil, sdk.ErrInvalidSocialRequest
		}
		ids[i] = id
	}
	if remotePublicationID(chatID, ids) != value {
		return 0, nil, sdk.ErrInvalidSocialRequest
	}
	return chatID, ids, nil
}
func supportedPhotoType(v string) bool {
	return v == "image/jpeg" || v == "image/png" || v == "image/webp"
}
func photoExtension(v string) string {
	if v == "image/png" {
		return ".png"
	}
	if v == "image/webp" {
		return ".webp"
	}
	return ".jpg"
}
func unsupported(code string) error {
	r, _ := sdk.NewRemoteError(sdk.ErrorUnsupported, code, "", 0)
	return r
}

var _ sdk.SocialPublisher = (*Connector)(nil)
var _ sdk.SocialEditor = (*Connector)(nil)
var _ sdk.SocialDeleter = (*Connector)(nil)
