package megamarket

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
	ErrConfigurationMissing = errors.New("megamarket: configuration missing")
	ErrInvalidConfiguration = errors.New("megamarket: invalid configuration")
)

type Scheme string

const (
	SchemeDBS Scheme = "dbs"
	SchemeFBO Scheme = "fbo"
)

type Warehouse struct{ ID, Name string }

type Configuration struct {
	MerchantID int64
	Scheme     Scheme
	Warehouses []Warehouse
}

type ConfigurationSource interface {
	Resolve(context.Context, sdk.Account) (Configuration, error)
}

func (c Configuration) Validate() error {
	if c.MerchantID < 1 || (c.Scheme != SchemeDBS && c.Scheme != SchemeFBO) || len(c.Warehouses) < 1 || len(c.Warehouses) > 256 {
		return ErrInvalidConfiguration
	}
	seen := map[string]struct{}{}
	for _, w := range c.Warehouses {
		if !validText(w.ID, 128) || !validText(w.Name, 300) {
			return ErrInvalidConfiguration
		}
		if _, ok := seen[w.ID]; ok {
			return ErrInvalidConfiguration
		}
		seen[w.ID] = struct{}{}
	}
	return nil
}

func (c Configuration) fingerprint(surface string) string {
	parts := []string{surface, strconv.FormatInt(c.MerchantID, 10), string(c.Scheme)}
	for _, w := range c.Warehouses {
		parts = append(parts, w.ID, w.Name)
	}
	d := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(d[:])
}
