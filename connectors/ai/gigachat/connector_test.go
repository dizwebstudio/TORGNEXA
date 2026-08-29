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

type fixtureTransport struct {
	requests []Request
	// rejectCompletionAuthorization, when set, makes exactly one completion
	// call bearing that Authorization header value fail with 401, simulating
	// a cached token the remote has revoked out of band.
	rejectCompletionAuthorization string
}

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.requests = append(f.requests, request)
	switch request.Path {
	case "/api/v2/oauth":
		body, _ := json.Marshal(tokenResponse{AccessToken: "giga-access-token", ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()})
		return Response{StatusCode: 200, Body: body}, nil
	case "/api/v1/chat/completions":
		if f.rejectCompletionAuthorization != "" && request.Headers["Authorization"] == f.rejectCompletionAuthorization {
			f.rejectCompletionAuthorization = ""
			return Response{StatusCode: 401}, nil
		}
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

func TestConnectorCompleteCachesTokenAcrossCalls(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("YmFzZTY0Y3JlZHM=")}}
	account := testAccount()

	if _, _, err := connector.Complete(context.Background(), account, runtime, "", "GigaChat", "", "first"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, _, err := connector.Complete(context.Background(), account, runtime, "", "GigaChat", "", "second"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	oauthCalls := 0
	for _, r := range transport.requests {
		if r.Path == "/api/v2/oauth" {
			oauthCalls++
		}
	}
	if oauthCalls != 1 {
		t.Fatalf("expected the second Complete call to reuse the cached token instead of re-exchanging it, got %d oauth calls", oauthCalls)
	}
	if len(transport.requests) != 3 {
		t.Fatalf("expected 1 oauth exchange + 2 completions (3 requests), got %d", len(transport.requests))
	}
}

func TestConnectorCompleteRefreshesTokenOn401(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 12, 2, 0, 0, 0, time.UTC) })
	runtime := fakeRuntime{secrets: fakeSecrets{credential: []byte("YmFzZTY0Y3JlZHM=")}}
	account := testAccount()

	// Seed a cached token that the remote will reject, simulating an
	// out-of-band revoked token still inside its locally cached window.
	connector.storeAccessToken(account.ID, "stale-token", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli())
	transport.rejectCompletionAuthorization = "Bearer stale-token"

	text, _, err := connector.Complete(context.Background(), account, runtime, "", "GigaChat", "", "prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "hello from gigachat" {
		t.Fatalf("unexpected result: %q", text)
	}
	oauthCalls, completionCalls := 0, 0
	for _, r := range transport.requests {
		switch r.Path {
		case "/api/v2/oauth":
			oauthCalls++
		case "/api/v1/chat/completions":
			completionCalls++
		}
	}
	if oauthCalls != 1 {
		t.Fatalf("expected exactly one refresh oauth exchange after the 401, got %d", oauthCalls)
	}
	if completionCalls != 2 {
		t.Fatalf("expected the rejected completion plus one retry (2 completion calls), got %d", completionCalls)
	}
}
