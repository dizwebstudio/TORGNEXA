package ok

import (
	"context"
	"encoding/json"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const maxAnalyticsItems = 50

func (c *Connector) ReadSocialAnalytics(ctx context.Context, account sdk.Account, runtime sdk.Runtime, req sdk.SocialAnalyticsRequest) ([]sdk.SocialPublicationAnalytics, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil || sdk.RequireCapability(Manifest(), "social.analytics.read") != nil || req.Validate(maxAnalyticsItems) != nil {
		return nil, sdk.ErrInvalidSocialRequest
	}
	cfg, err := c.configuration(ctx, account)
	if err != nil {
		return nil, err
	}
	out := make([]sdk.SocialPublicationAnalytics, 0, len(req.RemotePublicationIDs))
	err = c.withCredentials(ctx, runtime, account, cfg, func(token, appSecret []byte) error {
		for _, remoteID := range req.RemotePublicationIDs {
			g, topic, ok := parseRemoteID(remoteID)
			if !ok || g != cfg.GroupID {
				return sdk.ErrInvalidSocialRequest
			}
			raw, e := c.call(ctx, token, appSecret, cfg.ApplicationKey, "GET", "group.getStatTopic", []Param{{Name: "topic_id", Value: topic}, {Name: "fields", Value: "id,reach,reach_own,link_clicks,complaints,hides_from_feed"}}, false)
			if e != nil {
				return e
			}
			var env struct {
				Topic struct {
					ID         string `json:"id"`
					Reach      int64  `json:"reach"`
					ReachOwn   int64  `json:"reach_own"`
					LinkClicks int64  `json:"link_clicks"`
					Complaints int64  `json:"complaints"`
					Hides      int64  `json:"hides_from_feed"`
				} `json:"topic"`
			}
			if json.Unmarshal(raw, &env) != nil || env.Topic.ID != topic || env.Topic.Reach < 0 || env.Topic.ReachOwn < 0 || env.Topic.LinkClicks < 0 || env.Topic.Complaints < 0 || env.Topic.Hides < 0 {
				return ErrInvalidResponse
			}
			item := sdk.SocialPublicationAnalytics{RemotePublicationID: remoteID, ReachTotal: env.Topic.Reach, ReachFollowers: env.Topic.ReachOwn, LinkClicks: env.Topic.LinkClicks, Reports: env.Topic.Complaints, Hides: env.Topic.Hides, ObservedAt: c.now().UTC()}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			out = append(out, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
