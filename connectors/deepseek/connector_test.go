package deepseek

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
	at := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "deepseek-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "deepseek", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct{ gotRequest Request }

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.gotRequest = request
	body, _ := json.Marshal(chatResponse{Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "hello from deepseek"}}}})
	return Response{StatusCode: 200, Body: body}, nil
}

func TestConnectorCompleteDefaultsToDeepSeekHost(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("deepseek-key")}}
	text, model, err := connector.Complete(context.Background(), testAccount(), runtime, "", "deepseek-chat", "", "summarize sales")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello from deepseek" || model != "deepseek-chat" {
		t.Fatalf("unexpected result: %q %q", text, model)
	}
	if transport.gotRequest.Host != DefaultHost || DefaultHost != "api.deepseek.com" {
		t.Fatalf("unexpected host: %q", transport.gotRequest.Host)
	}
	if transport.gotRequest.Headers["Authorization"] != "Bearer deepseek-key" {
		t.Fatalf("unexpected auth: %q", transport.gotRequest.Headers["Authorization"])
	}
}
