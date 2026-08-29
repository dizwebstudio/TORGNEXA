package threads

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	maxTextRunes  = 500
	maxImages     = 20
	maxImageBytes = 8 << 20
	maxVideoBytes = 1 << 30
)

type StagedMedia struct {
	URL       string
	ExpiresAt time.Time
}
type MediaStager interface {
	Stage(context.Context, sdk.Account, sdk.SocialMediaRef, sdk.MediaDescriptor, io.Reader) (StagedMedia, error)
}

func (c *Connector) PublishSocial(ctx context.Context, a sdk.Account, r sdk.Runtime, req sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), req) != nil || len(req.Buttons) != 0 {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if utf8.RuneCountInString(req.Text) > maxTextRunes {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if req.Kind != sdk.SocialPostText && (c.stager == nil || media == nil) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if req.Kind == sdk.SocialPostMedia && (len(req.Media) < 1 || len(req.Media) > maxImages) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.SocialPublishResult{}, e
	}
	staged := make([]StagedMedia, 0, len(req.Media))
	for _, ref := range req.Media {
		item, se := c.stageReleased(ctx, a, media, ref)
		if se != nil {
			return sdk.SocialPublishResult{}, se
		}
		staged = append(staged, item)
	}
	var result sdk.SocialPublishResult
	e = c.useSecret(ctx, r, a.SecretReference, validToken, func(token []byte) error {
		creationID, ce := c.createPostContainer(ctx, token, cfg.ThreadsUserID, req, staged)
		if ce != nil {
			return ce
		}
		if ce = c.awaitContainer(ctx, token, creationID); ce != nil {
			return ce
		}
		raw, ce := c.call(ctx, token, "POST", "/"+apiVersion+"/"+cfg.ThreadsUserID+"/threads_publish", []Param{{Name: "creation_id", Value: creationID}}, true)
		if ce != nil {
			return ce
		}
		var out struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &out) != nil || !digits(out.ID, 5, 64) {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: "threads:" + cfg.ThreadsUserID + ":" + out.ID, Status: sdk.SocialRemotePublished, ObservedAt: c.now().UTC()}
		return result.Validate()
	})
	return result, e
}
func (c *Connector) createPostContainer(ctx context.Context, token []byte, userID string, req sdk.SocialPublishRequest, staged []StagedMedia) (string, error) {
	params := []Param{}
	switch req.Kind {
	case sdk.SocialPostText:
		params = []Param{{Name: "media_type", Value: "TEXT"}, {Name: "text", Value: req.Text}}
	case sdk.SocialPostMedia:
		if len(staged) == 1 {
			params = []Param{{Name: "media_type", Value: "IMAGE"}, {Name: "image_url", Value: staged[0].URL}, {Name: "text", Value: req.Text}}
		} else {
			children := make([]string, 0, len(staged))
			for _, m := range staged {
				id, e := c.createContainer(ctx, token, userID, []Param{{Name: "media_type", Value: "IMAGE"}, {Name: "image_url", Value: m.URL}, {Name: "is_carousel_item", Value: "true"}})
				if e != nil {
					return "", e
				}
				if e = c.awaitContainer(ctx, token, id); e != nil {
					return "", e
				}
				children = append(children, id)
			}
			params = []Param{{Name: "media_type", Value: "CAROUSEL"}, {Name: "children", Value: strings.Join(children, ",")}, {Name: "text", Value: req.Text}}
		}
	case sdk.SocialPostVideo:
		params = []Param{{Name: "media_type", Value: "VIDEO"}, {Name: "video_url", Value: staged[0].URL}, {Name: "text", Value: req.Text}}
	default:
		return "", sdk.ErrInvalidSocialRequest
	}
	return c.createContainer(ctx, token, userID, params)
}
func (c *Connector) stageReleased(ctx context.Context, a sdk.Account, media sdk.MediaAccessor, ref sdk.SocialMediaRef) (StagedMedia, error) {
	reader, desc, e := media.OpenReleased(ctx, a, ref)
	if e != nil {
		return StagedMedia{}, e
	}
	if reader == nil {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	defer reader.Close()
	if desc.Validate() != nil || desc.SizeBytes < 1 {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	if ref.Kind == sdk.SocialMediaImage {
		if (desc.MediaType != "image/jpeg" && desc.MediaType != "image/png") || desc.SizeBytes > maxImageBytes {
			return StagedMedia{}, sdk.ErrInvalidSocialRequest
		}
	} else if ref.Kind == sdk.SocialMediaVideo {
		if desc.MediaType != "video/mp4" || desc.SizeBytes > maxVideoBytes {
			return StagedMedia{}, sdk.ErrInvalidSocialRequest
		}
	} else {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	st, e := c.stager.Stage(ctx, a, ref, desc, io.LimitReader(reader, desc.SizeBytes))
	if e != nil {
		return StagedMedia{}, e
	}
	now := c.now().UTC()
	if !validStagedURL(st.URL) || !st.ExpiresAt.After(now.Add(2*time.Minute)) || st.ExpiresAt.After(now.Add(24*time.Hour)) {
		return StagedMedia{}, sdk.ErrInvalidSocialRequest
	}
	return st, nil
}
func (c *Connector) createContainer(ctx context.Context, token []byte, userID string, params []Param) (string, error) {
	raw, e := c.call(ctx, token, "POST", "/"+apiVersion+"/"+userID+"/threads", params, true)
	if e != nil {
		return "", e
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
		raw, e := c.call(ctx, token, "GET", "/"+apiVersion+"/"+id, []Param{{Name: "fields", Value: "status"}}, false)
		if e != nil {
			return e
		}
		var out struct {
			Status string `json:"status"`
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
			if e = c.wait(ctx, time.Second); e != nil {
				return e
			}
		}
	}
	return newRemote(sdk.ErrorTransient, "media_processing", "", 0)
}
func (c *Connector) ReadSocialPublicationStatus(ctx context.Context, a sdk.Account, r sdk.Runtime, remoteID string) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	user, mediaID, ok := parseRemoteID(remoteID)
	if !ok {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return sdk.SocialPublishResult{}, e
	}
	if user != cfg.ThreadsUserID {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialPublishResult
	e = c.useSecret(ctx, r, a.SecretReference, validToken, func(token []byte) error {
		raw, ce := c.call(ctx, token, "GET", "/"+apiVersion+"/"+mediaID, []Param{{Name: "fields", Value: "id,media_type"}}, false)
		if ce != nil {
			return ce
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
	return result, e
}
func parseRemoteID(v string) (string, string, bool) {
	if !strings.HasPrefix(v, "threads:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(v, "threads:")
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

var _ sdk.SocialPublisher = (*Connector)(nil)
var _ sdk.SocialPublicationStatusReader = (*Connector)(nil)
