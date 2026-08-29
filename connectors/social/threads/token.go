package threads

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrTokenSinkMissing = errors.New("threads: token sink missing")

type TokenSink interface {
	RotateSecret(context.Context, sdk.SecretReference, []byte, time.Time) error
}

type TokenLifecycleResult struct {
	RotatedAt time.Time
	ExpiresAt time.Time
}

func (r TokenLifecycleResult) Validate() error {
	if r.RotatedAt.IsZero() || r.ExpiresAt.IsZero() || r.RotatedAt.Location() != time.UTC || r.ExpiresAt.Location() != time.UTC || !r.ExpiresAt.After(r.RotatedAt) {
		return ErrInvalidResponse
	}
	return nil
}

func (c *Connector) ExchangeLongLivedToken(ctx context.Context, a sdk.Account, r sdk.Runtime, sink TokenSink) (TokenLifecycleResult, error) {
	if sink == nil || c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return TokenLifecycleResult{}, ErrTokenSinkMissing
	}
	cfg, e := c.configuration(ctx, a)
	if e != nil {
		return TokenLifecycleResult{}, e
	}
	var result TokenLifecycleResult
	e = c.useSecret(ctx, r, a.SecretReference, validToken, func(short []byte) error {
		return c.useSecret(ctx, r, cfg.AppSecretReference, validAppSecret, func(appSecret []byte) error {
			return c.rotateToken(ctx, a.SecretReference, sink, short, appSecret, "/access_token", "th_exchange_token", &result)
		})
	})
	return result, e
}

func (c *Connector) RefreshLongLivedToken(ctx context.Context, a sdk.Account, r sdk.Runtime, sink TokenSink) (TokenLifecycleResult, error) {
	if sink == nil || c == nil || c.transport == nil || r == nil || r.Secrets() == nil || sdk.ValidateAccountAgainstManifest(a, Manifest()) != nil {
		return TokenLifecycleResult{}, ErrTokenSinkMissing
	}
	if _, e := c.configuration(ctx, a); e != nil {
		return TokenLifecycleResult{}, e
	}
	var result TokenLifecycleResult
	e := c.useSecret(ctx, r, a.SecretReference, validToken, func(current []byte) error {
		return c.rotateToken(ctx, a.SecretReference, sink, current, nil, "/refresh_access_token", "th_refresh_token", &result)
	})
	return result, e
}

func (c *Connector) rotateToken(ctx context.Context, ref sdk.SecretReference, sink TokenSink, current, appSecret []byte, path, grant string, out *TokenLifecycleResult) error {
	if !validToken(current) || (path == "/access_token" && !validAppSecret(appSecret)) || (path == "/refresh_access_token" && len(appSecret) != 0) {
		return ErrInvalidCredentials
	}
	request := Request{
		Method:      "GET",
		Host:        apiHost,
		Path:        path,
		Params:      []Param{{Name: "grant_type", Value: grant}},
		AccessToken: append([]byte(nil), current...),
		AppSecret:   append([]byte(nil), appSecret...),
	}
	defer clear(request.AccessToken)
	defer clear(request.AppSecret)
	raw, e := c.do(ctx, request, false)
	if e != nil {
		return e
	}
	defer clear(raw)
	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(raw, &payload) != nil || !validToken([]byte(payload.AccessToken)) || payload.ExpiresIn < 86400 || payload.ExpiresIn > 90*24*3600 {
		return ErrInvalidResponse
	}
	next := []byte(payload.AccessToken)
	defer clear(next)
	rotated := c.now().UTC()
	expires := rotated.Add(time.Duration(payload.ExpiresIn) * time.Second)
	if e = sink.RotateSecret(ctx, ref, next, expires); e != nil {
		return e
	}
	*out = TokenLifecycleResult{RotatedAt: rotated, ExpiresAt: expires}
	return out.Validate()
}
