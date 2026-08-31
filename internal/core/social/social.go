// Package social defines TORGNEXA's provider-neutral content and publication core.
package social

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidRecord      = errors.New("social: invalid record")
	ErrInvalidScope       = errors.New("social: invalid tenant scope")
	ErrNotFound           = errors.New("social: record not found")
	ErrConflict           = errors.New("social: optimistic version conflict")
	ErrInvalidState       = errors.New("social: invalid lifecycle transition")
	ErrCapabilityMissing  = errors.New("social: channel capability missing")
	ErrChannelUnavailable = errors.New("social: channel account unavailable")
	ErrMediaUnavailable   = errors.New("social: released media required")
)

var (
	sortableIDPattern  = regexp.MustCompile(`^(?:[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}|[0-7][0-9A-HJKMNP-TV-Z]{25})$`)
	connectorIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	uploadIDPattern    = regexp.MustCompile(`^upl_[0-9a-f]{32}$`)
	tokenPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	sourcePattern      = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	reasonPattern      = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	buttonURLPattern   = regexp.MustCompile(`^https://[^\s]+$`)
)

type ContentID string
type VariantID string
type PublicationID string
type ChannelAccountID string

func ParseContentID(v string) (ContentID, error) {
	if !validSortableID(v) {
		return "", ErrInvalidRecord
	}
	return ContentID(v), nil
}
func ParseVariantID(v string) (VariantID, error) {
	if !validSortableID(v) {
		return "", ErrInvalidRecord
	}
	return VariantID(v), nil
}
func ParsePublicationID(v string) (PublicationID, error) {
	if !validSortableID(v) {
		return "", ErrInvalidRecord
	}
	return PublicationID(v), nil
}
func ParseChannelAccountID(v string) (ChannelAccountID, error) {
	if !validSortableID(v) {
		return "", ErrInvalidRecord
	}
	return ChannelAccountID(v), nil
}
func (id ContentID) String() string        { return string(id) }
func (id VariantID) String() string        { return string(id) }
func (id PublicationID) String() string    { return string(id) }
func (id ChannelAccountID) String() string { return string(id) }
func (id ContentID) Valid() bool           { return validSortableID(string(id)) }
func (id VariantID) Valid() bool           { return validSortableID(string(id)) }
func (id PublicationID) Valid() bool       { return validSortableID(string(id)) }
func (id ChannelAccountID) Valid() bool    { return validSortableID(string(id)) }

type Scope struct{ organizationID, workspaceID string }

func ParseScope(org, ws string) (Scope, error) {
	if !validSortableID(org) || !validSortableID(ws) {
		return Scope{}, ErrInvalidScope
	}
	return Scope{organizationID: org, workspaceID: ws}, nil
}
func (s Scope) OrganizationID() string { return s.organizationID }
func (s Scope) WorkspaceID() string    { return s.workspaceID }
func (s Scope) Valid() bool {
	return validSortableID(s.organizationID) && validSortableID(s.workspaceID)
}

type ContentStatus string

const (
	ContentDraft    ContentStatus = "draft"
	ContentReady    ContentStatus = "ready"
	ContentArchived ContentStatus = "archived"
)

func (s ContentStatus) Valid() bool {
	return s == ContentDraft || s == ContentReady || s == ContentArchived
}

type Content struct {
	ID             ContentID
	OrganizationID string
	WorkspaceID    string
	Title          string
	Body           string
	Status         ContentStatus
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (c Content) Validate() error {
	if !c.ID.Valid() || !validSortableID(c.OrganizationID) || !validSortableID(c.WorkspaceID) || !validOptionalText(c.Title, 300, false) || !validOptionalText(c.Body, 50000, true) || (c.Title == "" && c.Body == "") || !c.Status.Valid() || !validMetadata(c.Version, c.CreatedAt, c.UpdatedAt) {
		return ErrInvalidRecord
	}
	return nil
}

type CreateContent struct {
	ID    ContentID
	Title string
	Body  string
}

func (c CreateContent) Validate() error {
	if !c.ID.Valid() || !validOptionalText(c.Title, 300, false) || !validOptionalText(c.Body, 50000, true) || (c.Title == "" && c.Body == "") {
		return ErrInvalidRecord
	}
	return nil
}

type UpdateContent struct {
	ID              ContentID
	ExpectedVersion int64
	Title           string
	Body            string
}

func (c UpdateContent) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !validOptionalText(c.Title, 300, false) || !validOptionalText(c.Body, 50000, true) || (c.Title == "" && c.Body == "") {
		return ErrInvalidRecord
	}
	return nil
}

type ChangeContentStatus struct {
	ID              ContentID
	ExpectedVersion int64
	Status          ContentStatus
}

func (c ChangeContentStatus) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !c.Status.Valid() {
		return ErrInvalidRecord
	}
	return nil
}

func ValidateContentTransition(from, to ContentStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	if from == ContentDraft && (to == ContentReady || to == ContentArchived) {
		return nil
	}
	if from == ContentReady && (to == ContentDraft || to == ContentArchived) {
		return nil
	}
	return ErrInvalidState
}

type VariantFormat string

const (
	FormatText    VariantFormat = "text"
	FormatImage   VariantFormat = "image"
	FormatGallery VariantFormat = "gallery"
	FormatVideo   VariantFormat = "video"
	FormatArticle VariantFormat = "article"
)

func (f VariantFormat) Valid() bool {
	switch f {
	case FormatText, FormatImage, FormatGallery, FormatVideo, FormatArticle:
		return true
	default:
		return false
	}
}

type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
)

func (k MediaKind) Valid() bool { return k == MediaImage || k == MediaVideo }

type MediaRef struct {
	UploadID string
	Kind     MediaKind
	AltText  string
}

func (r MediaRef) Validate() error {
	if !uploadIDPattern.MatchString(r.UploadID) || !r.Kind.Valid() || !validOptionalText(r.AltText, 1000, false) {
		return ErrInvalidRecord
	}
	return nil
}

type ContentVariant struct {
	ID             VariantID
	OrganizationID string
	WorkspaceID    string
	ContentID      ContentID
	Format         VariantFormat
	Title          string
	Body           string
	Media          []MediaRef
	Buttons        []Button
	Version        int64
	CreatedAt      time.Time
}

func (v ContentVariant) Validate() error {
	if !v.ID.Valid() || !validSortableID(v.OrganizationID) || !validSortableID(v.WorkspaceID) || !v.ContentID.Valid() || !v.Format.Valid() || !validOptionalText(v.Title, 300, false) || !validOptionalText(v.Body, 50000, true) || v.Version != 1 || !isUTC(v.CreatedAt) || len(v.Media) > 20 || validateButtons(v.Buttons) != nil {
		return ErrInvalidRecord
	}
	seen := map[string]struct{}{}
	for _, media := range v.Media {
		if media.Validate() != nil {
			return ErrInvalidRecord
		}
		if _, ok := seen[media.UploadID]; ok {
			return ErrInvalidRecord
		}
		seen[media.UploadID] = struct{}{}
	}
	return validateVariantShape(v.Format, v.Title, v.Body, v.Media, v.Buttons)
}

type CreateVariant struct {
	ID        VariantID
	ContentID ContentID
	Format    VariantFormat
	Title     string
	Body      string
	Media     []MediaRef
	Buttons   []Button
}

func (c CreateVariant) Validate() error {
	if !c.ID.Valid() || !c.ContentID.Valid() || !c.Format.Valid() || !validOptionalText(c.Title, 300, false) || !validOptionalText(c.Body, 50000, true) || len(c.Media) > 20 || validateButtons(c.Buttons) != nil {
		return ErrInvalidRecord
	}
	seen := make(map[string]struct{}, len(c.Media))
	for _, media := range c.Media {
		if media.Validate() != nil {
			return ErrInvalidRecord
		}
		if _, duplicate := seen[media.UploadID]; duplicate {
			return ErrInvalidRecord
		}
		seen[media.UploadID] = struct{}{}
	}
	return validateVariantShape(c.Format, c.Title, c.Body, c.Media, c.Buttons)
}

func validateVariantShape(format VariantFormat, title, body string, media []MediaRef, buttons []Button) error {
	for _, ref := range media {
		if ref.Validate() != nil {
			return ErrInvalidRecord
		}
	}
	switch format {
	case FormatText:
		if body == "" || len(media) != 0 {
			return ErrInvalidRecord
		}
	case FormatImage:
		if len(media) != 1 || media[0].Kind != MediaImage {
			return ErrInvalidRecord
		}
	case FormatGallery:
		if len(media) < 2 || len(media) > 20 {
			return ErrInvalidRecord
		}
		for _, ref := range media {
			if ref.Kind != MediaImage {
				return ErrInvalidRecord
			}
		}
	case FormatVideo:
		if len(media) != 1 || media[0].Kind != MediaVideo {
			return ErrInvalidRecord
		}
	case FormatArticle:
		if title == "" || body == "" {
			return ErrInvalidRecord
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

// Button is a provider-neutral HTTPS link button attached to a publication.
// Callback-data buttons are intentionally outside this contract because they
// require an inbound update lifecycle and authorization policy.
type Button struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (button Button) Validate() error {
	if !validText(button.Text, 1, 64, false) || len(button.URL) > 2048 || !buttonURLPattern.MatchString(button.URL) || strings.ContainsAny(button.URL, `\"<>[]{}`) {
		return ErrInvalidRecord
	}
	return nil
}

func validateButtons(buttons []Button) error {
	if len(buttons) > 8 {
		return ErrInvalidRecord
	}
	for _, button := range buttons {
		if button.Validate() != nil {
			return ErrInvalidRecord
		}
	}
	return nil
}

// ValidateButtons validates an optional publication button set before any
// content or publication record is written.
func ValidateButtons(buttons []Button) error { return validateButtons(buttons) }

type Capability string

const (
	CapabilityPostText      Capability = "social.post.text"
	CapabilityPostMedia     Capability = "social.post.media"
	CapabilityPostVideo     Capability = "social.post.video"
	CapabilityPostButtons   Capability = "social.post.buttons"
	CapabilityPostEdit      Capability = "social.post.edit"
	CapabilityPostDelete    Capability = "social.post.delete"
	CapabilityCommentsRead  Capability = "social.comments.read"
	CapabilityCommentsReply Capability = "social.comments.reply"
	CapabilityAnalyticsRead Capability = "social.analytics.read"
)

func (c Capability) Valid() bool {
	switch c {
	case CapabilityPostText, CapabilityPostMedia, CapabilityPostVideo, CapabilityPostButtons, CapabilityPostEdit, CapabilityPostDelete, CapabilityCommentsRead, CapabilityCommentsReply, CapabilityAnalyticsRead:
		return true
	default:
		return false
	}
}

func CanonicalCapabilities(values []Capability) ([]Capability, error) {
	if len(values) == 0 || len(values) > 8 {
		return nil, ErrInvalidRecord
	}
	seen := make(map[Capability]struct{}, len(values))
	out := append([]Capability(nil), values...)
	for _, value := range out {
		if !value.Valid() {
			return nil, ErrInvalidRecord
		}
		if _, dup := seen[value]; dup {
			return nil, ErrInvalidRecord
		}
		seen[value] = struct{}{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func RequiredPublishCapability(format VariantFormat) (Capability, error) {
	switch format {
	case FormatText, FormatArticle:
		return CapabilityPostText, nil
	case FormatImage, FormatGallery:
		return CapabilityPostMedia, nil
	case FormatVideo:
		return CapabilityPostVideo, nil
	default:
		return "", ErrInvalidRecord
	}
}

type ChannelStatus string

const (
	ChannelDisabled ChannelStatus = "disabled"
	ChannelActive   ChannelStatus = "active"
)

func (s ChannelStatus) Valid() bool { return s == ChannelDisabled || s == ChannelActive }

type ChannelAccount struct {
	ID                 ChannelAccountID
	OrganizationID     string
	WorkspaceID        string
	ConnectorAccountID string
	DisplayName        string
	Capabilities       []Capability
	Status             ChannelStatus
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (a ChannelAccount) Validate() error {
	caps, err := CanonicalCapabilities(a.Capabilities)
	accountRefValid := connectorIDPattern.MatchString(a.ConnectorAccountID)
	if !a.ID.Valid() || !validSortableID(a.OrganizationID) || !validSortableID(a.WorkspaceID) || !accountRefValid || !validText(a.DisplayName, 1, 300, false) || err != nil || !equalCapabilities(caps, a.Capabilities) || !a.Status.Valid() || !validMetadata(a.Version, a.CreatedAt, a.UpdatedAt) {
		return ErrInvalidRecord
	}
	return nil
}
func (a ChannelAccount) Supports(cap Capability) bool {
	for _, item := range a.Capabilities {
		if item == cap {
			return true
		}
	}
	return false
}

type CreateChannelAccount struct {
	ID                 ChannelAccountID
	ConnectorAccountID string
	DisplayName        string
	Capabilities       []Capability
}

func (c CreateChannelAccount) Validate() error {
	caps, err := CanonicalCapabilities(c.Capabilities)
	accountRefValid := connectorIDPattern.MatchString(c.ConnectorAccountID)
	if !c.ID.Valid() || !accountRefValid || !validText(c.DisplayName, 1, 300, false) || err != nil || !equalCapabilities(caps, c.Capabilities) {
		return ErrInvalidRecord
	}
	return nil
}

type UpdateChannelAccount struct {
	ID              ChannelAccountID
	ExpectedVersion int64
	DisplayName     string
	Capabilities    []Capability
	Status          ChannelStatus
}

func (c UpdateChannelAccount) Validate() error {
	caps, err := CanonicalCapabilities(c.Capabilities)
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !validText(c.DisplayName, 1, 300, false) || err != nil || !equalCapabilities(caps, c.Capabilities) || !c.Status.Valid() {
		return ErrInvalidRecord
	}
	return nil
}

type ScheduleMode string

const (
	ScheduleImmediate ScheduleMode = "immediate"
	ScheduleAt        ScheduleMode = "at"
)

func (m ScheduleMode) Valid() bool { return m == ScheduleImmediate || m == ScheduleAt }

type Schedule struct {
	Mode      ScheduleMode
	PublishAt *time.Time
}

func ImmediateSchedule() Schedule { return Schedule{Mode: ScheduleImmediate} }
func AtSchedule(at time.Time) (Schedule, error) {
	at = at.UTC()
	s := Schedule{Mode: ScheduleAt, PublishAt: &at}
	if s.Validate() != nil {
		return Schedule{}, ErrInvalidRecord
	}
	return s, nil
}
func (s Schedule) Validate() error {
	if !s.Mode.Valid() {
		return ErrInvalidRecord
	}
	if s.Mode == ScheduleImmediate {
		if s.PublishAt != nil {
			return ErrInvalidRecord
		}
		return nil
	}
	if s.PublishAt == nil || !isUTC(*s.PublishAt) {
		return ErrInvalidRecord
	}
	return nil
}

type PublicationStatus string

const (
	PublicationScheduled  PublicationStatus = "scheduled"
	PublicationReady      PublicationStatus = "ready"
	PublicationPublishing PublicationStatus = "publishing"
	PublicationPublished  PublicationStatus = "published"
	PublicationFailed     PublicationStatus = "failed"
	PublicationCancelled  PublicationStatus = "cancelled"
)

func (s PublicationStatus) Valid() bool {
	switch s {
	case PublicationScheduled, PublicationReady, PublicationPublishing, PublicationPublished, PublicationFailed, PublicationCancelled:
		return true
	default:
		return false
	}
}

type Publication struct {
	ID               PublicationID
	OrganizationID   string
	WorkspaceID      string
	VariantID        VariantID
	ChannelAccountID ChannelAccountID
	Schedule         Schedule
	Status           PublicationStatus
	Attempt          int
	ReasonCode       string
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
	PublishedAt      *time.Time
}

func (p Publication) Validate() error {
	if !p.ID.Valid() || !validSortableID(p.OrganizationID) || !validSortableID(p.WorkspaceID) || !p.VariantID.Valid() || !p.ChannelAccountID.Valid() || p.Schedule.Validate() != nil || !p.Status.Valid() || p.Attempt < 0 || p.Attempt > 1000 || !validReason(p.ReasonCode) || !validMetadata(p.Version, p.CreatedAt, p.UpdatedAt) {
		return ErrInvalidRecord
	}
	if p.Status == PublicationScheduled && p.Schedule.Mode != ScheduleAt {
		return ErrInvalidRecord
	}
	if p.Status != PublicationScheduled && p.Status != PublicationCancelled && p.Schedule.Mode == ScheduleAt && p.Schedule.PublishAt == nil {
		return ErrInvalidRecord
	}
	if p.Status == PublicationPublished {
		if p.PublishedAt == nil || !isUTC(*p.PublishedAt) || p.PublishedAt.Before(p.CreatedAt) {
			return ErrInvalidRecord
		}
	} else if p.PublishedAt != nil {
		return ErrInvalidRecord
	}
	if p.Status == PublicationFailed && p.ReasonCode == "" {
		return ErrInvalidRecord
	}
	if p.Status != PublicationFailed && p.ReasonCode != "" {
		return ErrInvalidRecord
	}
	return nil
}

type CreatePublication struct {
	ID               PublicationID
	VariantID        VariantID
	ChannelAccountID ChannelAccountID
	Schedule         Schedule
}

func (c CreatePublication) Validate() error {
	if !c.ID.Valid() || !c.VariantID.Valid() || !c.ChannelAccountID.Valid() || c.Schedule.Validate() != nil {
		return ErrInvalidRecord
	}
	return nil
}

type ChangePublicationStatus struct {
	ID              PublicationID
	ExpectedVersion int64
	Status          PublicationStatus
	ReasonCode      string
}

func (c ChangePublicationStatus) Validate() error {
	if !c.ID.Valid() || c.ExpectedVersion < 1 || !c.Status.Valid() || !validReason(c.ReasonCode) {
		return ErrInvalidRecord
	}
	if c.Status == PublicationFailed && c.ReasonCode == "" {
		return ErrInvalidRecord
	}
	if c.Status != PublicationFailed && c.ReasonCode != "" {
		return ErrInvalidRecord
	}
	return nil
}

func InitialPublicationStatus(schedule Schedule) (PublicationStatus, error) {
	if schedule.Validate() != nil {
		return "", ErrInvalidRecord
	}
	if schedule.Mode == ScheduleAt {
		return PublicationScheduled, nil
	}
	return PublicationReady, nil
}

func ValidatePublicationTransition(from, to PublicationStatus) error {
	if !from.Valid() || !to.Valid() || from == to {
		return ErrInvalidState
	}
	switch from {
	case PublicationScheduled:
		if to == PublicationReady || to == PublicationCancelled {
			return nil
		}
	case PublicationReady:
		if to == PublicationPublishing || to == PublicationCancelled {
			return nil
		}
	case PublicationPublishing:
		if to == PublicationPublished || to == PublicationFailed {
			return nil
		}
	case PublicationFailed:
		if to == PublicationReady || to == PublicationCancelled {
			return nil
		}
	}
	return ErrInvalidState
}

func ValidatePublicationPlan(account ChannelAccount, variant ContentVariant) error {
	if account.Validate() != nil || variant.Validate() != nil {
		return ErrInvalidRecord
	}
	if account.Status != ChannelActive {
		return ErrChannelUnavailable
	}
	capability, err := RequiredPublishCapability(variant.Format)
	if err != nil {
		return err
	}
	if !account.Supports(capability) {
		return ErrCapabilityMissing
	}
	if len(variant.Buttons) > 0 && !account.Supports(CapabilityPostButtons) {
		return ErrCapabilityMissing
	}
	return nil
}

type StatusEvent struct {
	EventID            string
	PublicationID      PublicationID
	PublicationVersion int64
	Status             PublicationStatus
	Attempt            int
	ReasonCode         string
	CorrelationID      string
	OccurredAt         time.Time
}

func (e StatusEvent) Validate() error {
	if !validToken(e.EventID) || !e.PublicationID.Valid() || e.PublicationVersion < 1 || !e.Status.Valid() || e.Attempt < 0 || e.Attempt > 1000 || !validReason(e.ReasonCode) || !validOptionalToken(e.CorrelationID) || !isUTC(e.OccurredAt) {
		return ErrInvalidRecord
	}
	if e.Status == PublicationFailed && e.ReasonCode == "" {
		return ErrInvalidRecord
	}
	if e.Status != PublicationFailed && e.ReasonCode != "" {
		return ErrInvalidRecord
	}
	return nil
}

type Mutation struct {
	EventID, AuditID, ActorID, Source, CorrelationID, CausationID, TraceID string
	OccurredAt                                                             time.Time
}

func (m Mutation) Validate() error {
	if !validToken(m.EventID) || !validSortableID(m.AuditID) || !validToken(m.ActorID) || !sourcePattern.MatchString(m.Source) || !validToken(m.CorrelationID) || !validOptionalToken(m.CausationID) || !validOptionalToken(m.TraceID) || !isUTC(m.OccurredAt) {
		return ErrInvalidRecord
	}
	return nil
}

type Repository interface {
	Content(context.Context, Scope, ContentID) (Content, error)
	Variant(context.Context, Scope, VariantID) (ContentVariant, error)
	ChannelAccount(context.Context, Scope, ChannelAccountID) (ChannelAccount, error)
	Publication(context.Context, Scope, PublicationID) (Publication, error)
	PublicationStatusEvents(context.Context, Scope, PublicationID, int) ([]StatusEvent, error)
	DuePublications(context.Context, Scope, time.Time, int) ([]Publication, error)
	CreateContent(context.Context, Scope, CreateContent, Mutation) (Content, error)
	UpdateContent(context.Context, Scope, UpdateContent, Mutation) (Content, error)
	ChangeContentStatus(context.Context, Scope, ChangeContentStatus, Mutation) (Content, error)
	CreateVariant(context.Context, Scope, CreateVariant, Mutation) (ContentVariant, error)
	CreateChannelAccount(context.Context, Scope, CreateChannelAccount, Mutation) (ChannelAccount, error)
	UpdateChannelAccount(context.Context, Scope, UpdateChannelAccount, Mutation) (ChannelAccount, error)
	CreatePublication(context.Context, Scope, CreatePublication, Mutation) (Publication, error)
	ChangePublicationStatus(context.Context, Scope, ChangePublicationStatus, Mutation) (Publication, error)
}

func validMetadata(version int64, createdAt, updatedAt time.Time) bool {
	return version >= 1 && isUTC(createdAt) && isUTC(updatedAt) && !updatedAt.Before(createdAt)
}
func isUTC(v time.Time) bool           { return !v.IsZero() && v.Location() == time.UTC }
func validSortableID(v string) bool    { return sortableIDPattern.MatchString(v) }
func validToken(v string) bool         { return tokenPattern.MatchString(v) }
func validOptionalToken(v string) bool { return v == "" || validToken(v) }
func validReason(v string) bool        { return v == "" || reasonPattern.MatchString(v) }
func validText(v string, min, max int, layout bool) bool {
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	count := utf8.RuneCountInString(v)
	if count < min || count > max {
		return false
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			if layout && (r == '\n' || r == '\r' || r == '\t') {
				continue
			}
			return false
		}
	}
	return true
}
func validOptionalText(v string, max int, layout bool) bool {
	return v == "" || validText(v, 1, max, layout)
}
func equalCapabilities(a, b []Capability) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
