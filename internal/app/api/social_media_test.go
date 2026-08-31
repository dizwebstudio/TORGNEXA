package api

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

func TestReleasedUploadMediaReopensOnlyReleasedObject(t *testing.T) {
	scope := validTestScope(t)
	id := uploads.ID("upl_" + strings.Repeat("9", 32))
	ref := releasedRef(t, string(id))
	content := &fakeReleaseReader{key: ref.ObjectKey(), payload: []byte{0xff, 0xd8, 0xff, 0xe0}}
	media := releasedUploadMedia{gate: releaseGateStub{ref: ref}, content: content}
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	account := sdk.Account{
		ID: "max-account", OrganizationID: scope.OrganizationID().String(), WorkspaceID: scope.WorkspaceID().String(),
		ConnectorID: socialWebhookRouteA(), Family: sdk.FamilySocial, Status: sdk.AccountActive,
		SecretReference: sdk.SecretReference("sec:v1:" + strings.Repeat("1", 32)), Version: 1,
		Health: sdk.Health{Status: sdk.HealthHealthy, CheckedAt: now}, CreatedAt: now, UpdatedAt: now,
	}
	reader, descriptor, err := media.OpenReleased(context.Background(), account, sdk.SocialMediaRef{UploadID: string(id), Kind: sdk.SocialMediaImage})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != string(content.payload) || descriptor.MediaType != "image/jpeg" || descriptor.SizeBytes != 4 {
		t.Fatalf("body=%v descriptor=%+v err=%v", body, descriptor, err)
	}
}

func TestReleasedUploadMediaFailsClosedWithoutRelease(t *testing.T) {
	media := releasedUploadMedia{gate: releaseGateStub{err: uploads.ErrNotReleased}, content: &fakeReleaseReader{}}
	account := socialWebhookTestAccount()
	_, _, err := media.OpenReleased(context.Background(), account, sdk.SocialMediaRef{UploadID: "upl_" + strings.Repeat("9", 32), Kind: sdk.SocialMediaImage})
	if err == nil {
		t.Fatal("unreleased media was accepted")
	}
}

var _ uploads.ReleaseReader = (*fakeReleaseReader)(nil)
