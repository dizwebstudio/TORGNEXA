package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/catalogimagerepo"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

type releaseGateStub struct {
	ref uploads.ReleasedObjectRef
	err error
}

func (stub releaseGateStub) ResolveReleased(context.Context, tenancy.Scope, uploads.ID) (uploads.ReleasedObjectRef, error) {
	return stub.ref, stub.err
}

func releasedRef(t *testing.T, id string) uploads.ReleasedObjectRef {
	t.Helper()
	scope := validTestScope(t)
	repo := &fakeUploadRepository{records: map[uploads.ID]uploads.Record{uploads.ID(id): releasedTestRecord(t, scope, uploads.ID(id))}}
	gate, err := uploads.NewAccessGate(repo, uploads.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := gate.ResolveReleased(context.Background(), scope, uploads.ID(id))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestResolveImageURLAcceptsExternalHTTPSURLAlone(t *testing.T) {
	a := catalogAPI{}
	url, err := a.resolveImageURL(context.Background(), validTestScope(t), imageInput{URL: "https://cdn.example.test/a.png"})
	if err != nil || url != "https://cdn.example.test/a.png" {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func TestResolveImageURLResolvesReleasedUploadToItsContentPath(t *testing.T) {
	id := "upl_" + strings.Repeat("9", 32)
	a := catalogAPI{uploadAccess: releaseGateStub{ref: releasedRef(t, id)}}
	url, err := a.resolveImageURL(context.Background(), validTestScope(t), imageInput{UploadID: id})
	if err != nil || url != uploads.ContentPath(uploads.ID(id)) {
		t.Fatalf("url=%q err=%v", url, err)
	}
}

func TestResolveImageURLRejectsBothURLAndUploadID(t *testing.T) {
	a := catalogAPI{}
	_, err := a.resolveImageURL(context.Background(), validTestScope(t), imageInput{URL: "https://cdn.example.test/a.png", UploadID: "upl_" + strings.Repeat("9", 32)})
	if !errors.Is(err, catalogimagerepo.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveImageURLRejectsNeitherURLNorUploadID(t *testing.T) {
	a := catalogAPI{}
	_, err := a.resolveImageURL(context.Background(), validTestScope(t), imageInput{})
	if !errors.Is(err, catalogimagerepo.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveImageURLFailsClosedWhenUploadNotReleased(t *testing.T) {
	a := catalogAPI{uploadAccess: releaseGateStub{err: uploads.ErrNotReleased}}
	_, err := a.resolveImageURL(context.Background(), validTestScope(t), imageInput{UploadID: "upl_" + strings.Repeat("9", 32)})
	if !errors.Is(err, catalogimagerepo.ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
}
