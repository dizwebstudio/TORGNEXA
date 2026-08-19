package notifications

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
)

type fixedIDs struct{ n int }

func (g *fixedIDs) NewID(prefix string) (string, error) {
	g.n++
	return prefix + "test_" + string(rune('0'+g.n)), nil
}

type memoryRepo struct {
	n          map[string]Notification
	prefs      map[string]Preference
	deliveries []Delivery
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{n: map[string]Notification{}, prefs: map[string]Preference{}}
}
func key(recipient, dedupe string) string         { return recipient + "|" + dedupe }
func prefKey(recipient string, ch Channel) string { return recipient + "|" + string(ch) }
func (m *memoryRepo) Upsert(_ context.Context, _ tenancy.Scope, n Notification) (Notification, Disposition, error) {
	k := key(n.RecipientID, n.DedupeKey)
	old, ok := m.n[k]
	if !ok {
		m.n[k] = n
		return n, DispositionCreated, nil
	}
	sameOccurrence := (n.SourceEventID != "" && n.SourceEventID == old.SourceEventID) || n.LastOccurredAt.Equal(old.LastOccurredAt)
	if sameOccurrence {
		return old, DispositionReplay, nil
	}
	disposition := DispositionDeduplicated
	if n.Severity.rank() > old.Severity.rank() {
		old.Severity = n.Severity
		old.ReadAt = nil
		disposition = DispositionEscalated
	}
	old.Title = n.Title
	old.Body = n.Body
	old.OccurrenceCount++
	if n.LastOccurredAt.After(old.LastOccurredAt) {
		old.LastOccurredAt = n.LastOccurredAt
	}
	old.UpdatedAt = n.UpdatedAt
	m.n[k] = old
	return old, disposition, nil
}
func (m *memoryRepo) List(_ context.Context, _ tenancy.Scope, recipient string, _ int) ([]Notification, error) {
	out := []Notification{}
	for _, n := range m.n {
		if n.RecipientID == recipient {
			out = append(out, n)
		}
	}
	return out, nil
}
func (m *memoryRepo) MarkRead(_ context.Context, _ tenancy.Scope, recipient, id string, now time.Time) (Notification, error) {
	for k, n := range m.n {
		if n.RecipientID == recipient && n.ID == id {
			n.ReadAt = &now
			n.UpdatedAt = now
			m.n[k] = n
			return n, nil
		}
	}
	return Notification{}, ErrNotFound
}
func (m *memoryRepo) PutPreference(_ context.Context, _ tenancy.Scope, p Preference) (Preference, error) {
	old, ok := m.prefs[prefKey(p.RecipientID, p.Channel)]
	if ok {
		p.Version = old.Version + 1
	}
	m.prefs[prefKey(p.RecipientID, p.Channel)] = p
	return p, nil
}
func (m *memoryRepo) Preference(_ context.Context, _ tenancy.Scope, r string, c Channel) (Preference, error) {
	p, ok := m.prefs[prefKey(r, c)]
	if !ok {
		return Preference{}, ErrNotFound
	}
	return p, nil
}
func (m *memoryRepo) RecordDelivery(_ context.Context, _ tenancy.Scope, d Delivery) error {
	attempt := 1
	for _, old := range m.deliveries {
		if old.NotificationID == d.NotificationID && old.Channel == d.Channel && old.Occurrence == d.Occurrence && old.Attempt >= attempt {
			attempt = old.Attempt + 1
		}
	}
	d.Attempt = attempt
	m.deliveries = append(m.deliveries, d)
	return nil
}
func (m *memoryRepo) Deliveries(_ context.Context, _ tenancy.Scope, recipient, id string) ([]Delivery, error) {
	owned := false
	for _, n := range m.n {
		if n.ID == id && n.RecipientID == recipient {
			owned = true
			break
		}
	}
	if !owned {
		return nil, ErrNotFound
	}
	out := []Delivery{}
	for _, d := range m.deliveries {
		if d.NotificationID == id {
			out = append(out, d)
		}
	}
	return out, nil
}

type captureProvider struct {
	channel  Channel
	calls    int
	err      error
	failures int
	last     Notification
}

func (p *captureProvider) Channel() Channel { return p.channel }
func (p *captureProvider) Deliver(_ context.Context, _ tenancy.Scope, n Notification) error {
	p.calls++
	p.last = n
	if p.failures > 0 {
		p.failures--
		return p.err
	}
	return p.err
}

type captureSink struct {
	deliveries []eventbus.Delivery
	err        error
}

func (s *captureSink) Handle(_ context.Context, d eventbus.Delivery) error {
	s.deliveries = append(s.deliveries, d)
	return s.err
}

func testScope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope("018f0000-0000-7000-8000-000000000001", "018f0000-0000-7000-8000-000000000002")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func testRequest(sev Severity, at time.Time) Request {
	return Request{RecipientID: "user_123", DedupeKey: "connector:ozon:offline", Severity: sev, Title: "Ozon connector offline", Body: "Reconnect the account.", EntityType: "connector_account", EntityID: "account_1", OccurredAt: at}
}

func TestNotifyDeduplicatesAndOnlyRedeliversOnSeverityEscalation(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	ui := &captureProvider{channel: ChannelWebUI}
	web := &captureProvider{channel: ChannelWebhook}
	svc, _ := NewService(repo, []Provider{ui, web}, &fixedIDs{})
	svc.clock = func() time.Time { return now }
	// External webhook is explicit opt-in.
	repo.prefs[prefKey("user_123", ChannelWebhook)] = Preference{RecipientID: "user_123", Channel: ChannelWebhook, Enabled: true, MinSeverity: SeverityWarning, Version: 1, UpdatedAt: now}
	first, err := svc.Notify(context.Background(), testScope(t), testRequest(SeverityWarning, now))
	if err != nil {
		t.Fatal(err)
	}
	if first.OccurrenceCount != 1 || ui.calls != 1 || web.calls != 1 {
		t.Fatalf("first=%+v ui=%d web=%d", first, ui.calls, web.calls)
	}
	dup, err := svc.Notify(context.Background(), testScope(t), testRequest(SeverityWarning, now.Add(time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if dup.ID != first.ID || dup.OccurrenceCount != 2 || ui.calls != 1 || web.calls != 1 {
		t.Fatalf("dedupe=%+v ui=%d web=%d", dup, ui.calls, web.calls)
	}
	critical, err := svc.Notify(context.Background(), testScope(t), testRequest(SeverityCritical, now.Add(2*time.Minute)))
	if err != nil {
		t.Fatal(err)
	}
	if critical.ID != first.ID || critical.Severity != SeverityCritical || critical.OccurrenceCount != 3 || ui.calls != 2 || web.calls != 2 {
		t.Fatalf("escalation=%+v ui=%d web=%d", critical, ui.calls, web.calls)
	}
}

func TestPreferencesDefaultToInboxAndExternalWebhookOptIn(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	ui := &captureProvider{channel: ChannelWebUI}
	web := &captureProvider{channel: ChannelWebhook}
	svc, _ := NewService(repo, []Provider{ui, web}, &fixedIDs{})
	svc.clock = func() time.Time { return now }
	n, err := svc.Notify(context.Background(), testScope(t), testRequest(SeverityCritical, now))
	if err != nil {
		t.Fatal(err)
	}
	if ui.calls != 1 || web.calls != 0 {
		t.Fatalf("ui=%d webhook=%d", ui.calls, web.calls)
	}
	ds, _ := repo.Deliveries(context.Background(), testScope(t), "user_123", n.ID)
	if len(ds) != 2 || ds[0].Status != DeliverySucceeded || ds[1].Status != DeliverySuppressed {
		t.Fatalf("deliveries=%+v", ds)
	}
}

func TestProviderFailureUsesBoundedMachineCodeOnly(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	web := &captureProvider{channel: ChannelWebhook, err: errors.New("Bearer secret and remote body")}
	svc, _ := NewService(repo, []Provider{WebUIProvider{}, web}, &fixedIDs{})
	svc.clock = func() time.Time { return now }
	repo.prefs[prefKey("user_123", ChannelWebhook)] = Preference{RecipientID: "user_123", Channel: ChannelWebhook, Enabled: true, MinSeverity: SeverityInfo, Version: 1, UpdatedAt: now}
	n, err := svc.Notify(context.Background(), testScope(t), testRequest(SeverityWarning, now))
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("err=%v", err)
	}
	ds, _ := repo.Deliveries(context.Background(), testScope(t), "user_123", n.ID)
	if len(ds) != 2 || ds[1].Status != DeliveryFailed || ds[1].ErrorCode != "provider_failed" {
		t.Fatalf("deliveries=%+v", ds)
	}
}

func TestWebhookProviderDelegatesToDurableWebhookSink(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	sink := &captureSink{}
	p := WebhookProvider{Sink: sink}
	n := Notification{ID: "ntf_1", RecipientID: "user_1", DedupeKey: "approval:1", Severity: SeverityCritical, Title: "Approval required", Body: "Review change", EntityType: "approval", EntityID: "apr_1", OccurrenceCount: 1, FirstOccurredAt: now, LastOccurredAt: now, CreatedAt: now, UpdatedAt: now}
	if err := p.Deliver(context.Background(), testScope(t), n); err != nil {
		t.Fatal(err)
	}
	if len(sink.deliveries) != 1 {
		t.Fatalf("deliveries=%d", len(sink.deliveries))
	}
	d := sink.deliveries[0]
	if err := d.Validate(); err != nil {
		t.Fatalf("invalid delivery: %v", err)
	}
	if got := d.Event.Type.String(); got != "platform.notifications.notification_created.v1" {
		t.Fatalf("type=%s", got)
	}
	if d.Event.OrganizationID == "" || d.Event.WorkspaceID == "" {
		t.Fatal("tenant scope missing")
	}
}

func TestPreferenceSeverityThreshold(t *testing.T) {
	p := Preference{RecipientID: "user_1", Channel: ChannelWebhook, Enabled: true, MinSeverity: SeverityWarning, Version: 1, UpdatedAt: time.Now().UTC()}
	if p.Allows(SeverityInfo) || !p.Allows(SeverityWarning) || !p.Allows(SeverityCritical) {
		t.Fatalf("threshold broken")
	}
}

func TestPreferenceCategoriesAndQuietHours(t *testing.T) {
	now := time.Date(2026, 8, 10, 20, 30, 0, 0, time.UTC) // 23:30 Moscow
	n := testRequest(SeverityWarning, now)
	stored := Notification{ID: "ntf_quiet", RecipientID: n.RecipientID, DedupeKey: n.DedupeKey, Severity: n.Severity, Title: n.Title, Body: n.Body, EntityType: "order", EntityID: "order_1", OccurrenceCount: 1, FirstOccurredAt: now, LastOccurredAt: now, CreatedAt: now, UpdatedAt: now}
	p := Preference{RecipientID: n.RecipientID, Channel: ChannelEmail, Enabled: true, MinSeverity: SeverityInfo, Categories: []string{"commerce"}, QuietEnabled: true, QuietStart: "22:00", QuietEnd: "08:00", Timezone: "Europe/Moscow", Version: 1, UpdatedAt: now}
	if p.AllowsNotification(stored, now) {
		t.Fatal("warning must be suppressed during quiet hours")
	}
	stored.Severity = SeverityCritical
	if !p.AllowsNotification(stored, now) {
		t.Fatal("critical notification must bypass quiet hours")
	}
	stored.Severity, stored.EntityType = SeverityWarning, "security"
	if p.AllowsNotification(stored, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)) {
		t.Fatal("excluded category must be suppressed")
	}
}

func TestProviderFailureCanReplaySameOccurrenceWithoutIncrementingCount(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	web := &captureProvider{channel: ChannelWebhook, failures: 1, err: errors.New("temporary durable enqueue failure")}
	svc, _ := NewService(repo, []Provider{WebUIProvider{}, web}, &fixedIDs{})
	svc.clock = func() time.Time { return now }
	repo.prefs[prefKey("user_123", ChannelWebhook)] = Preference{RecipientID: "user_123", Channel: ChannelWebhook, Enabled: true, MinSeverity: SeverityInfo, Version: 1, UpdatedAt: now}
	req := testRequest(SeverityWarning, now)
	req.SourceEventID = "evt_retry_1"
	req.SourceEventType = eventbus.EventType("platform.orders.order_changed.v1")

	first, err := svc.Notify(context.Background(), testScope(t), req)
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("first err=%v", err)
	}
	if first.OccurrenceCount != 1 || web.calls != 1 {
		t.Fatalf("first=%+v calls=%d", first, web.calls)
	}
	web.err = nil
	second, err := svc.Notify(context.Background(), testScope(t), req)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.OccurrenceCount != 1 || web.calls != 2 {
		t.Fatalf("replay=%+v calls=%d", second, web.calls)
	}
	ds, err := repo.Deliveries(context.Background(), testScope(t), "user_123", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	var webhook []Delivery
	for _, d := range ds {
		if d.Channel == ChannelWebhook {
			webhook = append(webhook, d)
		}
	}
	if len(webhook) != 2 || webhook[0].Attempt != 1 || webhook[0].Status != DeliveryFailed || webhook[1].Attempt != 2 || webhook[1].Status != DeliverySucceeded {
		t.Fatalf("webhook attempts=%+v", webhook)
	}
}

func TestEnabledChannelWithoutProviderIsFailure(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	svc, err := NewService(repo, []Provider{WebUIProvider{}}, &fixedIDs{})
	if err != nil {
		t.Fatal(err)
	}
	svc.clock = func() time.Time { return now }
	repo.prefs[prefKey("user_123", ChannelWebhook)] = Preference{RecipientID: "user_123", Channel: ChannelWebhook, Enabled: true, MinSeverity: SeverityInfo, Version: 1, UpdatedAt: now}
	n, err := svc.Notify(context.Background(), testScope(t), testRequest(SeverityCritical, now))
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("err=%v", err)
	}
	ds, err := repo.Deliveries(context.Background(), testScope(t), "user_123", n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 2 || ds[1].Status != DeliveryFailed || ds[1].ErrorCode != "provider_unavailable" {
		t.Fatalf("deliveries=%+v", ds)
	}
}

func TestNotificationRejectsCredentialShapedPresentationText(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	repo := newMemoryRepo()
	svc, err := NewService(repo, []Provider{WebUIProvider{}}, &fixedIDs{})
	if err != nil {
		t.Fatal(err)
	}
	svc.clock = func() time.Time { return now }
	req := testRequest(SeverityWarning, now)
	req.Body = "authorization=Bearer should-never-leave-this-boundary"
	if _, err := svc.Notify(context.Background(), testScope(t), req); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestDeliveryAttemptBound(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	d := Delivery{NotificationID: "ntf_1", Channel: ChannelWebhook, Status: DeliveryFailed, ErrorCode: "provider_failed", Occurrence: 1, Attempt: MaxDeliveryAttempts + 1, AttemptedAt: now}
	if !errors.Is(d.Validate(), ErrInvalid) {
		t.Fatal("expected bounded attempts to fail closed")
	}
}

func TestFailedDeliveryRequiresMachineErrorCode(t *testing.T) {
	now := time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)
	d := Delivery{NotificationID: "ntf_1", Channel: ChannelWebhook, Status: DeliveryFailed, Occurrence: 1, Attempt: 1, AttemptedAt: now}
	if !errors.Is(d.Validate(), ErrInvalid) {
		t.Fatal("failed delivery without machine error code must be invalid")
	}
}
