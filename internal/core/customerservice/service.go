// Package customerservice contains the provider-neutral customer-service
// contracts used by the unified inbox. It stores only minimized customer
// references and sanitized message content; canonical commerce aggregates stay
// owned by their respective domains.
package customerservice

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalid     = errors.New("customer service: invalid value")
	ErrAIDraftOnly = errors.New("customer service: AI output is draft-only")
	ErrConflict    = errors.New("customer service: version conflict")
)

const (
	MaxTextLength       = 16000
	MaxSubjectLength    = 500
	MaxReferencesLength = 192
	MaxTimelineItems    = 500
)

// ConversationType classifies the inbound customer-service surface.
type ConversationType string

const (
	TypeMessage         ConversationType = "message"
	TypeReview          ConversationType = "review"
	TypeQuestion        ConversationType = "question"
	TypeClaim           ConversationType = "claim"
	TypeReturnRequest   ConversationType = "return_request"
	TypeDeliveryFailure ConversationType = "delivery_failure"
)

func (v ConversationType) Valid() bool {
	return v == TypeMessage || v == TypeReview || v == TypeQuestion || v == TypeClaim || v == TypeReturnRequest || v == TypeDeliveryFailure
}

// ConversationState is the operator-visible lifecycle of a thread.
type ConversationState string

const (
	StateUnread          ConversationState = "unread"
	StateOpen            ConversationState = "open"
	StatePendingCustomer ConversationState = "pending_customer"
	StatePendingInternal ConversationState = "pending_internal"
	StateResolved        ConversationState = "resolved"
	StateClosed          ConversationState = "closed"
	StateSpam            ConversationState = "spam"
)

func (v ConversationState) Valid() bool {
	return v == StateUnread || v == StateOpen || v == StatePendingCustomer || v == StatePendingInternal || v == StateResolved || v == StateClosed || v == StateSpam
}

// Priority determines queue ordering and the selected SLA policy.
type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func (v Priority) Valid() bool {
	return v == PriorityLow || v == PriorityNormal || v == PriorityHigh || v == PriorityUrgent
}

// SourceQuality describes how much the connector evidence can be trusted.
type SourceQuality string

const (
	QualityObserved  SourceQuality = "observed"
	QualityConfirmed SourceQuality = "confirmed"
	QualityPartial   SourceQuality = "partial"
	QualityStale     SourceQuality = "stale"
	QualityUnknown   SourceQuality = "unknown"
)

func (v SourceQuality) Valid() bool {
	return v == QualityObserved || v == QualityConfirmed || v == QualityPartial || v == QualityStale || v == QualityUnknown
}

// IdentityState is deliberately conservative: only exact approved references
// may be marked verified.
type IdentityState string

const (
	IdentityVerified  IdentityState = "verified"
	IdentityAmbiguous IdentityState = "ambiguous"
	IdentityUnmatched IdentityState = "unmatched"
)

func (v IdentityState) Valid() bool {
	return v == IdentityVerified || v == IdentityAmbiguous || v == IdentityUnmatched
}

// MessageDirection identifies inbound and outbound history.
type MessageDirection string

const (
	DirectionInbound  MessageDirection = "inbound"
	DirectionOutbound MessageDirection = "outbound"
)

func (v MessageDirection) Valid() bool { return v == DirectionInbound || v == DirectionOutbound }

// Visibility prevents internal notes from being sent to a customer.
type Visibility string

const (
	VisibilityPublic   Visibility = "public"
	VisibilityInternal Visibility = "internal"
)

func (v Visibility) Valid() bool { return v == VisibilityPublic || v == VisibilityInternal }

// DeliveryState tracks a reply independently from the conversation state.
type DeliveryState string

const (
	DeliveryObserved DeliveryState = "observed"
	DeliveryDraft    DeliveryState = "draft"
	DeliveryQueued   DeliveryState = "queued"
	DeliverySent     DeliveryState = "sent"
	DeliveryAccepted DeliveryState = "accepted"
	DeliveryFailed   DeliveryState = "failed"
	DeliveryUnknown  DeliveryState = "unknown"
)

func (v DeliveryState) Valid() bool {
	return v == DeliveryObserved || v == DeliveryDraft || v == DeliveryQueued || v == DeliverySent || v == DeliveryAccepted || v == DeliveryFailed || v == DeliveryUnknown
}

// ModerationState is the minimum safety state required before public delivery.
type ModerationState string

const (
	ModerationPending  ModerationState = "pending"
	ModerationApproved ModerationState = "approved"
	ModerationBlocked  ModerationState = "blocked"
	ModerationSpam     ModerationState = "spam"
)

func (v ModerationState) Valid() bool {
	return v == ModerationPending || v == ModerationApproved || v == ModerationBlocked || v == ModerationSpam
}

// CustomerRef is a minimized, tenant-scoped link and not a second customer
// master. Full provider payloads and raw personal contacts are never accepted.
type CustomerRef struct {
	ID                string        `json:"id"`
	SourceSystem      string        `json:"source_system"`
	RemoteCustomerRef string        `json:"remote_customer_ref"`
	DisplayNameMask   string        `json:"display_name_mask,omitempty"`
	ContactMask       string        `json:"contact_mask,omitempty"`
	IdentityState     IdentityState `json:"identity_state"`
	ConfidenceBPS     int           `json:"confidence_bps"`
	Source            string        `json:"source"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	Version           int64         `json:"version"`
}

func (v CustomerRef) Validate() error {
	if !validRef(v.ID) || !validRef(v.SourceSystem) || !validRef(v.RemoteCustomerRef) || !v.IdentityState.Valid() || v.ConfidenceBPS < 0 || v.ConfidenceBPS > 10000 || !validRef(v.Source) || !validUTC(v.CreatedAt) || !validUTC(v.UpdatedAt) || v.Version < 1 {
		return ErrInvalid
	}
	if len(v.DisplayNameMask) > 160 || len(v.ContactMask) > 160 || strings.ContainsAny(v.DisplayNameMask+v.ContactMask, "\r\n\x00") {
		return ErrInvalid
	}
	return nil
}

// Conversation is the unified operator thread. Linked IDs reference canonical
// order, product, return and claim aggregates and are never copied aggregates.
type Conversation struct {
	ID                 string            `json:"id"`
	SourceSystem       string            `json:"source_system"`
	AccountID          string            `json:"account_id"`
	RemoteThreadID     string            `json:"remote_thread_id"`
	Type               ConversationType  `json:"type"`
	State              ConversationState `json:"state"`
	Priority           Priority          `json:"priority"`
	CustomerRefID      string            `json:"customer_ref_id,omitempty"`
	IdentityState      IdentityState     `json:"identity_state"`
	Subject            string            `json:"subject,omitempty"`
	OrderID            string            `json:"order_id,omitempty"`
	OrderItemID        string            `json:"order_item_id,omitempty"`
	ProductID          string            `json:"product_id,omitempty"`
	OfferID            string            `json:"offer_id,omitempty"`
	ReturnID           string            `json:"return_id,omitempty"`
	ClaimID            string            `json:"claim_id,omitempty"`
	AssigneeID         string            `json:"assignee_id,omitempty"`
	TeamID             string            `json:"team_id,omitempty"`
	SLAState           string            `json:"sla_state"`
	FirstResponseDueAt time.Time         `json:"first_response_due_at,omitempty"`
	ResolutionDueAt    time.Time         `json:"resolution_due_at,omitempty"`
	LastMessageAt      time.Time         `json:"last_message_at"`
	SourceQuality      SourceQuality     `json:"source_quality"`
	ModerationState    ModerationState   `json:"moderation_state"`
	Version            int64             `json:"version"`
	CreatedAt          time.Time         `json:"created_at"`
	UpdatedAt          time.Time         `json:"updated_at"`
}

func (v Conversation) Validate() error {
	if !validRef(v.ID) || !validRef(v.SourceSystem) || !validRef(v.AccountID) || !validRef(v.RemoteThreadID) || !v.Type.Valid() || !v.State.Valid() || !v.Priority.Valid() || !v.IdentityState.Valid() || !v.SourceQuality.Valid() || !v.ModerationState.Valid() || !validUTC(v.LastMessageAt) || !validUTC(v.CreatedAt) || !validUTC(v.UpdatedAt) || v.Version < 1 {
		return ErrInvalid
	}
	if len(v.Subject) > MaxSubjectLength || !safeText(v.Subject) || !validOptionalRef(v.CustomerRefID) || !validOptionalRef(v.OrderID) || !validOptionalRef(v.OrderItemID) || !validOptionalRef(v.ProductID) || !validOptionalRef(v.OfferID) || !validOptionalRef(v.ReturnID) || !validOptionalRef(v.ClaimID) || !validOptionalRef(v.AssigneeID) || !validOptionalRef(v.TeamID) {
		return ErrInvalid
	}
	if v.SLAState != "" && !validSLAState(v.SLAState) {
		return ErrInvalid
	}
	if !validOptionalUTC(v.FirstResponseDueAt) || !validOptionalUTC(v.ResolutionDueAt) {
		return ErrInvalid
	}
	return nil
}

// Message is immutable normalized content. SafeText is sanitized before it is
// persisted; ContentDigest permits reconciliation without retaining raw HTML.
type Message struct {
	ID              string           `json:"id"`
	ConversationID  string           `json:"conversation_id"`
	RemoteMessageID string           `json:"remote_message_id,omitempty"`
	Direction       MessageDirection `json:"direction"`
	Visibility      Visibility       `json:"visibility"`
	DeliveryState   DeliveryState    `json:"delivery_state"`
	SafeText        string           `json:"safe_text"`
	ContentDigest   string           `json:"content_digest"`
	Language        string           `json:"language,omitempty"`
	ModerationState ModerationState  `json:"moderation_state"`
	IdentityState   IdentityState    `json:"identity_state"`
	OrderID         string           `json:"order_id,omitempty"`
	ProductID       string           `json:"product_id,omitempty"`
	OccurredAt      time.Time        `json:"occurred_at"`
	ReceivedAt      time.Time        `json:"received_at"`
	CreatedAt       time.Time        `json:"created_at"`
}

func (v Message) Validate() error {
	if !validRef(v.ID) || !validRef(v.ConversationID) || !validOptionalRef(v.RemoteMessageID) || !v.Direction.Valid() || !v.Visibility.Valid() || !v.DeliveryState.Valid() || !v.ModerationState.Valid() || !v.IdentityState.Valid() || !validText(v.SafeText) || v.ContentDigest != Digest(v.SafeText) || !validUTC(v.OccurredAt) || !validUTC(v.ReceivedAt) || !validUTC(v.CreatedAt) {
		return ErrInvalid
	}
	if !validOptionalRef(v.OrderID) || !validOptionalRef(v.ProductID) || len(v.Language) > 16 || !validLanguage(v.Language) {
		return ErrInvalid
	}
	return nil
}

// Reply is the durable outbound intent/receipt. Internal notes are never
// eligible for remote delivery.
type Reply struct {
	ID             string        `json:"id"`
	ConversationID string        `json:"conversation_id"`
	Visibility     Visibility    `json:"visibility"`
	Origin         string        `json:"origin"`
	SafeText       string        `json:"safe_text"`
	ContentDigest  string        `json:"content_digest"`
	TemplateID     string        `json:"template_id,omitempty"`
	ApprovalRef    string        `json:"approval_ref,omitempty"`
	IdempotencyKey string        `json:"idempotency_key"`
	DeliveryState  DeliveryState `json:"delivery_state"`
	RemoteReceipt  string        `json:"remote_receipt,omitempty"`
	ErrorCode      string        `json:"error_code,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	Version        int64         `json:"version"`
}

func (v Reply) Validate() error {
	if !validRef(v.ID) || !validRef(v.ConversationID) || !v.Visibility.Valid() || (v.Origin != "human" && v.Origin != "template" && v.Origin != "ai_draft") || !validText(v.SafeText) || v.ContentDigest != Digest(v.SafeText) || !validRef(v.IdempotencyKey) || !v.DeliveryState.Valid() || !validOptionalRef(v.TemplateID) || !validOptionalRef(v.ApprovalRef) || !validOptionalRef(v.RemoteReceipt) || !validOptionalRef(v.ErrorCode) || !validUTC(v.CreatedAt) || !validUTC(v.UpdatedAt) || v.Version < 1 {
		return ErrInvalid
	}
	if v.Visibility == VisibilityInternal && v.DeliveryState != DeliveryDraft && v.DeliveryState != DeliveryObserved {
		return ErrInvalid
	}
	if v.Origin == "ai_draft" && v.DeliveryState != DeliveryDraft {
		return ErrAIDraftOnly
	}
	return nil
}

// Assignment is an append-only ownership transition.
type Assignment struct {
	ID              string    `json:"id"`
	ConversationID  string    `json:"conversation_id"`
	AssigneeID      string    `json:"assignee_id,omitempty"`
	TeamID          string    `json:"team_id,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	ExpectedVersion int64     `json:"expected_version"`
	CreatedAt       time.Time `json:"created_at"`
}

func (v Assignment) Validate() error {
	if !validRef(v.ID) || !validRef(v.ConversationID) || (!validOptionalRef(v.AssigneeID) && !validOptionalRef(v.TeamID)) || len(v.Reason) > 500 || !safeText(v.Reason) || v.ExpectedVersion < 1 || !validUTC(v.CreatedAt) {
		return ErrInvalid
	}
	return nil
}

// SLAPolicy stores versioned working-time values. Holidays use YYYY-MM-DD and
// are interpreted in the selected IANA timezone.
type SLAPolicy struct {
	ID                   string           `json:"id"`
	Version              int64            `json:"version"`
	ConversationType     ConversationType `json:"conversation_type"`
	Priority             Priority         `json:"priority"`
	Timezone             string           `json:"timezone"`
	FirstResponseMinutes int              `json:"first_response_minutes"`
	ResolutionMinutes    int              `json:"resolution_minutes"`
	Holidays             []string         `json:"holidays,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
}

func (v SLAPolicy) Validate() error {
	if !validRef(v.ID) || v.Version < 1 || !v.ConversationType.Valid() || !v.Priority.Valid() || v.FirstResponseMinutes < 1 || v.FirstResponseMinutes > 1000000 || v.ResolutionMinutes < v.FirstResponseMinutes || v.ResolutionMinutes > 2000000 || strings.TrimSpace(v.Timezone) == "" || !validUTC(v.CreatedAt) {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(v.Timezone); err != nil {
		return ErrInvalid
	}
	for _, holiday := range v.Holidays {
		if _, err := time.Parse("2006-01-02", holiday); err != nil {
			return ErrInvalid
		}
	}
	return nil
}

// Finding is an explainable local/remote drift item.
type Finding struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Kind           string    `json:"kind"`
	Severity       string    `json:"severity"`
	Status         string    `json:"status"`
	Explanation    string    `json:"explanation"`
	ExpectedDigest string    `json:"expected_digest,omitempty"`
	ObservedDigest string    `json:"observed_digest,omitempty"`
	DetectedAt     time.Time `json:"detected_at"`
	ResolvedAt     time.Time `json:"resolved_at,omitempty"`
}

func (v Finding) Validate() error {
	if !validRef(v.ID) || !validOptionalRef(v.ConversationID) || !validRef(v.Kind) || (v.Severity != "info" && v.Severity != "warn" && v.Severity != "block") || (v.Status != "open" && v.Status != "acknowledged" && v.Status != "resolved") || len(v.Explanation) < 1 || len(v.Explanation) > 500 || !safeText(v.Explanation) || !validOptionalDigest(v.ExpectedDigest) || !validOptionalDigest(v.ObservedDigest) || !validUTC(v.DetectedAt) || !validOptionalUTC(v.ResolvedAt) {
		return ErrInvalid
	}
	return nil
}

// InboundRecord is the sanitized connector input before persistence.
type InboundRecord struct {
	Conversation Conversation `json:"conversation"`
	Message      Message      `json:"message"`
	Customer     *CustomerRef `json:"customer,omitempty"`
	Fingerprint  string       `json:"fingerprint"`
}

func (v InboundRecord) Validate() error {
	if err := v.Conversation.Validate(); err != nil {
		return err
	}
	if err := v.Message.Validate(); err != nil || v.Message.ConversationID != v.Conversation.ID {
		return ErrInvalid
	}
	if v.Customer != nil && v.Customer.Validate() != nil {
		return ErrInvalid
	}
	if v.Fingerprint != Digest(v.Conversation.SourceSystem+"\x00"+v.Conversation.AccountID+"\x00"+v.Conversation.RemoteThreadID+"\x00"+v.Message.RemoteMessageID+"\x00"+v.Message.ContentDigest) {
		return ErrInvalid
	}
	return nil
}

// Filter controls bounded cursor queries in the inbox.
type Filter struct {
	State         ConversationState
	Type          ConversationType
	Priority      Priority
	AssigneeID    string
	TeamID        string
	CustomerRefID string
	Unresolved    bool
	SLAState      string
	Search        string
	AfterID       string
	Limit         int
}

// InboxPage is a cursor-paginated queue response.
type InboxPage struct {
	Items      []Conversation `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	HasMore    bool           `json:"-"`
}

// Thread contains safe thread detail and immutable message/reply history.
type Thread struct {
	Conversation Conversation `json:"conversation"`
	Customer     *CustomerRef `json:"customer,omitempty"`
	Messages     []Message    `json:"messages"`
	Replies      []Reply      `json:"replies"`
}

// TimelineEvent is a privacy-safe event in Customer 360.
type TimelineEvent struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	ReferenceID    string    `json:"reference_id"`
	ConversationID string    `json:"conversation_id,omitempty"`
	Summary        string    `json:"summary"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// Summary contains explainable queue/quality counters.
type Summary struct {
	Total          int           `json:"total"`
	Unread         int           `json:"unread"`
	Open           int           `json:"open"`
	Pending        int           `json:"pending"`
	Breached       int           `json:"breached"`
	Reviews        int           `json:"reviews"`
	Questions      int           `json:"questions"`
	Claims         int           `json:"claims"`
	UnknownReplies int           `json:"unknown_replies"`
	Quality        SourceQuality `json:"quality"`
}

// Digest returns a stable SHA-256 digest for safe content and reconciliation.
func Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// SanitizeText strips markup/control characters and unescapes entities. It is
// intentionally conservative and does not attempt to render user HTML.
func SanitizeText(value string) string {
	value = html.UnescapeString(value)
	var builder strings.Builder
	inTag := false
	for _, r := range value {
		switch {
		case r == '<':
			inTag = true
		case r == '>' && inTag:
			inTag = false
		case inTag:
		case r == '\x00' || unicode.IsControl(r) && r != '\n' && r != '\t':
		case r == '\n' || r == '\t' || !unicode.IsSpace(r) || builder.Len() > 0:
			builder.WriteRune(r)
		}
	}
	return strings.TrimSpace(builder.String())
}

// NewInbound normalizes untrusted connector text and creates an inbound record.
func NewInbound(conversation Conversation, message Message, customer *CustomerRef, now time.Time) (InboundRecord, error) {
	if now.IsZero() {
		return InboundRecord{}, ErrInvalid
	}
	now = now.UTC()
	conversation.State = StateUnread
	conversation.SourceQuality = defaultQuality(conversation.SourceQuality)
	conversation.ModerationState = defaultModeration(conversation.ModerationState)
	conversation.IdentityState = defaultIdentity(conversation.IdentityState)
	conversation.LastMessageAt = now
	conversation.UpdatedAt = now
	if conversation.CreatedAt.IsZero() {
		conversation.CreatedAt = now
	}
	message.Direction = DirectionInbound
	message.Visibility = VisibilityPublic
	message.DeliveryState = DeliveryObserved
	message.SafeText = SanitizeText(message.SafeText)
	message.ContentDigest = Digest(message.SafeText)
	message.ModerationState = defaultModeration(message.ModerationState)
	message.IdentityState = conversation.IdentityState
	message.ReceivedAt = now
	message.CreatedAt = now
	if message.OccurredAt.IsZero() {
		message.OccurredAt = now
	}
	record := InboundRecord{Conversation: conversation, Message: message, Customer: customer}
	record.Fingerprint = Digest(conversation.SourceSystem + "\x00" + conversation.AccountID + "\x00" + conversation.RemoteThreadID + "\x00" + message.RemoteMessageID + "\x00" + message.ContentDigest)
	if err := record.Validate(); err != nil {
		return InboundRecord{}, err
	}
	return record, nil
}

// BusinessDueAt adds working-time minutes without mutating the persisted UTC
// timestamps. It is deterministic for historical SLA policy versions.
func BusinessDueAt(start time.Time, minutes int, timezone string, holidays []string) (time.Time, error) {
	if start.IsZero() || minutes < 1 || timezone == "" {
		return time.Time{}, ErrInvalid
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, ErrInvalid
	}
	holidaySet := make(map[string]struct{}, len(holidays))
	for _, item := range holidays {
		if _, err := time.Parse("2006-01-02", item); err != nil {
			return time.Time{}, ErrInvalid
		}
		holidaySet[item] = struct{}{}
	}
	current := start.UTC().In(location)
	remaining := time.Duration(minutes) * time.Minute
	for remaining > 0 {
		day := current.Format("2006-01-02")
		workingDay := current.Weekday() != time.Saturday && current.Weekday() != time.Sunday
		if _, holiday := holidaySet[day]; holiday {
			workingDay = false
		}
		if workingDay {
			step := time.Minute
			if remaining < step {
				step = remaining
			}
			current = current.Add(step)
			remaining -= step
			continue
		}
		current = current.Add(24 * time.Hour)
		current = time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, location)
	}
	return current.UTC(), nil
}

// BuildTimeline returns a deterministic bounded timeline sorted by UTC time.
func BuildTimeline(events []TimelineEvent) ([]TimelineEvent, error) {
	if len(events) > MaxTimelineItems {
		return nil, ErrInvalid
	}
	result := append([]TimelineEvent(nil), events...)
	for _, event := range result {
		if !validRef(event.ID) || !validRef(event.Kind) || !validRef(event.ReferenceID) || len(event.Summary) > 500 || !safeText(event.Summary) || !validUTC(event.OccurredAt) {
			return nil, ErrInvalid
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].OccurredAt.Equal(result[j].OccurredAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].OccurredAt.Before(result[j].OccurredAt)
	})
	return result, nil
}

func validRef(value string) bool {
	if value == "" || len(value) > MaxReferencesLength {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9') && !strings.ContainsRune("._:/-", r) {
			return false
		}
	}
	return true
}

func validOptionalRef(value string) bool { return value == "" || validRef(value) }

func validText(value string) bool {
	return value != "" && len(value) <= MaxTextLength && safeText(value)
}

func safeText(value string) bool {
	return !strings.ContainsRune(value, '\x00') && !strings.Contains(value, "<script") && !strings.Contains(value, "javascript:")
}

func validUTC(value time.Time) bool { return !value.IsZero() && value.Equal(value.UTC()) }

func validOptionalUTC(value time.Time) bool { return value.IsZero() || validUTC(value) }

func validOptionalDigest(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validLanguage(value string) bool {
	if value == "" {
		return true
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && r != '-' {
			return false
		}
	}
	return true
}

func validSLAState(value string) bool {
	return value == "new" || value == "in_progress" || value == "waiting" || value == "escalated" || value == "breached" || value == "met"
}

func defaultQuality(value SourceQuality) SourceQuality {
	if value == "" {
		return QualityObserved
	}
	return value
}

func defaultIdentity(value IdentityState) IdentityState {
	if value == "" {
		return IdentityUnmatched
	}
	return value
}

func defaultModeration(value ModerationState) ModerationState {
	if value == "" {
		return ModerationPending
	}
	return value
}

// ValidateCursorRef returns a stable error for untrusted cursor values.
func ValidateCursorRef(value string) error {
	if value != "" && !validRef(value) {
		return fmt.Errorf("%w: invalid cursor", ErrInvalid)
	}
	return nil
}
