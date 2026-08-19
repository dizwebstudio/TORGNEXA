package youtube

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

//go:embed manifest.json
var manifestFixture []byte

//go:embed fixtures/channel.json
var channelFixture []byte

//go:embed fixtures/upload-session.json
var sessionFixture []byte

//go:embed fixtures/video-processing.json
var processingFixture []byte

//go:embed fixtures/video-published.json
var publishedFixture []byte

//go:embed fixtures/comments-page.json
var commentsFixture []byte

type fakeConfig struct{ value Configuration }

func (f fakeConfig) Resolve(context.Context, sdk.Account) (Configuration, error) { return f.value, nil }

type fakeSecrets struct{ value []byte }

func (f fakeSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	b := append([]byte(nil), f.value...)
	defer clear(b)
	return cb(b)
}

type fakeRuntime struct{ secrets sdk.SecretAccessor }

func (f fakeRuntime) Secrets() sdk.SecretAccessor { return f.secrets }

type fakeMedia struct {
	data       []byte
	descriptor sdk.MediaDescriptor
	opens      int
}

func (f *fakeMedia) OpenReleased(context.Context, sdk.Account, sdk.SocialMediaRef) (io.ReadCloser, sdk.MediaDescriptor, error) {
	f.opens++
	return io.NopCloser(bytes.NewReader(f.data)), f.descriptor, nil
}

type fakeTransport struct {
	channel     Channel
	session     UploadSession
	processing  VideoRecord
	published   VideoRecord
	comments    CommentPage
	channelErr  error
	startErr    error
	readErr     error
	commentErr  error
	ambiguousAt int
	probeOffset int64
	uploadCalls int
	starts      []UploadMetadata
	chunks      []UploadChunkRequest
	probes      int
}

func (f *fakeTransport) ResolveOwnedChannel(context.Context, []byte) (Channel, error) {
	return f.channel, f.channelErr
}
func (f *fakeTransport) StartResumableUpload(_ context.Context, _ []byte, channel string, m UploadMetadata) (UploadSession, error) {
	if channel != f.channel.ID {
		return UploadSession{}, &GoogleFailure{Kind: FailureNotFound, Reason: "channelNotFound"}
	}
	f.starts = append(f.starts, m)
	return f.session, f.startErr
}
func (f *fakeTransport) UploadChunk(_ context.Context, _ []byte, r UploadChunkRequest) (UploadProgress, error) {
	f.uploadCalls++
	clone := r
	clone.Body = append([]byte(nil), r.Body...)
	f.chunks = append(f.chunks, clone)
	if f.ambiguousAt > 0 && f.uploadCalls == f.ambiguousAt {
		return UploadProgress{}, &GoogleFailure{Kind: FailureUnavailable, RequestID: "upload-ambiguous"}
	}
	next := r.Offset + int64(len(r.Body))
	if next < r.TotalBytes {
		return UploadProgress{NextOffset: next}, nil
	}
	return UploadProgress{NextOffset: r.TotalBytes, Complete: true, Video: f.processing}, nil
}
func (f *fakeTransport) ProbeResumableUpload(context.Context, []byte, string, int64) (UploadProgress, error) {
	f.probes++
	return UploadProgress{NextOffset: f.probeOffset}, nil
}
func (f *fakeTransport) ReadVideo(context.Context, []byte, string) (VideoRecord, error) {
	return f.published, f.readErr
}
func (f *fakeTransport) ListCommentThreads(context.Context, []byte, string, string, int) (CommentPage, error) {
	return f.comments, f.commentErr
}

func fixedNow() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) }
func account() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "youtube-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "youtube", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func runtime() sdk.Runtime {
	return fakeRuntime{secrets: fakeSecrets{value: []byte("youtube-oauth-test-token-0123456789012345")}}
}
func config() Configuration {
	return Configuration{ChannelID: "UCfixtureChannel00000001", CategoryID: "22", PrivacyStatus: "unlisted", NotifySubscribers: false, SelfDeclaredMadeForKids: false, ContainsSyntheticMedia: false}
}
func descriptor(n int) sdk.MediaDescriptor {
	return sdk.MediaDescriptor{MediaType: "video/mp4", SizeBytes: int64(n), SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
}
func request() sdk.SocialPublishRequest {
	return sdk.SocialPublishRequest{PublicationID: "01890f4d-1e10-7cc0-9c4a-000000000048", Kind: sdk.SocialPostVideo, Text: "Release title\nLonger release description", Media: []sdk.SocialMediaRef{{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaVideo}}}
}
func fixtureTransport(t *testing.T) *fakeTransport {
	t.Helper()
	var ch Channel
	var session UploadSession
	var processing VideoRecord
	var published VideoRecord
	var comments CommentPage
	if json.Unmarshal(channelFixture, &ch) != nil || json.Unmarshal(sessionFixture, &session) != nil || json.Unmarshal(processingFixture, &processing) != nil || json.Unmarshal(publishedFixture, &published) != nil || json.Unmarshal(commentsFixture, &comments) != nil {
		t.Fatal("fixture parse failed")
	}
	return &fakeTransport{channel: ch, session: session, processing: processing, published: published, comments: comments}
}

func TestManifestMatchesJSON(t *testing.T) {
	var file sdk.Manifest
	if err := json.Unmarshal(manifestFixture, &file); err != nil {
		t.Fatal(err)
	}
	got := Manifest().Canonical()
	file = file.Canonical()
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(file)
	if !bytes.Equal(a, b) {
		t.Fatalf("manifest drift\n%s\n%s", a, b)
	}
	if err := got.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestHealthRequiresOwnedConfiguredChannel(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	health, err := c.Health(context.Background(), account(), runtime())
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	tr.channel.ID = "UCforeignChannel0000001"
	health, err = c.Health(context.Background(), account(), runtime())
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "remote_contract_invalid" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}

func TestResumableUploadChunksAndMetadata(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	c.chunkSize = chunkQuantum
	body := bytes.Repeat([]byte{0x42}, 2*chunkQuantum+12345)
	media := &fakeMedia{data: body, descriptor: descriptor(len(body))}
	got, err := c.PublishSocial(context.Background(), account(), runtime(), request(), media)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemotePublicationID != "youtube:UCfixtureChannel00000001:dQw4w9WgXcQ" || got.Status != sdk.SocialRemoteProcessing {
		t.Fatalf("unexpected result %+v", got)
	}
	if media.opens != 1 || len(tr.starts) != 1 || len(tr.chunks) != 3 {
		t.Fatalf("opens=%d starts=%d chunks=%d", media.opens, len(tr.starts), len(tr.chunks))
	}
	m := tr.starts[0]
	if m.ExternalID != request().PublicationID || m.Title != "Release title" || m.Description != request().Text || m.CategoryID != "22" || m.PrivacyStatus != "unlisted" {
		t.Fatalf("metadata %+v", m)
	}
	if len(tr.chunks[0].Body)%chunkQuantum != 0 || len(tr.chunks[1].Body)%chunkQuantum != 0 || len(tr.chunks[2].Body) != 12345 {
		t.Fatalf("chunk sizes %d %d %d", len(tr.chunks[0].Body), len(tr.chunks[1].Body), len(tr.chunks[2].Body))
	}
}

func TestInterruptedChunkProbesAndResumes(t *testing.T) {
	tr := fixtureTransport(t)
	tr.ambiguousAt = 1
	tr.probeOffset = 0
	c := New(tr, fakeConfig{config()}, fixedNow)
	c.chunkSize = chunkQuantum
	body := bytes.Repeat([]byte{0x33}, chunkQuantum+100)
	got, err := c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sdk.SocialRemoteProcessing || tr.probes != 1 || len(tr.chunks) != 3 {
		t.Fatalf("got=%+v probes=%d chunks=%d", got, tr.probes, len(tr.chunks))
	}
	if tr.chunks[0].Offset != 0 || tr.chunks[1].Offset != 0 || tr.chunks[2].Offset != chunkQuantum {
		t.Fatalf("offsets %d %d %d", tr.chunks[0].Offset, tr.chunks[1].Offset, tr.chunks[2].Offset)
	}
}

func TestProbeCanConfirmWholeAmbiguousChunk(t *testing.T) {
	tr := fixtureTransport(t)
	tr.ambiguousAt = 1
	tr.probeOffset = chunkQuantum
	c := New(tr, fakeConfig{config()}, fixedNow)
	c.chunkSize = chunkQuantum
	body := bytes.Repeat([]byte{0x11}, chunkQuantum+77)
	_, err := c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	if err != nil {
		t.Fatal(err)
	}
	if tr.probes != 1 || len(tr.chunks) != 2 || tr.chunks[1].Offset != chunkQuantum {
		t.Fatalf("probes=%d chunks=%d offset=%d", tr.probes, len(tr.chunks), tr.chunks[1].Offset)
	}
}

func TestUploadQuotaAndAmbiguousSessionFailClosed(t *testing.T) {
	body := bytes.Repeat([]byte{0x42}, 100)
	tr := fixtureTransport(t)
	tr.startErr = &GoogleFailure{Kind: FailureQuotaExceeded, Reason: "uploadLimitExceeded", RequestID: "quota-1"}
	c := New(tr, fakeConfig{config()}, fixedNow)
	_, err := c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "upload_limit_exceeded" {
		t.Fatalf("quota=%#v err=%v", remote, err)
	}
	tr = fixtureTransport(t)
	tr.startErr = &GoogleFailure{Kind: FailureUnavailable, RequestID: "start-ambiguous"}
	c = New(tr, fakeConfig{config()}, fixedNow)
	_, err = c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorConflict || remote.Code != "upload_session_outcome_unknown" {
		t.Fatalf("start ambiguity=%#v err=%v", remote, err)
	}
}

func TestReadStatusMapsProcessingPublishedAndFailure(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	id := "youtube:UCfixtureChannel00000001:dQw4w9WgXcQ"
	got, err := c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), id)
	if err != nil || got.Status != sdk.SocialRemotePublished {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	tr.published = VideoRecord{VideoID: "dQw4w9WgXcQ", ChannelID: config().ChannelID, UploadStatus: "rejected", RejectionReason: "copyright"}
	got, err = c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), id)
	if err != nil || got.Status != sdk.SocialRemoteFailed || got.ReasonCode != "copyright" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	_, err = c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "youtube:UCforeignChannel0000001:dQw4w9WgXcQ")
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("foreign id accepted: %v", err)
	}
}

func TestCommentsAreBoundedAndAccountScoped(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	req := sdk.SocialCommentReadRequest{RemotePublicationID: "youtube:UCfixtureChannel00000001:dQw4w9WgXcQ", Limit: 20}
	page, err := c.ReadSocialComments(context.Background(), account(), runtime(), req)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].RemoteCommentID != "UgfixtureComment001" || page.NextCursor != "next_fixture_1" {
		t.Fatalf("page=%+v", page)
	}
	req.RemotePublicationID = "youtube:UCforeignChannel0000001:dQw4w9WgXcQ"
	_, err = c.ReadSocialComments(context.Background(), account(), runtime(), req)
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("foreign comments accepted: %v", err)
	}
}

func TestMetadataRejectsAngleBracketsAndDescriptionOver5000Bytes(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	bad := request()
	bad.Text = "bad <title>"
	_, err := c.PublishSocial(context.Background(), account(), runtime(), bad, &fakeMedia{data: []byte{1}, descriptor: descriptor(1)})
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("angle brackets accepted: %v", err)
	}
	bad = request()
	bad.Text = string(bytes.Repeat([]byte("a"), maxDescriptionBytes+1))
	_, err = c.PublishSocial(context.Background(), account(), runtime(), bad, &fakeMedia{data: []byte{1}, descriptor: descriptor(1)})
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("oversized description accepted: %v", err)
	}
}
