package instagram

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("instagram: configuration missing")
	ErrInvalidConfiguration = errors.New("instagram: invalid configuration")
)

type Configuration struct {
	InstagramUserID string
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if !digits(c.InstagramUserID, 5, 64) {
		return ErrInvalidConfiguration
	}
	return nil
}

func digits(v string, min, max int) bool {
	if len(v) < min || len(v) > max || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
