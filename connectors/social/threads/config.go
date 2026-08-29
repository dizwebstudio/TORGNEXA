package threads

import (
	"context"
	"errors"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("threads: configuration missing")
	ErrInvalidConfiguration = errors.New("threads: invalid configuration")
)

type Configuration struct {
	ThreadsUserID      string
	AppSecretReference sdk.SecretReference
}
type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if !digits(c.ThreadsUserID, 5, 64) || c.AppSecretReference == "" || !c.AppSecretReference.Valid() {
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
