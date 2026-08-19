package openaicompatible

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
	return sdk.Account{ID: "oai-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "openai-compatible", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct {
	gotRequest Request
}

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.gotRequest = request
	body, _ := json.Marshal(chatResponse{Model: "gpt-test", Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "hello"}}}})
	return Response{StatusCode: 200, Body: body}, nil
}

func TestConnectorCompleteBuildsRequestAndParsesResponse(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("sk-test")}}
	text, model, err := connector.Complete(context.Background(), testAccount(), runtime, "api.example.test", "gpt-4o-mini", "be terse", "summarize sales")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello" || model != "gpt-test" {
		t.Fatalf("unexpected result: %q %q", text, model)
	}
	if transport.gotRequest.Host != "api.example.test" || transport.gotRequest.Path != "/v1/chat/completions" {
		t.Fatalf("unexpected request target: %+v", transport.gotRequest)
	}
	if transport.gotRequest.Headers["Authorization"] != "Bearer sk-test" {
		t.Fatalf("unexpected auth header: %q", transport.gotRequest.Headers["Authorization"])
	}
	var body chatRequest
	if err := json.Unmarshal(transport.gotRequest.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "gpt-4o-mini" || len(body.Messages) != 2 || body.Messages[0].Role != "system" || body.Messages[1].Content != "summarize sales" {
		t.Fatalf("unexpected request body: %+v", body)
	}
}

func TestConnectorDefaultsToOpenAIHost(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, nil)
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("sk-test")}}
	if _, _, err := connector.Complete(context.Background(), testAccount(), runtime, "", "gpt-4o-mini", "", "x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport.gotRequest.Host != DefaultHost {
		t.Fatalf("unexpected default host: %q", transport.gotRequest.Host)
	}
}

func TestConnectorCompleteRejectsInvalidAccount(t *testing.T) {
	connector := New(&fixtureTransport{}, nil)
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("sk-test")}}
	invalid := testAccount()
	invalid.ConnectorID = "wrong-connector"
	if _, _, err := connector.Complete(context.Background(), invalid, runtime, "", "gpt-4o-mini", "", "x"); err == nil {
		t.Fatal("expected error for manifest mismatch")
	}
}
