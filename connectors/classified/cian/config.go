package cian

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrInvalidConfiguration = errors.New("cian: invalid configuration")

type Configuration struct {
	FeedURL string
}

func (c Configuration) Validate() error {
	if !validHTTPSURL(c.FeedURL, 4096) {
		return ErrInvalidConfiguration
	}
	return nil
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func validHTTPSURL(raw string, max int) bool {
	if raw == "" || raw != strings.TrimSpace(raw) || len(raw) > max || !strings.HasPrefix(raw, "https://") || strings.Contains(raw, "#") || strings.Contains(raw, "\\") {
		return false
	}
	rest := strings.TrimPrefix(raw, "https://")
	slash := strings.IndexByte(rest, '/')
	authority := rest
	if slash >= 0 {
		authority = rest[:slash]
	}
	if authority == "" || strings.ContainsAny(authority, "@%[]") || strings.HasPrefix(authority, ".") || strings.HasSuffix(authority, ".") || strings.Contains(authority, "..") {
		return false
	}
	host := authority
	if colon := strings.LastIndexByte(authority, ':'); colon >= 0 {
		if authority[colon+1:] != "443" || strings.Contains(authority[:colon], ":") {
			return false
		}
		host = authority[:colon]
	}
	if host == "" {
		return false
	}
	for _, r := range strings.ToLower(host) {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
			return false
		}
	}
	return true
}
