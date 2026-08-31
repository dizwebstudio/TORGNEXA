package maxconnector

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

//go:embed fixtures/get-me.json
var getMeFixture []byte

//go:embed fixtures/get-channel.json
var getChannelFixture []byte

//go:embed fixtures/get-membership.json
var getMembershipFixture []byte

//go:embed fixtures/send-message.json
var sendMessageFixture []byte

//go:embed fixtures/upload-init-image.json
var uploadInitImageFixture []byte

//go:embed fixtures/upload-image.json
var uploadImageFixture []byte

//go:embed fixtures/webhook-message-created.json
var webhookFixture []byte

type capturedUpload struct {
	URL, FileName, MediaType, SHA256 string
	SizeBytes                        int64
	Body                             []byte
}
type scriptedTransport struct {
	responses       []Response
	errs            []error
	requests        []Request
	uploadResponses []Response
	uploadErrs      []error
	uploads         []capturedUpload
}

func (t *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	r.AccessToken = append([]byte(nil), r.AccessToken...)
	r.Params = append([]Param(nil), r.Params...)
	r.Body = append([]byte(nil), r.Body...)
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
func (t *scriptedTransport) Upload(_ context.Context, r UploadRequest) (Response, error) {
	b, e := io.ReadAll(r.Body)
	if e != nil {
		return Response{}, e
	}
	t.uploads = append(t.uploads, capturedUpload{r.URL, r.FileName, r.MediaType, r.SHA256, r.SizeBytes, b})
	i := len(t.uploads) - 1
	if i < len(t.uploadErrs) && t.uploadErrs[i] != nil {
		return Response{}, t.uploadErrs[i]
	}
	if i >= len(t.uploadResponses) {
		return Response{}, errors.New("unexpected upload")
	}
	return t.uploadResponses[i], nil
}

type staticConfig struct{ value Configuration }

func (s staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return s.value, nil
}

type testRuntime struct {
	values map[sdk.SecretReference][]byte
}

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{r.values} }

type testSecrets struct {
	values map[sdk.SecretReference][]byte
}

func (s testSecrets) UseSecret(_ context.Context, ref sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s.values[ref]...)
	defer clear(v)
	return cb(v)
}

type testMedia struct {
	body  []byte
	desc  sdk.MediaDescriptor
	opens int
}

func (m *testMedia) OpenReleased(context.Context, sdk.Account, sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	m.opens++
	return io.NopCloser(bytes.NewReader(m.body)), m.desc, nil
}

type memoryDedup struct{ seen map[string]string }

func (d *memoryDedup) ClaimSocialWebhook(_ context.Context, _ sdk.Account, claim sdk.SocialWebhookClaim) (bool, error) {
	if d.seen == nil {
		d.seen = map[string]string{}
	}
	if prior, ok := d.seen[claim.DeliveryID]; ok {
		if prior != claim.ProviderFingerprint {
			return false, errors.New("collision")
		}
		return true, nil
	}
	d.seen[claim.DeliveryID] = claim.ProviderFingerprint
	return false, nil
}

func account() sdk.Account {
	at := time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "max-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "max-messenger", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func webhookRef() sdk.SecretReference { return "sec:v1:1123456789abcdef0123456789abcdef" }
func config() Configuration {
	return Configuration{ChatID: -70801090403050, WebhookSecretReference: webhookRef()}
}
func runtime() testRuntime {
	return testRuntime{values: map[sdk.SecretReference][]byte{account().SecretReference: []byte("max-bot-token-ABCDEFGHIJKLMNOPQRSTUVWXYZ"), webhookRef(): []byte("MaxWebhookSecret_2026")}}
}
func now() time.Time { return time.Date(2026, 8, 11, 11, 30, 0, 0, time.UTC) }
func pubID() string  { return "01890f4d-1e10-7cc0-9c4a-333333333333" }
func imageRef() sdk.SocialMediaRef {
	return sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage}
}

func TestManifestMatchesAndDeclaresExactScope(t *testing.T) {
	var got sdk.Manifest
	if json.Unmarshal(manifestJSON, &got) != nil || got.Validate() != nil || Manifest().Validate() != nil {
		t.Fatal("manifest invalid")
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatal("manifest drift")
	}
	for _, c := range []sdk.Capability{"social.post.text", "social.post.media", "social.post.video", "social.post.buttons", "social.post.edit", "social.post.delete", "social.webhooks"} {
		if !Manifest().Supports(c) {
			t.Fatalf("missing %s", c)
		}
	}
	if Manifest().Supports("social.analytics.read") {
		t.Fatal("undeclared capability")
	}
}
func TestConfigurationAndSecretsStrict(t *testing.T) {
	if config().Validate() != nil || !validToken(runtime().values[account().SecretReference]) || !validWebhookSecret(runtime().values[webhookRef()]) {
		t.Fatal("valid rejected")
	}
	if (Configuration{ChatID: 1}).Validate() != nil || (Configuration{ChatID: 1}).validateWebhook() == nil {
		t.Fatal("text-only configuration or webhook separation is inaccurate")
	}
	for _, c := range []Configuration{{}, {ChatID: -2, WebhookSecretReference: "bad"}} {
		if c.Validate() == nil {
			t.Fatalf("bad config accepted %+v", c)
		}
	}
	for _, secret := range [][]byte{[]byte("abcd"), []byte("bad secret spaces"), []byte("bad$secret")} {
		if validWebhookSecret(secret) {
			t.Fatalf("bad webhook secret %q", secret)
		}
	}
}
func TestHealthRequiresExactActiveChannelWritePermission(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: getMeFixture}, {StatusCode: 200, Body: getChannelFixture}, {StatusCode: 200, Body: getMembershipFixture}}}
	c := New(transport, staticConfig{config()}, now)
	h, e := c.Health(context.Background(), account(), runtime())
	if e != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", h, e)
	}
	if len(transport.requests) != 3 || transport.requests[0].Host != apiHost {
		t.Fatal("unexpected transport")
	}
}
func TestPublishTextAndButtons(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: sendMessageFixture}}}
	c := New(transport, staticConfig{config()}, now)
	r, e := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "hello", Buttons: []sdk.SocialButton{{Text: "Open", URL: "https://example.test"}}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if r.RemotePublicationID != "max:-70801090403050:mid.ffffbdb48e6c3775019d496b34394b84" {
		t.Fatalf("id=%s", r.RemotePublicationID)
	}
	var body map[string]any
	if json.Unmarshal(transport.requests[0].Body, &body) != nil {
		t.Fatal("bad body")
	}
	if body["notify"] != true {
		t.Fatal("channel notify must be true")
	}
	if string(bytes.Join([][]byte{transport.requests[0].AccessToken}, nil)) == "" {
		t.Fatal("missing auth")
	}
}
func TestPublishImageRevalidatesReleaseAndRestrictsUploadHost(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: uploadInitImageFixture}, {StatusCode: 200, Body: sendMessageFixture}}, uploadResponses: []Response{{StatusCode: 200, Body: uploadImageFixture}}}
	media := &testMedia{body: []byte("jpeg"), desc: sdk.MediaDescriptor{MediaType: "image/jpeg", SizeBytes: 4, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}
	c := New(transport, staticConfig{config()}, now)
	_, e := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Text: "photo", Media: []sdk.SocialMediaRef{imageRef()}}, media)
	if e != nil {
		t.Fatal(e)
	}
	if media.opens != 1 || len(transport.uploads) != 1 || transport.uploads[0].URL != "https://iu.oneme.ru/upload.do?sig=fixture" {
		t.Fatalf("media=%d uploads=%+v", media.opens, transport.uploads)
	}
	if validUploadURL("https://evil.example/upload", "image") || validUploadURL("https://iu.oneme.ru.evil.example/upload", "image") || validUploadURL("https://iu.oneme.ru:444/upload", "image") {
		t.Fatal("unsafe upload URL accepted")
	}
}

func TestReadStatusIsBoundToConfiguredChannel(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: sendMessageFixture}}}
	c := New(transport, staticConfig{config()}, now)
	remoteID := "max:-70801090403050:mid.ffffbdb48e6c3775019d496b34394b84"
	result, err := c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), remoteID)
	if err != nil || result.Status != sdk.SocialRemotePublished || result.RemotePublicationID != remoteID {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Method != "GET" || transport.requests[0].Path != "/messages/mid.ffffbdb48e6c3775019d496b34394b84" {
		t.Fatalf("requests=%+v", transport.requests)
	}
	if _, err = c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "max:-1:mid.ffffbdb48e6c3775019d496b34394b84"); err == nil || len(transport.requests) != 1 {
		t.Fatalf("foreign channel was not rejected before egress: %v", err)
	}
}

func TestWebhookCanonicalJSONDeduplicatesEquivalentEncoding(t *testing.T) {
	c := New(&scriptedTransport{}, staticConfig{config()}, now)
	dedup := &memoryDedup{}
	req := sdk.SocialWebhookRequest{VerificationToken: []byte("MaxWebhookSecret_2026"), Body: webhookFixture, ReceivedAt: now()}
	first, err := c.ReceiveSocialWebhook(context.Background(), account(), runtime(), req, dedup)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(webhookFixture))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		t.Fatal("fixture")
	}
	// MarshalIndent changes whitespace while preserving exact JSON numeric values.
	encoded, _ := json.MarshalIndent(value, "", "  ")
	req.Body = encoded
	second, err := c.ReceiveSocialWebhook(context.Background(), account(), runtime(), req, dedup)
	if err != nil || !second.Duplicate || second.DeliveryID != first.DeliveryID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
}

func TestAmbiguousWriteFailsClosed(t *testing.T) {
	transport := &scriptedTransport{errs: []error{errors.New("timeout")}}
	c := New(transport, staticConfig{config()}, now)
	_, e := c.PublishSocial(context.Background(), account(), runtime(), sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "hello"}, nil)
	var remote *sdk.RemoteError
	if !errors.As(e, &remote) || remote.Code != "write_outcome_unknown" || remote.Retryable() {
		t.Fatalf("err=%v", e)
	}
}
func TestWebhookSecretVerificationAndDedup(t *testing.T) {
	c := New(&scriptedTransport{}, staticConfig{config()}, now)
	dedup := &memoryDedup{}
	req := sdk.SocialWebhookRequest{VerificationToken: []byte("MaxWebhookSecret_2026"), Body: webhookFixture, ReceivedAt: now()}
	first, e := c.ReceiveSocialWebhook(context.Background(), account(), runtime(), req, dedup)
	if e != nil {
		t.Fatal(e)
	}
	if first.Duplicate || first.EventType != "max.message_created" || first.RemoteChannelID != "-70801090403050" || first.RemoteObjectID != "mid.ffffbdb48e6c3775019d496b34394b84" {
		t.Fatalf("first=%+v", first)
	}
	second, e := c.ReceiveSocialWebhook(context.Background(), account(), runtime(), req, dedup)
	if e != nil || !second.Duplicate || second.DeliveryID != first.DeliveryID {
		t.Fatalf("second=%+v err=%v", second, e)
	}
	req.VerificationToken = []byte("wrong-secret")
	if _, e = c.ReceiveSocialWebhook(context.Background(), account(), runtime(), req, dedup); !errors.Is(e, ErrWebhookUnauthorized) {
		t.Fatalf("wrong secret err=%v", e)
	}
}

func TestWebhookRejectsInvalidTimestampBeforeDedup(t *testing.T) {
	var raw map[string]any
	if json.Unmarshal(webhookFixture, &raw) != nil {
		t.Fatal("fixture")
	}
	raw["timestamp"] = float64(time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	body, _ := json.Marshal(raw)
	d := &memoryDedup{}
	c := New(&scriptedTransport{}, staticConfig{config()}, now)
	_, err := c.ReceiveSocialWebhook(context.Background(), account(), runtime(), sdk.SocialWebhookRequest{VerificationToken: []byte("MaxWebhookSecret_2026"), Body: body, ReceivedAt: now()}, d)
	if !errors.Is(err, ErrWebhookInvalid) || len(d.seen) != 0 {
		t.Fatalf("err=%v seen=%d", err, len(d.seen))
	}
}

func TestWebhookRejectsOtherChannelBeforeDedup(t *testing.T) {
	var raw map[string]any
	if json.Unmarshal(webhookFixture, &raw) != nil {
		t.Fatal("fixture")
	}
	raw["chat_id"] = float64(-123)
	body, _ := json.Marshal(raw)
	d := &memoryDedup{}
	c := New(&scriptedTransport{}, staticConfig{config()}, now)
	_, e := c.ReceiveSocialWebhook(context.Background(), account(), runtime(), sdk.SocialWebhookRequest{VerificationToken: []byte("MaxWebhookSecret_2026"), Body: body, ReceivedAt: now()}, d)
	if e == nil || len(d.seen) != 0 {
		t.Fatalf("err=%v seen=%d", e, len(d.seen))
	}
}
func TestSubscribeAndUnsubscribeUseSecretAndExactUpdateTypes(t *testing.T) {
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"success":true}`)}, {StatusCode: 200, Body: []byte(`{"success":true}`)}}}
	c := New(transport, staticConfig{config()}, now)
	endpoint := "https://hooks.example.test/max/account"
	if e := c.SubscribeSocialWebhook(context.Background(), account(), runtime(), endpoint); e != nil {
		t.Fatal(e)
	}
	if len(transport.requests) != 1 || transport.requests[0].Path != "/subscriptions" {
		t.Fatal("subscribe request")
	}
	if !bytes.Contains(transport.requests[0].Body, []byte("MaxWebhookSecret_2026")) || !bytes.Contains(transport.requests[0].Body, []byte("message_created")) {
		t.Fatal("missing subscription security/types")
	}
	if e := c.UnsubscribeSocialWebhook(context.Background(), account(), runtime(), endpoint); e != nil {
		t.Fatal(e)
	}
	if transport.requests[1].Method != "DELETE" {
		t.Fatal("unsubscribe method")
	}
}
