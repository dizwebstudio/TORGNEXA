package avito

import (
	"context"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"testing"
	"time"
)

type cfgSource struct{}

func (cfgSource) Resolve(context.Context, sdk.Account) (Configuration, error) {
	return Configuration{UserID: 42}, nil
}

type secrets struct{}

func (secrets) UseSecret(_ context.Context, _ sdk.SecretReference, cb func([]byte) error) error {
	return cb([]byte("synthetic-avito-oauth-access-token-0123456789"))
}

type runtime struct{}

func (runtime) Secrets() sdk.SecretAccessor { return secrets{} }

type transport struct{ fn func(Request) Response }

func (t transport) Do(_ context.Context, r Request) (Response, error) {
	if t.fn == nil {
		return Response{}, errors.New("no")
	}
	return t.fn(r), nil
}
func account() sdk.Account {
	now := time.Date(2026, 8, 11, 18, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "avito-a", OrganizationID: "018f47a0-1234-7890-8abc-1234567890ab", WorkspaceID: "018f47a0-1234-7890-8abc-1234567890ac", ConnectorID: "avito", Family: sdk.FamilyClassified, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: now, UpdatedAt: now}
}
func TestManifestAndRisk(t *testing.T) {
	if err := Manifest().Validate(); err != nil {
		t.Fatal(err)
	}
	if r, _ := sdk.ClassifiedCapabilityRisk("classified.messages.reply"); r != sdk.ClassifiedRiskWriteSensitive {
		t.Fatalf("risk=%s", r)
	}
}
func TestHealthBindsConfiguredUser(t *testing.T) {
	c := New(transport{fn: func(r Request) Response {
		if r.Path != "/core/v1/accounts/self" {
			t.Fatalf("path=%s", r.Path)
		}
		return Response{StatusCode: 200, Body: []byte(`{"id":42}`)}
	}}, cfgSource{}, nil)
	h, e := c.Health(context.Background(), account(), runtime{})
	if e != nil || h.Status != sdk.HealthHealthy {
		t.Fatalf("%v %+v", e, h)
	}
}
func TestReplyAmbiguousFailureDoesNotRetry(t *testing.T) {
	c := New(transport{fn: func(r Request) Response { return Response{StatusCode: 503} }}, cfgSource{}, nil)
	_, e := c.ReplyClassifiedMessage(context.Background(), account(), runtime{}, sdk.ClassifiedMessageReply{LeadRemoteID: "chat-1", Text: "hello"})
	var re *sdk.RemoteError
	if !errors.As(e, &re) || re.Code != "write_outcome_unknown" || re.RetryAfterMS > 0 {
		t.Fatalf("%T %v", e, e)
	}
}
func TestReadListings(t *testing.T) {
	c := New(transport{fn: func(r Request) Response {
		return Response{StatusCode: 200, Body: []byte(`{"resources":[{"id":101,"external_id":"sku-1","title":"Chair","status":"active","price":1299,"currency":"RUB","updated_at":"2026-08-11T12:00:00Z"}],"meta":{"page":1,"per_page":1,"pages":1}}`)}
	}}, cfgSource{}, nil)
	p, e := c.ReadClassifiedListings(context.Background(), account(), runtime{}, sdk.PageRequest{Limit: 1})
	if e != nil || len(p.Items) != 1 || p.Items[0].RemoteID != "101" {
		t.Fatalf("%v %+v", e, p)
	}
}
