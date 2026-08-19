package avito

import (
	"context"
	"encoding/base64"
	"encoding/json"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"strconv"
	"strings"
	"time"
)

func encodeOffset(v int) string {
	if v <= 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(v)))
}
func decodeOffset(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	b, e := base64.RawURLEncoding.DecodeString(s)
	if e != nil {
		return 0, sdk.ErrInvalidClassified
	}
	v, e := strconv.Atoi(string(b))
	if e != nil || v < 0 {
		return 0, sdk.ErrInvalidClassified
	}
	return v, nil
}

func (c *Connector) ReadClassifiedListings(ctx context.Context, a sdk.Account, rt sdk.Runtime, p sdk.PageRequest) (out sdk.ClassifiedListingPage, err error) {
	if sdk.RequireCapability(Manifest(), "classified.listings.read") != nil || p.Validate(100) != nil {
		return out, sdk.ErrInvalidClassified
	}
	off, e := decodeOffset(p.Cursor)
	if e != nil {
		return out, e
	}
	err = c.with(ctx, a, rt, func(_ Configuration, tok []byte) error {
		q := "per_page=" + strconv.Itoa(p.Limit) + "&page=" + strconv.Itoa(off/p.Limit+1)
		r, e := c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: "/core/v1/items", Query: q, Bearer: tok})
		if e != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if e = normalize(r, false); e != nil {
			return e
		}
		var root struct {
			Resources []struct {
				ID         json.Number `json:"id"`
				ExternalID string      `json:"external_id"`
				Title      string      `json:"title"`
				Status     string      `json:"status"`
				Price      json.Number `json:"price"`
				Currency   string      `json:"currency"`
				UpdatedAt  string      `json:"updated_at"`
			} `json:"resources"`
			Meta struct {
				Page    int `json:"page"`
				PerPage int `json:"per_page"`
				Pages   int `json:"pages"`
			} `json:"meta"`
		}
		d := json.NewDecoder(strings.NewReader(string(r.Body)))
		d.UseNumber()
		if d.Decode(&root) != nil {
			return ErrInvalidResponse
		}
		for _, v := range root.Resources {
			at, e := time.Parse(time.RFC3339, v.UpdatedAt)
			if e != nil {
				return ErrInvalidResponse
			}
			item := sdk.ClassifiedListing{RemoteID: v.ID.String(), ExternalID: v.ExternalID, Title: v.Title, Status: v.Status, UpdatedAt: at.UTC()}
			if v.Price != "" {
				item.Price = v.Price.String()
				item.Currency = v.Currency
				if item.Currency == "" {
					item.Currency = "RUB"
				}
			}
			if item.Validate() != nil {
				return ErrInvalidResponse
			}
			out.Items = append(out.Items, item)
		}
		if root.Meta.Page > 0 && root.Meta.Pages > root.Meta.Page {
			out.NextCursor = encodeOffset(off + p.Limit)
		}
		return out.Validate(p.Limit)
	})
	return out, err
}

func (c *Connector) ReadClassifiedLeads(ctx context.Context, a sdk.Account, rt sdk.Runtime, p sdk.PageRequest) (out sdk.ClassifiedLeadPage, err error) {
	if sdk.RequireCapability(Manifest(), "classified.leads.read") != nil || p.Validate(100) != nil {
		return out, sdk.ErrInvalidClassified
	}
	off, e := decodeOffset(p.Cursor)
	if e != nil {
		return out, e
	}
	err = c.with(ctx, a, rt, func(cfg Configuration, tok []byte) error {
		q := "limit=" + strconv.Itoa(p.Limit) + "&offset=" + strconv.Itoa(off)
		r, e := c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: pathUser(cfg, "/chats"), Query: q, Bearer: tok})
		if e != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if e = normalize(r, false); e != nil {
			return e
		}
		var root struct {
			Chats []struct {
				ID      string `json:"id"`
				Updated int64  `json:"updated"`
				Unread  int    `json:"unread_count"`
				Context struct {
					Value struct {
						ID json.Number `json:"id"`
					} `json:"value"`
				} `json:"context"`
			} `json:"chats"`
		}
		d := json.NewDecoder(strings.NewReader(string(r.Body)))
		d.UseNumber()
		if d.Decode(&root) != nil {
			return ErrInvalidResponse
		}
		for _, v := range root.Chats {
			if v.Updated <= 0 {
				return ErrInvalidResponse
			}
			x := sdk.ClassifiedLead{RemoteID: v.ID, State: "open", UnreadCount: v.Unread, UpdatedAt: time.Unix(v.Updated, 0).UTC()}
			if v.Context.Value.ID != "" {
				x.ListingRemoteID = v.Context.Value.ID.String()
			}
			if x.Validate() != nil {
				return ErrInvalidResponse
			}
			out.Items = append(out.Items, x)
		}
		if len(root.Chats) == p.Limit {
			out.NextCursor = encodeOffset(off + p.Limit)
		}
		return out.Validate(p.Limit)
	})
	return out, err
}

func (c *Connector) ReadClassifiedMessages(ctx context.Context, a sdk.Account, rt sdk.Runtime, lead string, p sdk.PageRequest) (out sdk.ClassifiedMessagePage, err error) {
	if sdk.RequireCapability(Manifest(), "classified.messages.read") != nil || !validID(lead) || p.Validate(100) != nil {
		return out, sdk.ErrInvalidClassified
	}
	off, e := decodeOffset(p.Cursor)
	if e != nil {
		return out, e
	}
	err = c.with(ctx, a, rt, func(cfg Configuration, tok []byte) error {
		q := "limit=" + strconv.Itoa(p.Limit) + "&offset=" + strconv.Itoa(off)
		path := "/messenger/v3/accounts/" + strconv.FormatInt(cfg.UserID, 10) + "/chats/" + lead + "/messages"
		r, e := c.transport.Do(ctx, Request{Method: "GET", Host: apiHost, Path: path, Query: q, Bearer: tok})
		if e != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if e = normalize(r, false); e != nil {
			return e
		}
		var root struct {
			Messages []struct {
				ID       string `json:"id"`
				AuthorID int64  `json:"author_id"`
				Created  int64  `json:"created"`
				Content  struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"messages"`
		}
		if json.Unmarshal(r.Body, &root) != nil {
			return ErrInvalidResponse
		}
		for _, v := range root.Messages {
			dir := "inbound"
			if v.AuthorID == cfg.UserID {
				dir = "outbound"
			}
			x := sdk.ClassifiedMessage{RemoteID: v.ID, LeadRemoteID: lead, Direction: dir, Text: v.Content.Text, CreatedAt: time.Unix(v.Created, 0).UTC()}
			if x.Validate() != nil {
				return ErrInvalidResponse
			}
			out.Items = append(out.Items, x)
		}
		if len(root.Messages) == p.Limit {
			out.NextCursor = encodeOffset(off + p.Limit)
		}
		return out.Validate(p.Limit)
	})
	return out, err
}

func (c *Connector) ReplyClassifiedMessage(ctx context.Context, a sdk.Account, rt sdk.Runtime, in sdk.ClassifiedMessageReply) (out sdk.ClassifiedMessageReceipt, err error) {
	if sdk.RequireCapability(Manifest(), "classified.messages.reply") != nil || in.Validate() != nil {
		return out, sdk.ErrInvalidClassified
	}
	body, _ := json.Marshal(map[string]any{"type": "text", "message": map[string]string{"text": in.Text}})
	err = c.with(ctx, a, rt, func(cfg Configuration, tok []byte) error {
		path := "/messenger/v1/accounts/" + strconv.FormatInt(cfg.UserID, 10) + "/chats/" + in.LeadRemoteID + "/messages"
		r, e := c.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: path, Body: body, Bearer: tok})
		if e != nil {
			return remote(sdk.ErrorConflict, "write_outcome_unknown", "", 0)
		}
		if e = normalize(r, true); e != nil {
			return e
		}
		var v struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(r.Body, &v) != nil || !validID(v.ID) {
			return ErrInvalidResponse
		}
		out = sdk.ClassifiedMessageReceipt{LeadRemoteID: in.LeadRemoteID, RemoteMessageID: v.ID}
		return out.Validate()
	})
	return out, err
}

func (c *Connector) ReadClassifiedStats(ctx context.Context, a sdk.Account, rt sdk.Runtime, q sdk.ClassifiedStatsQuery) (out []sdk.ClassifiedListingStats, err error) {
	if sdk.RequireCapability(Manifest(), "classified.stats.read") != nil || q.Validate(200) != nil {
		return nil, sdk.ErrInvalidClassified
	}
	ids := make([]int64, 0, len(q.ListingRemoteIDs))
	for _, s := range q.ListingRemoteIDs {
		v, e := strconv.ParseInt(s, 10, 64)
		if e != nil || v <= 0 {
			return nil, sdk.ErrInvalidClassified
		}
		ids = append(ids, v)
	}
	body, _ := json.Marshal(map[string]any{"item_ids": ids})
	err = c.with(ctx, a, rt, func(cfg Configuration, tok []byte) error {
		path := "/stats/v1/accounts/" + strconv.FormatInt(cfg.UserID, 10) + "/items"
		r, e := c.transport.Do(ctx, Request{Method: "POST", Host: apiHost, Path: path, Body: body, Bearer: tok})
		if e != nil {
			return remote(sdk.ErrorUnavailable, "transport_unavailable", "", 0)
		}
		if e = normalize(r, false); e != nil {
			return e
		}
		var root struct {
			Result []struct {
				ItemID    json.Number `json:"item_id"`
				UniqViews int64       `json:"uniq_views"`
				Contacts  int64       `json:"contacts"`
				Favorites int64       `json:"favorites"`
			} `json:"result"`
		}
		d := json.NewDecoder(strings.NewReader(string(r.Body)))
		d.UseNumber()
		if d.Decode(&root) != nil {
			return ErrInvalidResponse
		}
		for _, v := range root.Result {
			x := sdk.ClassifiedListingStats{ListingRemoteID: v.ItemID.String(), Views: v.UniqViews, Contacts: v.Contacts, Favorites: v.Favorites}
			if x.Validate() != nil {
				return ErrInvalidResponse
			}
			out = append(out, x)
		}
		return nil
	})
	return out, err
}
func validID(v string) bool {
	if v == "" || len(v) > 300 {
		return false
	}
	for _, r := range v {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
