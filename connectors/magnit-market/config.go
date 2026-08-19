package magnitmarket

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var (
	ErrConfigurationMissing = errors.New("magnit-market: configuration missing")
	ErrInvalidConfiguration = errors.New("magnit-market: invalid configuration")
)

type StockType string

const (
	StockTypeFBS StockType = "FBS"
	StockTypeFBO StockType = "FBO"
)

type Configuration struct {
	ShopID          int64
	StockType       StockType
	OrderWindowDays int
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if configuration.ShopID < 1 || (configuration.StockType != StockTypeFBS && configuration.StockType != StockTypeFBO) || configuration.OrderWindowDays < 1 || configuration.OrderWindowDays > 90 {
		return ErrInvalidConfiguration
	}
	return nil
}

func (configuration Configuration) fingerprint(surface string) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		surface,
		strconv.FormatInt(configuration.ShopID, 10),
		string(configuration.StockType),
		strconv.Itoa(configuration.OrderWindowDays),
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

func (configuration Configuration) inventoryLocationID() string {
	return "shop:" + strconv.FormatInt(configuration.ShopID, 10) + ":stock-type:" + string(configuration.StockType)
}
