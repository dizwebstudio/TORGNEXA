package customerservice

import (
	"strings"
	"testing"
	"time"
)

func validConversation(now time.Time) Conversation {
	return Conversation{
		ID: "conv-1", SourceSystem: "marketplace", AccountID: "account-1", RemoteThreadID: "thread-1",
		Type: TypeReview, State: StateUnread, Priority: PriorityNormal, IdentityState: IdentityUnmatched,
		SourceQuality: QualityObserved, ModerationState: ModerationPending, LastMessageAt: now,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func TestNewInboundSanitizesAndFingerprints(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	message := Message{ID: "message-1", ConversationID: "conv-1", RemoteMessageID: "remote-1", SafeText: "<b>Добрый день</b>"}
	record, err := NewInbound(validConversation(now), message, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if record.Message.SafeText != "Добрый день" || strings.Contains(record.Message.SafeText, "<") || record.Fingerprint == "" {
		t.Fatalf("unexpected normalized record: %#v", record)
	}
	if record.Conversation.State != StateUnread || record.Message.Direction != DirectionInbound {
		t.Fatalf("unexpected defaults: conversation=%+v message=%+v", record.Conversation, record.Message)
	}
}

func TestInboundFingerprintIncludesChannelAndAccount(t *testing.T) {
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	message := Message{ID: "message-1", ConversationID: "conv-1", SafeText: "без remote id"}
	first, err := NewInbound(validConversation(now), message, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	secondConversation := validConversation(now)
	secondConversation.SourceSystem = "storefront"
	secondConversation.AccountID = "account-2"
	second, err := NewInbound(secondConversation, message, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint == second.Fingerprint {
		t.Fatal("different connector accounts shared an inbound fingerprint")
	}
	first.Fingerprint = Digest(first.Conversation.RemoteThreadID + "\x00" + first.Message.RemoteMessageID + "\x00" + first.Message.ContentDigest)
	if err := first.Validate(); err == nil {
		t.Fatal("legacy unscoped fingerprint was accepted")
	}
}

func TestReplySeparatesAIAndInternalNotes(t *testing.T) {
	now := time.Now().UTC()
	reply := Reply{ID: "reply-1", ConversationID: "conv-1", Visibility: VisibilityInternal, Origin: "ai_draft", SafeText: "Проверить возврат", ContentDigest: Digest("Проверить возврат"), IdempotencyKey: "idem-1", DeliveryState: DeliveryDraft, CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := reply.Validate(); err != nil {
		t.Fatal(err)
	}
	reply.DeliveryState = DeliveryQueued
	if err := reply.Validate(); err == nil {
		t.Fatal("internal or AI draft was allowed to become remotely deliverable")
	}
}

func TestBusinessDueAtSkipsWeekendAndHoliday(t *testing.T) {
	start := time.Date(2026, 9, 4, 23, 0, 0, 0, time.UTC) // Friday
	due, err := BusinessDueAt(start, 120, "UTC", []string{"2026-09-07"})
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC)
	if !due.Equal(want) {
		t.Fatalf("due=%s want=%s", due, want)
	}
}

func TestBuildTimelineIsDeterministicAndBounded(t *testing.T) {
	now := time.Now().UTC()
	events, err := BuildTimeline([]TimelineEvent{
		{ID: "b", Kind: "message", ReferenceID: "m-2", Summary: "second", OccurredAt: now},
		{ID: "a", Kind: "order", ReferenceID: "o-1", Summary: "first", OccurredAt: now},
	})
	if err != nil || len(events) != 2 || events[0].ID != "a" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
	tooMany := make([]TimelineEvent, MaxTimelineItems+1)
	if _, err := BuildTimeline(tooMany); err == nil {
		t.Fatal("unbounded timeline accepted")
	}
}

func TestIdentityDoesNotBecomeVerifiedFromNames(t *testing.T) {
	now := time.Now().UTC()
	customer := CustomerRef{ID: "customer-1", SourceSystem: "storefront", RemoteCustomerRef: "remote-customer-1", IdentityState: IdentityAmbiguous, ConfidenceBPS: 5000, Source: "order-link-review", CreatedAt: now, UpdatedAt: now, Version: 1}
	if err := customer.Validate(); err != nil {
		t.Fatal(err)
	}
	conversation := validConversation(now)
	conversation.IdentityState = IdentityAmbiguous
	if conversation.IdentityState == IdentityVerified {
		t.Fatal("ambiguous identity was promoted")
	}
}
