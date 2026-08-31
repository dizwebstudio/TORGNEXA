package telegram

import (
	"context"
	"errors"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("telegram: configuration missing")
	ErrInvalidConfiguration = errors.New("telegram: invalid configuration")
)

// Configuration binds one connector account to exactly one Telegram channel.
// Telegram channel identifiers are negative signed 64-bit values. Usernames are
// deliberately not accepted because they can be renamed/reassigned.
type Configuration struct {
	ChatID                 int64
	WebhookSecretReference sdk.SecretReference
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if configuration.ChatID >= 0 || (configuration.WebhookSecretReference != "" && !configuration.WebhookSecretReference.Valid()) {
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
