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

// Configuration binds one connector account to exactly one MAX channel. The
// webhook verification reference is optional until the independently admitted
// webhook surface is configured; when present it must remain a separate secret.
type Configuration struct {
	ChatID                 int64
	WebhookSecretReference sdk.SecretReference
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if configuration.ChatID == 0 || (configuration.WebhookSecretReference != "" && !configuration.WebhookSecretReference.Valid()) {
		return ErrInvalidConfiguration
	}
	return nil
}

func (configuration Configuration) validateWebhook() error {
	if configuration.Validate() != nil || configuration.WebhookSecretReference == "" {
		return ErrInvalidConfiguration
	}
	return nil
}
