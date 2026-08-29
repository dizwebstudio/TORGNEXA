package instagram

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	maxCaptionRunes = 2200
	maxImages       = 10
	maxImageBytes   = 8 << 20
	maxVideoBytes   = 300 << 20
)

type StagedMedia struct {
	URL       string
	ExpiresAt time.Time
}
type MediaStager interface {
	Stage(context.Context, sdk.Account, sdk.SocialMediaRef, sdk.MediaDescriptor, io.Reader) (StagedMedia, error)
}

func (c *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || c.stager == nil || media == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), request) != nil || len(request.Buttons) != 0 {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if utf8.RuneCountInString(request.Text) > maxCaptionRunes {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if request.Kind == sdk.SocialPostMedia && (len(request.Media) < 1 || len(request.Media) > maxImages) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if request.Kind == sdk.SocialPostVideo && len(request.Media) != 1 {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	staged := make([]StagedMedia, 0, len(request.Media))
	for _, ref := range request.Media {
		item, e := c.stageReleased(ctx, account, media, ref)
		if e != nil {
			return sdk.SocialPublishResult{}, e
		}
		staged = append(staged, item)
	}
	var result sdk.SocialPublishResult
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		var creationID string
		var e error
		switch request.Kind {
		case sdk.SocialPostMedia:
			if len(staged) == 1 {
				creationID, e = c.createContainer(ctx, token, cfg.InstagramUserID, []Param{{Name: "image_url", Value: staged[0].URL}, {Name: "caption", Value: request.Text}})
			} else {
				creationID, e = c.createImageCarousel(ctx, token, cfg.InstagramUserID, request.Text, staged)
			}
		case sdk.SocialPostVideo:
			creationID, e = c.createContainer(ctx, token, cfg.InstagramUserID, []Param{{Name: "media_type", Value: "REELS"}, {Name: "video_url", Value: staged[0].URL}, {Name: "caption", Value: request.Text}})
		default:
			return sdk.ErrInvalidSocialRequest
		}
		if e != nil {
			return e
		}
		if e = c.awaitContainer(ctx, token, creationID); e != nil {
			return e
		}
		raw, e := c.call(ctx, token, "POST", "/"+apiVersion+"/"+cfg.InstagramUserID+"/media_publish", []Param{{Name: "creation_id", Value: creationID}}, true)
		if e != nil {
			return e
		}
		var published struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &published) != nil || !digits(published.ID, 5, 64) {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: "instagram:" + cfg.InstagramUserID + ":" + published.ID, Status: sdk.SocialRemotePublished, ObservedAt: c.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func (c *Connector) stageReleased(ctx context.Context, account sdk.Account, media sdk.MediaAccessor, ref sdk.SocialMediaRef) (StagedMedia, error) {
	reader, desc, err := media.OpenReleased(ctx, account, ref)
	if err != nil {
		return StagedMedia{}, err
	}
	if reader == nil {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	defer reader.Close()
	if desc.Validate() != nil || desc.SizeBytes < 1 {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	if ref.Kind == sdk.SocialMediaImage {
		if desc.MediaType != "image/jpeg" || desc.SizeBytes > maxImageBytes {
			return StagedMedia{}, sdk.ErrInvalidSocialRequest
		}
	} else if ref.Kind == sdk.SocialMediaVideo {
		if desc.MediaType != "video/mp4" || desc.SizeBytes > maxVideoBytes {
			return StagedMedia{}, sdk.ErrInvalidSocialRequest
		}
	} else {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	staged, err := c.stager.Stage(ctx, account, ref, desc, io.LimitReader(reader, desc.SizeBytes))
	if err != nil {
		return StagedMedia{}, err
	}
	now := c.now().UTC()
	if !validStagedURL(staged.URL) || !staged.ExpiresAt.After(now.Add(2*time.Minute)) || staged.ExpiresAt.After(now.Add(24*time.Hour)) {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	return staged, nil
}

func (c *Connector) createImageCarousel(ctx context.Context, token []byte, userID, caption string, staged []StagedMedia) (string, error) {
	children := make([]string, 0, len(staged))
	for _, item := range staged {
		id, err := c.createContainer(ctx, token, userID, []Param{{Name: "image_url", Value: item.URL}, {Name: "is_carousel_item", Value: "true"}})
		if err != nil {
			return "", err
		}
		if err = c.awaitContainer(ctx, token, id); err != nil {
			return "", err
		}
		children = append(children, id)
	}
	return c.createContainer(ctx, token, userID, []Param{{Name: "media_type", Value: "CAROUSEL"}, {Name: "children", Value: strings.Join(children, ",")}, {Name: "caption", Value: caption}})
}
func (c *Connector) createContainer(ctx context.Context, token []byte, userID string, params []Param) (string, error) {
	raw, err := c.call(ctx, token, "POST", "/"+apiVersion+"/"+userID+"/media", params, true)
	if err != nil {
		return "", err
	}
	var out struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &out) != nil || !digits(out.ID, 5, 64) {
		return "", ErrInvalidResponse
	}
	return out.ID, nil
}
func (c *Connector) awaitContainer(ctx context.Context, token []byte, id string) error {
	for attempt := 0; attempt < 5; attempt++ {
		raw, err := c.call(ctx, token, "GET", "/"+apiVersion+"/"+id, []Param{{Name: "fields", Value: "status_code"}}, false)
		if err != nil {
			return err
		}
		var out struct {
			Status string `json:"status_code"`
		}
		if json.Unmarshal(raw, &out) != nil {
			return ErrInvalidResponse
		}
		switch out.Status {
		case "FINISHED", "PUBLISHED":
			return nil
		case "ERROR", "EXPIRED":
			return newRemote(sdk.ErrorInvalidRequest, "media_rejected", "", 0)
		case "IN_PROGRESS":
		default:
			return ErrInvalidResponse
		}
		if attempt < 4 {
			if err = c.wait(ctx, time.Second); err != nil {
				return err
			}
		}
	}
	return newRemote(sdk.ErrorTransient, "media_processing", "", 0)
}

func (c *Connector) ReadSocialPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, mediaID, ok := parseRemoteID(remoteID)
	if !ok {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	if cfg != configuration.InstagramUserID {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialPublishResult
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, e := c.call(ctx, token, "GET", "/"+apiVersion+"/"+mediaID, []Param{{Name: "fields", Value: "id,media_type"}}, false)
		if e != nil {
			return e
		}
		var out struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &out) != nil || out.ID != mediaID {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: remoteID, Status: sdk.SocialRemotePublished, ObservedAt: c.now().UTC()}
		return result.Validate()
	})
	return result, err
}
func parseRemoteID(v string) (string, string, bool) {
	if !strings.HasPrefix(v, "instagram:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(v, "instagram:")
	user, media, ok := strings.Cut(rest, ":")
	if !ok || !digits(user, 5, 64) || !digits(media, 5, 64) {
		return "", "", false
	}
	return user, media, true
}
func validStagedURL(v string) bool {
	if len(v) < 12 || len(v) > 8192 || v != strings.TrimSpace(v) || !strings.HasPrefix(v, "https://") || strings.Contains(v, "\\") || strings.Contains(v, "#") {
		return false
	}
	rest := strings.TrimPrefix(v, "https://")
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return false
	}
	authority := rest[:slash]
	if strings.ContainsAny(authority, "@%[]") || strings.Contains(authority, "..") {
		return false
	}
	host := authority
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		if authority[colon+1:] != "443" || strings.Contains(authority[:colon], ":") {
			return false
		}
		host = authority[:colon]
	}
	if host == "" || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, r := range strings.ToLower(host) {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
			return false
		}
	}
	return true
}
func paramValue(params []Param, name string) string {
	for _, p := range params {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}
func atoi(v string) int { n, _ := strconv.Atoi(v); return n }

var _ sdk.SocialPublisher = (*Connector)(nil)
var _ sdk.SocialPublicationStatusReader = (*Connector)(nil)
