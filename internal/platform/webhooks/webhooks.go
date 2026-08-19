// Package webhooks implements TORGNEXA's durable, tenant-scoped outbound webhook boundary.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/eventbus"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	MaxEventsPerSubscription = 128
	MaxEndpointRunes         = 2048
	MaxWebhookBodyBytes      = eventbus.MaxEventDataBytes + 32*1024
	MinSigningSecretBytes    = 32
	MaxSigningSecretBytes    = 64
	DefaultReplayWindow      = 5 * time.Minute
)

var (
	ErrInvalid    = errors.New("webhooks: invalid value")
	ErrNotFound   = errors.New("webhooks: not found")
	ErrConflict   = errors.New("webhooks: conflict")
	ErrUnsafeURL  = errors.New("webhooks: unsafe endpoint")
	ErrNoDelivery = errors.New("webhooks: no delivery available")
)

type SubscriptionStatus string

const (
	SubscriptionActive   SubscriptionStatus = "active"
	SubscriptionDisabled SubscriptionStatus = "disabled"
)

type Subscription struct {
	ID                    string               `json:"id"`
	Endpoint              string               `json:"endpoint"`
	EventTypes            []eventbus.EventType `json:"event_types"`
	Status                SubscriptionStatus   `json:"status"`
	SigningSecret         secrets.Reference    `json:"-"`
	PreviousSigningSecret secrets.Reference    `json:"-"`
	PreviousValidUntil    *time.Time           `json:"-"`
	ConsecutiveFailures   int                  `json:"consecutive_failures"`
	Version               int64                `json:"version"`
	CreatedAt             time.Time            `json:"created_at"`
	UpdatedAt             time.Time            `json:"updated_at"`
}

func (s Subscription) Validate() error {
	if !validID(s.ID) || !validEndpointText(s.Endpoint) || len(s.EventTypes) == 0 || len(s.EventTypes) > MaxEventsPerSubscription || !s.SigningSecret.Valid() || s.Version < 1 || !utc(s.CreatedAt) || !utc(s.UpdatedAt) || s.UpdatedAt.Before(s.CreatedAt) || s.ConsecutiveFailures < 0 {
		return ErrInvalid
	}
	if s.Status != SubscriptionActive && s.Status != SubscriptionDisabled {
		return ErrInvalid
	}
	seen := map[eventbus.EventType]struct{}{}
	for _, typ := range s.EventTypes {
		if typ.Validate() != nil {
			return ErrInvalid
		}
		if _, ok := seen[typ]; ok {
			return ErrInvalid
		}
		seen[typ] = struct{}{}
	}
	if s.PreviousSigningSecret != "" {
		if !s.PreviousSigningSecret.Valid() || s.PreviousValidUntil == nil || !utc(*s.PreviousValidUntil) || !s.PreviousValidUntil.After(s.UpdatedAt) {
			return ErrInvalid
		}
	} else if s.PreviousValidUntil != nil {
		return ErrInvalid
	}
	return nil
}

func (s Subscription) Accepts(t eventbus.EventType) bool {
	if s.Status != SubscriptionActive {
		return false
	}
	for _, allowed := range s.EventTypes {
		if allowed == t {
			return true
		}
	}
	return false
}

type Envelope struct {
	DeliveryID     string             `json:"delivery_id"`
	EventID        string             `json:"event_id"`
	EventType      eventbus.EventType `json:"event_type"`
	OccurredAt     time.Time          `json:"occurred_at"`
	OrganizationID string             `json:"organization_id"`
	WorkspaceID    string             `json:"workspace_id"`
	Data           json.RawMessage    `json:"data"`
}

func (e Envelope) Validate() error {
	if !validID(e.DeliveryID) || !validID(e.EventID) || e.EventType.Validate() != nil || !utc(e.OccurredAt) || e.OrganizationID == "" || e.WorkspaceID == "" || eventbus.ValidateData(e.Data) != nil {
		return ErrInvalid
	}
	return nil
}
func (e Envelope) Marshal() ([]byte, error) {
	if e.Validate() != nil {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(e)
	if err != nil || len(raw) > MaxWebhookBodyBytes {
		return nil, ErrInvalid
	}
	return raw, nil
}

type DeliveryStatus string

const (
	DeliveryPending   DeliveryStatus = "pending"
	DeliveryInflight  DeliveryStatus = "inflight"
	DeliverySucceeded DeliveryStatus = "succeeded"
	DeliveryDLQ       DeliveryStatus = "dlq"
)

type Delivery struct {
	ID                           string
	SubscriptionID               string
	EventID                      string
	EventType                    eventbus.EventType
	Endpoint                     string
	SigningSecret                secrets.Reference
	Body                         []byte
	Status                       DeliveryStatus
	Attempt                      int
	AvailableAt                  time.Time
	LeaseToken                   string
	LeaseExpiresAt               *time.Time
	ReplayOf                     string
	CreatedAt                    time.Time
	ConsecutivePermanentFailures int
}

func (d Delivery) Validate() error {
	if !validID(d.ID) || !validID(d.SubscriptionID) || !validID(d.EventID) || d.EventType.Validate() != nil || !validEndpointText(d.Endpoint) || !d.SigningSecret.Valid() || len(d.Body) == 0 || len(d.Body) > MaxWebhookBodyBytes || d.Attempt < 0 || !utc(d.AvailableAt) || !utc(d.CreatedAt) {
		return ErrInvalid
	}
	switch d.Status {
	case DeliveryPending, DeliveryInflight, DeliverySucceeded, DeliveryDLQ:
	default:
		return ErrInvalid
	}
	if d.ReplayOf != "" && !validID(d.ReplayOf) {
		return ErrInvalid
	}
	return nil
}

type AttemptOutcome string

const (
	OutcomeSucceeded AttemptOutcome = "succeeded"
	OutcomeRetry     AttemptOutcome = "retry"
	OutcomeDLQ       AttemptOutcome = "dlq"
)

type AttemptResult struct {
	DeliveryID          string
	LeaseToken          string
	Attempt             int
	Outcome             AttemptOutcome
	HTTPStatus          int
	Duration            time.Duration
	ErrorCode           string
	NextAvailableAt     *time.Time
	CompletedAt         time.Time
	DisableSubscription bool
}

type HistoryEntry struct {
	DeliveryID  string         `json:"delivery_id"`
	Attempt     int            `json:"attempt"`
	Outcome     AttemptOutcome `json:"outcome"`
	HTTPStatus  int            `json:"http_status,omitempty"`
	DurationMS  int64          `json:"duration_ms"`
	ErrorCode   string         `json:"error_code,omitempty"`
	CompletedAt time.Time      `json:"completed_at"`
}

type Repository interface {
	CreateSubscription(context.Context, tenancy.Scope, Subscription) error
	Subscription(context.Context, tenancy.Scope, string) (Subscription, error)
	ListSubscriptions(context.Context, tenancy.Scope) ([]Subscription, error)
	DisableSubscription(context.Context, tenancy.Scope, string, time.Time) (Subscription, error)
	RotateSubscription(context.Context, tenancy.Scope, string, secrets.Reference, secrets.Reference, time.Time, time.Time) (Subscription, error)
	ClearPreviousSecret(context.Context, tenancy.Scope, string, secrets.Reference, time.Time) error
	MatchingSubscriptions(context.Context, tenancy.Scope, eventbus.EventType) ([]Subscription, error)
	Enqueue(context.Context, tenancy.Scope, Delivery) (bool, error)
	Claim(context.Context, tenancy.Scope, string, time.Time, time.Duration) (Delivery, error)
	Complete(context.Context, tenancy.Scope, AttemptResult) error
	Delivery(context.Context, tenancy.Scope, string) (Delivery, error)
	Replay(context.Context, tenancy.Scope, string, string, time.Time) (Delivery, error)
	History(context.Context, tenancy.Scope, string, int) ([]HistoryEntry, error)
}

type IDGenerator interface {
	NewID(prefix string) (string, error)
}
type RandomIDs struct{ Reader io.Reader }

func (g RandomIDs) NewID(prefix string) (string, error) {
	r := g.Reader
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, 16)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

type Service struct {
	repo      Repository
	secret    secrets.SecretProvider
	endpoints *EndpointPolicy
	ids       IDGenerator
	clock     func() time.Time
}

func NewService(repo Repository, secret secrets.SecretProvider, endpoints *EndpointPolicy, ids IDGenerator) (*Service, error) {
	if repo == nil || secret == nil || endpoints == nil {
		return nil, ErrInvalid
	}
	if ids == nil {
		ids = RandomIDs{}
	}
	return &Service{repo: repo, secret: secret, endpoints: endpoints, ids: ids, clock: time.Now}, nil
}

func (s *Service) CreateSubscription(ctx context.Context, scope tenancy.Scope, id, endpoint string, eventTypes []eventbus.EventType, signingMaterial []byte) (Subscription, error) {
	if ctx == nil || !scope.Valid() || s == nil || s.repo == nil || s.secret == nil || s.endpoints == nil {
		return Subscription{}, ErrInvalid
	}
	if len(signingMaterial) < MinSigningSecretBytes || len(signingMaterial) > MaxSigningSecretBytes {
		return Subscription{}, ErrInvalid
	}
	// Validate all caller-controlled subscription fields before creating a secret,
	// so malformed requests cannot leave orphaned secret material behind.
	if !validID(id) || !validEndpointText(endpoint) || len(eventTypes) == 0 || len(eventTypes) > MaxEventsPerSubscription {
		return Subscription{}, ErrInvalid
	}
	seen := make(map[eventbus.EventType]struct{}, len(eventTypes))
	for _, typ := range eventTypes {
		if typ.Validate() != nil {
			return Subscription{}, ErrInvalid
		}
		if _, exists := seen[typ]; exists {
			return Subscription{}, ErrInvalid
		}
		seen[typ] = struct{}{}
	}
	if _, err := s.endpoints.Resolve(ctx, endpoint); err != nil {
		return Subscription{}, err
	}
	refMeta, err := s.secret.Create(ctx, scope, secrets.ClassWebhookSigning, signingMaterial)
	if err != nil {
		return Subscription{}, err
	}
	now := s.clock().UTC()
	sub := Subscription{ID: id, Endpoint: endpoint, EventTypes: append([]eventbus.EventType(nil), eventTypes...), Status: SubscriptionActive, SigningSecret: refMeta.Reference, Version: 1, CreatedAt: now, UpdatedAt: now}
	if sub.Validate() != nil {
		_, _ = s.secret.Revoke(ctx, scope, refMeta.Reference)
		return Subscription{}, ErrInvalid
	}
	if err := s.repo.CreateSubscription(ctx, scope, sub); err != nil {
		_, _ = s.secret.Revoke(ctx, scope, refMeta.Reference)
		return Subscription{}, err
	}
	return sub, nil
}

func (s *Service) DisableSubscription(ctx context.Context, scope tenancy.Scope, subscriptionID string) error {
	if ctx == nil || s == nil || s.repo == nil || s.secret == nil || !scope.Valid() || !validID(subscriptionID) {
		return ErrInvalid
	}
	now := s.clock().UTC()
	sub, err := s.repo.DisableSubscription(ctx, scope, subscriptionID, now)
	if err != nil {
		return err
	}
	var revokeErr error
	for _, ref := range []secrets.Reference{sub.SigningSecret, sub.PreviousSigningSecret} {
		if ref == "" {
			continue
		}
		if _, err := s.secret.Revoke(ctx, scope, ref); err != nil && !errors.Is(err, secrets.ErrRevoked) {
			revokeErr = errors.Join(revokeErr, err)
		}
	}
	return revokeErr
}

func (s *Service) RotateSigningSecret(ctx context.Context, scope tenancy.Scope, subscriptionID string, material []byte, overlap time.Duration) (Subscription, error) {
	if overlap < DefaultReplayWindow || overlap > 24*time.Hour || len(material) < MinSigningSecretBytes || len(material) > MaxSigningSecretBytes {
		return Subscription{}, ErrInvalid
	}
	current, err := s.repo.Subscription(ctx, scope, subscriptionID)
	if err != nil {
		return Subscription{}, err
	}
	meta, err := s.secret.Create(ctx, scope, secrets.ClassWebhookSigning, material)
	if err != nil {
		return Subscription{}, err
	}
	now := s.clock().UTC()
	until := now.Add(overlap)
	updated, err := s.repo.RotateSubscription(ctx, scope, subscriptionID, meta.Reference, current.SigningSecret, until, now)
	if err != nil {
		_, _ = s.secret.Revoke(ctx, scope, meta.Reference)
		return Subscription{}, err
	}
	return updated, nil
}
func (s *Service) FinalizeRotation(ctx context.Context, scope tenancy.Scope, subscriptionID string) error {
	sub, err := s.repo.Subscription(ctx, scope, subscriptionID)
	if err != nil {
		return err
	}
	if sub.PreviousSigningSecret == "" {
		return nil
	}
	now := s.clock().UTC()
	if sub.PreviousValidUntil == nil || now.Before(*sub.PreviousValidUntil) {
		return ErrConflict
	}
	if _, err := s.secret.Revoke(ctx, scope, sub.PreviousSigningSecret); err != nil && !errors.Is(err, secrets.ErrRevoked) {
		return err
	}
	return s.repo.ClearPreviousSecret(ctx, scope, subscriptionID, sub.PreviousSigningSecret, now)
}

func (s *Service) Handle(ctx context.Context, incoming eventbus.Delivery) error {
	if incoming.Validate() != nil {
		return eventbus.Permanent("webhook_event_invalid")
	}
	scope, err := tenancy.ParseScope(incoming.Event.OrganizationID, incoming.Event.WorkspaceID)
	if err != nil {
		return eventbus.Permanent("webhook_scope_invalid")
	}
	subs, err := s.repo.MatchingSubscriptions(ctx, scope, incoming.Event.Type)
	if err != nil {
		return eventbus.Retryable("webhook_subscription_read")
	}
	for _, sub := range subs {
		if !sub.Accepts(incoming.Event.Type) {
			continue
		}
		id, err := s.ids.NewID("whd_")
		if err != nil {
			return eventbus.Retryable("webhook_id_generation")
		}
		env := Envelope{DeliveryID: id, EventID: incoming.Event.ID, EventType: incoming.Event.Type, OccurredAt: incoming.Event.OccurredAt.Time(), OrganizationID: incoming.Event.OrganizationID, WorkspaceID: incoming.Event.WorkspaceID, Data: append(json.RawMessage(nil), incoming.Event.Data...)}
		body, err := env.Marshal()
		if err != nil {
			return eventbus.Permanent("webhook_envelope_invalid")
		}
		now := s.clock().UTC()
		d := Delivery{ID: id, SubscriptionID: sub.ID, EventID: incoming.Event.ID, EventType: incoming.Event.Type, Endpoint: sub.Endpoint, SigningSecret: sub.SigningSecret, Body: body, Status: DeliveryPending, AvailableAt: now, CreatedAt: now}
		if _, err := s.repo.Enqueue(ctx, scope, d); err != nil {
			return eventbus.Retryable("webhook_enqueue_failed")
		}
	}
	return nil
}
func (s *Service) Replay(ctx context.Context, scope tenancy.Scope, deliveryID string) (Delivery, error) {
	id, err := s.ids.NewID("whd_")
	if err != nil {
		return Delivery{}, err
	}
	return s.repo.Replay(ctx, scope, deliveryID, id, s.clock().UTC())
}

func (s *Service) ListSubscriptions(ctx context.Context, scope tenancy.Scope) ([]Subscription, error) {
	if ctx == nil || s == nil || s.repo == nil || !scope.Valid() {
		return nil, ErrInvalid
	}
	return s.repo.ListSubscriptions(ctx, scope)
}

func (s *Service) DeliveryHistory(ctx context.Context, scope tenancy.Scope, deliveryID string, limit int) ([]HistoryEntry, error) {
	if ctx == nil || s == nil || s.repo == nil || !scope.Valid() || !validID(deliveryID) || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	return s.repo.History(ctx, scope, deliveryID, limit)
}

type BackoffPolicy struct {
	Base, Max    time.Duration
	MaxAttempts  int
	DisableAfter int
}

func DefaultBackoff() BackoffPolicy {
	return BackoffPolicy{Base: time.Second, Max: 15 * time.Minute, MaxAttempts: 8, DisableAfter: 5}
}
func (p BackoffPolicy) Validate() error {
	if p.Base <= 0 || p.Max < p.Base || p.MaxAttempts < 1 || p.MaxAttempts > 32 || p.DisableAfter < 1 {
		return ErrInvalid
	}
	return nil
}
func (p BackoffPolicy) Delay(attempt int) time.Duration {
	if attempt < 1 {
		return 0
	}
	d := p.Base
	for i := 1; i < attempt && d < p.Max; i++ {
		if d > p.Max/2 {
			d = p.Max
			break
		}
		d *= 2
	}
	if d > p.Max {
		return p.Max
	}
	return d
}

type Headers map[string]string

func Sign(secret []byte, deliveryID string, timestamp time.Time, body []byte) (Headers, error) {
	if len(secret) < MinSigningSecretBytes || !validID(deliveryID) || !utc(timestamp) || len(body) == 0 {
		return nil, ErrInvalid
	}
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(ts))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return Headers{"TORGNEXA-Delivery-Id": deliveryID, "TORGNEXA-Timestamp": ts, "TORGNEXA-Signature": "v1=" + hex.EncodeToString(mac.Sum(nil))}, nil
}
func VerifySignature(current, previous []byte, headers Headers, body []byte, now time.Time, window time.Duration) error {
	if len(current) < MinSigningSecretBytes || len(body) == 0 || !utc(now) || window <= 0 {
		return ErrInvalid
	}
	id := headers["TORGNEXA-Delivery-Id"]
	if !validID(id) {
		return ErrInvalid
	}
	rawTS := headers["TORGNEXA-Timestamp"]
	unix, err := strconv.ParseInt(rawTS, 10, 64)
	if err != nil {
		return ErrInvalid
	}
	ts := time.Unix(unix, 0).UTC()
	delta := now.Sub(ts)
	if delta < 0 {
		delta = -delta
	}
	if delta > window {
		return ErrConflict
	}
	sig := strings.TrimPrefix(headers["TORGNEXA-Signature"], "v1=")
	provided, err := hex.DecodeString(sig)
	if err != nil || len(provided) != sha256.Size {
		return ErrInvalid
	}
	matches := func(key []byte) bool {
		if len(key) < MinSigningSecretBytes {
			return false
		}
		mac := hmac.New(sha256.New, key)
		_, _ = mac.Write([]byte(rawTS))
		_, _ = mac.Write([]byte("."))
		_, _ = mac.Write(body)
		return hmac.Equal(provided, mac.Sum(nil))
	}
	if matches(current) || matches(previous) {
		return nil
	}
	return ErrConflict
}

type Endpoint struct {
	URL *url.URL
	IPs []net.IP
}
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}
type netResolver struct{}

func (netResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type EndpointPolicy struct {
	Resolver     Resolver
	AllowedPorts map[string]struct{}
}

func NewEndpointPolicy(resolver Resolver) *EndpointPolicy {
	if resolver == nil {
		resolver = netResolver{}
	}
	return &EndpointPolicy{Resolver: resolver, AllowedPorts: map[string]struct{}{"443": {}}}
}
func (p *EndpointPolicy) Resolve(ctx context.Context, raw string) (Endpoint, error) {
	if ctx == nil || p == nil || p.Resolver == nil || !validEndpointText(raw) {
		return Endpoint{}, ErrUnsafeURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return Endpoint{}, ErrUnsafeURL
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "443"
	}
	if _, ok := p.AllowedPorts[port]; !ok {
		return Endpoint{}, ErrUnsafeURL
	}
	var ips []net.IP
	if literal := net.ParseIP(host); literal != nil {
		ips = []net.IP{literal}
	} else {
		found, err := p.Resolver.LookupIPAddr(ctx, host)
		if err != nil || len(found) == 0 {
			return Endpoint{}, ErrUnsafeURL
		}
		for _, addr := range found {
			ips = append(ips, addr.IP)
		}
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return Endpoint{}, ErrUnsafeURL
		}
	}
	return Endpoint{URL: u, IPs: ips}, nil
}

var blockedCIDRs = mustCIDRs([]string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "2001:db8::/32"})

func mustCIDRs(values []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(values))
	for _, raw := range values {
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			panic(err)
		}
		out = append(out, n)
	}
	return out
}
func publicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if !ip.IsGlobalUnicast() {
		return false
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return false
		}
	}
	return true
}

type SendResult struct {
	StatusCode int
	Duration   time.Duration
	Err        error
}
type Transport interface {
	Send(context.Context, Endpoint, []byte, Headers) SendResult
}
type HTTPTransport struct{ Timeout time.Duration }

func (t HTTPTransport) Send(ctx context.Context, endpoint Endpoint, body []byte, headers Headers) SendResult {
	start := time.Now()
	if ctx == nil || endpoint.URL == nil || len(endpoint.IPs) == 0 || len(body) == 0 {
		return SendResult{Duration: time.Since(start), Err: ErrInvalid}
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	port := endpoint.URL.Port()
	if port == "" {
		port = "443"
	}
	ip := endpoint.IPs[0]
	dialer := &net.Dialer{Timeout: timeout}
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(c context.Context, network, addr string) (net.Conn, error) {
		return dialer.DialContext(c, network, net.JoinHostPort(ip.String(), port))
	}, TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect disabled") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL.String(), bytes.NewReader(body))
	if err != nil {
		return SendResult{Duration: time.Since(start), Err: ErrInvalid}
	}
	req.Host = endpoint.URL.Host
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "TORGNEXA-Webhooks/1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return SendResult{Duration: time.Since(start), Err: err}
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 32<<10)
	return SendResult{StatusCode: resp.StatusCode, Duration: time.Since(start)}
}

type Worker struct {
	Repo      Repository
	Secrets   secrets.SecretProvider
	Endpoints *EndpointPolicy
	Transport Transport
	Backoff   BackoffPolicy
	WorkerID  string
	Lease     time.Duration
	Clock     func() time.Time
}

func (w *Worker) ProcessOne(ctx context.Context, scope tenancy.Scope) (bool, error) {
	if ctx == nil || !scope.Valid() || w == nil || w.Repo == nil || w.Secrets == nil || w.Endpoints == nil || w.Transport == nil || w.WorkerID == "" || w.Backoff.Validate() != nil {
		return false, ErrInvalid
	}
	clock := w.Clock
	if clock == nil {
		clock = time.Now
	}
	lease := w.Lease
	if lease <= 0 {
		lease = 30 * time.Second
	}
	now := clock().UTC()
	d, err := w.Repo.Claim(ctx, scope, w.WorkerID, now, lease)
	if errors.Is(err, ErrNoDelivery) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	endpoint, resolveErr := w.Endpoints.Resolve(ctx, d.Endpoint)
	attempt := d.Attempt
	result := SendResult{Err: resolveErr}
	signErr := error(nil)
	if resolveErr == nil {
		signErr = w.Secrets.Use(ctx, scope, d.SigningSecret, func(material []byte) error {
			headers, err := Sign(material, d.ID, now, d.Body)
			if err != nil {
				return err
			}
			result = w.Transport.Send(ctx, endpoint, d.Body, headers)
			return nil
		})
	}
	outcome, code := classify(result, resolveErr, signErr)
	ar := AttemptResult{DeliveryID: d.ID, LeaseToken: d.LeaseToken, Attempt: attempt, Outcome: outcome, HTTPStatus: result.StatusCode, Duration: result.Duration, ErrorCode: code, CompletedAt: clock().UTC()}
	if outcome == OutcomeRetry {
		if attempt >= w.Backoff.MaxAttempts {
			ar.Outcome = OutcomeDLQ
			ar.ErrorCode = "retry_budget_exhausted"
		} else {
			next := ar.CompletedAt.Add(w.Backoff.Delay(attempt))
			ar.NextAvailableAt = &next
		}
	}
	if ar.Outcome == OutcomeDLQ && (code == "http_permanent" || code == "endpoint_unsafe") {
		ar.DisableSubscription = d.ConsecutivePermanentFailures+1 >= w.Backoff.DisableAfter
	}
	if err := w.Repo.Complete(ctx, scope, ar); err != nil {
		return false, err
	}
	return true, nil
}
func classify(result SendResult, resolveErr, signErr error) (AttemptOutcome, string) {
	if resolveErr != nil {
		return OutcomeDLQ, "endpoint_unsafe"
	}
	if signErr != nil {
		return OutcomeRetry, "signing_secret_unavailable"
	}
	if result.Err != nil {
		return OutcomeRetry, "network_error"
	}
	if result.StatusCode >= 200 && result.StatusCode <= 299 {
		return OutcomeSucceeded, ""
	}
	switch result.StatusCode {
	case 408, 425, 429:
		return OutcomeRetry, "http_retryable"
	}
	if result.StatusCode >= 500 {
		return OutcomeRetry, "http_retryable"
	}
	return OutcomeDLQ, "http_permanent"
}

func validID(v string) bool {
	if len(v) < 1 || len(v) > 128 || v != strings.TrimSpace(v) || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:/-", r)) {
			return false
		}
	}
	return true
}
func validEndpointText(v string) bool {
	return v != "" && v == strings.TrimSpace(v) && utf8.ValidString(v) && utf8.RuneCountInString(v) <= MaxEndpointRunes
}
func utc(t time.Time) bool { return !t.IsZero() && t.Location() == time.UTC }
func SafeErrorCode(code string) string {
	if code == "" {
		return ""
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return "internal_error"
		}
	}
	if len(code) > 64 {
		return "internal_error"
	}
	return code
}
func (h HistoryEntry) String() string {
	return fmt.Sprintf("webhook attempt %d %s", h.Attempt, h.Outcome)
}
