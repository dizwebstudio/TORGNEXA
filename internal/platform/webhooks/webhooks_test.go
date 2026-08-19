package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	orgID = "018f0000-0000-7000-8000-000000000001"
	wsID  = "018f0000-0000-7000-8000-000000000002"
)

func testScope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope(orgID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func testType(t *testing.T) eventbus.EventType {
	t.Helper()
	v, err := eventbus.ParseEventType("commerce.orders.order_created.v1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func testIncoming(t *testing.T, at time.Time) eventbus.Delivery {
	t.Helper()
	instant, err := domain.NewUTCInstant(at)
	if err != nil {
		t.Fatal(err)
	}
	return eventbus.Delivery{Event: eventbus.Event{ID: "evt_test_063", Type: testType(t), OccurredAt: instant, OrganizationID: orgID, WorkspaceID: wsID, EntityType: "order", EntityID: "order_test_063", Source: "orders", Data: []byte(`{"order_id":"order_test_063"}`)}, Attempt: 1, FirstObservedAt: instant}
}

func TestSignatureContractCurrentPreviousAndReplayWindow(t *testing.T) {
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	body := []byte(`{"delivery_id":"whd_test"}`)
	current := []byte("0123456789abcdef0123456789abcdef")
	previous := []byte("abcdef0123456789abcdef0123456789")
	headers, err := Sign(current, "whd_test", now, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(current, nil, headers, body, now.Add(4*time.Minute), DefaultReplayWindow); err != nil {
		t.Fatalf("verify current: %v", err)
	}
	oldHeaders, err := Sign(previous, "whd_test", now, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifySignature(current, previous, oldHeaders, body, now.Add(time.Minute), DefaultReplayWindow); err != nil {
		t.Fatalf("verify previous during overlap: %v", err)
	}
	if err := VerifySignature(current, previous, headers, append([]byte(nil), body[:len(body)-1]...), now, DefaultReplayWindow); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered body accepted: %v", err)
	}
	if err := VerifySignature(current, previous, headers, body, now.Add(DefaultReplayWindow+time.Second), DefaultReplayWindow); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale signature accepted: %v", err)
	}
	headers["TORGNEXA-Signature"] = "v1=" + strings.Repeat("00", 32)
	if err := VerifySignature(current, previous, headers, body, now, DefaultReplayWindow); !errors.Is(err, ErrConflict) {
		t.Fatalf("forged signature accepted: %v", err)
	}
}

func TestBackoffIsBoundedExponentialAndRetryBudgetExplicit(t *testing.T) {
	p := DefaultBackoff()
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i, d := range want {
		if got := p.Delay(i + 1); got != d {
			t.Fatalf("attempt %d delay %s want %s", i+1, got, d)
		}
	}
	if got := p.Delay(32); got != 15*time.Minute {
		t.Fatalf("cap=%s", got)
	}
	if p.MaxAttempts != 8 || p.DisableAfter != 5 {
		t.Fatalf("unexpected default policy: %#v", p)
	}
}

type fakeResolver map[string][]net.IP

type sequenceResolver struct {
	answers [][]net.IP
	calls   int
}

func (r *sequenceResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	idx := r.calls
	r.calls++
	if idx >= len(r.answers) {
		idx = len(r.answers) - 1
	}
	if idx < 0 {
		return nil, errors.New("no DNS answer")
	}
	out := make([]net.IPAddr, 0, len(r.answers[idx]))
	for _, ip := range r.answers[idx] {
		out = append(out, net.IPAddr{IP: ip})
	}
	return out, nil
}

func (r fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	ips := r[host]
	if len(ips) == 0 {
		return nil, errors.New("not found")
	}
	out := make([]net.IPAddr, len(ips))
	for i, ip := range ips {
		out[i] = net.IPAddr{IP: ip}
	}
	return out, nil
}
func TestEndpointPolicyRejectsSSRFAndRedirectInputs(t *testing.T) {
	p := NewEndpointPolicy(fakeResolver{"public.example": {net.ParseIP("8.8.8.8")}, "private.example": {net.ParseIP("10.0.0.7")}, "mixed.example": {net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}})
	if _, err := p.Resolve(context.Background(), "https://public.example/hooks"); err != nil {
		t.Fatalf("public endpoint: %v", err)
	}
	for _, raw := range []string{"http://public.example/hooks", "https://127.0.0.1/hooks", "https://private.example/hooks", "https://mixed.example/hooks", "https://user:pass@public.example/hooks", "https://public.example/hooks?token=x", "https://public.example:8443/hooks"} {
		if _, err := p.Resolve(context.Background(), raw); !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("unsafe endpoint accepted %q: %v", raw, err)
		}
	}
}

type fixedIDs struct {
	ids []string
	i   int
}

func (g *fixedIDs) NewID(string) (string, error) {
	if g.i >= len(g.ids) {
		return "", errors.New("no id")
	}
	v := g.ids[g.i]
	g.i++
	return v, nil
}

type fakeSecrets struct {
	values  map[secrets.Reference][]byte
	n       int
	revoked map[secrets.Reference]bool
}

func newFakeSecrets() *fakeSecrets {
	return &fakeSecrets{values: map[secrets.Reference][]byte{}, revoked: map[secrets.Reference]bool{}}
}
func (f *fakeSecrets) Create(_ context.Context, scope tenancy.Scope, class secrets.Class, material []byte) (secrets.Metadata, error) {
	if !scope.Valid() || class != secrets.ClassWebhookSigning || len(material) == 0 {
		return secrets.Metadata{}, secrets.ErrInvalidMaterial
	}
	f.n++
	ref := secrets.Reference("sec:v1:" + strings.Repeat("0", 30) + string([]byte{'0' + byte(f.n/10), '0' + byte(f.n%10)}))
	if !ref.Valid() {
		return secrets.Metadata{}, secrets.ErrInvalidReference
	}
	f.values[ref] = append([]byte(nil), material...)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	return secrets.Metadata{Reference: ref, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Class: class, Status: secrets.StatusActive, CurrentVersion: 1, CreatedAt: now, UpdatedAt: now}, nil
}
func (f *fakeSecrets) Use(_ context.Context, _ tenancy.Scope, ref secrets.Reference, cb func([]byte) error) error {
	if f.revoked[ref] {
		return secrets.ErrRevoked
	}
	v, ok := f.values[ref]
	if !ok {
		return secrets.ErrNotFound
	}
	return cb(append([]byte(nil), v...))
}
func (f *fakeSecrets) Describe(context.Context, tenancy.Scope, secrets.Reference) (secrets.Metadata, error) {
	return secrets.Metadata{}, nil
}
func (f *fakeSecrets) Rotate(context.Context, tenancy.Scope, secrets.Reference, []byte) (secrets.Metadata, error) {
	return secrets.Metadata{}, errors.New("unused")
}
func (f *fakeSecrets) Revoke(_ context.Context, _ tenancy.Scope, ref secrets.Reference) (secrets.Metadata, error) {
	f.revoked[ref] = true
	return secrets.Metadata{Reference: ref, Status: secrets.StatusRevoked}, nil
}

type memoryRepo struct {
	sub        Subscription
	deliveries map[string]Delivery
	attempts   []AttemptResult
	duplicate  map[string]bool
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{deliveries: map[string]Delivery{}, duplicate: map[string]bool{}}
}
func (m *memoryRepo) CreateSubscription(_ context.Context, _ tenancy.Scope, s Subscription) error {
	m.sub = s
	return nil
}
func (m *memoryRepo) Subscription(context.Context, tenancy.Scope, string) (Subscription, error) {
	if m.sub.ID == "" {
		return Subscription{}, ErrNotFound
	}
	return m.sub, nil
}
func (m *memoryRepo) ListSubscriptions(context.Context, tenancy.Scope) ([]Subscription, error) {
	return []Subscription{m.sub}, nil
}
func (m *memoryRepo) DisableSubscription(_ context.Context, _ tenancy.Scope, id string, now time.Time) (Subscription, error) {
	if m.sub.ID == "" || m.sub.ID != id {
		return Subscription{}, ErrNotFound
	}
	if m.sub.Status == SubscriptionDisabled {
		return m.sub, nil
	}
	if m.sub.Status != SubscriptionActive {
		return Subscription{}, ErrConflict
	}
	m.sub.Status = SubscriptionDisabled
	m.sub.Version++
	m.sub.UpdatedAt = now
	return m.sub, nil
}
func (m *memoryRepo) RotateSubscription(_ context.Context, _ tenancy.Scope, _ string, current, previous secrets.Reference, until, now time.Time) (Subscription, error) {
	m.sub.PreviousSigningSecret = previous
	m.sub.PreviousValidUntil = &until
	m.sub.SigningSecret = current
	m.sub.UpdatedAt = now
	m.sub.Version++
	return m.sub, nil
}
func (m *memoryRepo) ClearPreviousSecret(_ context.Context, _ tenancy.Scope, _ string, previous secrets.Reference, now time.Time) error {
	if m.sub.PreviousSigningSecret != previous {
		return ErrConflict
	}
	m.sub.PreviousSigningSecret = ""
	m.sub.PreviousValidUntil = nil
	m.sub.UpdatedAt = now
	return nil
}
func (m *memoryRepo) MatchingSubscriptions(_ context.Context, _ tenancy.Scope, t eventbus.EventType) ([]Subscription, error) {
	if m.sub.Accepts(t) {
		return []Subscription{m.sub}, nil
	}
	return nil, nil
}
func (m *memoryRepo) Enqueue(_ context.Context, _ tenancy.Scope, d Delivery) (bool, error) {
	key := d.SubscriptionID + "/" + d.EventID
	if m.duplicate[key] {
		return false, nil
	}
	m.duplicate[key] = true
	m.deliveries[d.ID] = d
	return true, nil
}
func (m *memoryRepo) Claim(_ context.Context, _ tenancy.Scope, worker string, now time.Time, lease time.Duration) (Delivery, error) {
	for id, d := range m.deliveries {
		if d.Status == DeliveryPending && !d.AvailableAt.After(now) {
			d.Status = DeliveryInflight
			d.Attempt++
			d.LeaseToken = worker + ":lease"
			x := now.Add(lease)
			d.LeaseExpiresAt = &x
			d.ConsecutivePermanentFailures = m.sub.ConsecutiveFailures
			m.deliveries[id] = d
			return d, nil
		}
	}
	return Delivery{}, ErrNoDelivery
}
func (m *memoryRepo) Complete(_ context.Context, _ tenancy.Scope, a AttemptResult) error {
	d := m.deliveries[a.DeliveryID]
	if d.LeaseToken != a.LeaseToken || d.Attempt != a.Attempt {
		return ErrConflict
	}
	m.attempts = append(m.attempts, a)
	d.LeaseToken = ""
	d.LeaseExpiresAt = nil
	switch a.Outcome {
	case OutcomeSucceeded:
		d.Status = DeliverySucceeded
		m.sub.ConsecutiveFailures = 0
	case OutcomeRetry:
		d.Status = DeliveryPending
		d.AvailableAt = *a.NextAvailableAt
	case OutcomeDLQ:
		d.Status = DeliveryDLQ
		if a.ErrorCode == "http_permanent" || a.ErrorCode == "endpoint_unsafe" {
			m.sub.ConsecutiveFailures++
			if a.DisableSubscription {
				m.sub.Status = SubscriptionDisabled
			}
		}
	}
	m.deliveries[d.ID] = d
	return nil
}
func (m *memoryRepo) Delivery(_ context.Context, _ tenancy.Scope, id string) (Delivery, error) {
	d, ok := m.deliveries[id]
	if !ok {
		return Delivery{}, ErrNotFound
	}
	return d, nil
}
func (m *memoryRepo) Replay(_ context.Context, _ tenancy.Scope, source, newID string, now time.Time) (Delivery, error) {
	d, ok := m.deliveries[source]
	if !ok {
		return Delivery{}, ErrNotFound
	}
	var e Envelope
	if err := jsonUnmarshal(d.Body, &e); err != nil {
		return Delivery{}, err
	}
	e.DeliveryID = newID
	body, err := e.Marshal()
	if err != nil {
		return Delivery{}, err
	}
	d.ID = newID
	d.Body = body
	d.Status = DeliveryPending
	d.Attempt = 0
	d.ReplayOf = source
	d.AvailableAt = now
	d.CreatedAt = now
	d.SigningSecret = m.sub.SigningSecret
	d.Endpoint = m.sub.Endpoint
	m.deliveries[newID] = d
	return d, nil
}
func (m *memoryRepo) History(context.Context, tenancy.Scope, string, int) ([]HistoryEntry, error) {
	return nil, nil
}

// keeps the test fixture's JSON dependency localized without exposing it in production code.
func jsonUnmarshal(raw []byte, v any) error { return json.Unmarshal(raw, v) }

type fakeTransport struct {
	results  []SendResult
	calls    int
	headers  Headers
	endpoint Endpoint
}

func (t *fakeTransport) Send(_ context.Context, e Endpoint, _ []byte, h Headers) SendResult {
	t.endpoint = e
	t.headers = h
	idx := t.calls
	t.calls++
	if idx >= len(t.results) {
		return SendResult{StatusCode: 204}
	}
	return t.results[idx]
}

func TestServiceEnqueuesIdempotentlyAndReplayGetsNewDeliveryID(t *testing.T) {
	scope := testScope(t)
	repo := newMemoryRepo()
	sec := newFakeSecrets()
	resolver := NewEndpointPolicy(fakeResolver{"hooks.example": {net.ParseIP("8.8.8.8")}})
	ids := &fixedIDs{ids: []string{"whd_00000000000000000000000000000001", "whd_00000000000000000000000000000002", "whd_00000000000000000000000000000003"}}
	svc, err := NewService(repo, sec, resolver, ids)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	_, err = svc.CreateSubscription(context.Background(), scope, "whs_test_063", "https://hooks.example/torgnexa", []eventbus.EventType{testType(t)}, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	incoming := testIncoming(t, now)
	if err := svc.Handle(context.Background(), incoming); err != nil {
		t.Fatal(err)
	}
	if err := svc.Handle(context.Background(), incoming); err != nil {
		t.Fatal(err)
	}
	if len(repo.deliveries) != 1 {
		t.Fatalf("duplicate event queued %d deliveries", len(repo.deliveries))
	}
	original := repo.deliveries["whd_00000000000000000000000000000001"]
	var env Envelope
	if err := jsonUnmarshal(original.Body, &env); err != nil {
		t.Fatal(err)
	}
	if env.DeliveryID != original.ID || env.EventID != incoming.Event.ID {
		t.Fatalf("wrong envelope %#v", env)
	}
	replay, err := svc.Replay(context.Background(), scope, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID == original.ID || replay.ReplayOf != original.ID {
		t.Fatalf("bad replay %#v", replay)
	}
	var replayEnv Envelope
	if err := jsonUnmarshal(replay.Body, &replayEnv); err != nil {
		t.Fatal(err)
	}
	if replayEnv.DeliveryID != replay.ID {
		t.Fatalf("replay body kept old delivery id: %#v", replayEnv)
	}
}

func TestDisableSubscriptionIsIdempotentAndRevokesSigningSecret(t *testing.T) {
	scope := testScope(t)
	repo := newMemoryRepo()
	sec := newFakeSecrets()
	resolver := NewEndpointPolicy(fakeResolver{"hooks.example": {net.ParseIP("8.8.8.8")}})
	svc, err := NewService(repo, sec, resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	sub, err := svc.CreateSubscription(context.Background(), scope, "whs_n8n_disable", "https://hooks.example/n8n", []eventbus.EventType{testType(t)}, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DisableSubscription(context.Background(), scope, sub.ID); err != nil {
		t.Fatal(err)
	}
	if repo.sub.Status != SubscriptionDisabled || !sec.revoked[sub.SigningSecret] {
		t.Fatalf("disable did not persist/revoke: sub=%#v revoked=%v", repo.sub, sec.revoked[sub.SigningSecret])
	}
	// Deactivation retries must remain safe. The in-memory repository models the same
	// idempotent semantics as the PostgreSQL CTE.
	if err := svc.DisableSubscription(context.Background(), scope, sub.ID); err != nil {
		t.Fatal(err)
	}
}

func TestWorkerRetriesSignsEachAttemptAndMovesToSuccess(t *testing.T) {
	scope := testScope(t)
	repo := newMemoryRepo()
	sec := newFakeSecrets()
	ref := secrets.Reference("sec:v1:" + strings.Repeat("1", 32))
	sec.values[ref] = []byte("0123456789abcdef0123456789abcdef")
	typ := testType(t)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo.sub = Subscription{ID: "whs_test", Endpoint: "https://hooks.example/t", EventTypes: []eventbus.EventType{typ}, Status: SubscriptionActive, SigningSecret: ref, Version: 1, CreatedAt: now, UpdatedAt: now}
	env := Envelope{DeliveryID: "whd_test", EventID: "evt_test", EventType: typ, OccurredAt: now, OrganizationID: orgID, WorkspaceID: wsID, Data: []byte(`{"x":1}`)}
	body, _ := env.Marshal()
	repo.deliveries["whd_test"] = Delivery{ID: "whd_test", SubscriptionID: "whs_test", EventID: "evt_test", EventType: typ, Endpoint: repo.sub.Endpoint, SigningSecret: ref, Body: body, Status: DeliveryPending, AvailableAt: now, CreatedAt: now}
	transport := &fakeTransport{results: []SendResult{{StatusCode: 503, Duration: 20 * time.Millisecond}, {StatusCode: 204, Duration: 15 * time.Millisecond}}}
	clock := now
	worker := &Worker{Repo: repo, Secrets: sec, Endpoints: NewEndpointPolicy(fakeResolver{"hooks.example": {net.ParseIP("8.8.8.8")}}), Transport: transport, Backoff: DefaultBackoff(), WorkerID: "worker-a", Lease: 30 * time.Second, Clock: func() time.Time { return clock }}
	ok, err := worker.ProcessOne(context.Background(), scope)
	if err != nil || !ok {
		t.Fatalf("first: %v %v", ok, err)
	}
	d := repo.deliveries["whd_test"]
	if d.Status != DeliveryPending || d.Attempt != 1 || !d.AvailableAt.Equal(now.Add(time.Second)) {
		t.Fatalf("retry state %#v", d)
	}
	if repo.attempts[0].ErrorCode != "http_retryable" {
		t.Fatalf("unsafe/unexpected code %#v", repo.attempts[0])
	}
	if err := VerifySignature(sec.values[ref], nil, transport.headers, body, now, DefaultReplayWindow); err != nil {
		t.Fatalf("worker signature: %v", err)
	}
	clock = d.AvailableAt
	ok, err = worker.ProcessOne(context.Background(), scope)
	if err != nil || !ok {
		t.Fatalf("second: %v %v", ok, err)
	}
	if repo.deliveries["whd_test"].Status != DeliverySucceeded || len(repo.attempts) != 2 {
		t.Fatalf("not succeeded %#v", repo.deliveries["whd_test"])
	}
}

func TestWorkerReResolvesDNSAndFailsClosedOnRebinding(t *testing.T) {
	scope := testScope(t)
	repo := newMemoryRepo()
	sec := newFakeSecrets()
	resolver := &sequenceResolver{answers: [][]net.IP{
		{net.ParseIP("8.8.8.8")},   // subscription admission
		{net.ParseIP("8.8.8.8")},   // first delivery attempt
		{net.ParseIP("127.0.0.1")}, // retry: DNS rebinding must fail closed
	}}
	svc, err := NewService(repo, sec, NewEndpointPolicy(resolver), &fixedIDs{ids: []string{"whd_rebind"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	if _, err := svc.CreateSubscription(context.Background(), scope, "whs_rebind", "https://hooks.example/t", []eventbus.EventType{testType(t)}, []byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}
	if err := svc.Handle(context.Background(), testIncoming(t, now)); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{results: []SendResult{{StatusCode: 503}}}
	worker := &Worker{Repo: repo, Secrets: sec, Endpoints: svc.endpoints, Transport: transport, Backoff: DefaultBackoff(), WorkerID: "worker-rebind", Clock: func() time.Time { return now }}
	if ok, err := worker.ProcessOne(context.Background(), scope); err != nil || !ok {
		t.Fatalf("first attempt: ok=%v err=%v", ok, err)
	}
	now = now.Add(DefaultBackoff().Delay(1))
	if ok, err := worker.ProcessOne(context.Background(), scope); err != nil || !ok {
		t.Fatalf("rebound attempt: ok=%v err=%v", ok, err)
	}
	d := repo.deliveries["whd_rebind"]
	if d.Status != DeliveryDLQ {
		t.Fatalf("rebound delivery status=%s, want dlq", d.Status)
	}
	if len(repo.attempts) != 2 || repo.attempts[1].ErrorCode != "endpoint_unsafe" {
		t.Fatalf("attempts=%#v", repo.attempts)
	}
	if transport.calls != 1 {
		t.Fatalf("transport called after unsafe re-resolution: %d", transport.calls)
	}
}

func TestWorkerDLQAndDisablesAfterRepeatedPermanentFailures(t *testing.T) {
	scope := testScope(t)
	repo := newMemoryRepo()
	sec := newFakeSecrets()
	ref := secrets.Reference("sec:v1:" + strings.Repeat("2", 32))
	sec.values[ref] = []byte("0123456789abcdef0123456789abcdef")
	typ := testType(t)
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	repo.sub = Subscription{ID: "whs_test", Endpoint: "https://hooks.example/t", EventTypes: []eventbus.EventType{typ}, Status: SubscriptionActive, SigningSecret: ref, ConsecutiveFailures: 4, Version: 1, CreatedAt: now, UpdatedAt: now}
	env := Envelope{DeliveryID: "whd_perm", EventID: "evt_perm", EventType: typ, OccurredAt: now, OrganizationID: orgID, WorkspaceID: wsID, Data: []byte(`{"x":1}`)}
	body, _ := env.Marshal()
	repo.deliveries["whd_perm"] = Delivery{ID: "whd_perm", SubscriptionID: "whs_test", EventID: "evt_perm", EventType: typ, Endpoint: repo.sub.Endpoint, SigningSecret: ref, Body: body, Status: DeliveryPending, AvailableAt: now, CreatedAt: now}
	worker := &Worker{Repo: repo, Secrets: sec, Endpoints: NewEndpointPolicy(fakeResolver{"hooks.example": {net.ParseIP("8.8.8.8")}}), Transport: &fakeTransport{results: []SendResult{{StatusCode: 410}}}, Backoff: DefaultBackoff(), WorkerID: "worker-a", Clock: func() time.Time { return now }}
	ok, err := worker.ProcessOne(context.Background(), scope)
	if err != nil || !ok {
		t.Fatalf("process: %v %v", ok, err)
	}
	if repo.deliveries["whd_perm"].Status != DeliveryDLQ || repo.sub.Status != SubscriptionDisabled || repo.sub.ConsecutiveFailures != 5 {
		t.Fatalf("permanent failure did not disable: %#v %#v", repo.deliveries["whd_perm"], repo.sub)
	}
}

func TestRotationKeepsPreviousSecretUntilBoundedOverlap(t *testing.T) {
	scope := testScope(t)
	repo := newMemoryRepo()
	sec := newFakeSecrets()
	svc, _ := NewService(repo, sec, NewEndpointPolicy(fakeResolver{"hooks.example": {net.ParseIP("8.8.8.8")}}), &fixedIDs{ids: []string{"unused"}})
	now := time.Date(2026, 8, 10, 9, 0, 0, 0, time.UTC)
	svc.clock = func() time.Time { return now }
	sub, err := svc.CreateSubscription(context.Background(), scope, "whs_rotate", "https://hooks.example/t", []eventbus.EventType{testType(t)}, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	old := sub.SigningSecret
	rotated, err := svc.RotateSigningSecret(context.Background(), scope, sub.ID, []byte("abcdef0123456789abcdef0123456789"), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if rotated.SigningSecret == old || rotated.PreviousSigningSecret != old || rotated.PreviousValidUntil == nil {
		t.Fatalf("bad rotation %#v", rotated)
	}
	if err := svc.FinalizeRotation(context.Background(), scope, sub.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("overlap finalized early: %v", err)
	}
	now = now.Add(11 * time.Minute)
	if err := svc.FinalizeRotation(context.Background(), scope, sub.ID); err != nil {
		t.Fatal(err)
	}
	if !sec.revoked[old] || repo.sub.PreviousSigningSecret != "" {
		t.Fatalf("old secret not retired")
	}
}
