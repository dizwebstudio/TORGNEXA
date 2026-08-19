package maxconnector

import (
	"context"
	"errors"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("max: configuration missing")
	ErrInvalidConfiguration = errors.New("max: invalid configuration")
)

// Configuration binds one connector account to exactly one MAX channel and a
// separate webhook verification secret. Human-readable links/usernames are not
// channel identity and are deliberately excluded.
type Configuration struct {
	ChatID                 int64
	WebhookSecretReference sdk.SecretReference
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if configuration.ChatID == 0 || !configuration.WebhookSecretReference.Valid() || configuration.WebhookSecretReference == "" {
		return ErrInvalidConfiguration
	}
	return nil
}
