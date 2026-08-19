package rutube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	maxVideoBytes       = int64(10 << 30)
	minVideoBytes       = int64(16 << 10)
	maxTitleRunes       = 200
	maxDescriptionRunes = 5000
)

func (c *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, req sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || media == nil || runtime == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	if sdk.ValidateSocialPublish(Manifest(), req) != nil || req.Kind != sdk.SocialPostVideo || len(req.Buttons) != 0 {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	title, description := videoMetadata(req.PublicationID, req.Text)
	if !safeText(title, 1, maxTitleRunes) || (description != "" && !safeText(description, 1, maxDescriptionRunes)) {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	reader, descriptor, err := media.OpenReleased(ctx, account, req.Media[0])
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	if reader == nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	defer reader.Close()
	if descriptor.Validate() != nil || descriptor.MediaType != "video/mp4" || descriptor.SizeBytes < minVideoBytes || descriptor.SizeBytes > maxVideoBytes {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}

	var result sdk.SocialPublishResult
	err = c.withCredential(ctx, runtime, account.SecretReference, func(secret []byte) error {
		ticket, callErr := c.transport.CreateUpload(ctx, secret, CreateUploadRequest{
			ChannelID: cfg.ChannelID, ContractID: cfg.ContractID, ExternalID: req.PublicationID,
			Title: title, Description: description, MediaType: descriptor.MediaType,
			SizeBytes: descriptor.SizeBytes, ContentSHA256: descriptor.SHA256,
		})
		if callErr != nil {
			return normalizeFailure(callErr, true)
		}
		if !validUploadSession(ticket, descriptor.SizeBytes, c.now().UTC()) {
			return ErrInvalidResponse
		}
		if callErr = c.transport.Upload(ctx, secret, UploadRequest{
			SessionID: ticket.ID, ContractID: cfg.ContractID, FileName: req.Media[0].UploadID + ".mp4",
			MediaType: descriptor.MediaType, SizeBytes: descriptor.SizeBytes, ContentSHA256: descriptor.SHA256,
			Body: io.LimitReader(reader, descriptor.SizeBytes),
		}); callErr != nil {
			return normalizeUploadFailure(callErr)
		}
		record, callErr := c.transport.CommitUpload(ctx, secret, CommitUploadRequest{SessionID: ticket.ID, ChannelID: cfg.ChannelID, ContractID: cfg.ContractID, ExternalID: req.PublicationID})
		if callErr != nil {
			return normalizeFailure(callErr, true)
		}
		mapped, mapErr := c.mapRecord(cfg, record)
		if mapErr != nil {
			return mapErr
		}
		result = mapped
		return nil
	})
	return result, err
}

func normalizeUploadFailure(err error) error {
	var pf *PartnerFailure
	if errors.As(err, &pf) && pf != nil && validFailure(pf) {
		switch pf.Kind {
		case FailureRateLimited, FailureQuotaExceeded, FailureUnauthorized, FailureForbidden, FailureInvalidRequest, FailureRejected:
			return normalizeFailure(err, false)
		case FailureUnknownWrite, FailureUnavailable, FailureConflict:
			// A session exists but upload completion is not safely known. The
			// publication must reconcile instead of blindly creating a second session.
			return newRemote(sdk.ErrorConflict, "upload_outcome_unknown", pf.RequestID, 0)
		}
	}
	return newRemote(sdk.ErrorConflict, "upload_outcome_unknown", "", 0)
}

func (c *Connector) ReadSocialPublicationStatus(ctx context.Context, account sdk.Account, runtime sdk.Runtime, remoteID string) (sdk.SocialPublishResult, error) {
	channelID, videoID, ok := parseRemoteID(remoteID)
	if !ok || c == nil || c.transport == nil || runtime == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	if channelID != cfg.ChannelID {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	var result sdk.SocialPublishResult
	err = c.withCredential(ctx, runtime, account.SecretReference, func(secret []byte) error {
		record, callErr := c.transport.ReadVideo(ctx, secret, VideoStatusRequest{VideoID: videoID, ChannelID: cfg.ChannelID, ContractID: cfg.ContractID})
		if callErr != nil {
			return normalizeFailure(callErr, false)
		}
		mapped, mapErr := c.mapRecord(cfg, record)
		if mapErr != nil {
			return mapErr
		}
		if mapped.RemotePublicationID != remoteID {
			return ErrInvalidResponse
		}
		result = mapped
		return nil
	})
	return result, err
}

func (c *Connector) mapRecord(cfg Configuration, record VideoRecord) (sdk.SocialPublishResult, error) {
	if record.ChannelID != cfg.ChannelID || !safeID(record.VideoID, 1, 256) {
		return sdk.SocialPublishResult{}, ErrInvalidResponse
	}
	out := sdk.SocialPublishResult{RemotePublicationID: "rutube:" + cfg.ChannelID + ":" + record.VideoID, ObservedAt: c.now().UTC()}
	switch record.State {
	case VideoStateProcessing:
		if record.ReasonCode != "" {
			return sdk.SocialPublishResult{}, ErrInvalidResponse
		}
		out.Status = sdk.SocialRemoteProcessing
	case VideoStatePublished:
		if record.ReasonCode != "" {
			return sdk.SocialPublishResult{}, ErrInvalidResponse
		}
		out.Status = sdk.SocialRemotePublished
	case VideoStateFailed:
		if !safeOptionalCode(record.ReasonCode) || record.ReasonCode == "" {
			return sdk.SocialPublishResult{}, ErrInvalidResponse
		}
		out.Status, out.ReasonCode = sdk.SocialRemoteFailed, record.ReasonCode
	default:
		return sdk.SocialPublishResult{}, ErrInvalidResponse
	}
	if out.Validate() != nil {
		return sdk.SocialPublishResult{}, ErrInvalidResponse
	}
	return out, nil
}

func validUploadSession(s UploadSession, size int64, now time.Time) bool {
	if !safeID(s.ID, 1, 256) || s.MaxBytes < minVideoBytes || s.MaxBytes > maxVideoBytes || size > s.MaxBytes || s.ExpiresAt.IsZero() || s.ExpiresAt.Location() != time.UTC {
		return false
	}
	return s.ExpiresAt.After(now.Add(30 * time.Second))
}

func videoMetadata(publicationID, text string) (string, string) {
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		suffix := publicationID
		if len(suffix) > 12 {
			suffix = suffix[len(suffix)-12:]
		}
		return "TORGNEXA video " + suffix, ""
	}
	title := cleaned
	if i := strings.IndexByte(cleaned, '\n'); i >= 0 {
		title = strings.TrimSpace(cleaned[:i])
	}
	if title == "" {
		title = "TORGNEXA video"
	}
	title = truncateRunes(title, maxTitleRunes)
	description := truncateRunes(cleaned, maxDescriptionRunes)
	return title, description
}

func truncateRunes(v string, max int) string {
	if utf8.RuneCountInString(v) <= max {
		return v
	}
	out := make([]rune, 0, max)
	for _, r := range v {
		if len(out) == max {
			break
		}
		out = append(out, r)
	}
	return strings.TrimSpace(string(out))
}

func parseRemoteID(v string) (string, string, bool) {
	if !strings.HasPrefix(v, "rutube:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(v, "rutube:")
	channel, video, ok := strings.Cut(rest, ":")
	if !ok || !safeID(channel, 1, 128) || !safeID(video, 1, 256) {
		return "", "", false
	}
	return channel, video, true
}

func videoFileName(uploadID string) string { return fmt.Sprintf("%s.mp4", uploadID) }
