package ollama

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type testRuntime struct{}

func (testRuntime) Secrets() sdk.SecretAccessor { return testSecrets{} }

type testSecrets struct{}

func (testSecrets) UseSecret(_ context.Context, _ sdk.SecretReference, fn func([]byte) error) error {
	return fn([]byte("ollama"))
}
func testAccount() sdk.Account {
	at := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: "ollama-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "ollama", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct{ request Request }

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.request = request
	raw, _ := json.Marshal(chatResponse{Model: "llama3.2", Choices: []chatChoice{{Message: chatMessage{Role: "assistant", Content: "ok"}}}})
	return Response{StatusCode: 200, Body: raw}, nil
}

func TestCompleteUsesConfiguredLocalEndpoint(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, nil)
	text, model, err := connector.Complete(context.Background(), testAccount(), testRuntime{}, "http://ollama:11434/v1", "llama3.2", "be terse", "summarize sales")
	if err != nil || text != "ok" || model != "llama3.2" {
		t.Fatalf("text=%q model=%q err=%v", text, model, err)
	}
	if transport.request.BaseURL != "http://ollama:11434/v1" || transport.request.Path != "/chat/completions" {
		t.Fatalf("request=%+v", transport.request)
	}
	if transport.request.Headers["Authorization"] != "Bearer ollama" {
		t.Fatalf("auth=%q", transport.request.Headers["Authorization"])
	}
}

func TestHealthUsesCandidateTransport(t *testing.T) {
	health, err := New(candidateTransport{}, func() time.Time { return time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC) }).Health(context.Background(), testAccount(), testRuntime{})
	if err != nil || health.Status != sdk.HealthHealthy {
		t.Fatalf("health=%+v err=%v", health, err)
	}
}
