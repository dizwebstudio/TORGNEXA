package rutube

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
	channel    Channel
	session    UploadSession
	commit     VideoRecord
	status     VideoRecord
	channelErr error
	createErr  error
	uploadErr  error
	commitErr  error
	statusErr  error
	creates    []CreateUploadRequest
	uploads    []UploadRequest
	commits    []CommitUploadRequest
}

func (f *fakeTransport) ResolveChannel(context.Context, []byte, string, string) (Channel, error) {
	return f.channel, f.channelErr
}
func (f *fakeTransport) CreateUpload(_ context.Context, _ []byte, r CreateUploadRequest) (UploadSession, error) {
	f.creates = append(f.creates, r)
	return f.session, f.createErr
}
func (f *fakeTransport) Upload(_ context.Context, _ []byte, r UploadRequest) error {
	if r.Body != nil {
		_, _ = io.Copy(io.Discard, r.Body)
	}
	f.uploads = append(f.uploads, r)
	return f.uploadErr
}
func (f *fakeTransport) CommitUpload(_ context.Context, _ []byte, r CommitUploadRequest) (VideoRecord, error) {
	f.commits = append(f.commits, r)
	return f.commit, f.commitErr
}
func (f *fakeTransport) ReadVideo(context.Context, []byte, VideoStatusRequest) (VideoRecord, error) {
	return f.status, f.statusErr
}

func fixedNow() time.Time { return time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC) }
func account() sdk.Account {
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "rutube-main", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "rutube", Family: sdk.FamilySocial, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}
func runtime() sdk.Runtime {
	return fakeRuntime{secrets: fakeSecrets{value: []byte("rutube-partner-test-credential-0123456789")}}
}
func config() Configuration {
	return Configuration{ChannelID: "channel_fixture_001", ContractID: "partner-contract-v1"}
}
func descriptor(n int) sdk.MediaDescriptor {
	return sdk.MediaDescriptor{MediaType: "video/mp4", SizeBytes: int64(n), SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}
}
func request() sdk.SocialPublishRequest {
	return sdk.SocialPublishRequest{PublicationID: "01890f4d-1e10-7cc0-9c4a-000000000046", Kind: sdk.SocialPostVideo, Text: "Release title\nLonger release description", Media: []sdk.SocialMediaRef{{UploadID: "upl_0123456789abcdef0123456789abcdef", Kind: sdk.SocialMediaVideo}}}
}

func fixtureTransport(t *testing.T) *fakeTransport {
	t.Helper()
	var ch Channel
	var session struct {
		ID        string    `json:"id"`
		MaxBytes  int64     `json:"max_bytes"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	var processing struct {
		VideoID   string     `json:"video_id"`
		ChannelID string     `json:"channel_id"`
		State     VideoState `json:"state"`
	}
	var published struct {
		VideoID   string     `json:"video_id"`
		ChannelID string     `json:"channel_id"`
		State     VideoState `json:"state"`
	}
	if json.Unmarshal(channelFixture, &ch) != nil || json.Unmarshal(sessionFixture, &session) != nil || json.Unmarshal(processingFixture, &processing) != nil || json.Unmarshal(publishedFixture, &published) != nil {
		t.Fatal("fixture parse failed")
	}
	return &fakeTransport{channel: ch, session: UploadSession{ID: session.ID, MaxBytes: session.MaxBytes, ExpiresAt: session.ExpiresAt}, commit: VideoRecord{VideoID: processing.VideoID, ChannelID: processing.ChannelID, State: processing.State}, status: VideoRecord{VideoID: published.VideoID, ChannelID: published.ChannelID, State: published.State}}
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

func TestHealthRequiresExactChannel(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	health, err := c.Health(context.Background(), account(), runtime())
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
	tr.channel.ID = "other_channel"
	health, err = c.Health(context.Background(), account(), runtime())
	if err != nil || health.Status != sdk.HealthDegraded || health.ReasonCode != "remote_contract_invalid" {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}

func TestPublishVideoUploadStateMachine(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	body := bytes.Repeat([]byte{0x42}, 64<<10)
	media := &fakeMedia{data: body, descriptor: descriptor(len(body))}
	got, err := c.PublishSocial(context.Background(), account(), runtime(), request(), media)
	if err != nil {
		t.Fatal(err)
	}
	if got.RemotePublicationID != "rutube:channel_fixture_001:video_fixture_001" || got.Status != sdk.SocialRemoteProcessing {
		t.Fatalf("unexpected result %+v", got)
	}
	if media.opens != 1 || len(tr.creates) != 1 || len(tr.uploads) != 1 || len(tr.commits) != 1 {
		t.Fatalf("state machine calls create=%d upload=%d commit=%d opens=%d", len(tr.creates), len(tr.uploads), len(tr.commits), media.opens)
	}
	if tr.creates[0].ExternalID != request().PublicationID || tr.creates[0].Title != "Release title" || tr.creates[0].Description != request().Text || tr.creates[0].ChannelID != config().ChannelID {
		t.Fatalf("metadata mapping %+v", tr.creates[0])
	}
	if tr.uploads[0].SizeBytes != int64(len(body)) || tr.uploads[0].SessionID != tr.session.ID {
		t.Fatalf("upload mapping %+v", tr.uploads[0])
	}
}

func TestReadStatusMapsPublishedAndRejectsForeignChannel(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	got, err := c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "rutube:channel_fixture_001:video_fixture_001")
	if err != nil || got.Status != sdk.SocialRemotePublished {
		t.Fatalf("status=%+v err=%v", got, err)
	}
	_, err = c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "rutube:other_channel:video_fixture_001")
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("expected tenant/account boundary error, got %v", err)
	}
}

func TestQuotaAndRateLimitErrorsAreBounded(t *testing.T) {
	tr := fixtureTransport(t)
	tr.createErr = &PartnerFailure{Kind: FailureQuotaExceeded, Code: "daily_upload_quota", RequestID: "req-1", RetryAfter: 15 * time.Minute}
	c := New(tr, fakeConfig{config()}, fixedNow)
	body := bytes.Repeat([]byte{0x42}, 64<<10)
	_, err := c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "quota_exceeded" || remote.RetryAfterMS != int64((15*time.Minute)/time.Millisecond) {
		t.Fatalf("quota normalization: %#v %v", remote, err)
	}
	tr.createErr = &PartnerFailure{Kind: FailureRateLimited, RequestID: "req-2", RetryAfter: time.Minute}
	_, err = c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorRateLimited || remote.Code != "rate_limited" || remote.RetryAfterMS != int64(time.Minute/time.Millisecond) {
		t.Fatalf("rate normalization: %#v %v", remote, err)
	}
}

func TestAmbiguousUploadAndCommitFailClosed(t *testing.T) {
	body := bytes.Repeat([]byte{0x42}, 64<<10)
	tr := fixtureTransport(t)
	tr.uploadErr = &PartnerFailure{Kind: FailureUnavailable, RequestID: "upload-1"}
	c := New(tr, fakeConfig{config()}, fixedNow)
	_, err := c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	var remote *sdk.RemoteError
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorConflict || remote.Code != "upload_outcome_unknown" {
		t.Fatalf("upload ambiguity: %#v %v", remote, err)
	}
	if len(tr.commits) != 0 {
		t.Fatal("commit must not run after ambiguous upload")
	}

	tr = fixtureTransport(t)
	tr.commitErr = &PartnerFailure{Kind: FailureUnavailable, RequestID: "commit-1"}
	c = New(tr, fakeConfig{config()}, fixedNow)
	_, err = c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	if !errors.As(err, &remote) || remote.Category != sdk.ErrorConflict || remote.Code != "write_outcome_unknown" {
		t.Fatalf("commit ambiguity: %#v %v", remote, err)
	}
}

func TestFailedStatusRequiresSafeReason(t *testing.T) {
	tr := fixtureTransport(t)
	tr.status = VideoRecord{VideoID: "video_fixture_001", ChannelID: "channel_fixture_001", State: VideoStateFailed, ReasonCode: "moderation_rejected"}
	c := New(tr, fakeConfig{config()}, fixedNow)
	got, err := c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "rutube:channel_fixture_001:video_fixture_001")
	if err != nil || got.Status != sdk.SocialRemoteFailed || got.ReasonCode != "moderation_rejected" {
		t.Fatalf("failed status %+v err=%v", got, err)
	}
	tr.status.ReasonCode = "bad secret: abc"
	_, err = c.ReadSocialPublicationStatus(context.Background(), account(), runtime(), "rutube:channel_fixture_001:video_fixture_001")
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected fail-closed status, got %v", err)
	}
}

func TestRejectsNonVideoAndOversizedSession(t *testing.T) {
	tr := fixtureTransport(t)
	c := New(tr, fakeConfig{config()}, fixedNow)
	bad := request()
	bad.Kind = sdk.SocialPostText
	bad.Media = nil
	_, err := c.PublishSocial(context.Background(), account(), runtime(), bad, &fakeMedia{})
	if !errors.Is(err, sdk.ErrInvalidSocialRequest) {
		t.Fatalf("non-video accepted: %v", err)
	}
	body := bytes.Repeat([]byte{0x42}, 64<<10)
	tr.session.MaxBytes = 32 << 10
	_, err = c.PublishSocial(context.Background(), account(), runtime(), request(), &fakeMedia{data: body, descriptor: descriptor(len(body))})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("undersized session accepted: %v", err)
	}
}
