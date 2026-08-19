package vk

import (
	"context"
	"errors"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("vk: configuration missing")
	ErrInvalidConfiguration = errors.New("vk: invalid configuration")
)

// Configuration contains only non-secret account binding data. GroupID is the
// positive VK community ID; the adapter converts it to the negative wall owner
// ID required by wall methods.
type Configuration struct {
	GroupID int64
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if configuration.GroupID < 1 {
		return ErrInvalidConfiguration
	}
	return nil
}
