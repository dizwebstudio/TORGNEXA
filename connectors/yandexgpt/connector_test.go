package yandexgpt

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
	at := time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "yagpt-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "yandexgpt", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct{ gotRequest Request }

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.gotRequest = request
	body, _ := json.Marshal(completionResponse{Result: completionResult{Alternatives: []alternative{{Message: message{Text: "hello from yandex"}}}}})
	return Response{StatusCode: 200, Body: body}, nil
}

func TestConnectorCompleteRequiresFolderID(t *testing.T) {
	connector := New(&fixtureTransport{}, nil)
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("key")}}
	if _, _, err := connector.Complete(context.Background(), testAccount(), runtime, "", "yandexgpt-lite", "", "summarize"); err == nil {
		t.Fatal("expected error for missing folder id")
	}
}

func TestConnectorCompleteBuildsModelURI(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("ai-key")}}
	text, model, err := connector.Complete(context.Background(), testAccount(), runtime, "b1gfolder", "yandexgpt-lite", "be terse", "summarize sales")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello from yandex" || model != "yandexgpt-lite" {
		t.Fatalf("unexpected result: %q %q", text, model)
	}
	if transport.gotRequest.Host != Host {
		t.Fatalf("unexpected host: %q", transport.gotRequest.Host)
	}
	if transport.gotRequest.Headers["Authorization"] != "Api-Key ai-key" {
		t.Fatalf("unexpected auth: %q", transport.gotRequest.Headers["Authorization"])
	}
	var body completionRequest
	if err := json.Unmarshal(transport.gotRequest.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.ModelURI != "gpt://b1gfolder/yandexgpt-lite" {
		t.Fatalf("unexpected modelUri: %q", body.ModelURI)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" {
		t.Fatalf("unexpected messages: %+v", body.Messages)
	}
}
