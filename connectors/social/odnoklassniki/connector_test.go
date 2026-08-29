package ok

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
var manifestJSON []byte

//go:embed fixtures/group.json
var groupFixture []byte

//go:embed fixtures/photo-ticket.json
var photoTicketFixture []byte

//go:embed fixtures/photo-upload.json
var photoUploadFixture []byte

//go:embed fixtures/video-ticket.json
var videoTicketFixture []byte

//go:embed fixtures/topic.json
var topicFixture []byte

//go:embed fixtures/topic-status.json
var topicStatusFixture []byte

//go:embed fixtures/topic-stat.json
var topicStatFixture []byte

type scriptedTransport struct {
	responses       []Response
	errs            []error
	requests        []Request
	uploadResponses []Response
	uploads         []UploadRequest
}

func (s *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	r.AccessToken = append([]byte(nil), r.AccessToken...)
	r.Params = append([]Param(nil), r.Params...)
	s.requests = append(s.requests, r)
	i := len(s.requests) - 1
	if i < len(s.errs) && s.errs[i] != nil {
		return Response{}, s.errs[i]
	}
	if i >= len(s.responses) {
		return Response{StatusCode: 404}, nil
	}
	return s.responses[i], nil
}
func (s *scriptedTransport) Upload(_ context.Context, r UploadRequest) (Response, error) {
	for i := range r.Files {
		b, _ := io.ReadAll(r.Files[i].Body)
		r.Files[i].Body = bytes.NewReader(b)
	}
	s.uploads = append(s.uploads, r)
	i := len(s.uploads) - 1
	if i >= len(s.uploadResponses) {
		return Response{StatusCode: 404}, nil
	}
	return s.uploadResponses[i], nil
}

type cfgSource struct{}

func (cfgSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{GroupID: "70000000000001", ApplicationKey: "CBAOK.public.key", AppSecretReference: "sec:v1:abcdef0123456789abcdef0123456789"}, nil
}

type secrets struct{}

func (secrets) UseSecret(_ context.Context, ref sdk.SecretReference, cb func([]byte) error) error {
	var b []byte
	if ref == "sec:v1:abcdef0123456789abcdef0123456789" {
		b = []byte("0123456789abcdef0123456789abcdef")
	} else {
		b = []byte("oauth-token-0123456789abcdef0123456789abcdef")
	}
	defer clear(b)
	return cb(b)
}

type runtime struct{}

func (runtime) Secrets() sdk.SecretAccessor { return secrets{} }
func account() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "odnoklassniki-test", OrganizationID: "01900000-0000-7000-8000-000000000001", WorkspaceID: "01900000-0000-7000-8000-000000000002", ConnectorID: "odnoklassniki", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type mediaAccessor struct {
	body []byte
	desc sdk.MediaDescriptor
}

func (m mediaAccessor) OpenReleased(context.Context, sdk.Account, sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	return io.NopCloser(bytes.NewReader(m.body)), m.desc, nil
}
func sha() string { return "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" }
func pub(kind sdk.SocialPostKind, media []sdk.SocialMediaRef) sdk.SocialPublishRequest {
	return sdk.SocialPublishRequest{PublicationID: "01900000-0000-7000-8000-000000000003", Kind: kind, Text: "hello", Media: media}
}

func TestManifestJSONMatches(t *testing.T) {
	var got sdk.Manifest
	if err := json.Unmarshal(manifestJSON, &got); err != nil {
		t.Fatal(err)
	}
	if got.Validate() != nil {
		t.Fatal(got.Validate())
	}
	want := Manifest().Canonical()
	got = got.Canonical()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest mismatch\n%#v\n%#v", got, want)
	}
}
func TestHealthExactGroup(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: groupFixture}}}
	c := New(tr, cfgSource{}, func() time.Time { return time.Date(2026, 8, 12, 0, 1, 0, 0, time.UTC) })
	h, err := c.Health(context.Background(), account(), runtime{})
	if err != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", h, err)
	}
	if len(tr.requests) != 1 || tr.requests[0].APIMethod != "group.getInfo" {
		t.Fatalf("request=%+v", tr.requests)
	}
}
func TestTextPublishAndStatus(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: topicFixture}, {StatusCode: 200, Body: topicStatusFixture}}}
	c := New(tr, cfgSource{}, func() time.Time { return time.Date(2026, 8, 12, 0, 2, 0, 0, time.UTC) })
	r, err := c.PublishSocial(context.Background(), account(), runtime{}, pub(sdk.SocialPostText, nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.RemotePublicationID != "ok:70000000000001:12345678901234" {
		t.Fatal(r)
	}
	status, err := c.ReadSocialPublicationStatus(context.Background(), account(), runtime{}, r.RemotePublicationID)
	if err != nil || status.Status != sdk.SocialRemotePublished {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	if param(tr.requests[0].Params, "sig") == "" || param(tr.requests[0].Params, "application_key") != "CBAOK.public.key" {
		t.Fatal("signature/application key missing")
	}
}
func TestImagePublish(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: photoTicketFixture}, {StatusCode: 200, Body: topicFixture}}, uploadResponses: []Response{{StatusCode: 200, Body: photoUploadFixture}}}
	c := New(tr, cfgSource{}, nil)
	ref := sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage}
	m := mediaAccessor{body: []byte("jpeg"), desc: sdk.MediaDescriptor{MediaType: "image/jpeg", SizeBytes: 4, SHA256: sha()}}
	r, err := c.PublishSocial(context.Background(), account(), runtime{}, pub(sdk.SocialPostMedia, []sdk.SocialMediaRef{ref}), m)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != sdk.SocialRemotePublished || len(tr.uploads) != 1 || len(tr.uploads[0].Files) != 1 {
		t.Fatalf("r=%+v uploads=%d", r, len(tr.uploads))
	}
}
func TestVideoPublish(t *testing.T) {
	body := bytes.Repeat([]byte{1}, 16<<10)
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: videoTicketFixture}, {StatusCode: 200, Body: nil}, {StatusCode: 200, Body: topicFixture}}, uploadResponses: []Response{{StatusCode: 200, Body: []byte(`{}`)}}}
	c := New(tr, cfgSource{}, nil)
	ref := sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaVideo}
	m := mediaAccessor{body: body, desc: sdk.MediaDescriptor{MediaType: "video/mp4", SizeBytes: int64(len(body)), SHA256: sha()}}
	if _, err := c.PublishSocial(context.Background(), account(), runtime{}, pub(sdk.SocialPostVideo, []sdk.SocialMediaRef{ref}), m); err != nil {
		t.Fatal(err)
	}
	if tr.requests[1].APIMethod != "video.update" {
		t.Fatalf("requests=%+v", tr.requests)
	}
}
func TestAnalytics(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: topicStatFixture}}}
	c := New(tr, cfgSource{}, func() time.Time { return time.Date(2026, 8, 12, 0, 3, 0, 0, time.UTC) })
	items, err := c.ReadSocialAnalytics(context.Background(), account(), runtime{}, sdk.SocialAnalyticsRequest{RemotePublicationIDs: []string{"ok:70000000000001:12345678901234"}})
	if err != nil || len(items) != 1 || items[0].ReachTotal != 100 || items[0].LinkClicks != 9 {
		t.Fatalf("items=%+v err=%v", items, err)
	}
}
func TestAmbiguousWriteFailsClosed(t *testing.T) {
	tr := &scriptedTransport{errs: []error{errors.New("network")}}
	c := New(tr, cfgSource{}, nil)
	_, err := c.PublishSocial(context.Background(), account(), runtime{}, pub(sdk.SocialPostText, nil), nil)
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Code != "write_outcome_unknown" {
		t.Fatalf("err=%v", err)
	}
}
func param(ps []Param, n string) string {
	for _, p := range ps {
		if p.Name == n {
			return p.Value
		}
	}
	return ""
}
