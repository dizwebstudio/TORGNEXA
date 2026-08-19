package gigachat

import (
	"context"
	"encoding/json"
	"strings"
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
	return sdk.Account{ID: "giga-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "gigachat", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct{ requests []Request }

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.requests = append(f.requests, request)
	switch request.Path {
	case "/api/v2/oauth":
		body, _ := json.Marshal(tokenResponse{AccessToken: "giga-access-token"})
		return Response{StatusCode: 200, Body: body}, nil
	case "/api/v1/chat/completions":
		body, _ := json.Marshal(chatResponse{Choices: []chatChoice{{Message: chatMessage{Content: "hello from gigachat"}}}})
		return Response{StatusCode: 200, Body: body}, nil
	default:
		return Response{StatusCode: 404}, nil
	}
}

func TestConnectorCompleteExchangesTokenThenCompletes(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("YmFzZTY0Y3JlZHM=")}}
	text, model, err := connector.Complete(context.Background(), testAccount(), runtime, "", "GigaChat", "", "summarize sales")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello from gigachat" || model != "GigaChat" {
		t.Fatalf("unexpected result: %q %q", text, model)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("expected 2 requests (oauth + completion), got %d", len(transport.requests))
	}
	oauth, completion := transport.requests[0], transport.requests[1]
	if oauth.Host != OAuthHost || !strings.Contains(oauth.Headers["Authorization"], "Basic YmFzZTY0Y3JlZHM=") {
		t.Fatalf("unexpected oauth request: %+v", oauth)
	}
	if oauth.Headers["Content-Type"] != "application/x-www-form-urlencoded" || string(oauth.Body) != "scope=GIGACHAT_API_PERS" {
		t.Fatalf("unexpected oauth body/content-type: %+v", oauth)
	}
	if completion.Host != CompletionHost || completion.Headers["Authorization"] != "Bearer giga-access-token" {
		t.Fatalf("unexpected completion request: %+v", completion)
	}
}
