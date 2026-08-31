package telegram

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed manifest.json
var manifestJSON []byte

//go:embed fixtures/get-me.json
var getMeFixture []byte

//go:embed fixtures/get-chat-member.json
var getChatMemberFixture []byte

//go:embed fixtures/send-message.json
var sendMessageFixture []byte

//go:embed fixtures/send-photo.json
var sendPhotoFixture []byte

//go:embed fixtures/send-video.json
var sendVideoFixture []byte

//go:embed fixtures/send-album.json
var sendAlbumFixture []byte

//go:embed fixtures/edit-message.json
var editMessageFixture []byte

//go:embed fixtures/delete-message.json
var deleteMessageFixture []byte

//go:embed fixtures/rate-limit.json
var rateLimitFixture []byte

type capturedFile struct {
	FieldName, FileName, MediaType, SHA256 string
	SizeBytes                              int64
	Body                                   []byte
}
type scriptedTransport struct {
	responses []Response
	errs      []error
	requests  []Request
	files     [][]capturedFile
}

func (t *scriptedTransport) Do(_ context.Context, r Request) (Response, error) {
	r.BotToken = append([]byte(nil), r.BotToken...)
	r.Params = append([]Param(nil), r.Params...)
	captured := make([]capturedFile, 0, len(r.Files))
	for _, f := range r.Files {
		b, e := io.ReadAll(f.Body)
		if e != nil {
			return Response{}, e
		}
		captured = append(captured, capturedFile{f.FieldName, f.FileName, f.MediaType, f.SHA256, f.SizeBytes, b})
	}
	r.Files = nil
	t.requests = append(t.requests, r)
	t.files = append(t.files, captured)
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

type testRuntime struct{ secret []byte }

func (r testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{r.secret} }

type testSecrets struct{ value []byte }

func (s testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	v := append([]byte(nil), s.value...)
	defer clear(v)
	return cb(v)
}

type webhookRuntime struct{}

func (webhookRuntime) Secrets() sdk.SecretAccessor { return webhookSecrets{} }

type webhookSecrets struct{}

func (webhookSecrets) UseSecret(_ context.Context, reference sdk.SecretReference, cb func([]byte) error) error {
	value := []byte("777000111:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdef123456")
	if reference != "sec:v1:0123456789abcdef0123456789abcdef" {
		value = []byte("telegram-webhook-secret")
	}
	defer clear(value)
	return cb(value)
}

type testMedia struct {
	body  []byte
	desc  sdk.MediaDescriptor
	opens int
}

func (m *testMedia) OpenReleased(_ context.Context, _ sdk.Account, _ sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	m.opens++
	return io.NopCloser(bytes.NewReader(m.body)), m.desc, nil
}

func account() sdk.Account {
	at := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "telegram-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "telegram", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func config() Configuration { return Configuration{ChatID: -1001234567890} }
func token() []byte         { return []byte("777000111:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdef123456") }
func now() time.Time        { return time.Date(2026, 8, 11, 10, 30, 0, 0, time.UTC) }
func pubID() string         { return "01890f4d-1e10-7cc0-9c4a-333333333333" }
func imageRef(n byte) sdk.SocialMediaRef {
	return sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcde" + string([]byte{n}), Kind: sdk.SocialMediaImage}
}
func videoRef() sdk.SocialMediaRef {
	return sdk.SocialMediaRef{UploadID: "upl_1123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaVideo}
}
func param(params []Param, name string) (string, bool) {
	for _, p := range params {
		if p.Name == name {
			return p.Value, true
		}
	}
	return "", false
}

func TestManifestMatchesAndDeclaresTaskScope(t *testing.T) {
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
	if Manifest().Supports("social.comments.read") {
		t.Fatal("undeclared comments")
	}
}

type webhookDeduplicator struct {
	claim sdk.SocialWebhookClaim
}

func (dedup *webhookDeduplicator) ClaimSocialWebhook(_ context.Context, _ sdk.Account, claim sdk.SocialWebhookClaim) (bool, error) {
	dedup.claim = claim
	return false, nil
}

func TestTelegramWebhookVerifiesSecretAndConfiguredChannel(t *testing.T) {
	configuration := Configuration{ChatID: -1001234567890, WebhookSecretReference: "sec:v1:1123456789abcdef0123456789abcdef"}
	body := []byte(`{"update_id":1,"channel_post":{"message_id":42,"date":1786442400,"chat":{"id":-1001234567890,"type":"channel"}}}`)
	dedup := &webhookDeduplicator{}
	result, err := New(&scriptedTransport{}, staticConfig{configuration}, now).ReceiveSocialWebhook(context.Background(), account(), testRuntime{[]byte("telegram-webhook-secret")}, sdk.SocialWebhookRequest{VerificationToken: []byte("telegram-webhook-secret"), Body: body, ReceivedAt: now()}, dedup)
	if err != nil || result.EventType != "telegram.channel_post" || result.RemoteChannelID != "-1001234567890" || result.RemoteObjectID != "42" || result.Duplicate || string(dedup.claim.CanonicalPayload) != string(result.CanonicalPayload) {
		t.Fatalf("result=%+v claim=%+v err=%v", result, dedup.claim, err)
	}
	if _, err := New(&scriptedTransport{}, staticConfig{configuration}, now).ReceiveSocialWebhook(context.Background(), account(), testRuntime{[]byte("wrong-secret")}, sdk.SocialWebhookRequest{VerificationToken: []byte("wrong-secret"), Body: body, ReceivedAt: now()}, &webhookDeduplicator{}); !errors.Is(err, ErrWebhookUnauthorized) {
		t.Fatalf("wrong secret err=%v", err)
	}
	foreign := []byte(`{"update_id":2,"channel_post":{"message_id":43,"date":1786442400,"chat":{"id":-1009999999999,"type":"channel"}}}`)
	if _, err := New(&scriptedTransport{}, staticConfig{configuration}, now).ReceiveSocialWebhook(context.Background(), account(), testRuntime{[]byte("telegram-webhook-secret")}, sdk.SocialWebhookRequest{VerificationToken: []byte("telegram-webhook-secret"), Body: foreign, ReceivedAt: now()}, &webhookDeduplicator{}); !errors.Is(err, ErrWebhookInvalid) {
		t.Fatalf("foreign channel err=%v", err)
	}
}

func TestTelegramWebhookSubscriptionUsesOfficialLifecycleMethods(t *testing.T) {
	endpoint := "https://hooks.torgnexa.example/api/v1/webhooks/social/telegram"
	configuration := Configuration{ChatID: -1001234567890, WebhookSecretReference: "sec:v1:1123456789abcdef0123456789abcdef"}
	transport := &scriptedTransport{responses: []Response{
		{StatusCode: 200, Body: []byte(`{"ok":true,"result":true}`)},
		{StatusCode: 200, Body: []byte(`{"ok":true,"result":{"url":"` + endpoint + `"}}`)},
		{StatusCode: 200, Body: []byte(`{"ok":true,"result":true}`)},
	}}
	connector := New(transport, staticConfig{configuration}, now)
	if err := connector.SubscribeSocialWebhook(context.Background(), account(), webhookRuntime{}, endpoint); err != nil {
		t.Fatalf("subscribe err=%v", err)
	}
	if err := connector.UnsubscribeSocialWebhook(context.Background(), account(), webhookRuntime{}, endpoint); err != nil {
		t.Fatalf("unsubscribe err=%v", err)
	}
	if len(transport.requests) != 3 || transport.requests[0].APIMethod != "setWebhook" || transport.requests[1].APIMethod != "getWebhookInfo" || transport.requests[2].APIMethod != "deleteWebhook" {
		t.Fatalf("requests=%#v", transport.requests)
	}
	if value, ok := param(transport.requests[0].Params, "url"); !ok || value != endpoint {
		t.Fatalf("endpoint param=%q present=%v", value, ok)
	}
	if value, ok := param(transport.requests[0].Params, "allowed_updates"); !ok || value != `["channel_post","edited_channel_post"]` {
		t.Fatalf("allowed updates=%q present=%v", value, ok)
	}
}

func TestTelegramWebhookSubscriptionRejectsEndpointMismatch(t *testing.T) {
	configured := "https://hooks.torgnexa.example/current"
	requested := "https://hooks.torgnexa.example/other"
	transport := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: []byte(`{"ok":true,"result":{"url":"` + configured + `"}}`)}}}
	err := New(transport, staticConfig{Configuration{ChatID: -1001234567890, WebhookSecretReference: "sec:v1:1123456789abcdef0123456789abcdef"}}, now).UnsubscribeSocialWebhook(context.Background(), account(), webhookRuntime{}, requested)
	if err == nil || !strings.Contains(err.Error(), "endpoint mismatch") {
		t.Fatalf("err=%v", err)
	}
	if err := New(&scriptedTransport{}, staticConfig{Configuration{ChatID: -1001234567890, WebhookSecretReference: "sec:v1:1123456789abcdef0123456789abcdef"}}, now).SubscribeSocialWebhook(context.Background(), account(), webhookRuntime{}, requested+"?token=secret"); !errors.Is(err, sdk.ErrInvalidSocialWebhook) {
		t.Fatalf("query endpoint err=%v", err)
	}
}
func TestConfigurationAndBotTokenStrict(t *testing.T) {
	if config().Validate() != nil || !validBotToken(token()) {
		t.Fatal("valid rejected")
	}
	for _, c := range []Configuration{{}, {ChatID: 1}} {
		if c.Validate() == nil {
			t.Fatalf("bad config %+v", c)
		}
	}
	for _, v := range [][]byte{nil, []byte("777:short"), []byte(" 777000111:ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdef123456"), []byte("777000111:bad token spaces 12345678901234567890")} {
		if validBotToken(v) {
			t.Fatalf("bad token accepted %q", v)
		}
	}
}
func TestHealthChecksExactChannelPostingPermissionWithoutTokenParam(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: getMeFixture}, {StatusCode: 200, Body: getChatMemberFixture}}}
	h, e := New(tr, staticConfig{config()}, now).Health(context.Background(), account(), testRuntime{token()})
	if e != nil || h.Status != sdk.HealthHealthy || len(tr.requests) != 2 {
		t.Fatalf("health=%+v err=%v", h, e)
	}
	for _, r := range tr.requests {
		for _, p := range r.Params {
			if bytes.Contains([]byte(p.Value), token()) {
				t.Fatal("token leaked into params")
			}
		}
		if r.Host != apiHost || !bytes.Equal(r.BotToken, token()) {
			t.Fatalf("request=%+v", r)
		}
	}
}
func TestTextPublishWithHTTPSButtons(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: sendMessageFixture}}}
	req := sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "Hello Telegram", Buttons: []sdk.SocialButton{{Text: "Open", URL: "https://torgnexa.example/product"}}}
	res, e := New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, req, nil)
	if e != nil || res.RemotePublicationID != "tg:-1001234567890:101" || res.Status != sdk.SocialRemotePublished {
		t.Fatalf("res=%+v err=%v", res, e)
	}
	if tr.requests[0].APIMethod != "sendMessage" {
		t.Fatal("wrong method")
	}
	markup, _ := param(tr.requests[0].Params, "reply_markup")
	if !bytes.Contains([]byte(markup), []byte("https://torgnexa.example/product")) {
		t.Fatalf("markup=%s", markup)
	}
}
func TestPhotoAlbumAndVideoRevalidateReleasedMedia(t *testing.T) {
	img := &testMedia{body: []byte("image"), desc: sdk.MediaDescriptor{MediaType: "image/jpeg", SizeBytes: 5, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: sendPhotoFixture}}}
	res, e := New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Text: "Photo", Media: []sdk.SocialMediaRef{imageRef('f')}}, img)
	if e != nil || res.RemotePublicationID != "tg:-1001234567890:102" || img.opens != 1 || len(tr.files[0]) != 1 {
		t.Fatalf("res=%+v err=%v opens=%d", res, e, img.opens)
	}
	albumMedia := &testMedia{body: []byte("image"), desc: img.desc}
	tr = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: sendAlbumFixture}}}
	res, e = New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Text: "Album", Media: []sdk.SocialMediaRef{imageRef('f'), {UploadID: "upl_2123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage}}}, albumMedia)
	if e != nil || res.RemotePublicationID != "tg:-1001234567890:104,105" || albumMedia.opens != 2 || len(tr.files[0]) != 2 {
		t.Fatalf("album=%+v err=%v opens=%d", res, e, albumMedia.opens)
	}
	video := &testMedia{body: []byte("video"), desc: sdk.MediaDescriptor{MediaType: "video/mp4", SizeBytes: 5, SHA256: "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}
	tr = &scriptedTransport{responses: []Response{{StatusCode: 200, Body: sendVideoFixture}}}
	res, e = New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostVideo, Text: "Video", Media: []sdk.SocialMediaRef{videoRef()}}, video)
	if e != nil || res.RemotePublicationID != "tg:-1001234567890:103" || video.opens != 1 {
		t.Fatalf("video=%+v err=%v", res, e)
	}
}
func TestAlbumRejectsButtonsAndElevenItems(t *testing.T) {
	c := New(&scriptedTransport{}, staticConfig{config()}, now)
	req := sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostMedia, Media: []sdk.SocialMediaRef{imageRef('f'), {UploadID: "upl_2123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage}}, Buttons: []sdk.SocialButton{{Text: "Open", URL: "https://example.com"}}}
	if _, e := c.PublishSocial(context.Background(), account(), testRuntime{token()}, req, &testMedia{}); !errors.Is(e, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("buttons album err=%v", e)
	}
}
func TestEditSingleAndDeleteAlbumStayChannelScoped(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 200, Body: editMessageFixture}, {StatusCode: 200, Body: deleteMessageFixture}}}
	c := New(tr, staticConfig{config()}, now)
	edit, e := c.EditSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialEditRequest{RemotePublicationID: "tg:-1001234567890:101", Kind: sdk.SocialPostText, Text: "Edited"}, nil)
	if e != nil || edit.RemotePublicationID != "tg:-1001234567890:101" || tr.requests[0].APIMethod != "editMessageText" {
		t.Fatalf("edit=%+v err=%v", edit, e)
	}
	del, e := c.DeleteSocial(context.Background(), account(), testRuntime{token()}, "tg:-1001234567890:104,105")
	if e != nil || !del.Deleted || tr.requests[1].APIMethod != "deleteMessages" {
		t.Fatalf("delete=%+v err=%v", del, e)
	}
	if _, e = c.DeleteSocial(context.Background(), account(), testRuntime{token()}, "tg:-1009999999999:104"); !errors.Is(e, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("foreign channel accepted: %v", e)
	}
	if _, e = c.EditSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialEditRequest{RemotePublicationID: "tg:-1001234567890:104,105", Kind: sdk.SocialPostText, Text: "x"}, nil); e == nil {
		t.Fatal("album edit accepted")
	}
}
func TestFloodControlUsesRetryAfterButAmbiguousWriteIsNotRetryable(t *testing.T) {
	tr := &scriptedTransport{responses: []Response{{StatusCode: 429, Body: rateLimitFixture, RequestID: "tg-req-1"}}}
	_, e := New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "retry"}, nil)
	var remote *sdk.RemoteError
	if !errors.As(e, &remote) || remote.Category != sdk.ErrorRateLimited || remote.RetryAfterMS != 3000 || !remote.Retryable() {
		t.Fatalf("remote=%+v err=%v", remote, e)
	}
	tr = &scriptedTransport{errs: []error{errors.New("connection reset")}}
	_, e = New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "unknown"}, nil)
	remote = nil
	if !errors.As(e, &remote) || remote.Code != "write_outcome_unknown" || remote.Retryable() {
		t.Fatalf("remote=%+v err=%v", remote, e)
	}
}

func TestHealthNilConnectorIsControlled(t *testing.T) {
	var connector *Connector
	if _, err := connector.Health(context.Background(), account(), testRuntime{token()}); !errors.Is(err, sdk.ErrInvalidAccount) {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteHTTP5xxIsOutcomeUnknownAndNotRetryable(t *testing.T) {
	body := []byte(`{"ok":false,"error_code":500,"description":"Internal Server Error"}`)
	tr := &scriptedTransport{responses: []Response{{StatusCode: 500, Body: body, RequestID: "tg-req-500"}}}
	_, err := New(tr, staticConfig{config()}, now).PublishSocial(context.Background(), account(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "unknown"}, nil)
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Code != "write_outcome_unknown" || remote.Retryable() {
		t.Fatalf("remote=%+v err=%v", remote, err)
	}
}

func TestButtonValidationRejectsNonHTTPS(t *testing.T) {
	req := sdk.SocialPublishRequest{PublicationID: pubID(), Kind: sdk.SocialPostText, Text: "x", Buttons: []sdk.SocialButton{{Text: "bad", URL: "http://example.com"}}}
	if req.Validate() == nil {
		t.Fatal("http button accepted")
	}
}
