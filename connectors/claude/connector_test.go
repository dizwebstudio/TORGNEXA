package claude

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
	return sdk.Account{ID: "claude-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "claude", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct {
	gotRequest Request
}

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.gotRequest = request
	body, _ := json.Marshal(messageResponse{Model: "claude-sonnet-4-20250514", Content: []contentBlock{{Type: "text", Text: "hello from Claude"}}})
	return Response{StatusCode: 200, Body: body}, nil
}

func TestConnectorCompleteDefaultsToAnthropicHostAndHeaders(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 20, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("claude-key")}}
	text, model, err := connector.Complete(context.Background(), testAccount(), runtime, "", "claude-sonnet-4-20250514", "You are concise.", "summarize sales")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello from Claude" || model != "claude-sonnet-4-20250514" {
		t.Fatalf("unexpected result: %q %q", text, model)
	}
	if transport.gotRequest.Host != DefaultHost || DefaultHost != "api.anthropic.com" {
		t.Fatalf("unexpected host: %q", transport.gotRequest.Host)
	}
	if transport.gotRequest.Path != messagesPath || messagesPath != "/v1/messages" {
		t.Fatalf("unexpected path: %q", transport.gotRequest.Path)
	}
	if transport.gotRequest.Headers["x-api-key"] != "claude-key" || transport.gotRequest.Headers["anthropic-version"] != anthropicVersion {
		t.Fatalf("unexpected authentication headers: %#v", transport.gotRequest.Headers)
	}
	var payload messageRequest
	if err := json.Unmarshal(transport.gotRequest.Body, &payload); err != nil {
		t.Fatalf("request body is not JSON: %v", err)
	}
	if payload.MaxTokens != defaultMaxTokens || payload.System != "You are concise." || len(payload.Messages) != 1 || payload.Messages[0].Role != "user" || payload.Messages[0].Content != "summarize sales" {
		t.Fatalf("unexpected request payload: %#v", payload)
	}
}

func TestConnectorCompleteHonorsHostOverride(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, nil)
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("claude-key")}}
	if _, _, err := connector.Complete(context.Background(), testAccount(), runtime, "anthropic-proxy.example.com", "claude-sonnet-4-20250514", "", "summarize sales"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transport.gotRequest.Host != "anthropic-proxy.example.com" {
		t.Fatalf("unexpected host: %q", transport.gotRequest.Host)
	}
}

func TestConnectorRejectsResponseWithoutTextBlock(t *testing.T) {
	connector := New(emptyTransport{}, nil)
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("claude-key")}}
	if _, _, err := connector.Complete(context.Background(), testAccount(), runtime, "", "claude-sonnet-4-20250514", "", "prompt"); err == nil {
		t.Fatal("expected empty completion error")
	}
}

type emptyTransport struct{}

func (emptyTransport) Do(context.Context, Request) (Response, error) {
	return Response{StatusCode: 200, Body: []byte(`{"model":"claude-sonnet-4-20250514","content":[{"type":"tool_use"}]}`)}, nil
}
