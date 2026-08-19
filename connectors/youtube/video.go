package youtube

import (
	"context"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	maxVideoBytes       = int64(10 << 30) // frozen SDK-v1 MediaDescriptor limit
	minVideoBytes       = int64(1)
	maxTitleRunes       = 100
	maxDescriptionBytes = 5000
	chunkQuantum        = 256 << 10
	maxResumeProbes     = 5
)

func (c *Connector) PublishSocial(ctx context.Context, account sdk.Account, runtime sdk.Runtime, req sdk.SocialPublishRequest, media sdk.MediaAccessor) (sdk.SocialPublishResult, error) {
	if c == nil || c.transport == nil || media == nil || runtime == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.ValidateSocialPublish(Manifest(), req) != nil || req.Kind != sdk.SocialPostVideo || len(req.Buttons) != 0 {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialPublishResult{}, err
	}
	title, description, ok := videoMetadata(req.PublicationID, req.Text)
	if !ok {
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
	if descriptor.Validate() != nil || !strings.HasPrefix(descriptor.MediaType, "video/") || descriptor.SizeBytes < minVideoBytes || descriptor.SizeBytes > maxVideoBytes {
		return sdk.SocialPublishResult{}, sdk.ErrInvalidSocialRequest
	}

	metadata := UploadMetadata{
		ExternalID: req.PublicationID, Title: title, Description: description, CategoryID: cfg.CategoryID,
		PrivacyStatus: cfg.PrivacyStatus, NotifySubscribers: cfg.NotifySubscribers,
		SelfDeclaredMadeForKids: cfg.SelfDeclaredMadeForKids, ContainsSyntheticMedia: cfg.ContainsSyntheticMedia,
		MediaType: descriptor.MediaType, SizeBytes: descriptor.SizeBytes, ContentSHA256: descriptor.SHA256,
	}
	var result sdk.SocialPublishResult
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		session, callErr := c.transport.StartResumableUpload(ctx, token, cfg.ChannelID, metadata)
		if callErr != nil {
			return normalizeStartFailure(callErr)
		}
		if !safeID(session.ID, 8, 256) {
			return ErrInvalidResponse
		}
		record, uploadErr := c.uploadResumable(ctx, token, session.ID, reader, descriptor)
		if uploadErr != nil {
			return uploadErr
		}
		mapped, mapErr := c.mapVideo(cfg, record)
		if mapErr != nil {
			return mapErr
		}
		result = mapped
		return nil
	})
	return result, err
}

func normalizeStartFailure(err error) error {
	var gf *GoogleFailure
	if errors.As(err, &gf) && gf != nil && validFailure(gf) {
		switch gf.Kind {
		case FailureInvalidRequest, FailureUnauthorized, FailureForbidden, FailureRateLimited, FailureQuotaExceeded:
			return normalizeFailure(err, false)
		case FailureNotFound:
			return normalizeFailure(err, false)
		case FailureExpiredSession:
			return newRemote(sdk.ErrorConflict, "upload_session_expired", gf.RequestID, 0)
		}
	}
	return newRemote(sdk.ErrorConflict, "upload_session_outcome_unknown", requestIDFromFailure(err), 0)
}

func requestIDFromFailure(err error) string {
	var gf *GoogleFailure
	if errors.As(err, &gf) && gf != nil && len(gf.RequestID) <= 256 {
		return gf.RequestID
	}
	return ""
}

func (c *Connector) uploadResumable(ctx context.Context, token []byte, sessionID string, reader io.Reader, descriptor sdk.MediaDescriptor) (VideoRecord, error) {
	chunkSize := c.chunkSize
	if chunkSize < chunkQuantum || chunkSize%chunkQuantum != 0 || chunkSize > 64<<20 {
		return VideoRecord{}, ErrInvalidResponse
	}
	buf := make([]byte, chunkSize)
	offset := int64(0)
	for offset < descriptor.SizeBytes {
		remaining := descriptor.SizeBytes - offset
		want := chunkSize
		if int64(want) > remaining {
			want = int(remaining)
		}
		n, readErr := io.ReadFull(reader, buf[:want])
		if readErr != nil && !(readErr == io.ErrUnexpectedEOF && n == want) {
			return VideoRecord{}, sdk.ErrInvalidSocialRequest
		}
		if n != want {
			return VideoRecord{}, sdk.ErrInvalidSocialRequest
		}
		chunk := append([]byte(nil), buf[:n]...)
		chunkStart := offset
		for {
			progress, callErr := c.transport.UploadChunk(ctx, token, UploadChunkRequest{SessionID: sessionID, Offset: offset, TotalBytes: descriptor.SizeBytes, MediaType: descriptor.MediaType, Body: chunk[offset-chunkStart:]})
			if callErr != nil {
				confirmed, probeErr := c.resumeAfterFailure(ctx, token, sessionID, descriptor.SizeBytes, callErr)
				if probeErr != nil {
					return VideoRecord{}, probeErr
				}
				if confirmed.Complete {
					return confirmed.Video, nil
				}
				if confirmed.NextOffset < chunkStart || confirmed.NextOffset > chunkStart+int64(len(chunk)) || confirmed.NextOffset > descriptor.SizeBytes {
					return VideoRecord{}, ErrInvalidResponse
				}
				offset = confirmed.NextOffset
				if offset == chunkStart+int64(len(chunk)) {
					break
				}
				if offset < descriptor.SizeBytes && offset%chunkQuantum != 0 {
					return VideoRecord{}, ErrInvalidResponse
				}
				continue
			}
			if !validProgress(progress, offset, int64(len(chunk))-(offset-chunkStart), descriptor.SizeBytes) {
				return VideoRecord{}, ErrInvalidResponse
			}
			if progress.Complete {
				if progress.NextOffset != descriptor.SizeBytes {
					return VideoRecord{}, ErrInvalidResponse
				}
				return progress.Video, nil
			}
			offset = progress.NextOffset
			if offset != chunkStart+int64(len(chunk)) {
				return VideoRecord{}, ErrInvalidResponse
			}
			break
		}
	}
	return VideoRecord{}, ErrInvalidResponse
}

func (c *Connector) resumeAfterFailure(ctx context.Context, token []byte, sessionID string, total int64, cause error) (UploadProgress, error) {
	var gf *GoogleFailure
	if errors.As(cause, &gf) && gf != nil && validFailure(gf) {
		switch gf.Kind {
		case FailureInvalidRequest, FailureUnauthorized, FailureForbidden, FailureRateLimited, FailureQuotaExceeded:
			return UploadProgress{}, normalizeFailure(cause, false)
		case FailureExpiredSession, FailureNotFound:
			return UploadProgress{}, newRemote(sdk.ErrorConflict, "upload_session_expired", gf.RequestID, 0)
		}
	}
	for attempt := 0; attempt < maxResumeProbes; attempt++ {
		progress, err := c.transport.ProbeResumableUpload(ctx, token, sessionID, total)
		if err == nil {
			if progress.NextOffset < 0 || progress.NextOffset > total || (progress.NextOffset < total && progress.NextOffset%chunkQuantum != 0) {
				return UploadProgress{}, ErrInvalidResponse
			}
			return progress, nil
		}
		var probeFailure *GoogleFailure
		if errors.As(err, &probeFailure) && probeFailure != nil && validFailure(probeFailure) {
			switch probeFailure.Kind {
			case FailureExpiredSession, FailureNotFound:
				return UploadProgress{}, newRemote(sdk.ErrorConflict, "upload_session_expired", probeFailure.RequestID, 0)
			case FailureUnauthorized, FailureForbidden, FailureQuotaExceeded, FailureRateLimited, FailureInvalidRequest:
				return UploadProgress{}, normalizeFailure(err, false)
			}
		}
	}
	return UploadProgress{}, newRemote(sdk.ErrorConflict, "upload_outcome_unknown", requestIDFromFailure(cause), 0)
}

func validProgress(progress UploadProgress, offset, sent, total int64) bool {
	if offset < 0 || sent < 1 || total < 1 || progress.NextOffset < offset || progress.NextOffset > offset+sent || progress.NextOffset > total {
		return false
	}
	if progress.Complete {
		return progress.NextOffset == total && progress.Video.VideoID != ""
	}
	return progress.NextOffset > offset && (progress.NextOffset == total || progress.NextOffset%chunkQuantum == 0)
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
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		record, callErr := c.transport.ReadVideo(ctx, token, videoID)
		if callErr != nil {
			return normalizeFailure(callErr, false)
		}
		mapped, mapErr := c.mapVideo(cfg, record)
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

func (c *Connector) mapVideo(cfg Configuration, record VideoRecord) (sdk.SocialPublishResult, error) {
	if record.ChannelID != cfg.ChannelID || !safeID(record.VideoID, 6, 64) {
		return sdk.SocialPublishResult{}, ErrInvalidResponse
	}
	out := sdk.SocialPublishResult{RemotePublicationID: "youtube:" + cfg.ChannelID + ":" + record.VideoID, ObservedAt: c.now().UTC()}
	failure := safeProviderReason(record.FailureReason, record.RejectionReason)
	switch record.UploadStatus {
	case "failed", "rejected", "deleted":
		if failure == "" {
			failure = "video_rejected"
		}
		out.Status, out.ReasonCode = sdk.SocialRemoteFailed, failure
	case "uploaded":
		switch record.ProcessingStatus {
		case "", "processing":
			out.Status = sdk.SocialRemoteProcessing
		case "succeeded":
			out.Status = sdk.SocialRemotePublished
		case "failed", "terminated":
			if failure == "" {
				failure = "processing_failed"
			}
			out.Status, out.ReasonCode = sdk.SocialRemoteFailed, failure
		default:
			return sdk.SocialPublishResult{}, ErrInvalidResponse
		}
	case "processed":
		if record.ProcessingStatus != "" && record.ProcessingStatus != "succeeded" {
			return sdk.SocialPublishResult{}, ErrInvalidResponse
		}
		out.Status = sdk.SocialRemotePublished
	default:
		return sdk.SocialPublishResult{}, ErrInvalidResponse
	}
	if out.Validate() != nil {
		return sdk.SocialPublishResult{}, ErrInvalidResponse
	}
	return out, nil
}

func safeProviderReason(failure, rejection string) string {
	reason := rejection
	if reason == "" {
		reason = failure
	}
	switch reason {
	case "codec":
		return "codec"
	case "conversion":
		return "conversion"
	case "emptyFile":
		return "empty_file"
	case "invalidFile":
		return "invalid_file"
	case "tooSmall":
		return "too_small"
	case "uploadAborted":
		return "upload_aborted"
	case "claim":
		return "claim"
	case "copyright":
		return "copyright"
	case "duplicate":
		return "duplicate"
	case "inappropriate":
		return "inappropriate"
	case "legal":
		return "legal"
	case "length":
		return "length"
	case "termsOfUse":
		return "terms_of_use"
	case "trademark":
		return "trademark"
	case "uploaderAccountClosed":
		return "uploader_account_closed"
	case "uploaderAccountSuspended":
		return "uploader_account_suspended"
	case "other":
		return "processing_other"
	case "streamingFailed":
		return "streaming_failed"
	case "transcodeFailed":
		return "transcode_failed"
	case "uploadFailed":
		return "upload_failed"
	default:
		return ""
	}
}

func videoMetadata(publicationID, text string) (string, string, bool) {
	cleaned := strings.TrimSpace(text)
	title := ""
	if cleaned != "" {
		title = cleaned
		if i := strings.IndexByte(cleaned, '\n'); i >= 0 {
			title = strings.TrimSpace(cleaned[:i])
		}
	}
	if title == "" {
		suffix := publicationID
		if len(suffix) > 12 {
			suffix = suffix[len(suffix)-12:]
		}
		title = "TORGNEXA video " + suffix
	}
	title = truncateRunes(title, maxTitleRunes)
	if strings.ContainsAny(title, "<>") || strings.ContainsAny(cleaned, "<>") || !utf8.ValidString(title) || !utf8.ValidString(cleaned) || len([]byte(cleaned)) > maxDescriptionBytes {
		return "", "", false
	}
	if !safeText(title, 1, maxTitleRunes) {
		return "", "", false
	}
	return title, cleaned, true
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
	if !strings.HasPrefix(v, "youtube:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(v, "youtube:")
	channel, video, ok := strings.Cut(rest, ":")
	if !ok || !safeID(channel, 3, 128) || !safeID(video, 6, 64) {
		return "", "", false
	}
	return channel, video, true
}

var _ sdk.SocialPublisher = (*Connector)(nil)
var _ sdk.SocialPublicationStatusReader = (*Connector)(nil)
