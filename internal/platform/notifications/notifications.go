// Package notifications implements TORGNEXA's tenant-scoped notification inbox
// and provider-neutral delivery boundary.
package notifications

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	MaxTitleRunes       = 200
	MaxBodyRunes        = 4000
	MaxPageSize         = 200
	MaxDeliveryAttempts = 64
)

var (
	ErrInvalid        = errors.New("notifications: invalid value")
	ErrNotFound       = errors.New("notifications: not found")
	ErrConflict       = errors.New("notifications: conflict")
	ErrDeliveryFailed = errors.New("notifications: delivery failed")
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) Valid() bool {
	return s == SeverityInfo || s == SeverityWarning || s == SeverityCritical
}
func (s Severity) rank() int {
	switch s {
	case SeverityInfo:
		return 1
	case SeverityWarning:
		return 2
	case SeverityCritical:
		return 3
	}
	return 0
}

type Channel string

const (
	ChannelWebUI   Channel = "web_ui"
	ChannelWebhook Channel = "webhook"
	ChannelEmail   Channel = "email"
	ChannelSMS     Channel = "sms"
	ChannelChat    Channel = "chat"
)

func (c Channel) Valid() bool {
	return c == ChannelWebUI || c == ChannelWebhook || c == ChannelEmail || c == ChannelSMS || c == ChannelChat
}

type Notification struct {
	ID              string             `json:"id"`
	RecipientID     string             `json:"recipient_id"`
	DedupeKey       string             `json:"dedupe_key"`
	Severity        Severity           `json:"severity"`
	Title           string             `json:"title"`
	Body            string             `json:"body"`
	EntityType      string             `json:"entity_type,omitempty"`
	EntityID        string             `json:"entity_id,omitempty"`
	SourceEventID   string             `json:"source_event_id,omitempty"`
	SourceEventType eventbus.EventType `json:"source_event_type,omitempty"`
	OccurrenceCount int                `json:"occurrence_count"`
	FirstOccurredAt time.Time          `json:"first_occurred_at"`
	LastOccurredAt  time.Time          `json:"last_occurred_at"`
	ReadAt          *time.Time         `json:"read_at,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

func (n Notification) Validate() error {
	if !validID(n.ID) || !validID(n.RecipientID) || !validKey(n.DedupeKey, 200) || !n.Severity.Valid() || !validText(n.Title, 1, MaxTitleRunes) || !validText(n.Body, 0, MaxBodyRunes) || secrets.SensitiveString(n.Title) || secrets.SensitiveString(n.Body) || n.OccurrenceCount < 1 || !utc(n.FirstOccurredAt) || !utc(n.LastOccurredAt) || !utc(n.CreatedAt) || !utc(n.UpdatedAt) || n.LastOccurredAt.Before(n.FirstOccurredAt) || n.UpdatedAt.Before(n.CreatedAt) {
		return ErrInvalid
	}
	if (n.EntityType == "") != (n.EntityID == "") {
		return ErrInvalid
	}
	if n.EntityType != "" && (!validKey(n.EntityType, 128) || !validID(n.EntityID)) {
		return ErrInvalid
	}
	if n.SourceEventID != "" && !validID(n.SourceEventID) {
		return ErrInvalid
	}
	if n.SourceEventType != "" && n.SourceEventType.Validate() != nil {
		return ErrInvalid
	}
	if (n.SourceEventID == "") != (n.SourceEventType == "") {
		return ErrInvalid
	}
	if n.ReadAt != nil && (!utc(*n.ReadAt) || n.ReadAt.Before(n.CreatedAt)) {
		return ErrInvalid
	}
	return nil
}

type Preference struct {
	RecipientID  string    `json:"recipient_id"`
	Channel      Channel   `json:"channel"`
	Enabled      bool      `json:"enabled"`
	MinSeverity  Severity  `json:"min_severity"`
	Categories   []string  `json:"categories"`
	QuietEnabled bool      `json:"quiet_enabled"`
	QuietStart   string    `json:"quiet_start"`
	QuietEnd     string    `json:"quiet_end"`
	Timezone     string    `json:"timezone"`
	Version      int64     `json:"version"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (p Preference) Validate() error {
	if !validID(p.RecipientID) || !p.Channel.Valid() || !p.MinSeverity.Valid() || p.Version < 1 || !utc(p.UpdatedAt) || !validPreferenceCategories(p.Categories) || !validQuietPreference(p) {
		return ErrInvalid
	}
	return nil
}
func (p Preference) Allows(s Severity) bool {
	return p.Enabled && s.Valid() && s.rank() >= p.MinSeverity.rank()
}

func (p Preference) AllowsNotification(n Notification, at time.Time) bool {
	if !p.Allows(n.Severity) || !containsCategory(p.Categories, notificationCategory(n)) {
		return false
	}
	if !p.QuietEnabled || n.Severity == SeverityCritical {
		return true
	}
	location, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return false
	}
	local := at.In(location)
	minute := local.Hour()*60 + local.Minute()
	start := clockMinute(p.QuietStart)
	end := clockMinute(p.QuietEnd)
	if start == end {
		return false
	}
	if start < end {
		return minute < start || minute >= end
	}
	return minute < start && minute >= end
}

func DefaultPreference(recipient string, channel Channel, now time.Time) Preference {
	p := Preference{RecipientID: recipient, Channel: channel, Categories: []string{"commerce", "inventory", "integrations", "compliance", "security", "system"}, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "Europe/Moscow", Version: 1, UpdatedAt: now.UTC()}
	switch channel {
	case ChannelWebUI:
		p.Enabled = true
		p.MinSeverity = SeverityInfo
	case ChannelWebhook:
		p.Enabled = false
		p.MinSeverity = SeverityWarning
	case ChannelEmail, ChannelSMS, ChannelChat:
		p.Enabled = false
		p.MinSeverity = SeverityCritical
	}
	return p
}

type DeliveryStatus string

const (
	DeliverySucceeded  DeliveryStatus = "succeeded"
	DeliverySuppressed DeliveryStatus = "suppressed"
	DeliveryFailed     DeliveryStatus = "failed"
)

func (s DeliveryStatus) Valid() bool {
	return s == DeliverySucceeded || s == DeliverySuppressed || s == DeliveryFailed
}

type Delivery struct {
	NotificationID string         `json:"notification_id"`
	Channel        Channel        `json:"channel"`
	Status         DeliveryStatus `json:"status"`
	ErrorCode      string         `json:"error_code,omitempty"`
	Occurrence     int            `json:"occurrence"`
	Attempt        int            `json:"attempt"`
	AttemptedAt    time.Time      `json:"attempted_at"`
}

func (d Delivery) Validate() error {
	if !validID(d.NotificationID) || !d.Channel.Valid() || !d.Status.Valid() || d.Occurrence < 1 || d.Attempt < 1 || d.Attempt > MaxDeliveryAttempts || !utc(d.AttemptedAt) || !validErrorCode(d.ErrorCode) {
		return ErrInvalid
	}
	if d.Status == DeliveryFailed && d.ErrorCode == "" {
		return ErrInvalid
	}
	if d.Status != DeliveryFailed && d.ErrorCode != "" {
		return ErrInvalid
	}
	return nil
}

type Page struct {
	Items []Notification `json:"items"`
}

type Disposition string

const (
	DispositionCreated      Disposition = "created"
	DispositionDeduplicated Disposition = "deduplicated"
	DispositionEscalated    Disposition = "escalated"
	DispositionReplay       Disposition = "replay"
)

func (d Disposition) Valid() bool {
	return d == DispositionCreated || d == DispositionDeduplicated || d == DispositionEscalated || d == DispositionReplay
}

type Repository interface {
	Upsert(context.Context, tenancy.Scope, Notification) (Notification, Disposition, error)
	List(context.Context, tenancy.Scope, string, int) ([]Notification, error)
	MarkRead(context.Context, tenancy.Scope, string, string, time.Time) (Notification, error)
	PutPreference(context.Context, tenancy.Scope, Preference) (Preference, error)
	Preference(context.Context, tenancy.Scope, string, Channel) (Preference, error)
	RecordDelivery(context.Context, tenancy.Scope, Delivery) error
	Deliveries(context.Context, tenancy.Scope, string, string) ([]Delivery, error)
}

type Provider interface {
	Channel() Channel
	Deliver(context.Context, tenancy.Scope, Notification) error
}

type IDGenerator interface {
	NewID(prefix string) (string, error)
}
type RandomIDs struct{}

func (RandomIDs) NewID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

type Request struct {
	RecipientID     string
	DedupeKey       string
	Severity        Severity
	Title           string
	Body            string
	EntityType      string
	EntityID        string
	SourceEventID   string
	SourceEventType eventbus.EventType
	OccurredAt      time.Time
}

func (r Request) validate() error {
	if !validID(r.RecipientID) || !validKey(r.DedupeKey, 200) || !r.Severity.Valid() || !validText(r.Title, 1, MaxTitleRunes) || !validText(r.Body, 0, MaxBodyRunes) || secrets.SensitiveString(r.Title) || secrets.SensitiveString(r.Body) || !utc(r.OccurredAt) {
		return ErrInvalid
	}
	if (r.EntityType == "") != (r.EntityID == "") || (r.SourceEventID == "") != (r.SourceEventType == "") {
		return ErrInvalid
	}
	if r.EntityType != "" && (!validKey(r.EntityType, 128) || !validID(r.EntityID)) {
		return ErrInvalid
	}
	if r.SourceEventID != "" && (!validID(r.SourceEventID) || r.SourceEventType.Validate() != nil) {
		return ErrInvalid
	}
	return nil
}

type Service struct {
	repo  Repository
	sinks map[Channel]Provider
	ids   IDGenerator
	clock func() time.Time
}

func NewService(repo Repository, providers []Provider, ids IDGenerator) (*Service, error) {
	if repo == nil {
		return nil, ErrInvalid
	}
	if ids == nil {
		ids = RandomIDs{}
	}
	m := map[Channel]Provider{}
	for _, p := range providers {
		if p == nil || !p.Channel().Valid() {
			return nil, ErrInvalid
		}
		if _, ok := m[p.Channel()]; ok {
			return nil, ErrConflict
		}
		m[p.Channel()] = p
	}
	return &Service{repo: repo, sinks: m, ids: ids, clock: time.Now}, nil
}

func (s *Service) Notify(ctx context.Context, scope tenancy.Scope, req Request) (Notification, error) {
	if ctx == nil || !scope.Valid() || s == nil || s.repo == nil || req.validate() != nil {
		return Notification{}, ErrInvalid
	}
	id, err := s.ids.NewID("ntf_")
	if err != nil {
		return Notification{}, err
	}
	now := s.clock().UTC()
	n := Notification{ID: id, RecipientID: req.RecipientID, DedupeKey: req.DedupeKey, Severity: req.Severity, Title: req.Title, Body: req.Body, EntityType: req.EntityType, EntityID: req.EntityID, SourceEventID: req.SourceEventID, SourceEventType: req.SourceEventType, OccurrenceCount: 1, FirstOccurredAt: req.OccurredAt, LastOccurredAt: req.OccurredAt, CreatedAt: now, UpdatedAt: now}
	stored, disposition, err := s.repo.Upsert(ctx, scope, n)
	if err != nil {
		return Notification{}, err
	}
	// A distinct duplicate occurrence updates the inbox but is suppressed from fan-out.
	// Same-occurrence replay (for example an upstream retry) re-attempts delivery using
	// the provider's idempotent boundary; severity escalation is also delivered again.
	if disposition == DispositionDeduplicated {
		return stored, nil
	}
	deliveryFailed := false
	for _, ch := range []Channel{ChannelWebUI, ChannelWebhook, ChannelEmail, ChannelSMS, ChannelChat} {
		pref, err := s.repo.Preference(ctx, scope, req.RecipientID, ch)
		if errors.Is(err, ErrNotFound) {
			if ch == ChannelEmail || ch == ChannelSMS || ch == ChannelChat {
				continue
			}
			pref = DefaultPreference(req.RecipientID, ch, now)
		} else if err != nil {
			return Notification{}, err
		}
		d := Delivery{NotificationID: stored.ID, Channel: ch, Occurrence: stored.OccurrenceCount, Attempt: 1, AttemptedAt: now}
		if !pref.AllowsNotification(stored, now) {
			d.Status = DeliverySuppressed
			if err := s.repo.RecordDelivery(ctx, scope, d); err != nil {
				return Notification{}, err
			}
			continue
		}
		deliverySink := s.sinks[ch]
		if deliverySink == nil {
			d.Status = DeliveryFailed
			d.ErrorCode = "provider_unavailable"
			if err := s.repo.RecordDelivery(ctx, scope, d); err != nil {
				return Notification{}, err
			}
			deliveryFailed = true
			continue
		}
		if err := deliverySink.Deliver(ctx, scope, stored); err != nil {
			d.Status = DeliveryFailed
			d.ErrorCode = "provider_failed"
			if recErr := s.repo.RecordDelivery(ctx, scope, d); recErr != nil {
				return Notification{}, recErr
			}
			deliveryFailed = true
			continue
		}
		d.Status = DeliverySucceeded
		if err := s.repo.RecordDelivery(ctx, scope, d); err != nil {
			return Notification{}, err
		}
	}
	if deliveryFailed {
		return stored, ErrDeliveryFailed
	}
	return stored, nil
}

func (s *Service) RegisterProvider(sink Provider) error {
	if s == nil || sink == nil || !sink.Channel().Valid() {
		return ErrInvalid
	}
	if _, exists := s.sinks[sink.Channel()]; exists {
		return ErrConflict
	}
	s.sinks[sink.Channel()] = sink
	return nil
}

func (s *Service) List(ctx context.Context, scope tenancy.Scope, recipient string, limit int) ([]Notification, error) {
	if ctx == nil || !scope.Valid() || !validID(recipient) || limit < 1 || limit > MaxPageSize {
		return nil, ErrInvalid
	}
	return s.repo.List(ctx, scope, recipient, limit)
}
func (s *Service) MarkRead(ctx context.Context, scope tenancy.Scope, recipient, id string) (Notification, error) {
	if ctx == nil || !scope.Valid() || !validID(recipient) || !validID(id) {
		return Notification{}, ErrInvalid
	}
	return s.repo.MarkRead(ctx, scope, recipient, id, s.clock().UTC())
}
func (s *Service) PutPreference(ctx context.Context, scope tenancy.Scope, p Preference) (Preference, error) {
	if ctx == nil || !scope.Valid() {
		return Preference{}, ErrInvalid
	}
	if p.Version == 0 {
		p.Version = 1
	}
	p.UpdatedAt = s.clock().UTC()
	if p.Validate() != nil {
		return Preference{}, ErrInvalid
	}
	return s.repo.PutPreference(ctx, scope, p)
}
func (s *Service) GetPreference(ctx context.Context, scope tenancy.Scope, recipient string, ch Channel) (Preference, error) {
	if ctx == nil || !scope.Valid() || !validID(recipient) || !ch.Valid() {
		return Preference{}, ErrInvalid
	}
	p, err := s.repo.Preference(ctx, scope, recipient, ch)
	if errors.Is(err, ErrNotFound) {
		return DefaultPreference(recipient, ch, s.clock().UTC()), nil
	}
	return p, err
}
func (s *Service) Deliveries(ctx context.Context, scope tenancy.Scope, recipient, id string) ([]Delivery, error) {
	if ctx == nil || !scope.Valid() || !validID(recipient) || !validID(id) {
		return nil, ErrInvalid
	}
	return s.repo.Deliveries(ctx, scope, recipient, id)
}

// WebUIProvider acknowledges delivery because persistence in the canonical inbox
// is the Web UI delivery mechanism itself.
type WebUIProvider struct{}

func (WebUIProvider) Channel() Channel                                           { return ChannelWebUI }
func (WebUIProvider) Deliver(context.Context, tenancy.Scope, Notification) error { return nil }

type WebhookSink interface {
	Handle(context.Context, eventbus.Delivery) error
}
type WebhookProvider struct{ Sink WebhookSink }

func (WebhookProvider) Channel() Channel { return ChannelWebhook }
func (p WebhookProvider) Deliver(ctx context.Context, scope tenancy.Scope, n Notification) error {
	if p.Sink == nil || !scope.Valid() || n.Validate() != nil {
		return ErrInvalid
	}
	payload, err := json.Marshal(struct {
		NotificationID  string   `json:"notification_id"`
		RecipientID     string   `json:"recipient_id"`
		Severity        Severity `json:"severity"`
		Title           string   `json:"title"`
		Body            string   `json:"body"`
		EntityType      string   `json:"entity_type,omitempty"`
		EntityID        string   `json:"entity_id,omitempty"`
		OccurrenceCount int      `json:"occurrence_count"`
	}{n.ID, n.RecipientID, n.Severity, n.Title, n.Body, n.EntityType, n.EntityID, n.OccurrenceCount})
	if err != nil {
		return err
	}
	typ, _ := eventbus.ParseEventType("platform.notifications.notification_created.v1")
	instant, err := domain.NewUTCInstant(n.UpdatedAt)
	if err != nil {
		return err
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", n.ID, n.OccurrenceCount, n.Severity)))
	evt := eventbus.Event{ID: "evt_ntf_" + hex.EncodeToString(digest[:12]), Type: typ, OccurredAt: instant, OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(), EntityType: "notification", EntityID: n.ID, Source: "notifications", Data: payload}
	return p.Sink.Handle(ctx, eventbus.Delivery{Event: evt, Attempt: 1, FirstObservedAt: instant})
}

func validID(v string) bool { return validKey(v, 128) }
func validPreferenceCategories(values []string) bool {
	if len(values) == 0 {
		return true
	}
	if len(values) > 6 {
		return false
	}
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] || (v != "commerce" && v != "inventory" && v != "integrations" && v != "compliance" && v != "security" && v != "system") {
			return false
		}
		seen[v] = true
	}
	return true
}
func containsCategory(values []string, want string) bool {
	if len(values) == 0 {
		return true
	}
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func validQuietPreference(p Preference) bool {
	if !p.QuietEnabled && p.QuietStart == "" && p.QuietEnd == "" && p.Timezone == "" {
		return true
	}
	return validClock(p.QuietStart) && validClock(p.QuietEnd) && strings.TrimSpace(p.Timezone) != "" && len(p.Timezone) <= 64
}
func validClock(v string) bool {
	if len(v) != 5 || v[2] != ':' {
		return false
	}
	h, e1 := strconv.Atoi(v[:2])
	m, e2 := strconv.Atoi(v[3:])
	return e1 == nil && e2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60
}
func clockMinute(v string) int {
	h, _ := strconv.Atoi(v[:2])
	m, _ := strconv.Atoi(v[3:])
	return h*60 + m
}
func notificationCategory(n Notification) string {
	switch n.EntityType {
	case "order", "return", "product", "offer":
		return "commerce"
	case "inventory", "stock", "warehouse":
		return "inventory"
	case "connector", "sync", "reconciliation":
		return "integrations"
	case "compliance", "certificate", "document":
		return "compliance"
	case "security", "iam", "approval", "audit":
		return "security"
	default:
		return "system"
	}
}
func validKey(v string, max int) bool {
	if v == "" || v != strings.TrimSpace(v) || !utf8.ValidString(v) || len(v) > max {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/-", r)) {
			return false
		}
	}
	return true
}
func validText(v string, min, max int) bool {
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	n := utf8.RuneCountInString(v)
	return n >= min && n <= max
}
func utc(t time.Time) bool { return !t.IsZero() && t.Location() == time.UTC }
func validErrorCode(v string) bool {
	if v == "" {
		return true
	}
	if len(v) > 64 {
		return false
	}
	for i, r := range v {
		if i == 0 && (r < 'a' || r > 'z') {
			return false
		}
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}
