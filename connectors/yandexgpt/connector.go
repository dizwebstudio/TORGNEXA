// Package yandexgpt implements the Connector SDK boundary for Yandex
// Cloud's Foundation Models (YandexGPT) completion API. All socket I/O is
// host-mediated through Transport; this package only builds/parses JSON.
package yandexgpt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("yandexgpt: transport missing")

const Host = "llm.api.cloud.yandex.net"

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

type message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}
type completionOptions struct {
	Stream      bool    `json:"stream"`
	Temperature float64 `json:"temperature"`
	MaxTokens   string  `json:"maxTokens"`
}
type completionRequest struct {
	ModelURI          string            `json:"modelUri"`
	CompletionOptions completionOptions `json:"completionOptions"`
	Messages          []message         `json:"messages"`
}
type alternative struct {
	Message message `json:"message"`
}
type completionResult struct {
	Alternatives []alternative `json:"alternatives"`
}
type completionResponse struct {
	Result completionResult `json:"result"`
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

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("yandexgpt"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

// Health satisfies the frozen sdk.Connector boundary, which carries no
// tenant-configured FolderID. Unlike the other three AI connectors it
// therefore cannot perform a live completion probe here (YandexGPT has no
// remote call that omits the folder-scoped model URI); it validates the
// account/manifest binding and the stored credential's presence and reports
// HealthHealthy without an outbound request. A live check happens on the
// first real Complete call through HealthCheckWithFolder.
func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	return sdk.Health{Status: sdk.HealthHealthy, CheckedAt: at}, nil
}

// HealthCheckWithFolder performs the live probe Health cannot: it is not
// part of the sdk.Connector boundary and is only reachable through
// builtinruntime, which already knows the account's configured FolderID.
func (c *Connector) HealthCheckWithFolder(ctx context.Context, account sdk.Account, runtime sdk.Runtime, folderID string) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	status, reason := sdk.HealthHealthy, ""
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		_, _, callErr := c.complete(ctx, folderID, "yandexgpt-lite", credential, "", "ping")
		return callErr
	})
	if err != nil {
		status, reason = sdk.HealthUnavailable, "remote_unavailable"
	}
	return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
}

func (c *Connector) Complete(ctx context.Context, account sdk.Account, runtime sdk.Runtime, folderID, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
	if c == nil || c.transport == nil {
		return "", "", ErrTransportMissing
	}
	if runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return "", "", sdk.ErrInvalidHealth
	}
	err = runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		var callErr error
		text, resolvedModel, callErr = c.complete(ctx, folderID, model, credential, systemPrompt, userPrompt)
		return callErr
	})
	return text, resolvedModel, err
}

func (c *Connector) complete(ctx context.Context, folderID, model string, credential []byte, systemPrompt, userPrompt string) (string, string, error) {
	folderID = strings.TrimSpace(folderID)
	if len(credential) == 0 || strings.TrimSpace(userPrompt) == "" || folderID == "" {
		return "", "", errors.New("yandexgpt: invalid request")
	}
	var messages []message
	if system := strings.TrimSpace(systemPrompt); system != "" {
		messages = append(messages, message{Role: "system", Text: system})
	}
	messages = append(messages, message{Role: "user", Text: userPrompt})
	resolvedModel := strings.TrimSpace(model)
	if resolvedModel == "" {
		resolvedModel = "yandexgpt-lite"
	}
	raw, err := json.Marshal(completionRequest{
		ModelURI:          fmt.Sprintf("gpt://%s/%s", folderID, resolvedModel),
		CompletionOptions: completionOptions{Stream: false, Temperature: 0.3, MaxTokens: "2000"},
		Messages:          messages,
	})
	if err != nil {
		return "", "", err
	}
	response, err := c.transport.Do(ctx, Request{
		Host: Host, Path: "/foundationModels/v1/completion",
		Headers: map[string]string{"Authorization": "Api-Key " + string(credential), "Content-Type": "application/json"},
		Body:    raw,
	})
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("yandexgpt: remote status not ok")
	}
	var out completionResponse
	if err := json.Unmarshal(response.Body, &out); err != nil {
		return "", "", err
	}
	if len(out.Result.Alternatives) == 0 || strings.TrimSpace(out.Result.Alternatives[0].Message.Text) == "" {
		return "", "", errors.New("yandexgpt: empty completion")
	}
	return out.Result.Alternatives[0].Message.Text, resolvedModel, nil
}
