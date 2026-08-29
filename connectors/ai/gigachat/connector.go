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
	"sync"
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
	// ExpiresAt is Sber's own field: Unix milliseconds, not seconds.
	ExpiresAt int64 `json:"expires_at"`
}

// cachedToken is one account's OAuth access token, valid until expiresAt.
type cachedToken struct {
	value     string
	expiresAt time.Time
}

// tokenExpiryBuffer is subtracted from the provider-reported expiry so a
// cached token is never handed to the completion call within its final
// minute of validity.
const tokenExpiryBuffer = 60 * time.Second

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

	mu     sync.Mutex
	tokens map[string]cachedToken
}

func New(transport Transport, now func() time.Time) *Connector {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Connector{transport: transport, now: now, tokens: make(map[string]cachedToken)}
}

// cachedAccessToken returns accountID's still-valid access token, if any.
func (c *Connector) cachedAccessToken(accountID string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.tokens[accountID]
	if !ok || !c.now().Before(entry.expiresAt) {
		return "", false
	}
	return entry.value, true
}

// storeAccessToken caches token for accountID until its provider-reported
// expiry. A response with no usable expires_at is never cached, so a missing
// or malformed field only costs a wasted OAuth round trip next call, never a
// stale token used past its real lifetime.
func (c *Connector) storeAccessToken(accountID, token string, expiresAtMS int64) {
	if expiresAtMS <= 0 {
		return
	}
	expiresAt := time.UnixMilli(expiresAtMS).Add(-tokenExpiryBuffer)
	if !expiresAt.After(c.now()) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[accountID] = cachedToken{value: token, expiresAt: expiresAt}
}

func (c *Connector) invalidateAccessToken(accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.tokens, accountID)
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
		_, _, callErr := c.complete(ctx, account.ID, "GigaChat", credential, "", "ping")
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
		text, resolvedModel, callErr = c.complete(ctx, account.ID, model, credential, systemPrompt, userPrompt)
		return callErr
	})
	return text, resolvedModel, err
}

// exchangeToken performs the OAuth token exchange against OAuthHost. It is
// only called when no still-valid cached token exists for the account.
func (c *Connector) exchangeToken(ctx context.Context, credential []byte) (tokenResponse, error) {
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
		return tokenResponse{}, err
	}
	if tokenResp.StatusCode < 200 || tokenResp.StatusCode >= 300 {
		return tokenResponse{}, errors.New("gigachat: oauth status not ok")
	}
	var token tokenResponse
	if err := json.Unmarshal(tokenResp.Body, &token); err != nil || strings.TrimSpace(token.AccessToken) == "" {
		return tokenResponse{}, errors.New("gigachat: invalid oauth response")
	}
	return token, nil
}

func (c *Connector) completionCall(ctx context.Context, accessToken string, raw []byte) (Response, error) {
	return c.transport.Do(ctx, Request{
		Host: CompletionHost, Path: "/api/v1/chat/completions",
		Headers: map[string]string{"Authorization": "Bearer " + accessToken, "Content-Type": "application/json"},
		Body:    raw,
	})
}

func (c *Connector) complete(ctx context.Context, accountID, model string, credential []byte, systemPrompt, userPrompt string) (string, string, error) {
	if len(credential) == 0 || strings.TrimSpace(userPrompt) == "" {
		return "", "", errors.New("gigachat: invalid request")
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

	accessToken, cached := c.cachedAccessToken(accountID)
	if !cached {
		token, exchangeErr := c.exchangeToken(ctx, credential)
		if exchangeErr != nil {
			return "", "", exchangeErr
		}
		accessToken = token.AccessToken
		c.storeAccessToken(accountID, accessToken, token.ExpiresAt)
	}

	completionResp, err := c.completionCall(ctx, accessToken, raw)
	if err != nil {
		return "", "", err
	}
	if cached && completionResp.StatusCode == 401 {
		// The cached token was rejected remotely (e.g. revoked out of band):
		// drop it and retry once with a freshly exchanged token rather than
		// leaving every subsequent call broken until process restart.
		c.invalidateAccessToken(accountID)
		token, exchangeErr := c.exchangeToken(ctx, credential)
		if exchangeErr != nil {
			return "", "", exchangeErr
		}
		accessToken = token.AccessToken
		c.storeAccessToken(accountID, accessToken, token.ExpiresAt)
		completionResp, err = c.completionCall(ctx, accessToken, raw)
		if err != nil {
			return "", "", err
		}
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
