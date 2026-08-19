package ok

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("ok: configuration missing")
	ErrInvalidConfiguration = errors.New("ok: invalid configuration")
)

type Configuration struct {
	GroupID            string
	ApplicationKey     string
	AppSecretReference sdk.SecretReference
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if !digits(c.GroupID, 5, 64) || !safePublicKey(c.ApplicationKey) || c.AppSecretReference == "" || !c.AppSecretReference.Valid() {
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

func safePublicKey(v string) bool {
	if len(v) < 6 || len(v) > 256 || v != strings.TrimSpace(v) {
		return false
	}
	for _, r := range v {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}
