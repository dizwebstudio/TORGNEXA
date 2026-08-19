package connectors

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidSocialRequest = errors.New("connectors: invalid social request")

var socialUploadIDPattern = regexp.MustCompile(`^upl_[0-9a-f]{32}$`)
var socialButtonURLPattern = regexp.MustCompile(`^https://[^\s]+$`)

type SocialPostKind string

const (
	SocialPostText  SocialPostKind = "text"
	SocialPostMedia SocialPostKind = "media"
	SocialPostVideo SocialPostKind = "video"
)

func (kind SocialPostKind) Valid() bool {
	return kind == SocialPostText || kind == SocialPostMedia || kind == SocialPostVideo
}

type SocialMediaKind string

const (
	SocialMediaImage SocialMediaKind = "image"
	SocialMediaVideo SocialMediaKind = "video"
)

func (kind SocialMediaKind) Valid() bool { return kind == SocialMediaImage || kind == SocialMediaVideo }

// SocialMediaRef is a host-owned released-upload identity. It deliberately has
// no object key, filesystem path, public URL or credential-bearing field.
// MediaAccessor must revalidate release state immediately before every read.
type SocialMediaRef struct {
	UploadID string          `json:"upload_id"`
	Kind     SocialMediaKind `json:"kind"`
	AltText  string          `json:"alt_text,omitempty"`
}

func (ref SocialMediaRef) Validate() error {
	if !socialUploadIDPattern.MatchString(ref.UploadID) || !ref.Kind.Valid() || !validSocialText(ref.AltText, 1000, true) {
		return ErrInvalidSocialRequest
	}
	return nil
}

type MediaDescriptor struct {
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

func (descriptor MediaDescriptor) Validate() error {
	if descriptor.SizeBytes < 0 || descriptor.SizeBytes > 10<<30 || len(descriptor.SHA256) != 64 || !validSocialMediaType(descriptor.MediaType) {
		return ErrInvalidSocialRequest
	}
	for _, r := range descriptor.SHA256 {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return ErrInvalidSocialRequest
		}
	}
	return nil
}

// MediaAccessor is supplied by the host to a social connector call. It must
// resolve only a current Task-088 released object for the authenticated tenant.
type MediaAccessor interface {
	OpenReleased(context.Context, Account, SocialMediaRef) (io.ReadCloser, MediaDescriptor, error)
}

type SocialButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (button SocialButton) Validate() error {
	if !validSocialText(button.Text, 64, false) || len(button.URL) > 2048 || !socialButtonURLPattern.MatchString(button.URL) || strings.ContainsAny(button.URL, "\\\"<>[]{}") {
		return ErrInvalidSocialRequest
	}
	return nil
}

type SocialPublishRequest struct {
	PublicationID string           `json:"publication_id"`
	Kind          SocialPostKind   `json:"kind"`
	Text          string           `json:"text,omitempty"`
	Media         []SocialMediaRef `json:"media,omitempty"`
	Buttons       []SocialButton   `json:"buttons,omitempty"`
}

func (request SocialPublishRequest) Validate() error {
	if !sortableIDPattern.MatchString(request.PublicationID) || !request.Kind.Valid() || !validSocialText(request.Text, 50000, true) || len(request.Media) > 20 || len(request.Buttons) > 8 {
		return ErrInvalidSocialRequest
	}
	for _, button := range request.Buttons {
		if button.Validate() != nil {
			return ErrInvalidSocialRequest
		}
	}
	seen := make(map[string]struct{}, len(request.Media))
	for _, media := range request.Media {
		if media.Validate() != nil {
			return ErrInvalidSocialRequest
		}
		if _, duplicate := seen[media.UploadID]; duplicate {
			return ErrInvalidSocialRequest
		}
		seen[media.UploadID] = struct{}{}
	}
	switch request.Kind {
	case SocialPostText:
		if request.Text == "" || len(request.Media) != 0 {
			return ErrInvalidSocialRequest
		}
	case SocialPostMedia:
		if len(request.Media) < 1 {
			return ErrInvalidSocialRequest
		}
		for _, media := range request.Media {
			if media.Kind != SocialMediaImage {
				return ErrInvalidSocialRequest
			}
		}
	case SocialPostVideo:
		if len(request.Media) != 1 || request.Media[0].Kind != SocialMediaVideo {
			return ErrInvalidSocialRequest
		}
	}
	return nil
}

func RequiredSocialPublishCapability(kind SocialPostKind) (Capability, error) {
	switch kind {
	case SocialPostText:
		return Capability("social.post.text"), nil
	case SocialPostMedia:
		return Capability("social.post.media"), nil
	case SocialPostVideo:
		return Capability("social.post.video"), nil
	default:
		return "", ErrInvalidSocialRequest
	}
}

// ValidateSocialPublish performs the mandatory provider-neutral capability
// check before any remote side effect.
func ValidateSocialPublish(manifest Manifest, request SocialPublishRequest) error {
	if err := manifest.Validate(); err != nil || manifest.Family != FamilySocial || request.Validate() != nil {
		return ErrInvalidSocialRequest
	}
	capability, err := RequiredSocialPublishCapability(request.Kind)
	if err != nil {
		return err
	}
	if err := RequireCapability(manifest, capability); err != nil {
		return err
	}
	if len(request.Buttons) > 0 {
		return RequireCapability(manifest, Capability("social.post.buttons"))
	}
	return nil
}

type SocialRemoteStatus string

const (
	SocialRemoteProcessing SocialRemoteStatus = "processing"
	SocialRemotePublished  SocialRemoteStatus = "published"
	SocialRemoteFailed     SocialRemoteStatus = "failed"
)

func (status SocialRemoteStatus) Valid() bool {
	return status == SocialRemoteProcessing || status == SocialRemotePublished || status == SocialRemoteFailed
}

type SocialPublishResult struct {
	RemotePublicationID string             `json:"remote_publication_id"`
	Status              SocialRemoteStatus `json:"status"`
	ReasonCode          string             `json:"reason_code,omitempty"`
	ObservedAt          time.Time          `json:"observed_at"`
}

func (result SocialPublishResult) Validate() error {
	if !validRemoteReadID(result.RemotePublicationID) || !result.Status.Valid() || result.ObservedAt.IsZero() || result.ObservedAt.Location() != time.UTC {
		return ErrInvalidSocialRequest
	}
	if result.Status == SocialRemoteFailed {
		if !safeCodePattern.MatchString(result.ReasonCode) {
			return ErrInvalidSocialRequest
		}
	} else if result.ReasonCode != "" {
		return ErrInvalidSocialRequest
	}
	return nil
}

// SocialPublisher is the additive SDK-v1 operation surface for social post
// capabilities. Scheduling remains canonical TORGNEXA state; this call means
// "publish now" after the scheduler has made the publication READY.
type SocialPublisher interface {
	PublishSocial(context.Context, Account, Runtime, SocialPublishRequest, MediaAccessor) (SocialPublishResult, error)
}

type SocialEditRequest struct {
	RemotePublicationID string           `json:"remote_publication_id"`
	Kind                SocialPostKind   `json:"kind"`
	Text                string           `json:"text,omitempty"`
	Media               []SocialMediaRef `json:"media,omitempty"`
	Buttons             []SocialButton   `json:"buttons,omitempty"`
}

func (request SocialEditRequest) Validate() error {
	if !validRemoteReadID(request.RemotePublicationID) {
		return ErrInvalidSocialRequest
	}
	probe := SocialPublishRequest{PublicationID: "01890f4d-1e10-7cc0-9c4a-000000000001", Kind: request.Kind, Text: request.Text, Media: request.Media, Buttons: request.Buttons}
	return probe.Validate()
}

func ValidateSocialEdit(manifest Manifest, request SocialEditRequest) error {
	if err := manifest.Validate(); err != nil || manifest.Family != FamilySocial || request.Validate() != nil {
		return ErrInvalidSocialRequest
	}
	if err := RequireCapability(manifest, Capability("social.post.edit")); err != nil {
		return err
	}
	capability, err := RequiredSocialPublishCapability(request.Kind)
	if err != nil {
		return err
	}
	if err := RequireCapability(manifest, capability); err != nil {
		return err
	}
	if len(request.Buttons) > 0 {
		return RequireCapability(manifest, Capability("social.post.buttons"))
	}
	return nil
}

type SocialDeleteResult struct {
	RemotePublicationID string    `json:"remote_publication_id"`
	Deleted             bool      `json:"deleted"`
	ObservedAt          time.Time `json:"observed_at"`
}

func (result SocialDeleteResult) Validate() error {
	if !validRemoteReadID(result.RemotePublicationID) || !result.Deleted || result.ObservedAt.IsZero() || result.ObservedAt.Location() != time.UTC {
		return ErrInvalidSocialRequest
	}
	return nil
}

type SocialEditor interface {
	EditSocial(context.Context, Account, Runtime, SocialEditRequest, MediaAccessor) (SocialPublishResult, error)
}

type SocialDeleter interface {
	DeleteSocial(context.Context, Account, Runtime, string) (SocialDeleteResult, error)
}

// SocialPublicationStatusReader supports connectors whose remote media publish
// operation completes asynchronously.
type SocialPublicationStatusReader interface {
	ReadSocialPublicationStatus(context.Context, Account, Runtime, string) (SocialPublishResult, error)
}

func validSocialText(value string, max int, optional bool) bool {
	if value == "" {
		return optional
	}
	if value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > max {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			if r == '\n' || r == '\r' || r == '\t' {
				continue
			}
			return false
		}
	}
	return true
}

func validSocialMediaType(value string) bool {
	if len(value) < 3 || len(value) > 255 || strings.Count(value, "/") != 1 {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
