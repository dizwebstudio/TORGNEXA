package vk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxVKComments = 100

type commentCursor struct {
	Version int   `json:"v"`
	GroupID int64 `json:"g"`
	PostID  int64 `json:"p"`
	Offset  int   `json:"o"`
}

func encodeCommentCursor(groupID, postID int64, offset int) string {
	encoded, _ := json.Marshal(commentCursor{Version: 1, GroupID: groupID, PostID: postID, Offset: offset})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

func decodeCommentCursor(value string, groupID, postID int64) (int, error) {
	if value == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) > 1024 {
		return 0, sdk.ErrInvalidSocialRequest
	}
	var cursor commentCursor
	if json.Unmarshal(decoded, &cursor) != nil || cursor.Version != 1 || cursor.GroupID != groupID || cursor.PostID != postID || cursor.Offset < 0 || cursor.Offset > 1_000_000_000 {
		return 0, sdk.ErrInvalidSocialRequest
	}
	return cursor.Offset, nil
}

func (connector *Connector) ReadSocialComments(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialCommentReadRequest) (sdk.SocialCommentPage, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(maxVKComments) != nil || sdk.RequireCapability(Manifest(), "social.comments.read") != nil {
		return sdk.SocialCommentPage{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialCommentPage{}, err
	}
	groupID, postID, err := parseRemotePublicationID(request.RemotePublicationID)
	if err != nil || groupID != configuration.GroupID {
		return sdk.SocialCommentPage{}, sdk.ErrInvalidSocialRequest
	}
	offset, err := decodeCommentCursor(request.Cursor, groupID, postID)
	if err != nil {
		return sdk.SocialCommentPage{}, err
	}
	var page sdk.SocialCommentPage
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, callErr := connector.call(ctx, token, "wall.getComments", []Param{
			{Name: "owner_id", Value: strconv.FormatInt(-groupID, 10)},
			{Name: "post_id", Value: strconv.FormatInt(postID, 10)},
			{Name: "offset", Value: strconv.Itoa(offset)},
			{Name: "count", Value: strconv.Itoa(request.Limit)},
			{Name: "sort", Value: "asc"},
		})
		if callErr != nil {
			return callErr
		}
		var response struct {
			Count int64 `json:"count"`
			Items []struct {
				ID             int64  `json:"id"`
				FromID         int64  `json:"from_id"`
				Date           int64  `json:"date"`
				Text           string `json:"text"`
				ReplyToComment int64  `json:"reply_to_comment"`
			} `json:"items"`
		}
		if json.Unmarshal(raw, &response) != nil || response.Count < 0 || len(response.Items) > request.Limit {
			return ErrInvalidResponse
		}
		page.Items = make([]sdk.SocialComment, 0, len(response.Items))
		for _, item := range response.Items {
			if item.ID < 1 || item.FromID == 0 || item.Date < 1 {
				return ErrInvalidResponse
			}
			comment := sdk.SocialComment{
				RemoteCommentID:     strconv.FormatInt(item.ID, 10),
				RemotePublicationID: request.RemotePublicationID,
				AuthorRemoteID:      strconv.FormatInt(item.FromID, 10),
				Text:                item.Text,
				CreatedAt:           unixUTC(item.Date),
			}
			if item.ReplyToComment > 0 {
				comment.ParentRemoteCommentID = strconv.FormatInt(item.ReplyToComment, 10)
			}
			if comment.Validate() != nil {
				return ErrInvalidResponse
			}
			page.Items = append(page.Items, comment)
		}
		if int64(offset+len(response.Items)) < response.Count {
			page.NextCursor = encodeCommentCursor(groupID, postID, offset+len(response.Items))
		}
		return page.Validate(maxVKComments)
	})
	return page, err
}

func (connector *Connector) ReplySocialComment(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialCommentReplyRequest) (sdk.SocialCommentReplyResult, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate() != nil || sdk.RequireCapability(Manifest(), "social.comments.reply") != nil {
		return sdk.SocialCommentReplyResult{}, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return sdk.SocialCommentReplyResult{}, err
	}
	groupID, postID, err := parseRemotePublicationID(request.RemotePublicationID)
	if err != nil || groupID != configuration.GroupID {
		return sdk.SocialCommentReplyResult{}, sdk.ErrInvalidSocialRequest
	}
	params := []Param{
		{Name: "owner_id", Value: strconv.FormatInt(-groupID, 10)},
		{Name: "post_id", Value: strconv.FormatInt(postID, 10)},
		{Name: "from_group", Value: strconv.FormatInt(groupID, 10)},
		{Name: "message", Value: request.Text},
		{Name: "guid", Value: request.IdempotencyKey},
	}
	if request.ReplyToCommentID != "" {
		commentID, parseErr := strconv.ParseInt(request.ReplyToCommentID, 10, 64)
		if parseErr != nil || commentID < 1 || strconv.FormatInt(commentID, 10) != request.ReplyToCommentID {
			return sdk.SocialCommentReplyResult{}, sdk.ErrInvalidSocialRequest
		}
		params = append(params, Param{Name: "reply_to_comment", Value: request.ReplyToCommentID})
	}
	var result sdk.SocialCommentReplyResult
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		raw, callErr := connector.call(ctx, token, "wall.createComment", params)
		if callErr != nil {
			return callErr
		}
		var response struct {
			CommentID int64 `json:"comment_id"`
		}
		if json.Unmarshal(raw, &response) != nil || response.CommentID < 1 {
			return ErrInvalidResponse
		}
		result = sdk.SocialCommentReplyResult{RemoteCommentID: strconv.FormatInt(response.CommentID, 10), CreatedAt: connector.now().UTC()}
		return result.Validate()
	})
	return result, err
}

func (connector *Connector) ReadSocialAnalytics(ctx context.Context, account sdk.Account, runtime sdk.Runtime, request sdk.SocialAnalyticsRequest) ([]sdk.SocialPublicationAnalytics, error) {
	if connector == nil || connector.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || request.Validate(30) != nil || sdk.RequireCapability(Manifest(), "social.analytics.read") != nil {
		return nil, sdk.ErrInvalidSocialRequest
	}
	configuration, err := connector.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	postIDs := make([]int64, 0, len(request.RemotePublicationIDs))
	for _, remoteID := range request.RemotePublicationIDs {
		groupID, postID, parseErr := parseRemotePublicationID(remoteID)
		if parseErr != nil || groupID != configuration.GroupID {
			return nil, sdk.ErrInvalidSocialRequest
		}
		postIDs = append(postIDs, postID)
	}
	var result []sdk.SocialPublicationAnalytics
	err = connector.withToken(ctx, runtime, account.SecretReference, func(token []byte) error {
		values := make([]string, 0, len(postIDs))
		for _, id := range postIDs {
			values = append(values, strconv.FormatInt(id, 10))
		}
		raw, callErr := connector.call(ctx, token, "stats.getPostReach", []Param{
			{Name: "owner_id", Value: strconv.FormatInt(-configuration.GroupID, 10)},
			{Name: "post_ids", Value: joinComma(values)},
		})
		if callErr != nil {
			return callErr
		}
		var response []struct {
			PostID           int64 `json:"post_id"`
			Hide             int64 `json:"hide"`
			JoinGroup        int64 `json:"join_group"`
			Links            int64 `json:"links"`
			ReachSubscribers int64 `json:"reach_subscribers"`
			ReachTotal       int64 `json:"reach_total"`
			Report           int64 `json:"report"`
			ToGroup          int64 `json:"to_group"`
			Unsubscribe      int64 `json:"unsubscribe"`
		}
		if json.Unmarshal(raw, &response) != nil || len(response) > len(request.RemotePublicationIDs) {
			return ErrInvalidResponse
		}
		allowed := make(map[int64]struct{}, len(postIDs))
		for _, id := range postIDs {
			allowed[id] = struct{}{}
		}
		seen := make(map[int64]struct{}, len(response))
		observedAt := connector.now().UTC()
		result = make([]sdk.SocialPublicationAnalytics, 0, len(response))
		for _, item := range response {
			if item.PostID < 1 {
				return ErrInvalidResponse
			}
			if _, ok := allowed[item.PostID]; !ok {
				return ErrInvalidResponse
			}
			if _, duplicate := seen[item.PostID]; duplicate {
				return ErrInvalidResponse
			}
			seen[item.PostID] = struct{}{}
			projection := sdk.SocialPublicationAnalytics{
				RemotePublicationID: remotePublicationID(configuration.GroupID, item.PostID),
				ReachTotal:          item.ReachTotal,
				ReachFollowers:      item.ReachSubscribers,
				LinkClicks:          item.Links,
				CommunityVisits:     item.ToGroup,
				CommunityJoins:      item.JoinGroup,
				Reports:             item.Report,
				Hides:               item.Hide,
				Unsubscribes:        item.Unsubscribe,
				ObservedAt:          observedAt,
			}
			if projection.Validate() != nil {
				return ErrInvalidResponse
			}
			result = append(result, projection)
		}
		return nil
	})
	return result, err
}

func unixUTC(value int64) (resultTime time.Time) { return time.Unix(value, 0).UTC() }

func joinComma(values []string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += "," + value
	}
	return result
}

var _ sdk.SocialCommentReader = (*Connector)(nil)
var _ sdk.SocialCommentReplier = (*Connector)(nil)
var _ sdk.SocialAnalyticsReader = (*Connector)(nil)
