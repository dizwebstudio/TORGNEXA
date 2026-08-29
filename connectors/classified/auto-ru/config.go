package autoru

import (
	"context"
	"errors"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var ErrInvalidConfiguration = errors.New("auto-ru: invalid configuration")

type Configuration struct {
	AccountID int64
	DealerID  string
}

func (c Configuration) Validate() error {
	if c.AccountID <= 0 {
		return ErrInvalidConfiguration
	}
	if c.DealerID != "" {
		if c.DealerID != strings.TrimSpace(c.DealerID) || len(c.DealerID) > 32 {
			return ErrInvalidConfiguration
		}
		if v, err := strconv.ParseInt(c.DealerID, 10, 64); err != nil || v <= 0 {
			return ErrInvalidConfiguration
		}
	}
	return nil
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}
