package threads

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed manifest.json
var manifestFixture []byte

//go:embed fixtures/profile.json
var profileFixture []byte

//go:embed fixtures/container.json
var containerFixture []byte

//go:embed fixtures/status.json
var statusFixture []byte

//go:embed fixtures/published.json
var publishedFixture []byte

//go:embed fixtures/token.json
var tokenFixture []byte

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (t *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	r.AccessToken = append([]byte(nil), r.AccessToken...)
	r.AppSecret = append([]byte(nil), r.AppSecret...)
	r.Params = append([]Param(nil), r.Params...)
	t.requests = append(t.requests, r)
	i := len(t.requests) - 1
	if i < len(t.errs) && t.errs[i] != nil {
		return Response{}, t.errs[i]
	}
	if i >= len(t.responses) {
		return Response{}, errors.New("unexpected call")
	}
	return t.responses[i], nil
}

type staticConfig struct{ v Configuration }

func (s staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) { return s.v, nil }

type secrets struct {
	values map[sdk.SecretReference][]byte
}

func (s secrets) UseSecret(_ context.Context, ref sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s.values[ref]...)
	defer clear(v)
	return cb(v)
}

type runtimeFixture struct{ s sdk.SecretAccessor }

func (r runtimeFixture) Secrets() sdk.SecretAccessor { return r.s }

type mediaFixture struct {
	body  []byte
	desc  sdk.MediaDescriptor
	opens int
}

func (m *mediaFixture) OpenReleased(context.Context, sdk.Account, sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	m.opens++
	return io.NopCloser(bytes.NewReader(m.body)), m.desc, nil
}

type stagerFixture struct{ calls int }

func (s *stagerFixture) Stage(_ context.Context, _ sdk.Account, ref sdk.SocialMediaRef, _ sdk.MediaDescriptor, r io.Reader) (StagedMedia, error) {
	s.calls++
	_, _ = io.ReadAll(r)
	return StagedMedia{URL: "https://media.example.test/" + ref.UploadID + "?sig=fixture", ExpiresAt: now().Add(time.Hour)}, nil
}

type tokenSinkFixture struct {
	calls   int
	ref     sdk.SecretReference
	value   []byte
	expires time.Time
}

func (s *tokenSinkFixture) RotateSecret(_ context.Context, ref sdk.SecretReference, v []byte, expires time.Time) error {
	s.calls++
	s.ref = ref
	s.value = append([]byte(nil), v...)
	s.expires = expires
	return nil
}
func now() time.Time { return time.Date(2026, 8, 11, 22, 0, 0, 0, time.UTC) }
func config() Configuration {
	return Configuration{ThreadsUserID: "22449949450000001", AppSecretReference: "sec:v1:1123456789abcdef0123456789abcdef"}
}
func account() sdk.Account {
	at := now().Add(-time.Hour)
	return sdk.Account{ID: "threads-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "threads", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func runtime() sdk.Runtime {
	return runtimeFixture{s: secrets{values: map[sdk.SecretReference][]byte{account().SecretReference: []byte("THQVJ-current-user-token-0123456789abcdef"), config().AppSecretReference: []byte("0123456789abcdef0123456789abcdef")}}}
}
func pubID() string { return "01890f4d-1e10-7cc0-9c4a-000000000001" }
func ref(kind sdk.SocialMediaKind) sdk.SocialMediaRef {
	return sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: kind, AltText: "alt"}
}
func TestManifestMatchesFile(t *testing.T) {
	var disk sdk.Manifest
	if json.Unmarshal(manifestFixture, &disk) != nil {
		t.Fatal("manifest json")
	}
	if Manifest().Validate() != nil || !reflect.DeepEqual(Manifest().Canonical(), disk.Canonical()) {
		t.Fatalf("manifest mismatch")
	}
}
func TestHealthBindsExactThreadsUser(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: profileFixture}}}
	c := New(tr, staticConfig{config()}, &stagerFixture{}, now)
	h, e := c.Health(context.Background(), account(), runtime())
	if e != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", h, e)
	}
	if len(tr.requests) != 1 || tr.requests[0].Path != "/v1.0/me" {
		t.Fatalf("requests=%+v", tr.requests)
	}
}
func TestPublishText(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: containerFixture}, {StatusCode: 200, Body: statusFixture}, {StatusCode: 200, Body: publishedFixture}}}
	c := New(tr, staticConfig{config()}, nil, now)
	c.wait = func(context.Context, time.Duration) error { return nil }
	got, e := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "hello threads"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if got.RemotePublicationID != "threads:22449949450000001:17990000000000001" {
		t.Fatalf("got=%+v", got)
	}
	if paramValue(tr.requests[0].Params, "media_type") != "TEXT" || paramValue(tr.requests[0].Params, "text") != "hello threads" || tr.requests[2].Path != "/v1.0/22449949450000001/threads_publish" {
		t.Fatalf("requests=%+v", tr.requests)
	}
}
func TestPublishImageValidatesAndStages(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: containerFixture}, {StatusCode: 200, Body: statusFixture}, {StatusCode: 200, Body: publishedFixture}}}
	st := &stagerFixture{}
	c := New(tr, staticConfig{config()}, st, now)
	c.wait = func(context.Context, time.Duration) error { return nil }
	m := &mediaFixture{body: []byte("png"), desc: sdk.MediaDescriptor{MediaType: "image/png", SizeBytes: 3, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}
	_, e := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Text: "image", Media: []sdk.SocialMediaRef{ref(sdk.SocialMediaImage)}}, m)
	if e != nil {
		t.Fatal(e)
	}
	if m.opens != 1 || st.calls != 1 || paramValue(tr.requests[0].Params, "media_type") != "IMAGE" || paramValue(tr.requests[0].Params, "image_url") == "" {
		t.Fatalf("opens=%d stages=%d requests=%+v", m.opens, st.calls, tr.requests)
	}
}
func TestTextLimitRejectedBeforeEgress(t *testing.T) {
	tr := &scriptedTransport{}
	c := New(tr, staticConfig{config()}, nil, now)
	text := bytes.Repeat([]byte("x"), 501)
	_, e := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: string(text)}, nil)
	if !errors.Is(e, sdk.ErrInvalidSocialRequest) || len(tr.requests) != 0 {
		t.Fatalf("err=%v requests=%d", e, len(tr.requests))
	}
}
func TestExchangeLongLivedTokenRotatesSecretWithoutReturningPlaintext(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: tokenFixture}}}
	c := New(tr, staticConfig{config()}, nil, now)
	sink := &tokenSinkFixture{}
	got, e := c.ExchangeLongLivedToken(context.Background(), account(), runtime(), sink)
	if e != nil {
		t.Fatal(e)
	}
	if sink.calls != 1 || sink.ref != account().SecretReference || string(sink.value) != "THQVJ-long-lived-fixture-token-abcdefghijklmnopqrstuvwxyz0123456789" || !got.ExpiresAt.Equal(now().Add(5183944*time.Second)) {
		t.Fatalf("sink=%+v got=%+v", sink, got)
	}
	req := tr.requests[0]
	if req.Path != "/access_token" || paramValue(req.Params, "grant_type") != "th_exchange_token" || len(req.AppSecret) == 0 || paramValue(req.Params, "client_secret") != "" || paramValue(req.Params, "access_token") != "" {
		t.Fatalf("request=%+v", req)
	}
}
func TestRefreshLongLivedTokenUsesOfficialRefreshGrant(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: tokenFixture}}}
	c := New(tr, staticConfig{config()}, nil, now)
	sink := &tokenSinkFixture{}
	_, e := c.RefreshLongLivedToken(context.Background(), account(), runtime(), sink)
	if e != nil {
		t.Fatal(e)
	}
	req := tr.requests[0]
	if req.Path != "/refresh_access_token" || paramValue(req.Params, "grant_type") != "th_refresh_token" || len(req.AppSecret) != 0 || paramValue(req.Params, "client_secret") != "" || paramValue(req.Params, "access_token") != "" {
		t.Fatalf("request=%+v", req)
	}
}
func TestWriteAmbiguityFailClosed(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 503, Body: []byte(`{"error":{"code":2,"fbtrace_id":"thr-1"}}`)}}}
	c := New(tr, staticConfig{config()}, nil, now)
	_, e := c.createContainer(context.Background(), []byte("THQVJ-current-user-token-0123456789abcdef"), config().ThreadsUserID, []Param{{Name: "media_type", Value: "TEXT"}, {Name: "text", Value: "x"}})
	var remote *sdk.RemoteError
	if !errors.As(e, &remote) || remote.Code != "write_outcome_unknown" || remote.Retryable() {
		t.Fatalf("err=%#v", e)
	}
}
func TestReadStatusRejectsForeignUserBeforeEgress(t *testing.T) {
	tr := &scriptedTransport{}
	c := New(tr, staticConfig{config()}, nil, now)
	_, e := c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "threads:22449949450000099:17990000000000001")
	if !errors.Is(e, sdk.ErrInvalidSocialRequest) || len(tr.requests) != 0 {
		t.Fatalf("err=%v requests=%d", e, len(tr.requests))
	}
}
