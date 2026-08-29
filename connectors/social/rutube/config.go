package rutube

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("rutube: configuration missing")
	ErrInvalidConfiguration = errors.New("rutube: invalid configuration")
)

type Configuration struct {
	ChannelID  string
	ContractID string
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if !safeID(c.ChannelID, 1, 128) || !safeID(c.ContractID, 3, 128) {
		return ErrInvalidConfiguration
	}
	return nil
}

func safeID(v string, min, max int) bool {
	if len(v) < min || len(v) > max || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' || r == ':') {
			return false
		}
	}
	return true
}
