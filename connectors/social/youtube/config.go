package youtube

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("youtube: configuration missing")
	ErrInvalidConfiguration = errors.New("youtube: invalid configuration")
)

type Configuration struct {
	ChannelID               string
	CategoryID              string
	PrivacyStatus           string
	NotifySubscribers       bool
	SelfDeclaredMadeForKids bool
	ContainsSyntheticMedia  bool
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if !safeID(c.ChannelID, 3, 128) || !digits(c.CategoryID, 1, 8) {
		return ErrInvalidConfiguration
	}
	switch c.PrivacyStatus {
	case "private", "public", "unlisted":
		return nil
	default:
		return ErrInvalidConfiguration
	}
}

func safeID(v string, min, max int) bool {
	if len(v) < min || len(v) > max || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
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
