package instagram

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

type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
}

func (t *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	r.AccessToken = append([]byte(nil), r.AccessToken...)
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

type staticConfig struct{ value Configuration }

func (s staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return s.value, nil
}

type testSecrets struct{ value []byte }

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s.value...)
	defer clear(v)
	return cb(v)
}

type testRuntime struct{ s sdk.SecretAccessor }

func (r testRuntime) Secrets() sdk.SecretAccessor { return r.s }

type testMedia struct {
	bodies map[string][]byte
	desc   map[string]sdk.MediaDescriptor
	opens  int
}

func (m *testMedia) OpenReleased(_ context.Context, _ sdk.Account, ref sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	m.opens++
	b, ok := m.bodies[ref.UploadID]
	if !ok {
		return nil, sdk.MediaDescriptor{}, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(b)), m.desc[ref.UploadID], nil
}

type stagedCall struct {
	ref  sdk.SocialMediaRef
	desc sdk.MediaDescriptor
	body []byte
}
type testStager struct {
	calls []stagedCall
	base  string
}

func (s *testStager) Stage(_ context.Context, _ sdk.Account, ref sdk.SocialMediaRef, desc sdk.MediaDescriptor, r io.Reader) (StagedMedia, error) {
	b, _ := io.ReadAll(r)
	s.calls = append(s.calls, stagedCall{ref: ref, desc: desc, body: b})
	return StagedMedia{URL: s.base + "/" + ref.UploadID + "?sig=fixture", ExpiresAt: now().Add(time.Hour)}, nil
}

func now() time.Time { return time.Date(2026, 8, 11, 21, 0, 0, 0, time.UTC) }
func account() sdk.Account {
	at := now().Add(-time.Hour)
	return sdk.Account{ID: "instagram-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "instagram", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func runtime() sdk.Runtime {
	return testRuntime{s: testSecrets{value: []byte("IGQVJ-fixture-long-lived-token-0123456789")}}
}
func config() Configuration { return Configuration{InstagramUserID: "17841400000000001"} }
func pubID() string         { return "01890f4d-1e10-7cc0-9c4a-000000000001" }
func imageRef(n byte) sdk.SocialMediaRef {
	return sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcde" + string([]byte{n}), Kind: sdk.SocialMediaImage, AltText: "image"}
}

func TestManifestMatchesFile(t *testing.T) {
	var disk sdk.Manifest
	if json.Unmarshal(manifestFixture, &disk) != nil {
		t.Fatal("manifest json")
	}
	if Manifest().Validate() != nil || !reflect.DeepEqual(Manifest().Canonical(), disk.Canonical()) {
		t.Fatalf("manifest mismatch: %#v %#v", Manifest(), disk)
	}
}
func TestHealthRequiresExactProfessionalAccount(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: profileFixture}}}
	c := New(tr, staticConfig{config()}, &testStager{}, now)
	h, err := c.Health(context.Background(), account(), runtime())
	if err != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", h, err)
	}
	if len(tr.requests) != 1 || tr.requests[0].Host != apiHost || tr.requests[0].Path != "/v26.0/17841400000000001" {
		t.Fatalf("request=%+v", tr.requests)
	}
}
func TestPublishSingleImageRevalidatesAndStages(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: containerFixture}, {StatusCode: 200, Body: statusFixture}, {StatusCode: 200, Body: publishedFixture}}}
	st := &testStager{base: "https://media.example.test"}
	c := New(tr, staticConfig{config()}, st, now)
	c.wait = func(context.Context, time.Duration) error { return nil }
	ref := sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage, AltText: "alt"}
	m := &testMedia{bodies: map[string][]byte{ref.UploadID: []byte("jpeg")}, desc: map[string]sdk.MediaDescriptor{ref.UploadID: {MediaType: "image/jpeg", SizeBytes: 4, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}}
	got, err := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Text: "caption", Media: []sdk.SocialMediaRef{ref}}, m)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemotePublicationID != "instagram:17841400000000001:18000000000000001" || got.Status != sdk.SocialRemotePublished {
		t.Fatalf("got=%+v", got)
	}
	if m.opens != 1 || len(st.calls) != 1 || string(st.calls[0].body) != "jpeg" {
		t.Fatalf("opens=%d staged=%+v", m.opens, st.calls)
	}
	if len(tr.requests) != 3 || tr.requests[0].Method != "POST" || tr.requests[0].Path != "/v26.0/17841400000000001/media" || paramValue(tr.requests[0].Params, "image_url") == "" || paramValue(tr.requests[0].Params, "caption") != "caption" || tr.requests[2].Path != "/v26.0/17841400000000001/media_publish" {
		t.Fatalf("requests=%+v", tr.requests)
	}
}
func TestPublishRejectsNonJPEGBeforeStaging(t *testing.T) {
	tr := &scriptedTransport{}
	st := &testStager{base: "https://media.example.test"}
	c := New(tr, staticConfig{config()}, st, now)
	ref := sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage}
	m := &testMedia{bodies: map[string][]byte{ref.UploadID: []byte("png")}, desc: map[string]sdk.MediaDescriptor{ref.UploadID: {MediaType: "image/png", SizeBytes: 3, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}}
	_, err := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Media: []sdk.SocialMediaRef{ref}}, m)
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) || len(st.calls) != 0 || len(tr.requests) != 0 {
		t.Fatalf("err=%v stage=%d requests=%d", err, len(st.calls), len(tr.requests))
	}
}
func TestWriteAmbiguityIsNotRetryable(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 503, RequestID: "ig-req-1", Body: []byte(`{"error":{"code":2,"fbtrace_id":"ig-req-1"}}`)}}}
	c := New(tr, staticConfig{config()}, &testStager{base: "https://media.example.test"}, now)
	_, err := c.createContainer(context.Background(), []byte("IGQVJ-fixture-long-lived-token-0123456789"), config().InstagramUserID, []Param{{Name: "image_url", Value: "https://media.example.test/x.jpg"}})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Code != "write_outcome_unknown" || remote.Retryable() {
		t.Fatalf("err=%#v", err)
	}
}
func TestReadStatusRejectsForeignAccountBeforeEgress(t *testing.T) {
	tr := &scriptedTransport{}
	c := New(tr, staticConfig{config()}, &testStager{}, now)
	_, err := c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "instagram:17841400000000099:18000000000000001")
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) || len(tr.requests) != 0 {
		t.Fatalf("err=%v requests=%d", err, len(tr.requests))
	}
}
func TestStagedURLValidation(t *testing.T) {
	for _, bad := range []string{"http://media.example/x", "https://user@media.example/x", "https://media.example:444/x", "https://media.example/x#frag", "https://media..example/x"} {
		if validStagedURL(bad) {
			t.Fatalf("accepted %s", bad)
		}
	}
	if !validStagedURL("https://media.example.test/file?sig=a%2Fb") {
		t.Fatal("valid signed url rejected")
	}
}
