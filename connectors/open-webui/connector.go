// Package openwebui implements the Open WebUI OpenAI-compatible gateway
// boundary. Network access is host-mediated through Transport; the connector
// never owns DNS, sockets or local-network policy.
package openwebui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("open-webui: transport missing")

const (
	DefaultBaseURL     = "http://open-webui:3000/api"
	DefaultHealthModel = "local-model"
)

// Request is the host-mediated local HTTP request.
type Request struct {
	BaseURL string
	Path    string
	Headers map[string]string
	Body    []byte
}

// Response is the bounded response returned by the host transport.
type Response struct {
	StatusCode int
	Body       []byte
}

// Transport performs one request under host-owned local egress policy.
type Transport interface {
	Do(context.Context, Request) (Response, error)
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
	Model   string       `json:"model"`
}

// Connector is a stateless Open WebUI gateway adapter.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs an Open WebUI connector using a host-owned transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical Open WebUI connector manifest.
func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("open-webui"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health performs one bounded non-streaming gateway completion.
func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	status, reason := sdk.HealthHealthy, ""
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		_, _, callErr := c.complete(ctx, DefaultBaseURL, DefaultHealthModel, credential, "", "ping")
		return callErr
	})
	if err != nil {
		status, reason = sdk.HealthUnavailable, "remote_unavailable"
	}
	return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
}

// Complete sends one bounded text completion through Open WebUI.
func (c *Connector) Complete(ctx context.Context, account sdk.Account, runtime sdk.Runtime, baseURL, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
	if c == nil || c.transport == nil {
		return "", "", ErrTransportMissing
	}
	if runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return "", "", sdk.ErrInvalidHealth
	}
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		var callErr error
		text, resolvedModel, callErr = c.complete(ctx, baseURL, model, credential, systemPrompt, userPrompt)
		return callErr
	})
	return text, resolvedModel, err
}

func (c *Connector) complete(ctx context.Context, baseURL, model string, credential []byte, systemPrompt, userPrompt string) (string, string, error) {
	if len(credential) == 0 || strings.TrimSpace(userPrompt) == "" {
		return "", "", errors.New("open-webui: invalid request")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	messages := make([]chatMessage, 0, 2)
	if system := strings.TrimSpace(systemPrompt); system != "" {
		messages = append(messages, chatMessage{Role: "system", Content: system})
	}
	messages = append(messages, chatMessage{Role: "user", Content: userPrompt})
	raw, err := json.Marshal(chatRequest{Model: model, Messages: messages})
	if err != nil {
		return "", "", err
	}
	response, err := c.transport.Do(ctx, Request{BaseURL: baseURL, Path: "/chat/completions", Headers: map[string]string{"Authorization": "Bearer " + string(credential), "Content-Type": "application/json"}, Body: raw})
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("open-webui: remote status not ok")
	}
	var out chatResponse
	if err := json.Unmarshal(response.Body, &out); err != nil {
		return "", "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", "", errors.New("open-webui: empty completion")
	}
	resolvedModel := out.Model
	if resolvedModel == "" {
		resolvedModel = model
	}
	return out.Choices[0].Message.Content, resolvedModel, nil
}
