package vk

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

//go:embed fixtures/wall-post.json
var wallPostFixture []byte

//go:embed fixtures/wall-post-status.json
var wallPostStatusFixture []byte

//go:embed fixtures/upload-server.json
var uploadServerFixture []byte

//go:embed fixtures/upload-photo.json
var uploadPhotoFixture []byte

//go:embed fixtures/save-photo.json
var savePhotoFixture []byte

//go:embed fixtures/comments.json
var commentsFixture []byte

//go:embed fixtures/comment-reply.json
var commentReplyFixture []byte

//go:embed fixtures/post-reach.json
var postReachFixture []byte

//go:embed fixtures/api-rate-limit.json
var rateLimitFixture []byte

type scriptedTransport struct {
	apiResponses    []Response
	apiErrors       []error
	uploadResponses []Response
	uploadErrors    []error
	requests        []Request
	uploads         []capturedUpload
}

type capturedUpload struct {
	URL, FieldName, FileName, MediaType, SHA256 string
	SizeBytes                                   int64
	Body                                        []byte
}

func (transport *scriptedTransport) Do(_ context.Context, request Request) (Response, error) {
	request.AccessToken = append([]byte(nil), request.AccessToken...)
	request.Params = append([]Param(nil), request.Params...)
	transport.requests = append(transport.requests, request)
	index := len(transport.requests) - 1
	if index < len(transport.apiErrors) && transport.apiErrors[index] != nil {
		return Response{}, transport.apiErrors[index]
	}
	if index >= len(transport.apiResponses) {
		return Response{}, errors.New("unexpected api call")
	}
	return transport.apiResponses[index], nil
}

func (transport *scriptedTransport) Upload(_ context.Context, request UploadRequest) (Response, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return Response{}, err
	}
	transport.uploads = append(transport.uploads, capturedUpload{URL: request.URL, FieldName: request.FieldName, FileName: request.FileName, MediaType: request.MediaType, SizeBytes: request.SizeBytes, SHA256: request.SHA256, Body: body})
	index := len(transport.uploads) - 1
	if index < len(transport.uploadErrors) && transport.uploadErrors[index] != nil {
		return Response{}, transport.uploadErrors[index]
	}
	if index >= len(transport.uploadResponses) {
		return Response{}, errors.New("unexpected upload call")
	}
	return transport.uploadResponses[index], nil
}

type staticConfig struct{ value Configuration }

func (source staticConfig) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return source.value, nil
}

type testRuntime struct{ secret []byte }

func (runtime testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{value: runtime.secret} }

type testSecrets struct{ value []byte }

func (secrets testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, callback func([]byte) error) error {
	value := append([]byte(nil), secrets.value...)
	defer clear(value)
	return callback(value)
}

type testMedia struct {
	body       []byte
	descriptor sdk.MediaDescriptor
	opens      int
}

func (media *testMedia) OpenReleased(_ context.Context, _ sdk.Account, _ sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	media.opens++
	return io.NopCloser(bytes.NewReader(media.body)), media.descriptor, nil
}

func testAccount() sdk.Account {
	created := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "vk-account", OrganizationID: "01890f4d-1e10-7cc0-9c4a-111111111111", WorkspaceID: "01890f4d-1e10-7cc0-9c4a-222222222222", ConnectorID: "vk", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}
func token() []byte         { return []byte("vk1.synthetic-user-oauth-token-0123456789abcdef") }
func config() Configuration { return Configuration{GroupID: 12345} }
func now() time.Time        { return time.Date(2026, 8, 11, 10, 30, 0, 0, time.UTC) }
func publicationID() string { return "01890f4d-1e10-7cc0-9c4a-333333333333" }
func replyID() string       { return "01890f4d-1e10-7cc0-9c4a-444444444444" }
func uploadRef() sdk.SocialMediaRef {
	return sdk.SocialMediaRef{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaImage, AltText: "Product"}
}

func TestManifestMatchesJSONAndConservativeCapabilities(t *testing.T) {
	var got sdk.Manifest
	if json.Unmarshal(manifestJSON, &got) != nil || got.Validate() != nil || Manifest().Validate() != nil {
		t.Fatal("manifest invalid")
	}
	if !reflect.DeepEqual(got.Canonical(), Manifest().Canonical()) {
		t.Fatal("manifest drift")
	}
	for _, capability := range []sdk.Capability{"social.post.video", "social.post.edit", "social.post.delete"} {
		if Manifest().Supports(capability) {
			t.Fatalf("unqualified capability declared: %s", capability)
		}
	}
	if Manifest().Auth[0].Kind != sdk.AuthOAuth2 || Manifest().Auth[0].SecretClass != "social.user-oauth" {
		t.Fatalf("auth=%+v", Manifest().Auth)
	}
}

func TestConfigurationAndTokenAreStrict(t *testing.T) {
	if config().Validate() != nil || !validAccessToken(token()) {
		t.Fatal("valid config/token rejected")
	}
	if !errors.Is((Configuration{}).Validate(), ErrInvalidConfiguration) {
		t.Fatal("zero group accepted")
	}
	for _, bad := range [][]byte{nil, []byte("short"), []byte(" token-with-spaces "), []byte("token\nwith-newline-0123456789")} {
		if validAccessToken(bad) {
			t.Fatalf("bad token accepted: %q", bad)
		}
	}
}

func TestHealthUsesUserOAuthWithoutLeakingTokenIntoParams(t *testing.T) {
	transport := &scriptedTransport{apiResponses: []Response{{StatusCode: 200, Body: []byte(`{"response":{"groups":[{"id":12345}]}}`)}}}
	health, err := New(transport, staticConfig{config()}, now).Health(context.Background(), testAccount(), testRuntime{token()})
	if err != nil || health.Status != sdk.HealthHealthy || len(transport.requests) != 1 {
		t.Fatalf("health=%+v err=%v requests=%d", health, err, len(transport.requests))
	}
	request := transport.requests[0]
	if request.APIMethod != "groups.getById" || request.Host != apiHost || !hasParam(request.Params, "v", apiVersion) || hasParam(request.Params, "access_token", string(token())) {
		t.Fatalf("request=%+v", request)
	}
}

func TestTextPublishUsesCanonicalPublicationAsVKGuid(t *testing.T) {
	transport := &scriptedTransport{apiResponses: []Response{{StatusCode: 200, Body: wallPostFixture}}}
	request := sdk.SocialPublishRequest{PublicationID: publicationID(), Kind: sdk.SocialPostText, Text: "Hello VK"}
	result, err := New(transport, staticConfig{config()}, now).PublishSocial(context.Background(), testAccount(), testRuntime{token()}, request, nil)
	if err != nil || result.RemotePublicationID != "-12345_456" || result.Status != sdk.SocialRemotePublished || !result.ObservedAt.Equal(now()) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(transport.requests) != 1 || transport.requests[0].APIMethod != "wall.post" || !hasParam(transport.requests[0].Params, "guid", publicationID()) || !hasParam(transport.requests[0].Params, "owner_id", "-12345") || !hasParam(transport.requests[0].Params, "from_group", "1") {
		t.Fatalf("params=%+v", transport.requests[0].Params)
	}
}

func TestMediaPublishRevalidatesReleasedUploadAndUsesSafeUploadHost(t *testing.T) {
	transport := &scriptedTransport{
		apiResponses:    []Response{{StatusCode: 200, Body: uploadServerFixture}, {StatusCode: 200, Body: savePhotoFixture}, {StatusCode: 200, Body: wallPostFixture}},
		uploadResponses: []Response{{StatusCode: 200, Body: uploadPhotoFixture}},
	}
	media := &testMedia{body: []byte("synthetic-image"), descriptor: sdk.MediaDescriptor{MediaType: "image/jpeg", SizeBytes: int64(len("synthetic-image")), SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}
	request := sdk.SocialPublishRequest{PublicationID: publicationID(), Kind: sdk.SocialPostMedia, Text: "Photo", Media: []sdk.SocialMediaRef{uploadRef()}}
	result, err := New(transport, staticConfig{config()}, now).PublishSocial(context.Background(), testAccount(), testRuntime{token()}, request, media)
	if err != nil || result.RemotePublicationID != "-12345_456" || media.opens != 1 || len(transport.uploads) != 1 {
		t.Fatalf("result=%+v err=%v opens=%d uploads=%d", result, err, media.opens, len(transport.uploads))
	}
	if transport.requests[0].APIMethod != "photos.getWallUploadServer" || transport.requests[1].APIMethod != "photos.saveWallPhoto" || transport.requests[2].APIMethod != "wall.post" || !hasParam(transport.requests[2].Params, "attachments", "photo-12345_9001") {
		t.Fatalf("requests=%+v", transport.requests)
	}
	if string(transport.uploads[0].Body) != "synthetic-image" || transport.uploads[0].URL != "https://pu.vk.com/c12345/upload.php?act=do_add" {
		t.Fatalf("upload=%+v", transport.uploads[0])
	}
}

func TestMediaPublishRejectsUntrustedUploadURLBeforeNetworkUpload(t *testing.T) {
	transport := &scriptedTransport{apiResponses: []Response{{StatusCode: 200, Body: []byte(`{"response":{"upload_url":"https://evil.example/upload"}}`)}}}
	media := &testMedia{body: []byte("x"), descriptor: sdk.MediaDescriptor{MediaType: "image/jpeg", SizeBytes: 1, SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}}
	_, err := New(transport, staticConfig{config()}, now).PublishSocial(context.Background(), testAccount(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: publicationID(), Kind: sdk.SocialPostMedia, Media: []sdk.SocialMediaRef{uploadRef()}}, media)
	if !errors.Is(err, ErrInvalidResponse) || len(transport.uploads) != 0 {
		t.Fatalf("err=%v uploads=%d", err, len(transport.uploads))
	}
}

func TestRemoteStatusMapsPresentAndMissingPost(t *testing.T) {
	transport := &scriptedTransport{apiResponses: []Response{{StatusCode: 200, Body: wallPostStatusFixture}, {StatusCode: 200, Body: []byte(`{"response":[]}`)}}}
	connector := New(transport, staticConfig{config()}, now)
	result, err := connector.ReadSocialPublicationStatus(context.Background(), testAccount(), testRuntime{token()}, "-12345_456")
	if err != nil || result.Status != sdk.SocialRemotePublished {
		t.Fatalf("published=%+v err=%v", result, err)
	}
	missing, err := connector.ReadSocialPublicationStatus(context.Background(), testAccount(), testRuntime{token()}, "-12345_999")
	if err != nil || missing.Status != sdk.SocialRemoteFailed || missing.ReasonCode != "remote_missing" {
		t.Fatalf("missing=%+v err=%v", missing, err)
	}
}

func TestCommentsReadReplyAndAnalyticsRemainGroupScoped(t *testing.T) {
	transport := &scriptedTransport{apiResponses: []Response{{StatusCode: 200, Body: commentsFixture}, {StatusCode: 200, Body: commentReplyFixture}, {StatusCode: 200, Body: postReachFixture}}}
	connector := New(transport, staticConfig{config()}, now)
	page, err := connector.ReadSocialComments(context.Background(), testAccount(), testRuntime{token()}, sdk.SocialCommentReadRequest{RemotePublicationID: "-12345_456", Limit: 100})
	if err != nil || len(page.Items) != 2 || page.Items[1].ParentRemoteCommentID != "77" || page.Items[0].AuthorRemoteID != "1001" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	reply, err := connector.ReplySocialComment(context.Background(), testAccount(), testRuntime{token()}, sdk.SocialCommentReplyRequest{RemotePublicationID: "-12345_456", ReplyToCommentID: "77", Text: "Thanks", IdempotencyKey: replyID()})
	if err != nil || reply.RemoteCommentID != "79" || !hasParam(transport.requests[1].Params, "guid", replyID()) || !hasParam(transport.requests[1].Params, "from_group", "12345") {
		t.Fatalf("reply=%+v err=%v params=%+v", reply, err, transport.requests[1].Params)
	}
	analytics, err := connector.ReadSocialAnalytics(context.Background(), testAccount(), testRuntime{token()}, sdk.SocialAnalyticsRequest{RemotePublicationIDs: []string{"-12345_456"}})
	if err != nil || len(analytics) != 1 || analytics[0].ReachTotal != 300 || analytics[0].ReachFollowers != 120 || analytics[0].LinkClicks != 11 || analytics[0].CommunityJoins != 3 {
		t.Fatalf("analytics=%+v err=%v", analytics, err)
	}
	if _, err := connector.ReadSocialComments(context.Background(), testAccount(), testRuntime{token()}, sdk.SocialCommentReadRequest{RemotePublicationID: "-99999_456", Limit: 1}); !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("foreign group accepted: %v", err)
	}
}

func TestVKEnvelopeErrorsNormalizeWithoutRawMessage(t *testing.T) {
	transport := &scriptedTransport{apiResponses: []Response{{StatusCode: 200, Body: rateLimitFixture, RequestID: "vk-req-1"}}}
	_, err := New(transport, staticConfig{config()}, now).PublishSocial(context.Background(), testAccount(), testRuntime{token()}, sdk.SocialPublishRequest{PublicationID: publicationID(), Kind: sdk.SocialPostText, Text: "retry"}, nil)
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "rate_limited" || remote.RetryAfterMS != 1000 || remote.RemoteRequestID != "vk-req-1" {
		t.Fatalf("remote=%+v err=%v", remote, err)
	}
	if bytes.Contains([]byte(err.Error()), []byte("Too many requests")) {
		t.Fatalf("raw provider message leaked: %v", err)
	}
}

func TestCommentCursorIsOpaqueAndBoundToPublication(t *testing.T) {
	cursor := encodeCommentCursor(12345, 456, 100)
	if offset, err := decodeCommentCursor(cursor, 12345, 456); err != nil || offset != 100 {
		t.Fatalf("offset=%d err=%v", offset, err)
	}
	if _, err := decodeCommentCursor(cursor, 12345, 457); !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("cursor replay accepted: %v", err)
	}
}

func hasParam(params []Param, name, value string) bool {
	for _, param := range params {
		if param.Name == name && param.Value == value {
			return true
		}
	}
	return false
}
