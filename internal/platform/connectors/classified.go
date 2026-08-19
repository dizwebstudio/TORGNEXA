package connectors

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var ErrInvalidClassified = errors.New("connectors: invalid classified projection")

type ClassifiedRisk string

const (
	ClassifiedRiskRead           ClassifiedRisk = "read"
	ClassifiedRiskWriteSensitive ClassifiedRisk = "write_sensitive"
)

type ClassifiedPublicationKind string

type ClassifiedPublicationSection string

type ClassifiedPublicationState string

const (
	ClassifiedPublicationVehicle  ClassifiedPublicationKind = "vehicle"
	ClassifiedPublicationProperty ClassifiedPublicationKind = "property"

	ClassifiedPublicationNew  ClassifiedPublicationSection = "new"
	ClassifiedPublicationUsed ClassifiedPublicationSection = "used"

	ClassifiedPublicationSubmitted  ClassifiedPublicationState = "submitted"
	ClassifiedPublicationProcessing ClassifiedPublicationState = "processing"
	ClassifiedPublicationSucceeded  ClassifiedPublicationState = "succeeded"
	ClassifiedPublicationFailed     ClassifiedPublicationState = "failed"
)

type ClassifiedPublicationRequest struct {
	Kind      ClassifiedPublicationKind    `json:"kind"`
	Section   ClassifiedPublicationSection `json:"section"`
	SourceURL string                       `json:"source_url"`
}

func (r ClassifiedPublicationRequest) Validate() error {
	if (r.Kind != ClassifiedPublicationVehicle && r.Kind != ClassifiedPublicationProperty) || (r.Section != ClassifiedPublicationNew && r.Section != ClassifiedPublicationUsed) || !validPublicationSourceURL(r.SourceURL) {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedPublicationReceipt struct {
	RemoteTaskID string                     `json:"remote_task_id"`
	State        ClassifiedPublicationState `json:"state"`
}

func (r ClassifiedPublicationReceipt) Validate() error {
	if !validRemoteReadID(r.RemoteTaskID) || !validPublicationState(r.State) {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedPublicationStatus struct {
	RemoteTaskID string                     `json:"remote_task_id"`
	State        ClassifiedPublicationState `json:"state"`
	Total        int64                      `json:"total"`
	Inserted     int64                      `json:"inserted"`
	Updated      int64                      `json:"updated"`
	Deleted      int64                      `json:"deleted"`
	Skipped      int64                      `json:"skipped"`
	Errors       int64                      `json:"errors"`
	Notices      int64                      `json:"notices"`
	CheckedAt    time.Time                  `json:"checked_at"`
}

func (s ClassifiedPublicationStatus) Validate() error {
	if !validRemoteReadID(s.RemoteTaskID) || !validPublicationState(s.State) || s.Total < 0 || s.Inserted < 0 || s.Updated < 0 || s.Deleted < 0 || s.Skipped < 0 || s.Errors < 0 || s.Notices < 0 || s.CheckedAt.IsZero() || s.CheckedAt.Location() != time.UTC {
		return ErrInvalidClassified
	}
	if s.Inserted+s.Updated+s.Deleted+s.Skipped > s.Total && s.Total != 0 {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedPublisher interface {
	PublishClassified(context.Context, Account, Runtime, ClassifiedPublicationRequest) (ClassifiedPublicationReceipt, error)
}

type ClassifiedPublicationStatusReader interface {
	ReadClassifiedPublicationStatus(context.Context, Account, Runtime, string) (ClassifiedPublicationStatus, error)
}

func validPublicationSourceURL(raw string) bool {
	if raw == "" || raw != strings.TrimSpace(raw) || len(raw) > 4096 || !utf8.ValidString(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return false
	}
	return true
}

func validPublicationState(v ClassifiedPublicationState) bool {
	switch v {
	case ClassifiedPublicationSubmitted, ClassifiedPublicationProcessing, ClassifiedPublicationSucceeded, ClassifiedPublicationFailed:
		return true
	default:
		return false
	}
}

type ClassifiedListing struct {
	RemoteID   string    `json:"remote_id"`
	ExternalID string    `json:"external_id,omitempty"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	Price      string    `json:"price,omitempty"`
	Currency   string    `json:"currency,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (v ClassifiedListing) Validate() error {
	if !validRemoteReadID(v.RemoteID) || !validOptionalReadText(v.ExternalID, 300) || !validReadText(v.Title, 500) || !validReadText(v.Status, 64) || v.UpdatedAt.IsZero() || v.UpdatedAt.Location() != time.UTC {
		return ErrInvalidClassified
	}
	if v.Price != "" && !validUnsignedMoney(v.Price) {
		return ErrInvalidClassified
	}
	if v.Currency != "" && !validCurrency(v.Currency) {
		return ErrInvalidClassified
	}
	if (v.Price == "") != (v.Currency == "") {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedListingPage struct {
	Items      []ClassifiedListing `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (p ClassifiedListingPage) Validate(max int) error {
	if max < 1 || len(p.Items) > max || len(p.NextCursor) > 4096 || !utf8.ValidString(p.NextCursor) {
		return ErrInvalidClassified
	}
	seen := map[string]struct{}{}
	for _, x := range p.Items {
		if x.Validate() != nil {
			return ErrInvalidClassified
		}
		if _, ok := seen[x.RemoteID]; ok {
			return ErrInvalidClassified
		}
		seen[x.RemoteID] = struct{}{}
	}
	return nil
}

type ClassifiedLead struct {
	RemoteID        string    `json:"remote_id"`
	ListingRemoteID string    `json:"listing_remote_id,omitempty"`
	State           string    `json:"state"`
	UnreadCount     int       `json:"unread_count"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (v ClassifiedLead) Validate() error {
	if !validRemoteReadID(v.RemoteID) || !validOptionalRemoteReadID(v.ListingRemoteID) || !validReadText(v.State, 64) || v.UnreadCount < 0 || v.UpdatedAt.IsZero() || v.UpdatedAt.Location() != time.UTC {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedLeadPage struct {
	Items      []ClassifiedLead `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func (p ClassifiedLeadPage) Validate(max int) error {
	if max < 1 || len(p.Items) > max || len(p.NextCursor) > 4096 || !utf8.ValidString(p.NextCursor) {
		return ErrInvalidClassified
	}
	for _, x := range p.Items {
		if x.Validate() != nil {
			return ErrInvalidClassified
		}
	}
	return nil
}

type ClassifiedMessage struct {
	RemoteID     string    `json:"remote_id"`
	LeadRemoteID string    `json:"lead_remote_id"`
	Direction    string    `json:"direction"`
	Text         string    `json:"text,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (v ClassifiedMessage) Validate() error {
	if !validRemoteReadID(v.RemoteID) || !validRemoteReadID(v.LeadRemoteID) || (v.Direction != "inbound" && v.Direction != "outbound") || v.CreatedAt.IsZero() || v.CreatedAt.Location() != time.UTC || !validOptionalReadText(v.Text, 4000) {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedMessagePage struct {
	Items      []ClassifiedMessage `json:"items"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

func (p ClassifiedMessagePage) Validate(max int) error {
	if max < 1 || len(p.Items) > max || len(p.NextCursor) > 4096 || !utf8.ValidString(p.NextCursor) {
		return ErrInvalidClassified
	}
	for _, x := range p.Items {
		if x.Validate() != nil {
			return ErrInvalidClassified
		}
	}
	return nil
}

type ClassifiedMessageReply struct {
	LeadRemoteID string `json:"lead_remote_id"`
	Text         string `json:"text"`
}

func (r ClassifiedMessageReply) Validate() error {
	if !validRemoteReadID(r.LeadRemoteID) || !validReadText(r.Text, 4000) {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedMessageReceipt struct {
	LeadRemoteID    string `json:"lead_remote_id"`
	RemoteMessageID string `json:"remote_message_id"`
}

func (r ClassifiedMessageReceipt) Validate() error {
	if !validRemoteReadID(r.LeadRemoteID) || !validRemoteReadID(r.RemoteMessageID) {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedStatsQuery struct {
	ListingRemoteIDs []string `json:"listing_remote_ids"`
}

func (q ClassifiedStatsQuery) Validate(max int) error {
	if len(q.ListingRemoteIDs) < 1 || len(q.ListingRemoteIDs) > max {
		return ErrInvalidClassified
	}
	seen := map[string]struct{}{}
	for _, id := range q.ListingRemoteIDs {
		if !validRemoteReadID(id) {
			return ErrInvalidClassified
		}
		if _, ok := seen[id]; ok {
			return ErrInvalidClassified
		}
		seen[id] = struct{}{}
	}
	return nil
}

type ClassifiedListingStats struct {
	ListingRemoteID string `json:"listing_remote_id"`
	Views           int64  `json:"views"`
	Contacts        int64  `json:"contacts"`
	Favorites       int64  `json:"favorites"`
}

func (s ClassifiedListingStats) Validate() error {
	if !validRemoteReadID(s.ListingRemoteID) || s.Views < 0 || s.Contacts < 0 || s.Favorites < 0 {
		return ErrInvalidClassified
	}
	return nil
}

type ClassifiedListingReader interface {
	ReadClassifiedListings(context.Context, Account, Runtime, PageRequest) (ClassifiedListingPage, error)
}
type ClassifiedLeadReader interface {
	ReadClassifiedLeads(context.Context, Account, Runtime, PageRequest) (ClassifiedLeadPage, error)
}
type ClassifiedMessageReader interface {
	ReadClassifiedMessages(context.Context, Account, Runtime, string, PageRequest) (ClassifiedMessagePage, error)
}
type ClassifiedMessageReplier interface {
	ReplyClassifiedMessage(context.Context, Account, Runtime, ClassifiedMessageReply) (ClassifiedMessageReceipt, error)
}
type ClassifiedStatsReader interface {
	ReadClassifiedStats(context.Context, Account, Runtime, ClassifiedStatsQuery) ([]ClassifiedListingStats, error)
}

func ClassifiedCapabilityRisk(cap Capability) (ClassifiedRisk, error) {
	switch cap {
	case "classified.listings.read", "classified.leads.read", "classified.messages.read", "classified.stats.read", "classified.publications.status.read":
		return ClassifiedRiskRead, nil
	case "classified.messages.reply", "classified.publications.write":
		return ClassifiedRiskWriteSensitive, nil
	default:
		return "", ErrUnknownCapability
	}
}

func safeClassifiedText(v string, max int) bool {
	return v == strings.TrimSpace(v) && utf8.ValidString(v) && utf8.RuneCountInString(v) <= max
}
