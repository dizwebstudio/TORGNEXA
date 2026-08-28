// Package claude implements the Connector SDK boundary for Anthropic's
// Claude Messages API. Socket I/O is host-mediated through Transport so the
// connector never owns DNS, TLS or network policy.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("claude: transport missing")

const (
	DefaultHost        = "api.anthropic.com"
	messagesPath       = "/v1/messages"
	anthropicVersion   = "2023-06-01"
	DefaultHealthModel = "claude-sonnet-4-20250514"
	defaultMaxTokens   = 2048
)

// Request is the host-mediated HTTP request needed by the Claude connector.
type Request struct {
	Host    string
	Path    string
	Headers map[string]string
	Body    []byte
}

// Response is the bounded response returned by the host transport.
type Response struct {
	StatusCode int
	Body       []byte
}

// Transport performs one provider request under host-owned egress policy.
type Transport interface {
	Do(context.Context, Request) (Response, error)
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messageRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type messageResponse struct {
	Model   string         `json:"model"`
	Content []contentBlock `json:"content"`
}

// Connector is a stateless Claude API adapter.
type Connector struct {
	transport Transport
	now       func() time.Time
}

// New constructs a Claude connector using a host-owned transport.
func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now}
}

// Manifest returns the canonical Claude connector manifest.
func Manifest() sdk.Manifest { manifest, _ := sdk.CatalogManifest("claude"); return manifest }

// Manifest returns the canonical Claude connector manifest.
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health performs one bounded non-streaming completion and normalizes any
// provider or transport failure to the SDK health vocabulary.
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

// Complete sends one bounded text completion through Anthropic's Messages
// endpoint. The optional host is a validated HTTPS proxy override supplied by
// the host runtime; an empty host uses api.anthropic.com.
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
		return "", "", errors.New("claude: invalid request")
	}
	if host == "" {
		host = DefaultHost
	}
	raw, err := json.Marshal(messageRequest{
		Model: model, MaxTokens: defaultMaxTokens,
		System:   strings.TrimSpace(systemPrompt),
		Messages: []message{{Role: "user", Content: userPrompt}},
	})
	if err != nil {
		return "", "", err
	}
	response, err := c.transport.Do(ctx, Request{
		Host: host,
		Path: messagesPath,
		Headers: map[string]string{
			"x-api-key":         string(credential),
			"anthropic-version": anthropicVersion,
			"Content-Type":      "application/json",
		},
		Body: raw,
	})
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("claude: remote status not ok")
	}
	var out messageResponse
	if err := json.Unmarshal(response.Body, &out); err != nil {
		return "", "", err
	}
	for _, block := range out.Content {
		if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
			resolvedModel := out.Model
			if resolvedModel == "" {
				resolvedModel = model
			}
			return block.Text, resolvedModel, nil
		}
	}
	return "", "", errors.New("claude: empty completion")
}
