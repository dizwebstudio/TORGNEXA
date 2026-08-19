package avito

import (
	"context"
	"errors"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

type Configuration struct{ UserID int64 }

func (c Configuration) Validate() error {
	if c.UserID <= 0 {
		return errors.New("avito: invalid configuration")
	}
	return nil
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}
