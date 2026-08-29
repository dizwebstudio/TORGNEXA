// Package grok implements xAI's Grok Chat Completions API boundary. Socket
// I/O is host-mediated through Transport and credentials stay callback-scoped.
package grok

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("grok: transport missing")

const (
	DefaultHost        = "api.x.ai"
	DefaultHealthModel = "grok-4.6"
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
	Do(context.Context, Request) (Response, error)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
}
type chatChoice struct {
	Message message `json:"message"`
}
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Model   string       `json:"model"`
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
func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("grok"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	status, reason := sdk.HealthHealthy, ""
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		_, _, callErr := c.complete(ctx, "", DefaultHealthModel, credential, "", "ping")
		return callErr
	})
	if err != nil {
		status, reason = sdk.HealthUnavailable, "remote_unavailable"
	}
	return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
}

func (c *Connector) Complete(ctx context.Context, account sdk.Account, runtime sdk.Runtime, host, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
	if c == nil || c.transport == nil {
		return "", "", ErrTransportMissing
	}
	if runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return "", "", sdk.ErrInvalidHealth
	}
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		var callErr error
		text, resolvedModel, callErr = c.complete(ctx, host, model, credential, systemPrompt, userPrompt)
		return callErr
	})
	return text, resolvedModel, err
}

func (c *Connector) complete(ctx context.Context, host, model string, credential []byte, systemPrompt, userPrompt string) (string, string, error) {
	if len(credential) == 0 || strings.TrimSpace(userPrompt) == "" {
		return "", "", errors.New("grok: invalid request")
	}
	if strings.TrimSpace(host) == "" {
		host = DefaultHost
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultHealthModel
	}
	if strings.ContainsAny(host, "/:@?#[]\\\r\n\x00") || strings.ContainsAny(model, "/\\\r\n\x00") || strings.Contains(model, "..") {
		return "", "", errors.New("grok: invalid endpoint")
	}
	messages := make([]message, 0, 2)
	if system := strings.TrimSpace(systemPrompt); system != "" {
		messages = append(messages, message{Role: "system", Content: system})
	}
	messages = append(messages, message{Role: "user", Content: userPrompt})
	raw, err := json.Marshal(chatRequest{Model: model, Messages: messages, Stream: false})
	if err != nil {
		return "", "", err
	}
	response, err := c.transport.Do(ctx, Request{Host: host, Path: "/v1/chat/completions", Headers: map[string]string{"Authorization": "Bearer " + string(credential), "Content-Type": "application/json"}, Body: raw})
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("grok: remote status not ok")
	}
	var out chatResponse
	if err := json.Unmarshal(response.Body, &out); err != nil {
		return "", "", err
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", "", errors.New("grok: empty completion")
	}
	if out.Model == "" {
		out.Model = model
	}
	return out.Choices[0].Message.Content, out.Model, nil
}
