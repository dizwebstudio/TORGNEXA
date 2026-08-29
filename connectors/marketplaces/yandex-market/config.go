package yandexmarket

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
	ErrConfigurationMissing = errors.New("yandexmarket: configuration missing")
	ErrInvalidConfiguration = errors.New("yandexmarket: invalid configuration")
)

type InventoryMode string
type PriceMode string

const (
	InventoryPartnerWarehouses  InventoryMode = "partner_warehouses"
	InventoryCampaignWarehouses InventoryMode = "campaign_warehouses"
	PriceBusinessWide           PriceMode     = "business_wide"
	PriceCampaignUnique         PriceMode     = "campaign_unique"
)

type Warehouse struct {
	ID   int64
	Name string
}

type Configuration struct {
	BusinessID    int64
	CampaignID    int64
	InventoryMode InventoryMode
	PriceMode     PriceMode
	Warehouses    []Warehouse
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (configuration Configuration) Validate() error {
	if configuration.BusinessID < 1 || configuration.CampaignID < 1 || (configuration.PriceMode != PriceBusinessWide && configuration.PriceMode != PriceCampaignUnique) {
		return ErrInvalidConfiguration
	}
	switch configuration.InventoryMode {
	case InventoryPartnerWarehouses:
		if len(configuration.Warehouses) != 0 {
			return ErrInvalidConfiguration
		}
	case InventoryCampaignWarehouses:
		if len(configuration.Warehouses) < 1 || len(configuration.Warehouses) > 256 {
			return ErrInvalidConfiguration
		}
		seen := make(map[int64]struct{}, len(configuration.Warehouses))
		for _, warehouse := range configuration.Warehouses {
			if warehouse.ID < 1 || !validText(warehouse.Name, 300) {
				return ErrInvalidConfiguration
			}
			if _, duplicate := seen[warehouse.ID]; duplicate {
				return ErrInvalidConfiguration
			}
			seen[warehouse.ID] = struct{}{}
		}
	default:
		return ErrInvalidConfiguration
	}
	return nil
}

func (configuration Configuration) fingerprint(surface string) string {
	parts := []string{surface, strconv.FormatInt(configuration.BusinessID, 10), strconv.FormatInt(configuration.CampaignID, 10), string(configuration.InventoryMode), string(configuration.PriceMode)}
	for _, warehouse := range configuration.Warehouses {
		parts = append(parts, strconv.FormatInt(warehouse.ID, 10), warehouse.Name)
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}
