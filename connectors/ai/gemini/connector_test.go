package gemini

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
	return sdk.Account{ID: "gemini-test", OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorID: "gemini", Family: sdk.FamilyAI, Status: sdk.AccountActive, SecretReference: "sec:v1:0123456789abcdef0123456789abcdef", Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: at, UpdatedAt: at}
}

type fixtureTransport struct{ got Request }

func (f *fixtureTransport) Do(_ context.Context, request Request) (Response, error) {
	f.got = request
	body, _ := json.Marshal(generateResponse{Candidates: []candidate{{Content: content{Parts: []part{{Text: "hello from Gemini"}}}}}, ModelVersion: "gemini-3.7-flash"})
	return Response{StatusCode: 200, Body: body}, nil
}

func TestConnectorCompleteUsesGeminiAPI(t *testing.T) {
	transport := &fixtureTransport{}
	connector := New(transport, func() time.Time { return time.Date(2026, 8, 28, 2, 0, 0, 0, time.UTC) })
	text, model, err := connector.Complete(context.Background(), testAccount(), fakeRuntime{secrets: fakeSecrets{credential: []byte("gemini-key")}}, "", "gemini-3.7-flash", "", "summarize sales")
	if err != nil || text != "hello from Gemini" || model != "gemini-3.7-flash" {
		t.Fatalf("unexpected completion: %q %q %v", text, model, err)
	}
	if transport.got.Host != DefaultHost || transport.got.Path != "/v1beta/models/gemini-3.7-flash:generateContent" || transport.got.Headers["x-goog-api-key"] != "gemini-key" {
		t.Fatalf("unexpected request: %+v", transport.got)
	}
}
