package vk

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
	maxVKPhotos     = 10
	maxVKPhotoBytes = 50 << 20
)

func (connector *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), request) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if request.Kind == sdk.SocialPostMedia && (media == nil || len(request.Media) > maxVKPhotos) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	var result sdk.SocialPublishResult
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		attachments := make([]string, 0, len(request.Media))
		for _, ref := range request.Media {
			attachment, uploadErr := connector.uploadPhoto(ctx, account, token, configuration, media, ref)
			if uploadErr != nil {
				return uploadErr
			}
			attachments = append(attachments, attachment)
		}
		params := []Param{
			{Name: "owner_id", Value: strconv.FormatInt(-configuration.GroupID, 10)},
			{Name: "from_group", Value: "1"},
			{Name: "guid", Value: request.PublicationID},
		}
		if request.Text != "" {
			params = append(params, Param{Name: "message", Value: request.Text})
		}
		if len(attachments) > 0 {
			params = append(params, Param{Name: "attachments", Value: strings.Join(attachments, ",")})
		}
		raw, callErr := connector.call(ctx, token, "wall.post", params)
		if callErr != nil {
			return callErr
		}
		var response struct {
			PostID int64 `json:"post_id"`
		}
		if json.Unmarshal(raw, &response) != nil || response.PostID < 1 {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{
			RemotePublicationID: remotePublicationID(configuration.GroupID, response.PostID),
			Status:              sdk.SocialRemotePublished,
			ObservedAt:          connector.now().UTC(),
		}
		return result.Validate()
	})
	return result, err
}

func (connector *Connector) uploadPhoto(ctx context.Context, account sdk.Account, token []byte, configuration Configuration, media sdk.MediaAccessor, ref sdk.SocialMediaRef) (string, error) {
	if ref.Kind != sdk.SocialMediaImage {
		return "", sdk.ErrInvalidSocialRequest
	}
	reader, descriptor, err := media.OpenReleased(ctx, account, ref)
	if err != nil {
		return "", err
	}
	if reader == nil {
		return "", sdk.ErrInvalidSocialRequest
	}
	defer reader.Close()
	if descriptor.Validate() != nil || descriptor.SizeBytes < 1 || descriptor.SizeBytes > maxVKPhotoBytes || !supportedPhotoType(descriptor.MediaType) {
		return "", sdk.ErrInvalidSocialRequest
	}
	raw, err := connector.call(ctx, token, "photos.getWallUploadServer", []Param{{Name: "group_id", Value: strconv.FormatInt(configuration.GroupID, 10)}})
	if err != nil {
		return "", err
	}
	var server struct {
		UploadURL string `json:"upload_url"`
	}
	if json.Unmarshal(raw, &server) != nil || !validUploadURL(server.UploadURL) {
		return "", ErrInvalidResponse
	}
	uploadResponse, err := connector.transport.Upload(ctx, UploadRequest{
		URL:       server.UploadURL,
		FieldName: "photo",
		FileName:  ref.UploadID + photoExtension(descriptor.MediaType),
		MediaType: descriptor.MediaType,
		SizeBytes: descriptor.SizeBytes,
		SHA256:    descriptor.SHA256,
		Body:      io.LimitReader(reader, descriptor.SizeBytes),
	})
	if err != nil {
		return "", normalizedTransportError()
	}
	if remote := normalizeHTTP(uploadResponse); remote != nil {
		return "", remote
	}
	var uploaded struct {
		Server int64  `json:"server"`
		Photo  string `json:"photo"`
		Hash   string `json:"hash"`
	}
	if json.Unmarshal(uploadResponse.Body, &uploaded) != nil || uploaded.Server == 0 || !validOpaqueUploadValue(uploaded.Photo, 4<<20) || !validOpaqueUploadValue(uploaded.Hash, 4096) {
		return "", ErrInvalidResponse
	}
	savedRaw, err := connector.call(ctx, token, "photos.saveWallPhoto", []Param{
		{Name: "group_id", Value: strconv.FormatInt(configuration.GroupID, 10)},
		{Name: "server", Value: strconv.FormatInt(uploaded.Server, 10)},
		{Name: "photo", Value: uploaded.Photo},
		{Name: "hash", Value: uploaded.Hash},
	})
	if err != nil {
		return "", err
	}
	var saved []struct {
		ID      int64 `json:"id"`
		OwnerID int64 `json:"owner_id"`
	}
	if json.Unmarshal(savedRaw, &saved) != nil || len(saved) != 1 || saved[0].ID < 1 || saved[0].OwnerID == 0 {
		return "", ErrInvalidResponse
	}
	return fmt.Sprintf("photo%d_%d", saved[0].OwnerID, saved[0].ID), nil
}

func (connector *Connector) ReadSocialPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialPublishResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	groupID, postID, err := parseRemotePublicationID(remoteID)
	if err != nil || groupID != configuration.GroupID {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialPublishResult
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, callErr := connector.call(ctx, token, "wall.getById", []Param{{Name: "posts", Value: remotePublicationID(groupID, postID)}})
		if callErr != nil {
			return callErr
		}
		found, parseErr := wallPostExists(raw, -groupID, postID)
		if parseErr != nil {
			return parseErr
		}
		result = sdk.SocialPublishResult{RemotePublicationID: remoteID, Status: sdk.SocialRemotePublished, ObservedAt: connector.now().UTC()}
		if !found {
			result.Status = sdk.SocialRemoteFailed
			result.ReasonCode = "remote_missing"
		}
		return result.Validate()
	})
	return result, err
}

func wallPostExists(raw json.RawMessage, ownerID, postID int64) (bool, error) {
	type post struct {
		ID      int64 `json:"id"`
		OwnerID int64 `json:"owner_id"`
	}
	var items []post
	if json.Unmarshal(raw, &items) != nil {
		var wrapper struct {
			Items []post `json:"items"`
		}
		if json.Unmarshal(raw, &wrapper) != nil {
			return false, ErrInvalidResponse
		}
		items = wrapper.Items
	}
	for _, item := range items {
		if item.ID == postID && item.OwnerID == ownerID {
			return true, nil
		}
	}
	return false, nil
}

func remotePublicationID(groupID, postID int64) string {
	return fmt.Sprintf("-%d_%d", groupID, postID)
}

func parseRemotePublicationID(value string) (int64, int64, error) {
	if value == "" || !strings.HasPrefix(value, "-") || strings.Count(value, "_") != 1 {
		return 0, 0, sdk.ErrInvalidSocialRequest
	}
	owner, post, ok := strings.Cut(value[1:], "_")
	if !ok {
		return 0, 0, sdk.ErrInvalidSocialRequest
	}
	groupID, err1 := strconv.ParseInt(owner, 10, 64)
	postID, err2 := strconv.ParseInt(post, 10, 64)
	if err1 != nil || err2 != nil || groupID < 1 || postID < 1 || remotePublicationID(groupID, postID) != value {
		return 0, 0, sdk.ErrInvalidSocialRequest
	}
	return groupID, postID, nil
}

func supportedPhotoType(value string) bool {
	return value == "image/jpeg" || value == "image/png" || value == "image/webp"
}

func photoExtension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}

func validUploadURL(value string) bool {
	// Provider packages intentionally cannot import net/http or net/url. Parse
	// only the narrow signed-upload form we are willing to hand to host egress.
	const prefix = "https://"
	if !strings.HasPrefix(value, prefix) || len(value) > 8192 || strings.Contains(value, "\\") || strings.Contains(value, "#") {
		return false
	}
	rest := value[len(prefix):]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash == len(rest)-1 {
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
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	for _, r := range host {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
			return false
		}
	}
	return host == "vk.com" || strings.HasSuffix(host, ".vk.com") || host == "userapi.com" || strings.HasSuffix(host, ".userapi.com")
}

func validOpaqueUploadValue(value string, max int) bool {
	if value == "" || len(value) > max || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

var _ sdk.SocialPublisher = (*Connector)(nil)
var _ sdk.SocialPublicationStatusReader = (*Connector)(nil)
