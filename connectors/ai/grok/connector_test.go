package grok

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type fakeSecrets struct{ credential []byte }

func (f fakeSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn(f.credential)
}

type fakeRuntime struct{ secrets sdk.SecretAccessor }

func (f fakeRuntime) Secrets() sdk.SecretAccessor { return f.secrets }
func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "grok-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "grok", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct{ got Request }

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.got = request
	body, _ := json.Marshal(chatResponse{Choices: []chatChoice{{Message: message{Role: "assistant", Content: "hello from Grok"}}}, Model: "grok-4.6"})
	return Response{StatusCode: 200, Body: body}, nil
}
func TestConnectorCompleteUsesXAIChatCompletions(t *testing.T) {
	tr := &fixtureTransport{}
	c := New(tr, nil)
	text, model, err := c.Complete(context.Background(), testAccount(), fakeRuntime{secrets: fakeSecrets{credential: []byte("xai-key")}}, "", "grok-4.6", "", "summarize sales")
	if err != nil || text != "hello from Grok" || model != "grok-4.6" {
		t.Fatalf("unexpected completion: %q %q %v", text, model, err)
	}
	if tr.got.Host != DefaultHost || tr.got.Path != "/v1/chat/completions" || tr.got.Headers["Authorization"] != "Bearer xai-key" {
		t.Fatalf("unexpected request: %+v", tr.got)
	}
}
