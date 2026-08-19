package connectors

import (
	"context"
	"time"
)

// SocialCommentReadRequest is the provider-neutral bounded comments read
// surface. RemotePublicationID remains adapter-owned and Cursor is opaque to
// the host.
type SocialCommentReadRequest struct {
	RemotePublicationID string `json:"remote_publication_id"`
	Cursor              string `json:"cursor,omitempty"`
	Limit               int    `json:"limit"`
}

func (request SocialCommentReadRequest) Validate(maxLimit int) error {
	if !validRemoteReadID(request.RemotePublicationID) || request.Limit < 1 || maxLimit < 1 || request.Limit > maxLimit || len(request.Cursor) > 4096 {
		return ErrInvalidSocialRequest
	}
	return nil
}

// SocialComment is a minimal provider-neutral projection. Provider-specific
// profile objects, raw payloads and moderation metadata do not cross the SDK.
type SocialComment struct {
	RemoteCommentID       string    `json:"remote_comment_id"`
	RemotePublicationID   string    `json:"remote_publication_id"`
	ParentRemoteCommentID string    `json:"parent_remote_comment_id,omitempty"`
	AuthorRemoteID        string    `json:"author_remote_id"`
	Text                  string    `json:"text"`
	CreatedAt             time.Time `json:"created_at"`
}

func (comment SocialComment) Validate() error {
	if !validRemoteReadID(comment.RemoteCommentID) || !validRemoteReadID(comment.RemotePublicationID) ||
		(comment.ParentRemoteCommentID != "" && !validRemoteReadID(comment.ParentRemoteCommentID)) ||
		!validRemoteReadID(comment.AuthorRemoteID) || !validSocialText(comment.Text, 50000, true) ||
		comment.CreatedAt.IsZero() || comment.CreatedAt.Location() != time.UTC {
		return ErrInvalidSocialRequest
	}
	return nil
}

type SocialCommentPage struct {
	Items      []SocialComment `json:"items"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

func (page SocialCommentPage) Validate(maxItems int) error {
	if maxItems < 1 || len(page.Items) > maxItems || len(page.NextCursor) > 4096 {
		return ErrInvalidSocialRequest
	}
	seen := make(map[string]struct{}, len(page.Items))
	for _, item := range page.Items {
		if item.Validate() != nil {
			return ErrInvalidSocialRequest
		}
		if _, duplicate := seen[item.RemoteCommentID]; duplicate {
			return ErrInvalidSocialRequest
		}
		seen[item.RemoteCommentID] = struct{}{}
	}
	return nil
}

type SocialCommentReader interface {
	ReadSocialComments(context.Context, Account, Runtime, SocialCommentReadRequest) (SocialCommentPage, error)
}

type SocialCommentReplyRequest struct {
	RemotePublicationID string `json:"remote_publication_id"`
	ReplyToCommentID    string `json:"reply_to_comment_id,omitempty"`
	Text                string `json:"text"`
	IdempotencyKey      string `json:"idempotency_key"`
}

func (request SocialCommentReplyRequest) Validate() error {
	if !validRemoteReadID(request.RemotePublicationID) || (request.ReplyToCommentID != "" && !validRemoteReadID(request.ReplyToCommentID)) ||
		!validSocialText(request.Text, 10000, false) || !sortableIDPattern.MatchString(request.IdempotencyKey) {
		return ErrInvalidSocialRequest
	}
	return nil
}

type SocialCommentReplyResult struct {
	RemoteCommentID string    `json:"remote_comment_id"`
	CreatedAt       time.Time `json:"created_at"`
}

func (result SocialCommentReplyResult) Validate() error {
	if !validRemoteReadID(result.RemoteCommentID) || result.CreatedAt.IsZero() || result.CreatedAt.Location() != time.UTC {
		return ErrInvalidSocialRequest
	}
	return nil
}

type SocialCommentReplier interface {
	ReplySocialComment(context.Context, Account, Runtime, SocialCommentReplyRequest) (SocialCommentReplyResult, error)
}

// SocialAnalyticsRequest is deliberately bounded to the smallest common
// provider-neutral post analytics surface. Adapters may return fewer metrics,
// but must not invent values for unsupported counters.
type SocialAnalyticsRequest struct {
	RemotePublicationIDs []string `json:"remote_publication_ids"`
}

func (request SocialAnalyticsRequest) Validate(maxItems int) error {
	if maxItems < 1 || len(request.RemotePublicationIDs) < 1 || len(request.RemotePublicationIDs) > maxItems {
		return ErrInvalidSocialRequest
	}
	seen := make(map[string]struct{}, len(request.RemotePublicationIDs))
	for _, id := range request.RemotePublicationIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidSocialRequest
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrInvalidSocialRequest
		}
		seen[id] = struct{}{}
	}
	return nil
}

type SocialPublicationAnalytics struct {
	RemotePublicationID string    `json:"remote_publication_id"`
	ReachTotal          int64     `json:"reach_total"`
	ReachFollowers      int64     `json:"reach_followers"`
	LinkClicks          int64     `json:"link_clicks"`
	CommunityVisits     int64     `json:"community_visits"`
	CommunityJoins      int64     `json:"community_joins"`
	Reports             int64     `json:"reports"`
	Hides               int64     `json:"hides"`
	Unsubscribes        int64     `json:"unsubscribes"`
	ObservedAt          time.Time `json:"observed_at"`
}

func (item SocialPublicationAnalytics) Validate() error {
	if !validRemoteReadID(item.RemotePublicationID) || item.ReachTotal < 0 || item.ReachFollowers < 0 || item.LinkClicks < 0 ||
		item.CommunityVisits < 0 || item.CommunityJoins < 0 || item.Reports < 0 || item.Hides < 0 || item.Unsubscribes < 0 ||
		item.ObservedAt.IsZero() || item.ObservedAt.Location() != time.UTC {
		return ErrInvalidSocialRequest
	}
	return nil
}

type SocialAnalyticsReader interface {
	ReadSocialAnalytics(context.Context, Account, Runtime, SocialAnalyticsRequest) ([]SocialPublicationAnalytics, error)
}
