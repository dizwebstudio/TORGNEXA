// Package gemini implements the Google Gemini generateContent API boundary.
// Socket I/O is host-mediated through Transport; the connector only builds
// and parses bounded JSON payloads.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTransportMissing = errors.New("gemini: transport missing")

const (
	DefaultHost        = "generativelanguage.googleapis.com"
	DefaultHealthModel = "gemini-3.7-flash"
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

type part struct {
	Text string `json:"text"`
}
type content struct {
	Role  string `json:"role,omitempty"`
	Parts []part `json:"parts"`
}
type generationConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
}
type generateRequest struct {
	Contents          []content        `json:"contents"`
	SystemInstruction *content         `json:"systemInstruction,omitempty"`
	GenerationConfig  generationConfig `json:"generationConfig"`
}
type candidate struct {
	Content content `json:"content"`
}
type generateResponse struct {
	Candidates   []candidate `json:"candidates"`
	ModelVersion string      `json:"modelVersion"`
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

func Manifest() sdk.Manifest                { manifest, _ := sdk.CatalogManifest("gemini"); return manifest }
func (c *Connector) Manifest() sdk.Manifest { return Manifest() }

func (c *Connector) Health(ctx context.Context, account sdk.Account, runtime sdk.Runtime) (sdk.Health, error) {
	if c == nil || c.transport == nil || runtime == nil || runtime.Secrets() == nil || sdk.ValidateAccountAgainstManifest(account, Manifest()) != nil {
		return sdk.Health{}, sdk.ErrInvalidHealth
	}
	at := c.now().UTC()
	status, reason := sdk.HealthHealthy, ""
	err := runtime.Secrets().UseSecret(ctx, account.SecretReference, func(credential []byte) error {
		_, _, callErr := c.complete(ctx, DefaultHealthModel, credential, "", "ping")
		return callErr
	})
	if err != nil {
		status, reason = sdk.HealthUnavailable, "remote_unavailable"
	}
	return sdk.Health{Status: status, ReasonCode: reason, CheckedAt: at}, nil
}

func (c *Connector) Complete(ctx context.Context, account sdk.Account, runtime sdk.Runtime, _ string, model, systemPrompt, userPrompt string) (text, resolvedModel string, err error) {
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
		return "", "", errors.New("gemini: invalid request")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultHealthModel
	}
	model = strings.TrimPrefix(model, "models/")
	if model == "" || strings.ContainsAny(model, "/\\\r\n\x00") || strings.Contains(model, "..") {
		return "", "", errors.New("gemini: invalid model")
	}
	request := generateRequest{
		Contents:         []content{{Role: "user", Parts: []part{{Text: userPrompt}}}},
		GenerationConfig: generationConfig{Temperature: 0.3, MaxOutputTokens: 2048},
	}
	if system := strings.TrimSpace(systemPrompt); system != "" {
		request.SystemInstruction = &content{Parts: []part{{Text: system}}}
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return "", "", err
	}
	response, err := c.transport.Do(ctx, Request{
		Host: DefaultHost, Path: "/v1beta/models/" + model + ":generateContent",
		Headers: map[string]string{"x-goog-api-key": string(credential), "Content-Type": "application/json"}, Body: raw,
	})
	if err != nil {
		return "", "", err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", errors.New("gemini: remote status not ok")
	}
	var out generateResponse
	if err := json.Unmarshal(response.Body, &out); err != nil {
		return "", "", err
	}
	for _, item := range out.Candidates {
		for _, itemPart := range item.Content.Parts {
			if text := strings.TrimSpace(itemPart.Text); text != "" {
				if out.ModelVersion == "" {
					out.ModelVersion = model
				}
				return text, out.ModelVersion, nil
			}
		}
	}
	return "", "", errors.New("gemini: empty completion")
}
