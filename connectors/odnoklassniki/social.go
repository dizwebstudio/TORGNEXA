package ok

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
	maxOKImages    = 20
	maxOKImageSize = 32 << 20 // adapter safety ceiling; provider may reject lower limits dynamically
	minOKVideoSize = 16 << 10
	maxOKVideoSize = 1 << 30
)

type mediaAttachment struct {
	Media []attachmentItem `json:"media"`
}
type attachmentItem struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	List []attachmentRef `json:"list,omitempty"`
}
type attachmentRef struct {
	ID string `json:"id"`
}

func (c *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, req sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), req) != nil || len(req.Buttons) != 0 {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if req.Kind == sdk.SocialPostMedia && (media == nil || len(req.Media) > maxOKImages) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if req.Kind == sdk.SocialPostVideo && media == nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	var result sdk.SocialPublishResult
	err = c.withCredentials(ctx, runtime, account, cfg, func(token, appSecret []byte) error {
		attachment := mediaAttachment{}
		if req.Text != "" {
			attachment.Media = append(attachment.Media, attachmentItem{Type: "text", Text: req.Text})
		}
		switch req.Kind {
		case sdk.SocialPostText:
		case sdk.SocialPostMedia:
			tokens, e := c.uploadPhotos(ctx, account, media, req.Media, token, appSecret, cfg)
			if e != nil {
				return e
			}
			refs := make([]attachmentRef, 0, len(tokens))
			for _, t := range tokens {
				refs = append(refs, attachmentRef{ID: t})
			}
			attachment.Media = append(attachment.Media, attachmentItem{Type: "photo", List: refs})
		case sdk.SocialPostVideo:
			vid, e := c.uploadVideo(ctx, account, media, req.Media[0], token, appSecret, cfg)
			if e != nil {
				return e
			}
			attachment.Media = append(attachment.Media, attachmentItem{Type: "movie", List: []attachmentRef{{ID: vid}}})
		default:
			return sdk.ErrInvalidSocialRequest
		}
		encoded, e := json.Marshal(attachment)
		if e != nil || len(encoded) == 0 || len(encoded) > 1<<20 {
			return sdk.ErrInvalidSocialRequest
		}
		raw, e := c.call(ctx, token, appSecret, cfg.ApplicationKey, "POST", "mediatopic.post", []Param{{Name: "type", Value: "GROUP_THEME"}, {Name: "gid", Value: cfg.GroupID}, {Name: "attachment", Value: string(encoded)}, {Name: "onBehalfOfGroup", Value: "true"}}, true)
		if e != nil {
			return e
		}
		var topicID string
		if json.Unmarshal(raw, &topicID) != nil || !safeRemoteID(topicID) {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: "ok:" + cfg.GroupID + ":" + topicID, Status: sdk.SocialRemotePublished, ObservedAt: c.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func (c *Connector) uploadPhotos(ctx context.Context, account sdk.Account, media sdk.MediaAccessor, refs []sdk.SocialMediaRef, token, appSecret []byte, cfg Configuration) ([]string, error) {
	files := make([]FilePart, 0, len(refs))
	closers := make([]io.Closer, 0, len(refs))
	sizes := make([]string, 0, len(refs))
	defer func() {
		for _, cl := range closers {
			_ = cl.Close()
		}
	}()
	for i, ref := range refs {
		r, d, err := media.OpenReleased(ctx, account, ref)
		if err != nil {
			return nil, err
		}
		if r == nil {
			return nil, sdk.ErrInvalidSocialRequest
		}
		if d.Validate() != nil || ref.Kind != sdk.SocialMediaImage || d.SizeBytes < 1 || d.SizeBytes > maxOKImageSize || (d.MediaType != "image/jpeg" && d.MediaType != "image/png") {
			r.Close()
			return nil, sdk.ErrInvalidSocialRequest
		}
		closers = append(closers, r)
		ext := ".jpg"
		if d.MediaType == "image/png" {
			ext = ".png"
		}
		files = append(files, FilePart{FieldName: fmt.Sprintf("pic%d", i+1), FileName: ref.UploadID + ext, MediaType: d.MediaType, SizeBytes: d.SizeBytes, SHA256: d.SHA256, Body: io.LimitReader(r, d.SizeBytes)})
		sizes = append(sizes, strconv.FormatInt(d.SizeBytes, 10))
	}
	raw, err := c.call(ctx, token, appSecret, cfg.ApplicationKey, "POST", "photosV2.getUploadUrl", []Param{{Name: "gid", Value: cfg.GroupID}, {Name: "count", Value: strconv.Itoa(len(files))}, {Name: "sizes", Value: strings.Join(sizes, ",")}}, true)
	if err != nil {
		return nil, err
	}
	var ticket struct {
		UploadURL string   `json:"upload_url"`
		PhotoIDs  []string `json:"photo_ids"`
	}
	if json.Unmarshal(raw, &ticket) != nil || !validUploadURL(ticket.UploadURL) || len(ticket.PhotoIDs) != len(files) {
		return nil, ErrInvalidResponse
	}
	for _, id := range ticket.PhotoIDs {
		if !safeRemoteID(id) {
			return nil, ErrInvalidResponse
		}
	}
	resp, err := c.transport.Upload(ctx, UploadRequest{URL: ticket.UploadURL, Files: files})
	if err != nil {
		return nil, newRemote(sdk.ErrorUnavailable, "media_upload_unavailable", "", 0)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || len(resp.Body) > maxBodyBytes {
		return nil, normalizeHTTP(resp, false)
	}
	var uploaded struct {
		Photos map[string]struct {
			Token string `json:"token"`
		} `json:"photos"`
	}
	if json.Unmarshal(resp.Body, &uploaded) != nil || len(uploaded.Photos) < len(ticket.PhotoIDs) {
		return nil, ErrInvalidResponse
	}
	out := make([]string, 0, len(ticket.PhotoIDs))
	for _, id := range ticket.PhotoIDs {
		v, ok := uploaded.Photos[id]
		if !ok || !safeOpaque(v.Token, 4096) {
			return nil, ErrInvalidResponse
		}
		out = append(out, v.Token)
	}
	return out, nil
}

func (c *Connector) uploadVideo(ctx context.Context, account sdk.Account, media sdk.MediaAccessor, ref sdk.SocialMediaRef, token, appSecret []byte, cfg Configuration) (string, error) {
	r, d, err := media.OpenReleased(ctx, account, ref)
	if err != nil {
		return "", err
	}
	if r == nil {
		return "", sdk.ErrInvalidSocialRequest
	}
	defer r.Close()
	if d.Validate() != nil || ref.Kind != sdk.SocialMediaVideo || d.MediaType != "video/mp4" || d.SizeBytes < minOKVideoSize || d.SizeBytes > maxOKVideoSize {
		return "", sdk.ErrInvalidSocialRequest
	}
	raw, err := c.call(ctx, token, appSecret, cfg.ApplicationKey, "POST", "video.getUploadUrl", []Param{{Name: "gid", Value: cfg.GroupID}, {Name: "file_name", Value: ref.UploadID + ".mp4"}, {Name: "file_size", Value: strconv.FormatInt(d.SizeBytes, 10)}, {Name: "post_form", Value: "true"}}, true)
	if err != nil {
		return "", err
	}
	var ticket struct {
		UploadURL string          `json:"upload_url"`
		VideoID   json.RawMessage `json:"video_id"`
	}
	if json.Unmarshal(raw, &ticket) != nil || !validUploadURL(ticket.UploadURL) {
		return "", ErrInvalidResponse
	}
	vid := rawID(ticket.VideoID)
	if !safeRemoteID(vid) {
		return "", ErrInvalidResponse
	}
	resp, err := c.transport.Upload(ctx, UploadRequest{URL: ticket.UploadURL, Files: []FilePart{{FieldName: "data", FileName: ref.UploadID + ".mp4", MediaType: d.MediaType, SizeBytes: d.SizeBytes, SHA256: d.SHA256, Body: io.LimitReader(r, d.SizeBytes)}}})
	if err != nil {
		return "", newRemote(sdk.ErrorUnavailable, "media_upload_unavailable", "", 0)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", normalizeHTTP(resp, false)
	}
	_, err = c.call(ctx, token, appSecret, cfg.ApplicationKey, "POST", "video.update", []Param{{Name: "vid", Value: vid}, {Name: "publish", Value: "true"}}, true)
	if err != nil {
		return "", err
	}
	return vid, nil
}

func (c *Connector) ReadSocialPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialPublishResult, error) {
	groupID, topicID, ok := parseRemoteID(remoteID)
	if !ok {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	if groupID != cfg.GroupID {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialPublishResult
	err = c.withCredentials(ctx, runtime, account, cfg, func(token, appSecret []byte) error {
		raw, e := c.call(ctx, token, appSecret, cfg.ApplicationKey, "GET", "mediatopic.getByIds", []Param{{Name: "topic_ids", Value: topicID}}, false)
		if e != nil {
			return e
		}
		if !containsTopicID(raw, topicID) {
			return ErrInvalidResponse
		}
		result = sdk.SocialPublishResult{RemotePublicationID: remoteID, Status: sdk.SocialRemotePublished, ObservedAt: c.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func parseRemoteID(v string) (string, string, bool) {
	if !strings.HasPrefix(v, "ok:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(v, "ok:")
	g, t, ok := strings.Cut(rest, ":")
	if !ok || !digits(g, 5, 64) || !safeRemoteID(t) {
		return "", "", false
	}
	return g, t, true
}
func safeRemoteID(v string) bool {
	if len(v) < 1 || len(v) > 256 || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
func safeOpaque(v string, max int) bool {
	if len(v) < 1 || len(v) > max || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func validUploadURL(v string) bool {
	if len(v) < 12 || len(v) > 8192 || v != strings.TrimSpace(v) || !strings.HasPrefix(v, "https://") || strings.ContainsAny(v, "\\#") {
		return false
	}
	rest := strings.TrimPrefix(v, "https://")
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return false
	}
	auth := rest[:slash]
	if strings.ContainsAny(auth, "@%[]") || strings.Contains(auth, "..") {
		return false
	}
	host := auth
	if colon := strings.LastIndexByte(auth, ':'); colon >= 0 {
		if auth[colon+1:] != "443" || strings.Contains(auth[:colon], ":") {
			return false
		}
		host = auth[:colon]
	}
	host = strings.ToLower(host)
	return host == "ok.ru" || strings.HasSuffix(host, ".ok.ru") || host == "mycdn.me" || strings.HasSuffix(host, ".mycdn.me") || host == "odnoklassniki.ru" || strings.HasSuffix(host, ".odnoklassniki.ru")
}
func containsTopicID(raw []byte, id string) bool {
	var any interface{}
	if json.Unmarshal(raw, &any) != nil {
		return false
	}
	return walkID(any, id)
}
func walkID(v interface{}, id string) bool {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, vv := range x {
			if (k == "id" || k == "topic_id" || k == "topicId") && fmt.Sprint(vv) == id {
				return true
			}
			if walkID(vv, id) {
				return true
			}
		}
	case []interface{}:
		for _, vv := range x {
			if walkID(vv, id) {
				return true
			}
		}
	}
	return false
}
