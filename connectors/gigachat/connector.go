// Package gigachat implements the Connector SDK boundary for Sber's
// GigaChat API. All socket I/O is host-mediated through Transport; this
// package only builds/parses JSON and form-encoded request bodies.
//
// GigaChat's public endpoints are signed by the Russian national root CA
// ("Russian Trusted Root CA" / Minsvyaz), which most default trust stores do
// not carry. Deployments that need GigaChat reachability must add that CA to
// the outbound TLS trust store used by the host transport; this package
// never disables certificate verification to work around a missing CA.
package gigachat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("gigachat: transport missing")

const (
	OAuthHost      = "ngw.devices.sberbank.ru"
	CompletionHost = "gigachat.devices.sberbank.ru"
)

type Request struct {
	Host    string
	Path    string
	Headers map[string]string
	Body    []byte
}

type Response struct {
	StatusCode int
	Body       []byte
}

type Transport interface {
	Do(ctx context.Context, request Request) (Response, error)
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
}
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}
type chatChoice struct {
	Message chatMessage `json:"message"`
}
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

func randomRequestID() string {
	raw := make([]byte, 16)
	_, _ = rand.Read(raw)
	return hex.EncodeToString(raw)
}

type Connector struct {
	transport Transport
	now       func() time.Time
}

func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now}
}

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("gigachat"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	status, reason := sdk.HealthHealthy, ""
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		_, _, callErr := c.complete(ctx, "GigaChat", credential, "", "ping")
		return callErr
	})
	if err != nil {
		status, reason = sdk.HealthUnavailable, "remote_unavailable"
	}
	return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
}

func (c *Connector) Complete(ctx context.Context, account sdk.Account, runtime sdk.Runtime, _ /* host override unused: GigaChat has fixed hosts */, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
	if c == nil || c.transport == nil {
		return "", "", ErrTransportMissing
	}
	if runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return "", "", sdk.ErrInvalidHealth
	}
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		var callErr error
		text, resolvedModel, callErr = c.complete(ctx, model, credential, systemPrompt, userPrompt)
		return callErr
	})
	return text, resolvedModel, err
}

func (c *Connector) complete(ctx context.Context, model string, credential []byte, systemPrompt, userPrompt string) (string, string, error) {
	if len(credential) == 0 || strings.TrimSpace(userPrompt) == "" {
		return "", "", errors.New("gigachat: invalid request")
	}
	tokenResp, err := c.transport.Do(ctx, Request{
		Host: OAuthHost, Path: "/api/v2/oauth",
		Headers: map[string]string{
			"Authorization": "Basic " + string(credential),
			"RqUID":         randomRequestID(),
			"Content-Type":  "application/x-www-form-urlencoded",
			"Accept":        "application/json",
		},
		Body: []byte("scope=GIGACHAT_API_PERS"),
	})
	if err != nil {
		return "", "", err
	}
	if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
		return "", "", errors.New("gigachat: oauth status not ok")
	}
	var token tokenResponse
	if err := json.Unmarshal(tokenResp.Body, &token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		return "", "", errors.New("gigachat: invalid oauth response")
	}

	var messages []chatMessage
	if system := strings.TrimSpace(systemPrompt); system != "" {
		messages = append(messages, chatMessage{Role: "system", Content: system})
	}
	messages = append(messages, chatMessage{Role: "user", Content: userPrompt})
	resolvedModel := model
	if resolvedModel == "" {
		resolvedModel = "GigaChat"
	}
	raw, err := json.Marshal(chatRequest{Model: resolvedModel, Messages: messages})
	if err != nil {
		return "", "", err
	}
	completionResp, err := c.transport.Do(ctx, Request{
		Host: CompletionHost, Path: "/api/v1/chat/completions",
		Headers: map[string]string{"Authorization": "Bearer " + token.AccessToken, "Content-Type": "application/json"},
		Body:    raw,
	})
	if err != nil {
		return "", "", err
	}
	if completionResp.StatusCode < 200 || completionResp.StatusCode >= 300 {
		return "", "", errors.New("gigachat: remote status not ok")
	}
	var out chatResponse
	if err := json.Unmarshal(completionResp.Body, &out); err != nil {
		return "", "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", "", errors.New("gigachat: empty completion")
	}
	return out.Choices[0].Message.Content, resolvedModel, nil
}
