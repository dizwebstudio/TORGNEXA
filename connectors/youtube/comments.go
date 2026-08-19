package youtube

import (
	"context"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxComments = 100

func (c *Connector) ReadSocialComments(ctx context.Context, account sdk.Account, runtime sdk.Runtime, req sdk.SocialCommentReadRequest) (sdk.SocialCommentPage, error) {
	if c == nil || c.transport == nil || runtime == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || req.Validate(maxComments) != nil || sdk.RequireCapability(Manifest(), "social.comments.read") != nil {
		return sdk.SocialCommentPage{}, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return sdk.SocialCommentPage{}, err
	}
	channelID, videoID, ok := parseRemoteID(req.RemotePublicationID)
	if !ok || channelID != cfg.ChannelID {
		return sdk.SocialCommentPage{}, sdk.ErrInvalidSocialRequest
	}
	var out sdk.SocialCommentPage
	err = c.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		page, callErr := c.transport.ListCommentThreads(ctx, token, videoID, req.Cursor, req.Limit)
		if callErr != nil {
			return normalizeFailure(callErr, false)
		}
		if len(page.Items) > req.Limit || len(page.NextPageToken) > 4096 {
			return ErrInvalidResponse
		}
		out.Items = make([]sdk.SocialComment, 0, len(page.Items))
		seen := make(map[string]struct{}, len(page.Items))
		for _, item := range page.Items {
			if !safeID(item.CommentID, 3, 256) || !safeID(item.AuthorChannelID, 3, 128) || !safeText(item.Text, 0, 50000) || item.PublishedAt.IsZero() || item.PublishedAt.Location() != time.UTC {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[item.CommentID]; duplicate {
				return ErrInvalidResponse
			}
			seen[item.CommentID] = struct{}{}
			comment := sdk.SocialComment{RemoteCommentID: item.CommentID, RemotePublicationID: req.RemotePublicationID, AuthorRemoteID: item.AuthorChannelID, Text: item.Text, CreatedAt: item.PublishedAt}
			if comment.Validate() != nil {
				return ErrInvalidResponse
			}
			out.Items = append(out.Items, comment)
		}
		out.NextCursor = page.NextPageToken
		return out.Validate(maxComments)
	})
	return out, err
}

var _ sdk.SocialCommentReader = (*Connector)(nil)
